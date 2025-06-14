package main

import (
	"fmt"
	"time"

	"github.com/DatanoiseTV/abletonlink-go"
)

func main() {
	// Create a Link instance with 120 BPM initial tempo
	link := abletonlink.NewLink(120.0)
	defer link.Destroy()

	// Set up callbacks
	link.SetNumPeersCallback(func(numPeers uint64) {
		fmt.Printf("Number of peers changed: %d\n", numPeers)
	})

	link.SetTempoCallback(func(tempo float64) {
		fmt.Printf("Tempo changed: %.2f BPM\n", tempo)
	})

	link.SetStartStopCallback(func(isPlaying bool) {
		fmt.Printf("Transport state changed: playing=%v\n", isPlaying)
	})

	// Enable Link
	fmt.Println("Enabling Link...")
	link.Enable(true)
	defer link.Enable(false)

	// Enable start/stop sync
	link.EnableStartStopSync(true)

	// Create session state for manipulating the timeline
	state := abletonlink.NewSessionState()
	defer state.Destroy()

	fmt.Printf("Link enabled: %v\n", link.IsEnabled())
	fmt.Printf("Start/stop sync enabled: %v\n", link.IsStartStopSyncEnabled())

	// Demo loop
	for i := 0; i < 30; i++ {
		// Capture current session state
		link.CaptureAppSessionState(state)

		// Get current time and tempo
		currentTime := link.ClockMicros()
		tempo := state.Tempo()
		numPeers := link.NumPeers()
		isPlaying := state.IsPlaying()

		// Calculate current beat and phase
		quantum := 4.0 // 4/4 time
		beat := state.BeatAtTime(currentTime, quantum)
		phase := state.PhaseAtTime(currentTime, quantum)

		fmt.Printf("Time: %d μs | Tempo: %.2f BPM | Beat: %.2f | Phase: %.2f | Peers: %d | Playing: %v\n",
			currentTime, tempo, beat, phase, numPeers, isPlaying)

		// Example: Set tempo to 130 BPM after 5 seconds
		if i == 5 {
			fmt.Println("Changing tempo to 130 BPM...")
			state.SetTempo(130.0, currentTime)
			link.CommitAppSessionState(state)
		}

		// Example: Start transport after 10 seconds
		if i == 10 && !isPlaying {
			fmt.Println("Starting transport...")
			state.SetIsPlaying(true, uint64(currentTime))
			link.CommitAppSessionState(state)
		}

		// Example: Stop transport after 20 seconds
		if i == 20 && isPlaying {
			fmt.Println("Stopping transport...")
			state.SetIsPlaying(false, uint64(currentTime))
			link.CommitAppSessionState(state)
		}

		time.Sleep(1 * time.Second)
	}

	fmt.Println("Example completed.")
}