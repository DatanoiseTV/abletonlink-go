package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/DatanoiseTV/abletonlink-go"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/sirupsen/logrus"
)

const (
	// MIDI timing constants
	midiClocksPerQuarterNote = 24
	microsecondsPerMinute    = 60_000_000
	defaultQuantum           = 4.0
	
	// Bridge configuration
	virtualPortName = "Link-MIDI Bridge"
	tempoTolerance  = 0.1 // BPM tolerance for tempo changes (responsive to fast changes)
)

// scheduledTransport represents a MIDI transport event scheduled for a specific time
type scheduledTransport struct {
	isPlaying     bool   // true for start/continue, false for stop
	scheduleTime  uint64 // Link time when to send MIDI message
	isContinue    bool   // true for continue, false for start
}

// Command line flags
var (
	listPorts         = flag.Bool("list-ports", false, "List available MIDI ports and exit")
	midiInPort        = flag.Int("midi-in-port", -1, "Physical MIDI input port number (use -list-ports to see available ports)")
	midiOutPort       = flag.Int("midi-out-port", -1, "Physical MIDI output port number (use -list-ports to see available ports)")
	initialTempo      = flag.Float64("tempo", 120.0, "Initial tempo in BPM")
	enableExternalSync = flag.Bool("enable-external-sync", false, "Enable external MIDI clock/transport sync (MIDI controls Link)")
	cuiMode           = flag.Bool("cui", false, "Enable console UI mode with real-time stats display")
)

// MIDILinkBridge provides bidirectional sync between MIDI clock and Ableton Link
type MIDILinkBridge struct {
	link        *abletonlink.Link
	state       *abletonlink.SessionState
	
	// MIDI
	midiIn      *abletonlink.MidiIn
	midiOut     *abletonlink.MidiOut
	
	// Synchronization state
	mu                  sync.RWMutex
	lastLinkTempo      float64
	lastMIDITempo      float64
	midiClockCount     int
	lastMIDIClockTime  time.Time
	linkIsPlaying      bool
	midiIsPlaying      bool
	
	// Transport quantization
	quantizeToBar      bool
	beatsPerBar        int
	
	// Scheduled MIDI transport events
	scheduledMIDIStart *scheduledTransport
	scheduledMIDIStop  *scheduledTransport
	
	// External sync mode
	externalSyncEnabled    bool   // When true, MIDI clock controls Link
	
	// Tempo smoothing for stability
	tempoHistory           []float64 // Recent tempo readings for averaging
	tempoHistorySize       int       // Max history size
	
	// Phase adjustment for jitter compensation
	clockTimings           []time.Time // Recent clock arrival times
	expectedClockInterval  time.Duration // Expected time between clocks
	phaseOffset            time.Duration // Accumulated phase correction
	
	// Feedback prevention
	lastMIDITransportSource string // Track source of last transport change
	
	// TUI components
	app        *tview.Application
	statsTable *tview.Table
	logView    *tview.TextView
	uiEnabled  bool
	
	// Context for shutdown
	ctx    context.Context
	cancel context.CancelFunc
	
}

// NewMIDILinkBridge creates a new bridge instance
func NewMIDILinkBridge(initialTempo float64, externalSync bool, enableUI bool) (*MIDILinkBridge, error) {
	// Create Link instance
	link := abletonlink.NewLink(initialTempo)
	state := abletonlink.NewSessionState()
	
	ctx, cancel := context.WithCancel(context.Background())
	
	bridge := &MIDILinkBridge{
		link:                link,
		state:               state,
		lastLinkTempo:       initialTempo,
		lastMIDITempo:       0.0, // Will be set when first MIDI clock is received
		quantizeToBar:       true,
		beatsPerBar:         4,
		externalSyncEnabled: externalSync,
		tempoHistory:        make([]float64, 0, 4), // Keep last 4 readings
		tempoHistorySize:    4,
		clockTimings:        make([]time.Time, 0, 8), // Keep last 8 clock timings
		uiEnabled:           enableUI,
		ctx:                 ctx,
		cancel:              cancel,
	}
	
	// Setup logging
	if enableUI {
		// In UI mode, disable standard output logging
		logrus.SetOutput(bridge.getLogWriter())
		logrus.SetLevel(logrus.InfoLevel)
	} else {
		logrus.SetLevel(logrus.WarnLevel) // Reduce noise in CLI mode
	}
	
	// Set up Link callbacks
	bridge.setupLinkCallbacks()
	
	// Setup UI if enabled
	if enableUI {
		bridge.setupUI()
	}
	
	return bridge, nil
}

// Start initializes and starts the bridge
func (b *MIDILinkBridge) Start() error {
	// Initialize MIDI
	if err := b.setupMIDI(); err != nil {
		return fmt.Errorf("failed to setup MIDI: %w", err)
	}
	
	// Enable Link
	b.link.Enable(true)
	b.link.EnableStartStopSync(true)
	
	// Start background tasks
	go b.midiClockSender()
	
	b.logInfo("MIDI-Link Bridge started")
	if *midiInPort >= 0 || *midiOutPort >= 0 {
		b.logInfo("Using physical MIDI ports")
	} else {
		b.logInfo("Virtual MIDI ports: %s In/Out", virtualPortName)
	}
	b.logInfo("Initial tempo: %.2f BPM", b.lastLinkTempo)
	if b.externalSyncEnabled {
		b.logInfo("External sync enabled: MIDI clock controls Link")
	} else {
		b.logInfo("Internal sync: Link controls MIDI output")
	}
	
	return nil
}

// Stop gracefully shuts down the bridge
func (b *MIDILinkBridge) Stop() {
	b.cancel()
	
	if b.midiIn != nil {
		b.midiIn.Close()
	}
	if b.midiOut != nil {
		b.midiOut.Close()
	}
	
	b.link.Enable(false)
	b.link.Destroy()
	b.state.Destroy()
	
	if !b.uiEnabled {
		fmt.Println("MIDI-Link Bridge stopped")
	}
}

// setupMIDI initializes MIDI input and output ports
func (b *MIDILinkBridge) setupMIDI() error {
	// Create MIDI input port
	inPort, err := abletonlink.NewMidiIn()
	if err != nil {
		return fmt.Errorf("failed to create MIDI input: %w", err)
	}
	
	// Open input port (virtual or physical)
	if *midiInPort >= 0 {
		// Open physical port
		portName := "Physical Port " + strconv.Itoa(*midiInPort)
		if err := inPort.OpenPort(uint(*midiInPort), portName); err != nil {
			inPort.Close()
			return fmt.Errorf("failed to open physical MIDI input port %d: %w", *midiInPort, err)
		}
		b.logInfo("Opened physical MIDI input port %d", *midiInPort)
	} else {
		// Open virtual port
		if err := inPort.OpenVirtualPort(virtualPortName + " In"); err != nil {
			inPort.Close()
			return fmt.Errorf("failed to open virtual MIDI input: %w", err)
		}
		b.logInfo("Opened virtual MIDI input port: %s In", virtualPortName)
	}
	b.midiIn = inPort
	
	// Create MIDI output port
	outPort, err := abletonlink.NewMidiOut()
	if err != nil {
		b.midiIn.Close()
		return fmt.Errorf("failed to create MIDI output: %w", err)
	}
	
	// Open output port (virtual or physical)
	if *midiOutPort >= 0 {
		// Open physical port
		portName := "Physical Port " + strconv.Itoa(*midiOutPort)
		if err := outPort.OpenPort(uint(*midiOutPort), portName); err != nil {
			b.midiIn.Close()
			outPort.Close()
			return fmt.Errorf("failed to open physical MIDI output port %d: %w", *midiOutPort, err)
		}
		b.logInfo("Opened physical MIDI output port %d", *midiOutPort)
	} else {
		// Open virtual port
		if err := outPort.OpenVirtualPort(virtualPortName + " Out"); err != nil {
			b.midiIn.Close()
			outPort.Close()
			return fmt.Errorf("failed to open virtual MIDI output: %w", err)
		}
		b.logInfo("Opened virtual MIDI output port: %s Out", virtualPortName)
	}
	b.midiOut = outPort
	
	// Configure MIDI input to receive ALL message types including timing/clock
	// Don't ignore timing messages (including MIDI clock 0xF8)
	b.midiIn.IgnoreTypes(false, false, true) // sysex=false, time=false, sense=true
	
	// Set up MIDI message handling
	b.midiIn.SetCallback(b.handleMIDIMessage)
	
	return nil
}

// setupLinkCallbacks configures Ableton Link callbacks
func (b *MIDILinkBridge) setupLinkCallbacks() {
	b.link.SetNumPeersCallback(func(numPeers uint64) {
		b.logInfo("Link peers: %d", numPeers)
	})
	
	b.link.SetTempoCallback(func(tempo float64) {
		b.mu.Lock()
		if abs(tempo-b.lastLinkTempo) > tempoTolerance {
			b.lastLinkTempo = tempo
			// Don't log Link tempo changes - too verbose when syncing from MIDI
		}
		b.mu.Unlock()
	})
	
	// Note: Transport handling is done in midiClockSender loop for precise timing
	// b.link.SetStartStopCallback(...) - disabled to avoid immediate transport messages
}

// handleMIDIMessage processes individual MIDI messages
func (b *MIDILinkBridge) handleMIDIMessage(data []byte) {
	select {
	case <-b.ctx.Done():
		return
	default:
	}
	
	if len(data) == 0 {
		return
	}
	
	// Log transport messages and other non-clock messages for debugging
	if len(data) >= 1 {
		switch data[0] {
		case 0xF8:
			// Don't log individual clocks to avoid spam
		case 0xFA, 0xFB, 0xFC:
			b.logInfo("MIDI Transport received: 0x%02X (%s)", data[0], 
				map[byte]string{0xFA: "Start", 0xFB: "Continue", 0xFC: "Stop"}[data[0]])
		default:
			// Only log non-clock messages for debugging
			if data[0] != 0xF8 {
				b.logInfo("Other MIDI message: 0x%02X (len: %d)", data[0], len(data))
			}
		}
	}
	
	// Check message type (first byte)
	switch data[0] {
	case 0xF8: // Timing Clock
		b.handleMIDIClock()
		
	case 0xFA: // Start
		b.handleMIDIStart()
		
	case 0xFC: // Stop
		b.handleMIDIStop()
		
	case 0xFB: // Continue
		b.handleMIDIContinue()
	}
}

// handleMIDIClock processes MIDI timing clock messages
func (b *MIDILinkBridge) handleMIDIClock() {
	now := time.Now()
	
	b.mu.Lock()
	defer b.mu.Unlock()
	
	b.midiClockCount++
	
	// Track clock arrival times for jitter analysis
	b.clockTimings = append(b.clockTimings, now)
	if len(b.clockTimings) > 8 {
		b.clockTimings = b.clockTimings[1:] // Keep last 8 timings
	}
	
	// Calculate tempo every 6 clocks for better response to rapid changes
	if b.midiClockCount%6 == 0 && !b.lastMIDIClockTime.IsZero() {
		duration := now.Sub(b.lastMIDIClockTime)
		if duration > 0 {
			// Calculate BPM from 6-clock duration (quarter quarter note)
			// 6 clocks = 1/4 quarter note, so multiply by 4 to get quarter note duration
			quarterNoteDuration := duration.Microseconds() * 4
			bpm := float64(microsecondsPerMinute) / float64(quarterNoteDuration)
			
			// Don't show tempo calculations - only show actual changes
			
			// Check if this is reasonable BPM
			if bpm >= 40 && bpm <= 200 {
				// For fast tempo changes, use immediate response without averaging
				if b.lastMIDITempo == 0.0 || abs(bpm-b.lastMIDITempo) > tempoTolerance {
					oldTempo := b.lastMIDITempo
					b.lastMIDITempo = bpm
					
					if oldTempo == 0.0 {
						b.logInfo("MIDI tempo: %.1f BPM", bpm)
					} else {
						b.logInfo("MIDI tempo: %.1f BPM (was %.1f)", bpm, oldTempo)
					}
					
					// Update Link tempo with phase adjustment if external sync is enabled
					if b.externalSyncEnabled {
						go b.updateLinkTempoWithPhaseAdjustment(bpm, now)
					}
				}
			}
		}
		b.lastMIDIClockTime = now
	} else if b.midiClockCount == 1 {
		// Start timing from first clock
		b.lastMIDIClockTime = now
		if b.externalSyncEnabled {
			b.logInfo("MIDI clock sync started")
		}
	}
}

// handleMIDIStart processes MIDI start messages
func (b *MIDILinkBridge) handleMIDIStart() {
	b.mu.Lock()
	b.midiIsPlaying = true
	b.midiClockCount = 0
	b.mu.Unlock()
	
	if b.externalSyncEnabled {
		b.logInfo("MIDI Start received - syncing to Link (external sync mode)")
		b.syncMIDITransportToLink(true, false) // not continue
	} else {
		b.logInfo("MIDI Start received")
	}
}

// handleMIDIStop processes MIDI stop messages
func (b *MIDILinkBridge) handleMIDIStop() {
	b.mu.Lock()
	b.midiIsPlaying = false
	b.midiClockCount = 0
	b.mu.Unlock()
	
	if b.externalSyncEnabled {
		b.logInfo("MIDI Stop received - syncing to Link (external sync mode)")
		b.syncMIDITransportToLink(false, false)
	} else {
		b.logInfo("MIDI Stop received")
	}
}

// handleMIDIContinue processes MIDI continue messages
func (b *MIDILinkBridge) handleMIDIContinue() {
	b.mu.Lock()
	b.midiIsPlaying = true
	b.mu.Unlock()
	
	if b.externalSyncEnabled {
		b.logInfo("MIDI Continue received - syncing to Link (external sync mode)")
		b.syncMIDITransportToLink(true, true) // is continue
	} else {
		b.logInfo("MIDI Continue received")
	}
}

// updateLinkTempo synchronizes MIDI tempo to Link using forceBeatAtTime
func (b *MIDILinkBridge) updateLinkTempo(bpm float64) {
	b.link.CaptureAppSessionState(b.state)
	currentTime := b.link.ClockMicros()
	
	// Use forceBeatAtTime to rudely take control of the Link session
	// This is appropriate when MIDI is the master clock source
	currentBeat := b.state.BeatAtTime(currentTime, defaultQuantum)
	b.state.SetTempo(bpm, currentTime)
	b.state.ForceBeatAtTime(currentBeat, uint64(currentTime), defaultQuantum)
	
	b.link.CommitAppSessionState(b.state)
	// Don't log - tempo change already logged above
}

// updateLinkTempoWithPhaseAdjustment synchronizes tempo with jitter compensation
func (b *MIDILinkBridge) updateLinkTempoWithPhaseAdjustment(bpm float64, clockTime time.Time) {
	b.link.CaptureAppSessionState(b.state)
	currentTime := b.link.ClockMicros()
	
	// Calculate expected clock interval from BPM
	expectedInterval := time.Duration(float64(time.Minute) / (bpm * midiClocksPerQuarterNote))
	b.expectedClockInterval = expectedInterval
	
	// Analyze jitter and calculate phase correction
	phaseCorrection := b.calculatePhaseCorrection(clockTime)
	
	// Apply phase correction to the Link timeline
	adjustedTime := uint64(currentTime) + uint64(phaseCorrection.Microseconds())
	
	currentBeat := b.state.BeatAtTime(currentTime, defaultQuantum)
	b.state.SetTempo(bpm, currentTime)
	
	// Use phase-adjusted time for more accurate beat alignment
	b.state.ForceBeatAtTime(currentBeat, adjustedTime, defaultQuantum)
	
	b.link.CommitAppSessionState(b.state)
	
	// Log phase adjustments (show all non-zero adjustments)
	if phaseCorrection != 0 {
		b.logInfo("Phase adjustment: %+.2fms (jitter compensation)", float64(phaseCorrection.Microseconds())/1000.0)
	}
}

// calculatePhaseCorrection analyzes clock jitter and returns a correction
func (b *MIDILinkBridge) calculatePhaseCorrection(currentClock time.Time) time.Duration {
	if len(b.clockTimings) < 4 || b.expectedClockInterval == 0 {
		return 0 // Not enough data
	}
	
	// Calculate average jitter from recent clock intervals
	var totalJitter time.Duration
	var jitterCount int
	
	for i := 1; i < len(b.clockTimings); i++ {
		actualInterval := b.clockTimings[i].Sub(b.clockTimings[i-1])
		jitter := actualInterval - b.expectedClockInterval
		totalJitter += jitter
		jitterCount++
	}
	
	if jitterCount == 0 {
		return 0
	}
	
	avgJitter := totalJitter / time.Duration(jitterCount)
	
	// Log jitter analysis when it's significant
	if abs(float64(avgJitter.Microseconds())) > 500 { // > 0.5ms
		b.logInfo("Clock jitter detected: %+.2fms avg over %d intervals", 
			float64(avgJitter.Microseconds())/1000.0, jitterCount)
	}
	
	// Apply gentle phase correction (max 2ms adjustment)
	maxCorrection := 2 * time.Millisecond
	originalJitter := avgJitter
	if avgJitter > maxCorrection {
		avgJitter = maxCorrection
	} else if avgJitter < -maxCorrection {
		avgJitter = -maxCorrection
	}
	
	// Log when we're clamping the correction
	if avgJitter != originalJitter {
		b.logInfo("Phase correction clamped: %+.2fms -> %+.2fms", 
			float64(originalJitter.Microseconds())/1000.0,
			float64(avgJitter.Microseconds())/1000.0)
	}
	
	// Accumulate phase offset with decay
	oldOffset := b.phaseOffset
	b.phaseOffset = (b.phaseOffset*3 + avgJitter) / 4 // 75% decay
	
	// Log phase offset changes
	if abs(float64(b.phaseOffset.Microseconds()-oldOffset.Microseconds())) > 100 { // > 0.1ms change
		b.logInfo("Phase offset updated: %+.2fms -> %+.2fms", 
			float64(oldOffset.Microseconds())/1000.0,
			float64(b.phaseOffset.Microseconds())/1000.0)
	}
	
	return b.phaseOffset
}

// syncMIDITransportToLink synchronizes MIDI transport to Link (external sync mode)
func (b *MIDILinkBridge) syncMIDITransportToLink(isPlaying bool, isContinue bool) {
	b.link.CaptureAppSessionState(b.state)
	currentTime := b.link.ClockMicros()
	
	if isPlaying {
		if !isContinue {
			// Use atomic operation to start transport and align to beat 0
			// This ensures transport state and beat alignment happen together
			b.state.SetIsPlayingAndRequestBeatAtTime(true, uint64(currentTime), 0.0, defaultQuantum)
			b.logInfo("Link transport started from MIDI (start) - atomic beat 0 alignment")
		} else {
			// For continue, preserve current beat position
			currentBeat := b.state.BeatAtTime(currentTime, defaultQuantum)
			// Use atomic operation to maintain beat continuity
			b.state.SetIsPlayingAndRequestBeatAtTime(true, uint64(currentTime), currentBeat, defaultQuantum)
			b.logInfo("Link transport continued from MIDI - preserved beat %.2f", currentBeat)
		}
	} else {
		// Stop Link transport immediately
		b.state.SetIsPlaying(false, uint64(currentTime))
		b.logInfo("Link transport stopped from MIDI")
	}
	
	b.link.CommitAppSessionState(b.state)
}

// ensurePhaseCoherentTransport ensures MIDI and Link maintain phase coherence during transport changes
func (b *MIDILinkBridge) ensurePhaseCoherentTransport(isPlaying bool, scheduleTime uint64) {
	b.link.CaptureAppSessionState(b.state)
	
	if isPlaying {
		// Calculate the beat at the scheduled start time
		scheduledBeat := b.state.BeatAtTime(int64(scheduleTime), defaultQuantum)
		
		// Ensure the beat is aligned to a musically meaningful boundary
		alignedBeat := float64(int(scheduledBeat)) // Align to beat boundary
		
		// Use atomic operation to ensure transport and beat are set together
		b.state.SetIsPlayingAndRequestBeatAtTime(true, scheduleTime, alignedBeat, defaultQuantum)
		
		// Calculate MIDI clock alignment
		midiClockBeat := alignedBeat * float64(midiClocksPerQuarterNote)
		
		b.logInfo("Phase-coherent transport: Link beat %.1f, MIDI clock %d", alignedBeat, int(midiClockBeat))
	} else {
		b.state.SetIsPlaying(false, scheduleTime)
	}
	
	b.link.CommitAppSessionState(b.state)
}

// updateLinkTransport synchronizes MIDI transport to Link and schedules MIDI messages
func (b *MIDILinkBridge) updateLinkTransport(isPlaying bool) {
	b.link.CaptureAppSessionState(b.state)
	currentTime := b.link.ClockMicros()
	
	if b.quantizeToBar {
		// Quantize transport changes to half-bar boundaries  
		quantum := defaultQuantum * float64(b.beatsPerBar)
		halfBarQuantum := quantum / 2.0  // Half-bar quantum
		if isPlaying {
			// Find next half-bar boundary for Link transport
			currentBeat := b.state.BeatAtTime(currentTime, halfBarQuantum)
			nextHalfBar := float64(int(currentBeat/halfBarQuantum) + 1) * halfBarQuantum
			nextHalfBarTime := b.state.TimeAtBeat(nextHalfBar, halfBarQuantum)
			
			// Set transport to start at the scheduled time
			b.state.SetIsPlaying(true, uint64(nextHalfBarTime))
			
			// Request that the beat aligns properly when transport starts
			// This ensures the beat grid is correctly aligned at transport start
			b.state.RequestBeatAtStartPlayingTime(nextHalfBar, halfBarQuantum)
			
			b.logInfo("Link transport start quantized to beat %.1f at time %d with aligned beat grid", nextHalfBar, nextHalfBarTime)
			
			// Schedule MIDI start message for the same half-bar boundary
			b.mu.Lock()
			wasPreviouslyPlaying := b.linkIsPlaying
			b.scheduledMIDIStart = &scheduledTransport{
				isPlaying:    true,
				scheduleTime: uint64(nextHalfBarTime),
				isContinue:   wasPreviouslyPlaying,
			}
			b.scheduledMIDIStop = nil // Clear any pending stop
			b.mu.Unlock()
			
			b.logInfo("Scheduled MIDI %s for time %d (in %.1f ms)", 
				func() string {
					if wasPreviouslyPlaying {
						return "Continue"
					}
					return "Start"
				}(), nextHalfBarTime, float64(int64(nextHalfBarTime)-int64(currentTime))/1000.0)
		} else {
			// Stop immediately (no quantization for stop)
			b.state.SetIsPlaying(isPlaying, uint64(currentTime))
			
			// Schedule immediate MIDI stop
			b.mu.Lock()
			b.scheduledMIDIStop = &scheduledTransport{
				isPlaying:    false,
				scheduleTime: uint64(currentTime),
				isContinue:   false,
			}
			b.scheduledMIDIStart = nil // Clear any pending start
			b.mu.Unlock()
			
			b.logInfo("Scheduled MIDI Stop for immediate delivery")
		}
	} else {
		b.state.SetIsPlaying(isPlaying, uint64(currentTime))
		
		// Schedule immediate MIDI message
		b.mu.Lock()
		if isPlaying {
			wasPreviouslyPlaying := b.linkIsPlaying
			b.scheduledMIDIStart = &scheduledTransport{
				isPlaying:    true,
				scheduleTime: uint64(currentTime),
				isContinue:   wasPreviouslyPlaying,
			}
			b.scheduledMIDIStop = nil
		} else {
			b.scheduledMIDIStop = &scheduledTransport{
				isPlaying:    false,
				scheduleTime: uint64(currentTime),
				isContinue:   false,
			}
			b.scheduledMIDIStart = nil
		}
		b.mu.Unlock()
	}
	
	b.link.CommitAppSessionState(b.state)
}

// scheduleLinkTransportChange schedules MIDI messages for Link transport changes from external sources
func (b *MIDILinkBridge) scheduleLinkTransportChange(isPlaying bool) {
	b.link.CaptureAppSessionState(b.state)
	currentTime := b.link.ClockMicros()
	
	b.mu.Lock()
	defer b.mu.Unlock()
	
	if b.quantizeToBar && isPlaying {
		// Check if Link has already scheduled a quantized transport start
		// This gives us more accurate timing information
		transportTime := b.state.TimeForIsPlaying()
		
		if transportTime > uint64(currentTime) {
			// Link has a future transport start scheduled, use that timing
			scheduledBeat := b.state.BeatAtTime(int64(transportTime), defaultQuantum)
			
			wasPreviouslyPlaying := b.linkIsPlaying
			b.scheduledMIDIStart = &scheduledTransport{
				isPlaying:    true,
				scheduleTime: transportTime,
				isContinue:   wasPreviouslyPlaying,
			}
			b.scheduledMIDIStop = nil
			
			b.logInfo("Scheduled MIDI %s to match Link transport at beat %.1f, time %d (in %.1f ms)", 
				func() string {
					if wasPreviouslyPlaying {
						return "Continue"
					}
					return "Start"
				}(), scheduledBeat, transportTime, float64(int64(transportTime)-currentTime)/1000.0)
		} else {
			// No future transport scheduled, quantize ourselves
			quantum := defaultQuantum * float64(b.beatsPerBar)
			halfBarQuantum := quantum / 2.0
			
			currentBeat := b.state.BeatAtTime(currentTime, halfBarQuantum)
			nextHalfBar := float64(int(currentBeat/halfBarQuantum) + 1) * halfBarQuantum
			targetTime := b.state.TimeAtBeat(nextHalfBar, halfBarQuantum)
			
			wasPreviouslyPlaying := b.linkIsPlaying
			b.scheduledMIDIStart = &scheduledTransport{
				isPlaying:    true,
				scheduleTime: uint64(targetTime),
				isContinue:   wasPreviouslyPlaying,
			}
			b.scheduledMIDIStop = nil
			
			b.logInfo("Scheduled MIDI %s for beat %.1f at time %d (in %.1f ms, half-bar quantized)", 
				func() string {
					if wasPreviouslyPlaying {
						return "Continue"
					}
					return "Start"
				}(), nextHalfBar, targetTime, float64(int64(targetTime)-currentTime)/1000.0)
		}
	} else if isPlaying {
		// Immediate start (no quantization)
		wasPreviouslyPlaying := b.linkIsPlaying
		b.scheduledMIDIStart = &scheduledTransport{
			isPlaying:    true,
			scheduleTime: uint64(currentTime),
			isContinue:   wasPreviouslyPlaying,
		}
		b.scheduledMIDIStop = nil
		
		b.logInfo("Scheduled immediate MIDI %s", func() string {
			if wasPreviouslyPlaying {
				return "Continue"
			}
			return "Start"
		}())
	} else {
		// Stop immediately (no quantization for stop)
		b.scheduledMIDIStop = &scheduledTransport{
			isPlaying:    false,
			scheduleTime: uint64(currentTime),
			isContinue:   false,
		}
		b.scheduledMIDIStart = nil // Clear any pending start
		
		b.logInfo("Scheduled immediate MIDI Stop")
	}
}

// midiClockSender sends MIDI timing clocks and scheduled transport messages
func (b *MIDILinkBridge) midiClockSender() {
	clockMsg := []byte{0xF8} // Timing Clock
	
	// MIDI clock quantum - each MIDI clock is 1/24 of a quarter note
	const midiClockQuantum = 1.0 / float64(midiClocksPerQuarterNote)
	
	var nextClockBeat float64 = 0
	var lastLinkPlaying bool = false
	var lastTransportTime uint64 = 0
	
	// High-frequency loop for precise timing (similar to audio callback)
	ticker := time.NewTicker(time.Millisecond * 1) // 1ms precision
	defer ticker.Stop()
	
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			// Get current Link time and state
			currentTime := b.link.ClockMicros()
			b.link.CaptureAppSessionState(b.state)
			
			isPlaying := b.state.IsPlaying()
			transportTime := b.state.TimeForIsPlaying()
			
			// Only detect and respond to Link transport changes when NOT in external sync mode
			// In external sync mode, MIDI is the master and controls Link
			if !b.externalSyncEnabled {
				// Detect Link transport changes from external sources (other Link apps)
				if isPlaying != lastLinkPlaying || (transportTime != lastTransportTime && transportTime > lastTransportTime) {
					// Calculate timing difference for precise scheduling
					timeDiff := int64(transportTime) - int64(currentTime)
					
					if isPlaying && !lastLinkPlaying {
						// Link started from external source
						b.logInfo("Link external transport start detected - will occur in %.1fms", float64(timeDiff)/1000.0)
						
						if timeDiff > 0 {
							// Transport start is in the future, schedule MIDI to coincide
							b.mu.Lock()
							b.scheduledMIDIStart = &scheduledTransport{
								isPlaying:    true,
								scheduleTime: transportTime, // Use exact Link transport time
								isContinue:   lastLinkPlaying, // Was previously playing
							}
							b.scheduledMIDIStop = nil
							b.mu.Unlock()
						} else {
							// Transport already started, send MIDI immediately
							b.scheduleLinkTransportChange(true)
						}
					} else if !isPlaying && lastLinkPlaying {
						// Link stopped from external source - schedule MIDI stop
						b.logInfo("Link external transport stop detected - scheduling MIDI output")
						b.scheduleLinkTransportChange(false)
					}
					lastLinkPlaying = isPlaying
					lastTransportTime = transportTime
				}
			}
			
			// Update internal state tracking
			b.mu.Lock()
			b.linkIsPlaying = isPlaying
			
			// Check for scheduled MIDI stop messages
			if b.scheduledMIDIStop != nil && uint64(currentTime) >= b.scheduledMIDIStop.scheduleTime {
				msg := []byte{0xFC} // Stop
				if err := b.midiOut.SendMessage(msg); err != nil {
					log.Printf("Failed to send scheduled MIDI Stop: %v", err)
				} else {
					timeDiff := int64(currentTime) - int64(b.scheduledMIDIStop.scheduleTime)
					b.logInfo("MIDI transport sent: Stop at time %d (scheduled: %d, diff: %.1f ms)", 
						currentTime, b.scheduledMIDIStop.scheduleTime, float64(timeDiff)/1000.0)
				}
				b.scheduledMIDIStop = nil // Clear the scheduled event
			}
			
			// Check for scheduled MIDI start/continue messages
			if b.scheduledMIDIStart != nil && uint64(currentTime) >= b.scheduledMIDIStart.scheduleTime {
				var msg []byte
				var msgType string
				
				if b.scheduledMIDIStart.isContinue {
					msg = []byte{0xFB} // Continue
					msgType = "Continue"
				} else {
					msg = []byte{0xFA} // Start
					msgType = "Start"
					
					// Get the exact beat at transport start time for phase-coherent alignment
					startBeat := b.state.BeatAtTime(int64(b.scheduledMIDIStart.scheduleTime), midiClockQuantum)
					
					// Align MIDI clock to the nearest clock boundary
					// This ensures MIDI clock phase matches Link beat phase
					nextClockBeat = float64(int(startBeat/midiClockQuantum)) * midiClockQuantum
					
					b.logInfo("MIDI clock aligned to beat %.3f at transport start", nextClockBeat)
				}
				
				if err := b.midiOut.SendMessage(msg); err != nil {
					log.Printf("Failed to send scheduled MIDI %s: %v", msgType, err)
				} else {
					timeDiff := int64(currentTime) - int64(b.scheduledMIDIStart.scheduleTime)
					b.logInfo("MIDI transport sent: %s at time %d (scheduled: %d, diff: %.1f ms)", 
						msgType, currentTime, b.scheduledMIDIStart.scheduleTime, float64(timeDiff)/1000.0)
				}
				
				b.scheduledMIDIStart = nil // Clear the scheduled event
			}
			
			b.mu.Unlock()
			
			// Only send MIDI clocks when playing
			if !isPlaying {
				continue
			}
			
			// Get current beat position with MIDI clock quantum
			currentBeat := b.state.BeatAtTime(currentTime, midiClockQuantum)
			
			// Check if it's time to send the next MIDI clock
			if currentBeat >= nextClockBeat {
				// Send MIDI clock
				if err := b.midiOut.SendMessage(clockMsg); err != nil {
					log.Printf("Failed to send MIDI clock: %v", err)
				}
				
				// Schedule next clock
				nextClockBeat += midiClockQuantum
				
				// Prevent drift by ensuring we don't skip clocks
				for currentBeat >= nextClockBeat {
					nextClockBeat += midiClockQuantum
				}
			}
		}
	}
}


// Utility functions

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func playingStateString(isPlaying bool) string {
	if isPlaying {
		return "playing"
	}
	return "stopped"
}

// setupUI initializes the TUI interface
func (b *MIDILinkBridge) setupUI() {
	b.app = tview.NewApplication()
	
	// Create stats table
	b.statsTable = tview.NewTable().SetBorders(true)
	b.statsTable.SetTitle(" MIDI-Link Bridge Stats ").SetBorder(true)
	
	// Create log view
	b.logView = tview.NewTextView().
		SetDynamicColors(true).
		SetChangedFunc(func() {
			b.app.Draw()
		})
	b.logView.SetTitle(" Log Messages ").SetBorder(true)
	
	// Create main layout
	flex := tview.NewFlex().
		AddItem(b.statsTable, 0, 1, true).
		AddItem(b.logView, 0, 1, false)
	
	b.app.SetRoot(flex, true)
	
	// Initialize stats table headers
	b.updateStatsTable()
	
	// Start UI update routine
	go b.uiUpdateLoop()
}

// getLogWriter returns a writer that sends logs to the UI
func (b *MIDILinkBridge) getLogWriter() *uiLogWriter {
	return &uiLogWriter{bridge: b}
}

// uiLogWriter implements io.Writer for logrus
type uiLogWriter struct {
	bridge *MIDILinkBridge
}

func (w *uiLogWriter) Write(p []byte) (n int, err error) {
	if w.bridge.logView != nil {
		w.bridge.logView.Write(p)
	}
	return len(p), nil
}

// updateStatsTable refreshes the stats display
func (b *MIDILinkBridge) updateStatsTable() {
	if !b.uiEnabled || b.statsTable == nil {
		return
	}
	
	b.mu.RLock()
	linkTempo := b.lastLinkTempo
	midiTempo := b.lastMIDITempo
	clockCount := b.midiClockCount
	linkPlaying := b.linkIsPlaying
	midiPlaying := b.midiIsPlaying
	phaseOffset := b.phaseOffset
	externalSync := b.externalSyncEnabled
	b.mu.RUnlock()
	
	// Clear and rebuild table
	b.statsTable.Clear()
	
	row := 0
	b.statsTable.SetCell(row, 0, tview.NewTableCell("Mode:").SetTextColor(tcell.ColorYellow))
	mode := "Link Master"
	if externalSync {
		mode = "MIDI Master"
	}
	b.statsTable.SetCell(row, 1, tview.NewTableCell(mode).SetTextColor(tcell.ColorWhite))
	row++
	
	b.statsTable.SetCell(row, 0, tview.NewTableCell("Link Tempo:").SetTextColor(tcell.ColorYellow))
	b.statsTable.SetCell(row, 1, tview.NewTableCell(fmt.Sprintf("%.1f BPM", linkTempo)).SetTextColor(tcell.ColorGreen))
	row++
	
	if midiTempo > 0 {
		b.statsTable.SetCell(row, 0, tview.NewTableCell("MIDI Tempo:").SetTextColor(tcell.ColorYellow))
		b.statsTable.SetCell(row, 1, tview.NewTableCell(fmt.Sprintf("%.1f BPM", midiTempo)).SetTextColor(tcell.ColorGreen))
		row++
	}
	
	b.statsTable.SetCell(row, 0, tview.NewTableCell("Link Transport:").SetTextColor(tcell.ColorYellow))
	linkStatus := "Stopped"
	statusColor := tcell.ColorRed
	if linkPlaying {
		linkStatus = "Playing"
		statusColor = tcell.ColorGreen
	}
	b.statsTable.SetCell(row, 1, tview.NewTableCell(linkStatus).SetTextColor(statusColor))
	row++
	
	b.statsTable.SetCell(row, 0, tview.NewTableCell("MIDI Transport:").SetTextColor(tcell.ColorYellow))
	midiStatus := "Stopped"
	statusColor = tcell.ColorRed
	if midiPlaying {
		midiStatus = "Playing"
		statusColor = tcell.ColorGreen
	}
	b.statsTable.SetCell(row, 1, tview.NewTableCell(midiStatus).SetTextColor(statusColor))
	row++
	
	if clockCount > 0 {
		b.statsTable.SetCell(row, 0, tview.NewTableCell("MIDI Clocks:").SetTextColor(tcell.ColorYellow))
		b.statsTable.SetCell(row, 1, tview.NewTableCell(fmt.Sprintf("%d", clockCount)).SetTextColor(tcell.ColorBlue))
		row++
	}
	
	if phaseOffset != 0 {
		b.statsTable.SetCell(row, 0, tview.NewTableCell("Phase Offset:").SetTextColor(tcell.ColorYellow))
		offsetColor := tcell.ColorGreen
		if abs(float64(phaseOffset.Microseconds())) > 1000 {
			offsetColor = tcell.ColorRed
		}
		b.statsTable.SetCell(row, 1, tview.NewTableCell(fmt.Sprintf("%+.2fms", float64(phaseOffset.Microseconds())/1000.0)).SetTextColor(offsetColor))
		row++
	}
	
	// Instructions
	row++
	b.statsTable.SetCell(row, 0, tview.NewTableCell("Press 'q' to quit").SetTextColor(tcell.ColorDarkGray))
}

// uiUpdateLoop updates the UI periodically
func (b *MIDILinkBridge) uiUpdateLoop() {
	if !b.uiEnabled {
		return
	}
	
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			if b.app != nil {
				b.app.QueueUpdateDraw(func() {
					b.updateStatsTable()
				})
			}
		}
	}
}

// runUI starts the TUI
func (b *MIDILinkBridge) runUI() error {
	if !b.uiEnabled {
		return nil
	}
	
	// Set up key bindings
	b.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' || event.Key() == tcell.KeyEscape {
			b.app.Stop()
			return nil
		}
		return event
	})
	
	return b.app.Run()
}

// logInfo logs an info message (will go to UI if enabled)
func (b *MIDILinkBridge) logInfo(msg string, args ...interface{}) {
	if b.uiEnabled {
		logrus.Infof(msg, args...)
	} else {
		fmt.Printf(msg+"\n", args...)
	}
}

func listAvailablePorts() {
	fmt.Println("Available MIDI Input Ports:")
	inPorts, err := abletonlink.ListInputPorts()
	if err != nil {
		fmt.Printf("Error listing input ports: %v\n", err)
	} else {
		for i, port := range inPorts {
			fmt.Printf("  %d: %s\n", i, port)
		}
	}

	fmt.Println("\nAvailable MIDI Output Ports:")
	outPorts, err := abletonlink.ListOutputPorts()
	if err != nil {
		fmt.Printf("Error listing output ports: %v\n", err)
	} else {
		for i, port := range outPorts {
			fmt.Printf("  %d: %s\n", i, port)
		}
	}
}

func main() {
	flag.Parse()

	// Handle port listing
	if *listPorts {
		listAvailablePorts()
		return
	}

	// Create bridge with specified initial tempo, sync mode, and UI mode
	bridge, err := NewMIDILinkBridge(*initialTempo, *enableExternalSync, *cuiMode)
	if err != nil {
		log.Fatalf("Failed to create bridge: %v", err)
	}
	
	// Start the bridge
	if err := bridge.Start(); err != nil {
		log.Fatalf("Failed to start bridge: %v", err)
	}
	
	if *cuiMode {
		// Run TUI mode
		if err := bridge.runUI(); err != nil {
			log.Fatalf("UI error: %v", err)
		}
	} else {
		// Wait for interrupt signal in CLI mode
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		
		fmt.Println("MIDI-Link Bridge running... Press Ctrl+C to stop")
		fmt.Println("\nUsage:")
		fmt.Println("  -list-ports              List available MIDI ports")
		fmt.Println("  -midi-in-port <n>        Use physical input port number n")
		fmt.Println("  -midi-out-port <n>       Use physical output port number n")
		fmt.Println("  -tempo <bpm>             Set initial tempo (default: 120)")
		fmt.Println("  -enable-external-sync    MIDI clock/transport controls Link (default: Link controls MIDI)")
		fmt.Println("  -cui                     Enable console UI mode")
		fmt.Println("")
		fmt.Println("Without port flags, virtual ports are created for other apps to connect to.")
		fmt.Println("")
		<-c
	}
	
	// Graceful shutdown
	bridge.Stop()
}
