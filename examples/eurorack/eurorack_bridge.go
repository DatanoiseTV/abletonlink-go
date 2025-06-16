package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"github.com/DatanoiseTV/abletonlink-go"
	"github.com/sirupsen/logrus"
	"github.com/warthog618/go-gpiocdev"
)

const (
	// Clock timing constants
	defaultQuantum           = 4.0
	microsecondsPerMinute    = 60_000_000
	
	// Eurorack timing
	eurorackPulseWidth      = 10 * time.Millisecond // 10ms trigger pulses (Eurorack standard)
	eurorackGateThreshold   = 2.0 // 2V threshold for input detection
	
	// Bridge configuration
	bridgeName = "Eurorack-Link Bridge"
	tempoTolerance = 0.1 // BPM tolerance for tempo changes
)

// GPIO pin definitions (BCM numbering for Raspberry Pi)
type GPIOPins struct {
	// Inputs
	ClockIn    int // External clock input
	StartIn    int // Start trigger input
	StopIn     int // Stop trigger input
	ResetIn    int // Reset trigger input
	
	// Outputs
	Clock1PPQN  int // 1 pulse per quarter note
	Clock2PPQN  int // 2 pulses per quarter note
	Clock4PPQN  int // 4 pulses per quarter note
	Clock24PPQN int // 24 pulses per quarter note (MIDI clock equivalent)
	StartOut    int // Start trigger output
	StopOut     int // Stop trigger output
	ResetOut    int // Reset trigger output
}

// Default GPIO configuration for Raspberry Pi
var defaultPins = GPIOPins{
	// Inputs
	ClockIn:    2,  // GPIO 2 (SDA)
	StartIn:    3,  // GPIO 3 (SCL)
	StopIn:     4,  // GPIO 4
	ResetIn:    17, // GPIO 17
	
	// Outputs
	Clock1PPQN:  18, // GPIO 18 (PWM0)
	Clock2PPQN:  19, // GPIO 19 (PWM1)
	Clock4PPQN:  20, // GPIO 20
	Clock24PPQN: 21, // GPIO 21
	StartOut:    22, // GPIO 22
	StopOut:     23, // GPIO 23
	ResetOut:    24, // GPIO 24
}

// Command line flags
var (
	initialTempo      = flag.Float64("tempo", 120.0, "Initial tempo in BPM")
	enableExternalSync = flag.Bool("enable-external-sync", false, "Enable external clock sync (GPIO clock controls Link)")
	cuiMode           = flag.Bool("cui", false, "Enable console UI mode with real-time stats display")
	realtimeMode      = flag.Bool("rt", false, "Enable real-time process priority (requires appropriate permissions)")
	configFile        = flag.String("config", "", "GPIO pin configuration file (JSON)")
	dryRun           = flag.Bool("dry-run", false, "Simulate GPIO operations without hardware access")
)

// EurorackLinkBridge provides bidirectional sync between Eurorack and Ableton Link
type EurorackLinkBridge struct {
	link        *abletonlink.Link
	state       *abletonlink.SessionState
	
	// GPIO management
	chip        *gpiocdev.Chip
	inputLines  map[string]*gpiocdev.Line
	outputLines map[string]*gpiocdev.Line
	pins        GPIOPins
	dryRun      bool
	
	// Synchronization state
	mu                  sync.RWMutex
	lastLinkTempo      float64
	lastExternalTempo  float64
	externalClockCount int
	lastExternalClockTime time.Time
	linkIsPlaying      bool
	externalIsPlaying  bool
	
	// Transport quantization
	quantizeToBar      bool
	beatsPerBar        int
	
	// External sync mode
	externalSyncEnabled    bool   // When true, external clock controls Link
	
	// Tempo smoothing for stability
	tempoHistory           []float64 // Recent tempo readings for averaging
	tempoHistorySize       int       // Max history size
	
	// Clock timing
	clockTimings           []time.Time // Recent clock arrival times
	expectedClockInterval  time.Duration // Expected time between clocks
	
	// Pulse generation tracking
	lastPulses map[string]time.Time // Track last pulse times for each output
	
	// TUI components
	tui       *EurorackTUIManager
	uiEnabled bool
	
	// Context for shutdown
	ctx    context.Context
	cancel context.CancelFunc
	
	// Configuration
	configPath string
}

// Config represents the saved configuration
type Config struct {
	Pins                GPIOPins `json:"gpio_pins"`
	ExternalSyncEnabled bool     `json:"external_sync_enabled"`
	QuantizeToBar       bool     `json:"quantize_to_bar"`
	BeatsPerBar         int      `json:"beats_per_bar"`
	InitialTempo        float64  `json:"initial_tempo"`
}

// NewEurorackLinkBridge creates a new Eurorack-Link bridge instance
func NewEurorackLinkBridge(tempo float64, externalSync bool, uiEnabled bool, dryRun bool) (*EurorackLinkBridge, error) {
	// Create Link instance
	link := abletonlink.NewLink(tempo)
	
	// Create session state
	state := abletonlink.NewSessionState()
	
	ctx, cancel := context.WithCancel(context.Background())
	
	bridge := &EurorackLinkBridge{
		link:                link,
		state:               state,
		pins:                defaultPins,
		dryRun:              dryRun,
		lastLinkTempo:       tempo,
		externalSyncEnabled: externalSync,
		quantizeToBar:       false,
		beatsPerBar:         4,
		tempoHistory:        make([]float64, 0, 10),
		tempoHistorySize:    10,
		lastPulses:          make(map[string]time.Time),
		uiEnabled:           uiEnabled,
		ctx:                 ctx,
		cancel:              cancel,
		configPath:          filepath.Join(os.ExpandEnv("$HOME"), ".eurorack-link-bridge.json"),
		inputLines:          make(map[string]*gpiocdev.Line),
		outputLines:         make(map[string]*gpiocdev.Line),
	}
	
	// Load configuration
	bridge.loadConfig()
	
	// Set up Link callbacks
	link.SetTempoCallback(func(tempo float64) {
		bridge.mu.Lock()
		bridge.lastLinkTempo = tempo
		bridge.mu.Unlock()
		bridge.logInfo("Link tempo changed: %.2f BPM", tempo)
	})
	
	link.SetStartStopCallback(func(isPlaying bool) {
		bridge.mu.Lock()
		bridge.linkIsPlaying = isPlaying
		bridge.mu.Unlock()
		bridge.logInfo("Link transport changed: playing=%v", isPlaying)
		
		// Send transport pulses
		if isPlaying {
			bridge.sendPulse("start")
		} else {
			bridge.sendPulse("stop")
		}
	})
	
	// Enable Link
	link.Enable(true)
	link.EnableStartStopSync(true)
	
	bridge.logInfo("Eurorack-Link Bridge initialized - Tempo: %.2f BPM, External Sync: %v", 
		tempo, externalSync)
	
	return bridge, nil
}

// Start begins the bridge operation
func (b *EurorackLinkBridge) Start() error {
	// Initialize GPIO if not in dry-run mode
	if !b.dryRun {
		if err := b.initGPIO(); err != nil {
			return fmt.Errorf("failed to initialize GPIO: %v", err)
		}
	}
	
	// Create TUI if enabled
	if b.uiEnabled {
		b.tui = NewEurorackTUIManager(b)
		
		// Set up structured logging to TUI
		logrus.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
		})
		logrus.SetOutput(b.tui.GetLogWriter())
		
		// Start TUI in background
		go func() {
			if err := b.tui.Run(); err != nil {
				b.logInfo("TUI error: %v", err)
			}
		}()
		
		// Give TUI time to initialize
		time.Sleep(100 * time.Millisecond)
	}
	
	// Start clock output routine
	go b.runClockOutput()
	
	// Start input monitoring if not in dry-run mode
	if !b.dryRun {
		go b.monitorInputs()
	}
	
	b.logInfo("Eurorack bridge started")
	
	return nil
}

// initGPIO initializes GPIO lines for inputs and outputs
func (b *EurorackLinkBridge) initGPIO() error {
	// Configure input lines with event handlers
	inputConfigs := map[string]int{
		"clock": b.pins.ClockIn,
		"start": b.pins.StartIn,
		"stop":  b.pins.StopIn,
		"reset": b.pins.ResetIn,
	}
	
	for name, pin := range inputConfigs {
		// Create event handler for this input
		eventHandler := func(inputName string) func(gpiocdev.LineEvent) {
			return func(evt gpiocdev.LineEvent) {
				if evt.Type == gpiocdev.LineEventRisingEdge {
					// Use the high-precision hardware timestamp from the event
					timestamp := time.Now()
					if evt.Timestamp > 0 {
						// evt.Timestamp is in nanoseconds since boot
						// Convert to actual time - this gives us hardware-level precision
						bootTime := time.Now().Add(-time.Duration(evt.Timestamp))
						timestamp = bootTime.Add(time.Duration(evt.Timestamp))
					}
					b.handleInputTrigger(inputName, timestamp)
				}
			}
		}(name)
		
		// Request line with rising edge detection and event handler
		line, err := gpiocdev.RequestLine("gpiochip0", pin,
			gpiocdev.WithPullDown,
			gpiocdev.WithRisingEdge,
			gpiocdev.WithEventHandler(eventHandler))
		if err != nil {
			return fmt.Errorf("failed to configure input pin %d (%s): %v", pin, name, err)
		}
		b.inputLines[name] = line
		b.logInfo("Configured GPIO %d as input: %s", pin, name)
	}
	
	// Configure output lines using the chip interface
	chip, err := gpiocdev.NewChip("gpiochip0")
	if err != nil {
		return fmt.Errorf("failed to open GPIO chip: %v", err)
	}
	b.chip = chip
	
	outputConfigs := map[string]int{
		"clock1":  b.pins.Clock1PPQN,
		"clock2":  b.pins.Clock2PPQN,
		"clock4":  b.pins.Clock4PPQN,
		"clock24": b.pins.Clock24PPQN,
		"start":   b.pins.StartOut,
		"stop":    b.pins.StopOut,
		"reset":   b.pins.ResetOut,
	}
	
	for name, pin := range outputConfigs {
		line, err := chip.RequestLine(pin, gpiocdev.AsOutput(0)) // Start with low output
		if err != nil {
			return fmt.Errorf("failed to configure output pin %d (%s): %v", pin, name, err)
		}
		b.outputLines[name] = line
		b.logInfo("Configured GPIO %d as output: %s", pin, name)
	}
	
	return nil
}

// monitorInputs is no longer needed - event handlers are set up in initGPIO
func (b *EurorackLinkBridge) monitorInputs() {
	// Event handlers are already configured in initGPIO
	// This function just waits for the context to be done
	<-b.ctx.Done()
}

// handleInputTrigger processes GPIO input triggers
func (b *EurorackLinkBridge) handleInputTrigger(input string, timestamp time.Time) {
	switch input {
	case "clock":
		if b.externalSyncEnabled {
			b.handleExternalClock(timestamp)
		}
		
	case "start":
		b.handleExternalStart(timestamp)
		
	case "stop":
		b.handleExternalStop(timestamp)
		
	case "reset":
		b.handleExternalReset(timestamp)
	}
}

// handleExternalClock processes external clock pulses
func (b *EurorackLinkBridge) handleExternalClock(timestamp time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	b.externalClockCount++
	
	if !b.lastExternalClockTime.IsZero() {
		// Calculate tempo from clock interval
		interval := timestamp.Sub(b.lastExternalClockTime)
		
		// Assuming 24 PPQN external clock (like MIDI)
		bpm := float64(microsecondsPerMinute) / (float64(interval.Microseconds()) * 24)
		
		// Add to tempo history for smoothing
		b.tempoHistory = append(b.tempoHistory, bpm)
		if len(b.tempoHistory) > b.tempoHistorySize {
			b.tempoHistory = b.tempoHistory[1:]
		}
		
		// Calculate smoothed tempo
		var sum float64
		for _, t := range b.tempoHistory {
			sum += t
		}
		smoothedTempo := sum / float64(len(b.tempoHistory))
		
		// Update Link tempo if significantly different
		if abs(smoothedTempo-b.lastExternalTempo) > tempoTolerance {
			b.lastExternalTempo = smoothedTempo
			
			// Update Link session
			b.link.CaptureAppSessionState(b.state)
			currentTime := b.link.ClockMicros()
			b.state.SetTempo(smoothedTempo, currentTime)
			b.link.CommitAppSessionState(b.state)
			
			b.logInfo("External clock tempo: %.2f BPM", smoothedTempo)
		}
	}
	
	b.lastExternalClockTime = timestamp
}

// handleExternalStart processes external start triggers
func (b *EurorackLinkBridge) handleExternalStart(timestamp time.Time) {
	b.logInfo("External start trigger received")
	
	b.link.CaptureAppSessionState(b.state)
	currentTime := b.link.ClockMicros()
	
	if b.quantizeToBar {
		// Quantize start to next bar
		b.state.SetIsPlayingAndRequestBeatAtTime(true, uint64(currentTime), 0.0, defaultQuantum)
	} else {
		b.state.SetIsPlaying(true, uint64(currentTime))
	}
	
	b.link.CommitAppSessionState(b.state)
}

// handleExternalStop processes external stop triggers
func (b *EurorackLinkBridge) handleExternalStop(timestamp time.Time) {
	b.logInfo("External stop trigger received")
	
	b.link.CaptureAppSessionState(b.state)
	currentTime := b.link.ClockMicros()
	b.state.SetIsPlaying(false, uint64(currentTime))
	b.link.CommitAppSessionState(b.state)
}

// handleExternalReset processes external reset triggers
func (b *EurorackLinkBridge) handleExternalReset(timestamp time.Time) {
	b.logInfo("External reset trigger received")
	
	b.link.CaptureAppSessionState(b.state)
	currentTime := b.link.ClockMicros()
	
	// Reset to beat 0 and stop
	b.state.SetIsPlaying(false, uint64(currentTime))
	b.state.RequestBeatAtTime(0.0, currentTime, defaultQuantum)
	b.link.CommitAppSessionState(b.state)
	
	// Send reset pulse to all outputs
	b.sendPulse("reset")
}

// runClockOutput generates clock pulses for various divisions
func (b *EurorackLinkBridge) runClockOutput() {
	// Use high-precision timer for clock generation
	ticker := time.NewTicker(time.Millisecond) // 1ms resolution
	defer ticker.Stop()
	
	var lastBeat1, lastBeat2, lastBeat4, lastBeat24 float64
	
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			b.link.CaptureAppSessionState(b.state)
			
			if !b.state.IsPlaying() {
				continue
			}
			
			currentTime := b.link.ClockMicros()
			
			// Calculate current beat positions for different divisions
			beat1 := b.state.BeatAtTime(currentTime, 1.0)   // 1 PPQN
			beat2 := b.state.BeatAtTime(currentTime, 0.5)   // 2 PPQN
			beat4 := b.state.BeatAtTime(currentTime, 0.25)  // 4 PPQN
			beat24 := b.state.BeatAtTime(currentTime, 1.0/24.0) // 24 PPQN
			
			// Generate pulses when crossing beat boundaries
			if int(beat1) > int(lastBeat1) {
				b.sendPulse("clock1")
			}
			if int(beat2) > int(lastBeat2) {
				b.sendPulse("clock2")
			}
			if int(beat4) > int(lastBeat4) {
				b.sendPulse("clock4")
			}
			if int(beat24) > int(lastBeat24) {
				b.sendPulse("clock24")
			}
			
			lastBeat1, lastBeat2, lastBeat4, lastBeat24 = beat1, beat2, beat4, beat24
		}
	}
}

// sendPulse sends a trigger pulse to the specified output
func (b *EurorackLinkBridge) sendPulse(output string) {
	if b.dryRun {
		b.logInfo("DRY RUN: Pulse sent to %s", output)
		return
	}
	
	line, exists := b.outputLines[output]
	if !exists {
		return
	}
	
	// Send high pulse
	line.SetValue(1)
	
	// Schedule low pulse after pulse width
	go func() {
		time.Sleep(eurorackPulseWidth)
		line.SetValue(0)
	}()
	
	// Track pulse timing for TUI display
	b.mu.Lock()
	b.lastPulses[output] = time.Now()
	b.mu.Unlock()
	
	// Log clock pulses for debugging (only for non-24PPQN to avoid spam)
	if output != "clock24" {
		b.logInfo("GPIO pulse: %s", output)
	}
}

// Stop gracefully shuts down the bridge
func (b *EurorackLinkBridge) Stop() {
	b.logInfo("Stopping Eurorack-Link Bridge...")
	
	// Cancel context to stop routines
	b.cancel()
	
	// Stop TUI if enabled
	if b.tui != nil {
		b.tui.Stop()
	}
	
	// Clean up GPIO
	if !b.dryRun && b.chip != nil {
		// Set all outputs low
		for _, line := range b.outputLines {
			line.SetValue(0)
		}
		
		// Close all lines
		for _, line := range b.inputLines {
			line.Close()
		}
		for _, line := range b.outputLines {
			line.Close()
		}
		
		// Close chip
		b.chip.Close()
	}
	
	// Save configuration
	b.saveConfig()
	
	// Disable and destroy Link
	b.link.Enable(false)
	b.state.Destroy()
	b.link.Destroy()
	
	b.logInfo("Eurorack-Link Bridge stopped")
}

// Configuration management

func (b *EurorackLinkBridge) loadConfig() {
	// Load from specified config file or default
	configPath := b.configPath
	if *configFile != "" {
		configPath = *configFile
	}
	
	data, err := os.ReadFile(configPath)
	if err != nil {
		return // No config file, use defaults
	}
	
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		b.logInfo("Failed to parse config: %v", err)
		return
	}
	
	// Apply configuration
	b.pins = cfg.Pins
	if !isFlagPassed("enable-external-sync") {
		b.externalSyncEnabled = cfg.ExternalSyncEnabled
	}
	b.quantizeToBar = cfg.QuantizeToBar
	b.beatsPerBar = cfg.BeatsPerBar
	
	if !isFlagPassed("tempo") && cfg.InitialTempo > 0 {
		b.lastLinkTempo = cfg.InitialTempo
	}
}

func (b *EurorackLinkBridge) saveConfig() {
	cfg := Config{
		Pins:                b.pins,
		ExternalSyncEnabled: b.externalSyncEnabled,
		QuantizeToBar:       b.quantizeToBar,
		BeatsPerBar:         b.beatsPerBar,
		InitialTempo:        b.lastLinkTempo,
	}
	
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		b.logInfo("Failed to marshal config: %v", err)
		return
	}
	
	if err := os.WriteFile(b.configPath, data, 0644); err != nil {
		b.logInfo("Failed to save config: %v", err)
	}
}

// Utility functions

func (b *EurorackLinkBridge) logInfo(msg string, args ...interface{}) {
	if b.uiEnabled {
		logrus.Infof(msg, args...)
	} else {
		fmt.Printf(msg+"\n", args...)
	}
}

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func main() {
	flag.Parse()
	
	// Set real-time priority if requested
	if *realtimeMode {
		if err := setRealtimePriority(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to set real-time priority: %v\n", err)
			fmt.Fprintf(os.Stderr, "Continuing with normal priority...\n\n")
		} else {
			fmt.Fprintf(os.Stderr, "Real-time priority enabled successfully.\n")
			logRealtimePriorityInfo()
		}
	}
	
	// Create bridge
	bridge, err := NewEurorackLinkBridge(*initialTempo, *enableExternalSync, *cuiMode, *dryRun)
	if err != nil {
		log.Fatalf("Failed to create bridge: %v", err)
	}
	
	// Start the bridge
	if err := bridge.Start(); err != nil {
		log.Fatalf("Failed to start bridge: %v", err)
	}
	
	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	
	if *cuiMode {
		// Run TUI (blocks until quit)
		<-sigChan
	} else {
		// Simple wait for interrupt
		fmt.Printf("Eurorack-Link Bridge running. Press Ctrl+C to stop.\n")
		fmt.Printf("GPIO Pins - Inputs: Clock=%d, Start=%d, Stop=%d, Reset=%d\n", 
			bridge.pins.ClockIn, bridge.pins.StartIn, bridge.pins.StopIn, bridge.pins.ResetIn)
		fmt.Printf("GPIO Pins - Outputs: 1PPQ=%d, 2PPQ=%d, 4PPQ=%d, 24PPQ=%d, Start=%d, Stop=%d, Reset=%d\n",
			bridge.pins.Clock1PPQN, bridge.pins.Clock2PPQN, bridge.pins.Clock4PPQN, bridge.pins.Clock24PPQN,
			bridge.pins.StartOut, bridge.pins.StopOut, bridge.pins.ResetOut)
		<-sigChan
	}
	
	// Graceful shutdown
	bridge.Stop()
}