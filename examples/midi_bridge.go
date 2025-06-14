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
)

const (
	// MIDI timing constants
	midiClocksPerQuarterNote = 24
	microsecondsPerMinute    = 60_000_000
	defaultQuantum           = 4.0
	
	// Bridge configuration
	virtualPortName = "Link-MIDI Bridge"
	tempoTolerance  = 0.5 // BPM tolerance for tempo changes (responsive to fast changes)
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
	
	// Feedback prevention
	lastMIDITransportSource string // Track source of last transport change
	
	// Context for shutdown
	ctx    context.Context
	cancel context.CancelFunc
	
}

// NewMIDILinkBridge creates a new bridge instance
func NewMIDILinkBridge(initialTempo float64, externalSync bool) (*MIDILinkBridge, error) {
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
		ctx:                 ctx,
		cancel:              cancel,
	}
	
	// Set up Link callbacks
	bridge.setupLinkCallbacks()
	
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
	
	fmt.Printf("MIDI-Link Bridge started\n")
	if *midiInPort >= 0 || *midiOutPort >= 0 {
		fmt.Printf("Using physical MIDI ports\n")
	} else {
		fmt.Printf("Virtual MIDI ports: %s In/Out\n", virtualPortName)
	}
	fmt.Printf("Initial tempo: %.2f BPM\n", b.lastLinkTempo)
	if b.externalSyncEnabled {
		fmt.Printf("External sync enabled: MIDI clock controls Link\n")
	} else {
		fmt.Printf("Internal sync: Link controls MIDI output\n")
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
	
	fmt.Println("MIDI-Link Bridge stopped")
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
		fmt.Printf("Opened physical MIDI input port %d\n", *midiInPort)
	} else {
		// Open virtual port
		if err := inPort.OpenVirtualPort(virtualPortName + " In"); err != nil {
			inPort.Close()
			return fmt.Errorf("failed to open virtual MIDI input: %w", err)
		}
		fmt.Printf("Opened virtual MIDI input port: %s In\n", virtualPortName)
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
		fmt.Printf("Opened physical MIDI output port %d\n", *midiOutPort)
	} else {
		// Open virtual port
		if err := outPort.OpenVirtualPort(virtualPortName + " Out"); err != nil {
			b.midiIn.Close()
			outPort.Close()
			return fmt.Errorf("failed to open virtual MIDI output: %w", err)
		}
		fmt.Printf("Opened virtual MIDI output port: %s Out\n", virtualPortName)
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
		fmt.Printf("Link peers: %d\n", numPeers)
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
			fmt.Printf("MIDI Transport received: 0x%02X (%s)\n", data[0], 
				map[byte]string{0xFA: "Start", 0xFB: "Continue", 0xFC: "Stop"}[data[0]])
		default:
			// Only log non-clock messages for debugging
			if data[0] != 0xF8 {
				fmt.Printf("Other MIDI message: 0x%02X (len: %d)\n", data[0], len(data))
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
						fmt.Printf("MIDI tempo: %.1f BPM\n", bpm)
					} else {
						fmt.Printf("MIDI tempo: %.1f BPM (was %.1f)\n", bpm, oldTempo)
					}
					
					// Update Link tempo if external sync is enabled
					if b.externalSyncEnabled {
						go b.updateLinkTempo(bpm)
					}
				}
			}
		}
		b.lastMIDIClockTime = now
	} else if b.midiClockCount == 1 {
		// Start timing from first clock
		b.lastMIDIClockTime = now
		if b.externalSyncEnabled {
			fmt.Printf("MIDI clock sync started\n")
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
		fmt.Println("MIDI Start received - syncing to Link (external sync mode)")
		b.syncMIDITransportToLink(true, false) // not continue
	} else {
		fmt.Println("MIDI Start received")
	}
}

// handleMIDIStop processes MIDI stop messages
func (b *MIDILinkBridge) handleMIDIStop() {
	b.mu.Lock()
	b.midiIsPlaying = false
	b.midiClockCount = 0
	b.mu.Unlock()
	
	if b.externalSyncEnabled {
		fmt.Println("MIDI Stop received - syncing to Link (external sync mode)")
		b.syncMIDITransportToLink(false, false)
	} else {
		fmt.Println("MIDI Stop received")
	}
}

// handleMIDIContinue processes MIDI continue messages
func (b *MIDILinkBridge) handleMIDIContinue() {
	b.mu.Lock()
	b.midiIsPlaying = true
	b.mu.Unlock()
	
	if b.externalSyncEnabled {
		fmt.Println("MIDI Continue received - syncing to Link (external sync mode)")
		b.syncMIDITransportToLink(true, true) // is continue
	} else {
		fmt.Println("MIDI Continue received")
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

// syncMIDITransportToLink synchronizes MIDI transport to Link (external sync mode)
func (b *MIDILinkBridge) syncMIDITransportToLink(isPlaying bool, isContinue bool) {
	b.link.CaptureAppSessionState(b.state)
	currentTime := b.link.ClockMicros()
	
	if isPlaying {
		// Force the Link session to align with MIDI timing when starting
		// Calculate the current beat position based on MIDI clock timing
		currentBeat := b.state.BeatAtTime(currentTime, defaultQuantum)
		
		// If this is a fresh start (not continue), align to beat 0
		if !isContinue {
			// Force beat 0 at the current time - MIDI Start resets the timeline
			b.state.ForceBeatAtTime(0.0, uint64(currentTime), defaultQuantum)
		} else {
			// For continue, maintain current beat position
			b.state.ForceBeatAtTime(currentBeat, uint64(currentTime), defaultQuantum)
		}
		
		b.state.SetIsPlaying(true, uint64(currentTime))
		fmt.Printf("Link transport started from MIDI (%s) - forced beat alignment\n", func() string {
			if isContinue {
				return "continue"
			}
			return "start"
		}())
	} else {
		// Stop Link transport immediately
		b.state.SetIsPlaying(false, uint64(currentTime))
		fmt.Printf("Link transport stopped from MIDI\n")
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
			
			b.state.SetIsPlaying(isPlaying, uint64(nextHalfBarTime))
			fmt.Printf("Link transport start quantized to beat %.1f at time %d\n", nextHalfBar, nextHalfBarTime)
			
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
			
			fmt.Printf("Scheduled MIDI %s for time %d (in %.1f ms)\n", 
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
			
			fmt.Printf("Scheduled MIDI Stop for immediate delivery\n")
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
		// Quantize to half-bar boundaries to wait at most half a bar
		quantum := defaultQuantum * float64(b.beatsPerBar)
		halfBarQuantum := quantum / 2.0  // Half-bar quantum
		
		currentBeat := b.state.BeatAtTime(currentTime, halfBarQuantum)
		// Find next half-bar boundary
		nextHalfBar := float64(int(currentBeat/halfBarQuantum) + 1) * halfBarQuantum
		targetTime := b.state.TimeAtBeat(nextHalfBar, halfBarQuantum)
		
		// Schedule MIDI start message for the half-bar boundary
		wasPreviouslyPlaying := b.linkIsPlaying
		b.scheduledMIDIStart = &scheduledTransport{
			isPlaying:    true,
			scheduleTime: uint64(targetTime),
			isContinue:   wasPreviouslyPlaying,
		}
		b.scheduledMIDIStop = nil // Clear any pending stop
		
		fmt.Printf("Scheduled MIDI %s for beat %.1f at time %d (in %.1f ms, half-bar quantized)\n", 
			func() string {
				if wasPreviouslyPlaying {
					return "Continue"
				}
				return "Start"
			}(), nextHalfBar, targetTime, float64(int64(targetTime)-int64(currentTime))/1000.0)
	} else if isPlaying {
		// Immediate start (no quantization)
		wasPreviouslyPlaying := b.linkIsPlaying
		b.scheduledMIDIStart = &scheduledTransport{
			isPlaying:    true,
			scheduleTime: uint64(currentTime),
			isContinue:   wasPreviouslyPlaying,
		}
		b.scheduledMIDIStop = nil
		
		fmt.Printf("Scheduled immediate MIDI %s\n", func() string {
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
		
		fmt.Printf("Scheduled immediate MIDI Stop\n")
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
					if isPlaying && !lastLinkPlaying {
						// Link started from external source - schedule MIDI start
						fmt.Printf("Link external transport start detected - scheduling MIDI output\n")
						b.scheduleLinkTransportChange(true)
					} else if !isPlaying && lastLinkPlaying {
						// Link stopped from external source - schedule MIDI stop
						fmt.Printf("Link external transport stop detected - scheduling MIDI output\n")
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
					fmt.Printf("MIDI transport sent: Stop at time %d (scheduled: %d, diff: %.1f ms)\n", 
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
					
					// Transport started - align to current MIDI clock boundary
					currentBeat := b.state.BeatAtTime(currentTime, midiClockQuantum)
					nextClockBeat = float64(int(currentBeat/midiClockQuantum)+1) * midiClockQuantum
				}
				
				if err := b.midiOut.SendMessage(msg); err != nil {
					log.Printf("Failed to send scheduled MIDI %s: %v", msgType, err)
				} else {
					timeDiff := int64(currentTime) - int64(b.scheduledMIDIStart.scheduleTime)
					fmt.Printf("MIDI transport sent: %s at time %d (scheduled: %d, diff: %.1f ms)\n", 
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

	// Create bridge with specified initial tempo and sync mode
	bridge, err := NewMIDILinkBridge(*initialTempo, *enableExternalSync)
	if err != nil {
		log.Fatalf("Failed to create bridge: %v", err)
	}
	
	// Start the bridge
	if err := bridge.Start(); err != nil {
		log.Fatalf("Failed to start bridge: %v", err)
	}
	
	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	
	fmt.Println("MIDI-Link Bridge running... Press Ctrl+C to stop")
	fmt.Println("\nUsage:")
	fmt.Println("  -list-ports              List available MIDI ports")
	fmt.Println("  -midi-in-port <n>        Use physical input port number n")
	fmt.Println("  -midi-out-port <n>       Use physical output port number n")
	fmt.Println("  -tempo <bpm>             Set initial tempo (default: 120)")
	fmt.Println("  -enable-external-sync    MIDI clock/transport controls Link (default: Link controls MIDI)")
	fmt.Println("")
	fmt.Println("Without port flags, virtual ports are created for other apps to connect to.")
	fmt.Println("")
	<-c
	
	// Graceful shutdown
	bridge.Stop()
}