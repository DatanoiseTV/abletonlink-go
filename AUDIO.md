# Using LinkAudio with abletonlink-go

`abletonlink-go` provides bindings for `LinkAudio`, which allows for the synchronization of audio data between multiple peers on a network. This document explains how to use this functionality, based on the examples in the codebase.

## Core Concepts

The audio functionality in `abletonlink-go` is based on a `Source` and `Sink` model:

*   **`Source`**: A `Source` is used to receive audio data from other peers. You create a `Source` for each audio channel you want to receive from.
*   **`Sink`**: A `Sink` is used to send your own application's audio to other peers on the Link network.

Your application acts as the central mixer or digital audio workstation (DAW). You are responsible for all audio processing, including mixing, resampling, and buffering.

## Enabling Audio

To use any of the audio features, you must first enable audio on your `Link` instance:

```go
link := abletonlink.NewLink(120.0)
link.EnableAudio(true)
```

## Receiving Audio (Source)

Receiving audio from other peers involves discovering their audio channels and creating a `Source` for each channel you want to listen to.

### 1. Discovering Channels

You can get a list of available audio channels from the Link network using the `Channels()` method:

```go
channels := link.Channels()
for _, channel := range channels {
    fmt.Printf("Found channel: %s from peer: %s
", channel.Name, channel.PeerName)
}
```

### 2. Creating a Source

Once you have identified the channel you want to receive audio from, you can create a `Source` for it. The `NewSource` function takes the channel ID and a callback function as arguments. The callback will be invoked whenever a new audio buffer is received from that channel.

```go
// Assuming 'selectedChannel' is a channel from the list obtained above
source := link.NewSource(selectedChannel.ID, func(samples []int16, info abletonlink.SourceBufferInfo) {
    // Process the received audio samples here
})
```

### 3. The Source Callback and Synchronization

The `Source` callback receives two arguments: `samples` (a slice of `int16` audio samples) and `info` (a `SourceBufferInfo` struct containing metadata about the buffer).

The `SourceBufferInfo` is crucial for synchronization. It contains the `SessionBeatTime` at which the buffer was sent. To synchronize this audio with your local Link clock, you need to convert this beat time to your local clock's time.

Here is a more detailed example of a `Source` callback:

```go
source := link.NewSource(selectedChannel.ID, func(samples []int16, info abletonlink.SourceBufferInfo) {
    // The session state is needed to map the beat time to the local clock
    state := abletonlink.NewSessionState()
    link.CaptureAudioSessionState(state)
    defer state.Destroy()

    // Get the time in microseconds for the start of the buffer
    startMicros := state.TimeAtBeat(info.SessionBeatTime, 4.0)

    // Now you can use startMicros to schedule the playback of the received samples
    // in your audio engine. A common technique is to use a delay line or
    // a ring buffer to handle jitter.
    fmt.Printf("Received %d samples at beat %f (time: %d us)
", len(samples), info.SessionBeatTime, startMicros)
})
```

## Sending Audio (Sink)

Sending your application's audio to the Link network involves creating a `Sink` and then continuously writing audio data to it in your audio processing loop.

### 1. Creating a Sink

You create a `Sink` with a name that will be visible to other peers on the network, and a maximum buffer size.

```go
// Create a sink with a max buffer size of 1024 frames
sink := link.NewSink("My Go App", 1024)
```

### 2. The Audio Sending Loop

In your application's audio processing loop (which should be driven by your audio hardware or a `time.Ticker`), you will perform the following steps to send audio:

1.  **Retain a buffer** from the sink.
2.  If the buffer is valid, **fill it with your audio data**.
3.  **Capture the audio session state**.
4.  **Commit the buffer** with the correct timing information.

Here is an example of an audio loop that sends a sine wave:

```go
const (
    sampleRate  = 48000
    channels    = 2
    quantum     = 4.0
    frameSize   = 512
)

go func() {
    ticker := time.NewTicker(time.Duration(frameSize*1000/sampleRate) * time.Millisecond)
    var phase float64

    for range ticker.C {
        // 1. Retain a buffer from the sink
        buffer := sink.RetainBuffer()
        if buffer == nil {
            continue
        }

        // 2. Fill the buffer with a sine wave
        for i := 0; i < frameSize; i++ {
            value := int16(math.Sin(phase) * 32767.0)
            buffer.Samples[i*channels] = value
            buffer.Samples[i*channels+1] = value
            phase += 2 * math.Pi * 440.0 / sampleRate
        }

        // 3. Capture the session state for timestamping
        state := abletonlink.NewSessionState()
        link.CaptureAudioSessionState(state)
        defer state.Destroy()

        // 4. Calculate the beat time at the start of the buffer
        // This is a simplified example; in a real app, you would have a more
        // accurate way of tracking the beat time of your audio generation.
        beatsAtBufferBegin := state.BeatAtTime(link.ClockMicros(), quantum)

        // 5. Commit the buffer
        sink.Commit(
            buffer,
            state,
            beatsAtBufferBegin,
            quantum,
            frameSize,
            channels,
            sampleRate,
        )
    }
}()
```

## Basic Scaffolding Template

Here is a basic `main.go` file that you can use as a starting point. This example creates a `Link` instance, enables audio, and sets up a `Sink` to send a sine wave. It also includes a placeholder for a `Source` to receive audio.

```go
package main

import (
    "fmt"
    "math"
    "os"
    "os/signal"
    "time"

    abletonlink "github.com/DatanoiseTV/abletonlink-go"
)

const (
    sampleRate = 48000
    channels   = 2
    quantum    = 4.0
    frameSize  = 512
)

func main() {
    // --- Basic Link Setup ---
    link := abletonlink.NewLink(120.0)
    defer link.Destroy()

    link.Enable(true)
    link.EnableAudio(true)

    fmt.Println("Ableton Link enabled with audio.")

    // --- Sending Audio (Sink) ---
    sink := link.NewSink("Go Sine Wave", frameSize)
    defer sink.Destroy()

    go func() {
        // This ticker simulates an audio callback
        ticker := time.NewTicker(time.Duration(frameSize*1000/sampleRate) * time.Millisecond)
        var phase float64

        for range ticker.C {
            // Retain a buffer from the sink
            buffer := sink.RetainBuffer()
            if buffer == nil {
                continue // No buffer available
            }

            // Fill the buffer with a sine wave
            for i := 0; i < frameSize; i++ {
                value := int16(math.Sin(phase) * 16000.0) // Lower volume
                buffer.Samples[i*channels] = value
                buffer.Samples[i*channels+1] = value
                phase += 2 * math.Pi * 440.0 / float64(sampleRate)
                if phase > 2*math.Pi {
                    phase -= 2 * math.Pi
                }
            }

            // Capture the session state for timestamping
            state := abletonlink.NewSessionState()
            link.CaptureAudioSessionState(state)

            // Calculate the beat time at the start of this buffer.
            // In a real audio app, you would have a more precise sample clock.
            beatsAtBufferBegin := state.BeatAtTime(link.ClockMicros(), quantum)

            // Commit the buffer to the Link network
            sink.Commit(
                buffer,
                state,
                beatsAtBufferBegin,
                quantum,
                uint64(frameSize),
                uint64(channels),
                uint32(sampleRate),
            )

            state.Destroy()
        }
    }()

    fmt.Println("Sending a sine wave on the 'Go Sine Wave' channel.")

    // --- Receiving Audio (Source) ---
    // This is a simplified example of how to set up a source.
    // In a real application, you would likely have a separate mechanism
    // for selecting and managing channels.
    go func() {
        var source *abletonlink.Source

        for {
            channels := link.Channels()
            if source == nil && len(channels) > 0 {
                // For this example, just grab the first available channel
                // that is not our own.
                for _, ch := range channels {
                    if ch.PeerName != "Go Sine Wave" {
                        fmt.Printf("Found channel: %s, creating source.
", ch.Name)
                        source = link.NewSource(ch.ID, func(samples []int16, info abletonlink.SourceBufferInfo) {
                            // Find the peak volume of the received audio
                            var peak int16
                            for _, sample := range samples {
                                if sample < 0 {
                                    sample = -sample
                                }
                                if sample > peak {
                                    peak = sample
                                }
                            }
                            fmt.Printf("Received audio from '%s' with peak level: %d      ", ch.Name, peak)
                        })
                        break
                    }
                }
            } else if source != nil && len(channels) == 0 {
                fmt.Println("
Source channel disappeared.")
                source.Destroy()
                source = nil
            }
            time.Sleep(1 * time.Second)
        }
    }()

    // --- Keep the application running ---
    fmt.Println("Press Ctrl+C to exit.")
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt)
    <-sigChan
}
```

This template should provide a solid foundation for building your own audio applications with `abletonlink-go`.