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
	initialTempo      = flag.Float64("tempo", 120.0, "Initial tempo in BPM (60-200)")
	enableExternalSync = flag.Bool("enable-external-sync", false, "Enable external clock sync mode (external GPIO clock controls Link tempo)")
	cuiMode           = flag.Bool("cui", false, "Enable console text UI with real-time monitoring and keyboard controls")
	oledMode          = flag.Bool("oled", false, "Enable OLED display (SSD1306 128x64) with rotary encoder interface")
	realtimeMode      = flag.Bool("rt", false, "Enable real-time process priority for low-latency operation (requires privileges)")
	configFile        = flag.String("config", "", "Load custom GPIO pin configuration from JSON file")
	dryRun           = flag.Bool("dry-run", false, "Simulate GPIO operations without hardware access (for testing)")
	showHelp         = flag.Bool("help", false, "Show detailed usage information and examples")
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
	
	// UI components
	tui       *EurorackTUIManager
	oled      *OLEDDisplay
	uiEnabled bool
	oledMode  bool
	
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
func NewEurorackLinkBridge(tempo float64, externalSync bool, uiEnabled bool, oledEnabled bool, dryRun bool) (*EurorackLinkBridge, error) {
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
		oledMode:            oledEnabled,
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
	
	// Create UI if enabled
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
	
	// Create OLED display if enabled
	if b.oledMode && !b.dryRun {
		oled, err := NewOLEDDisplay(b)
		if err != nil {
			b.logInfo("Failed to initialize OLED display: %v", err)
		} else {
			b.oled = oled
			b.logInfo("OLED display initialized")
		}
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
	
	// Stop UI if enabled
	if b.tui != nil {
		b.tui.Stop()
	}
	
	// Stop OLED display if enabled
	if b.oled != nil {
		b.oled.Stop()
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

func showUsage() {
	fmt.Printf("Eurorack-Link Bridge with OLED Display\n")
	fmt.Printf("======================================\n\n")
	fmt.Printf("A bidirectional bridge between Eurorack modular synthesizers and Ableton Link,\n")
	fmt.Printf("featuring hardware UI with OLED display and rotary encoder control.\n\n")
	
	fmt.Printf("USAGE:\n")
	fmt.Printf("  ./eurorack_bridge [OPTIONS]\n\n")
	
	fmt.Printf("OPTIONS:\n")
	flag.PrintDefaults()
	
	fmt.Printf("\nOPERATION MODES:\n")
	fmt.Printf("  Basic Mode     : Simple GPIO bridge without UI\n")
	fmt.Printf("  Console UI     : Text-based interface with keyboard controls (-cui)\n")
	fmt.Printf("  OLED Mode      : Hardware interface with display and encoder (-oled)\n")
	fmt.Printf("  External Sync  : GPIO clock controls Link tempo (-enable-external-sync)\n\n")
	
	fmt.Printf("EXAMPLES:\n")
	fmt.Printf("  ./eurorack_bridge\n")
	fmt.Printf("    Start basic bridge with default settings\n\n")
	
	fmt.Printf("  ./eurorack_bridge -oled -rt\n")
	fmt.Printf("    OLED interface with real-time priority\n\n")
	
	fmt.Printf("  ./eurorack_bridge -cui -tempo 140\n")
	fmt.Printf("    Console UI starting at 140 BPM\n\n")
	
	fmt.Printf("  ./eurorack_bridge -enable-external-sync -oled\n")
	fmt.Printf("    External clock control with OLED display\n\n")
	
	fmt.Printf("  ./eurorack_bridge -dry-run -oled\n")
	fmt.Printf("    Test OLED interface without GPIO hardware\n\n")
	
	fmt.Printf("  ./eurorack_bridge -config custom.json\n")
	fmt.Printf("    Use custom GPIO pin configuration\n\n")
	
	fmt.Printf("HARDWARE REQUIREMENTS:\n")
	fmt.Printf("  - Raspberry Pi with GPIO access\n")
	fmt.Printf("  - Level shifters for Eurorack voltage compatibility\n")
	fmt.Printf("  - For OLED mode: SSD1306 128x64 display (I2C)\n")
	fmt.Printf("  - For OLED mode: Rotary encoder with button\n")
	fmt.Printf("  - Optional: Hardware buttons for Back/Enter\n\n")
	
	fmt.Printf("GPIO PINS (BCM numbering):\n")
	fmt.Printf("  Inputs : Clock=%d, Start=%d, Stop=%d, Reset=%d\n", 
		defaultPins.ClockIn, defaultPins.StartIn, defaultPins.StopIn, defaultPins.ResetIn)
	fmt.Printf("  Outputs: 1PPQ=%d, 2PPQ=%d, 4PPQ=%d, 24PPQ=%d\n",
		defaultPins.Clock1PPQN, defaultPins.Clock2PPQN, defaultPins.Clock4PPQN, defaultPins.Clock24PPQN)
	fmt.Printf("  Transport: Start=%d, Stop=%d, Reset=%d\n",
		defaultPins.StartOut, defaultPins.StopOut, defaultPins.ResetOut)
	fmt.Printf("  OLED I2C: SDA=GPIO2, SCL=GPIO3\n")
	fmt.Printf("  Encoder: A=25, B=26, Button=27, Back=5, Enter=6, Custom=13\n\n")
	
	fmt.Printf("CONSOLE UI CONTROLS (when using -cui):\n")
	fmt.Printf("  Space  : Toggle Link transport (play/stop)\n")
	fmt.Printf("  R      : Send reset pulse and return to beat 0\n")
	fmt.Printf("  H      : Show/hide help overlay\n")
	fmt.Printf("  Q      : Quit application\n\n")
	
	fmt.Printf("OLED UI CONTROLS (when using -oled):\n")
	fmt.Printf("  Rotary Encoder : Navigate menus and adjust values\n")
	fmt.Printf("  Encoder Button : Enter submenu or save changes\n")
	fmt.Printf("  Back Button    : Return to previous menu\n")
	fmt.Printf("  Enter Button   : Alternative to encoder button\n\n")
	
	fmt.Printf("CONFIGURATION:\n")
	fmt.Printf("  Settings are saved to: ~/.eurorack-link-bridge.json\n")
	fmt.Printf("  Custom config with: -config /path/to/config.json\n\n")
	
	fmt.Printf("For detailed setup instructions, see README.md and README-OLED.md\n")
}

func main() {
	flag.Parse()
	
	// Show help if requested
	if *showHelp {
		showUsage()
		return
	}
	
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
	bridge, err := NewEurorackLinkBridge(*initialTempo, *enableExternalSync, *cuiMode, *oledMode, *dryRun)
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
	} else if *oledMode {
		// Run with OLED display
		fmt.Printf("Eurorack-Link Bridge with OLED running. Press Ctrl+C to stop.\n")
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