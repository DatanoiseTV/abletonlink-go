# Ableton Link Go Bindings

Professional Go bindings for [Ableton Link](https://github.com/Ableton/link), a technology that synchronizes musical tempo and timing across multiple applications running on one or more devices.

## Features

- Complete Go API coverage of Ableton Link functionality
- Automatic build system with included C library as git submodule
- Thread-safe operations with separate audio/application thread contexts
- Callback support for tempo, peer count, and transport state changes
- Cross-platform support (macOS, Linux, Windows)
- Static linking support for deployment

## Prerequisites

- Go 1.19 or later
- CMake 3.15 or later
- C++14 compatible compiler (GCC, Clang, or MSVC)
- Git (for submodules)

## Installation

### Clone with submodules

```bash
git clone --recursive https://github.com/DatanoiseTV/abletonlink-go.git
cd abletonlink-go
```

### If already cloned without --recursive

```bash
git clone https://github.com/DatanoiseTV/abletonlink-go.git
cd abletonlink-go
git submodule update --init --recursive
```

### Using as a Go module dependency

```bash
go get github.com/DatanoiseTV/abletonlink-go
```

## Building

### Quick Start

```bash
make          # Build everything
make build    # Build Go module only
make examples # Build examples
```

### Manual Build

```bash
./build.sh           # Build C library
go generate ./...    # Generate bindings
go build ./...       # Build Go module
```

### Static Builds

For deployments requiring static linking (Linux/Windows):

```bash
make build-static
# or
go build -tags static ./...
```

Note: Static linking is not supported on macOS due to system limitations.

## Quick Start

```go
package main

import (
    "fmt"
    "time"
    "github.com/DatanoiseTV/abletonlink-go"
)

func main() {
    // Create Link instance with 120 BPM
    link := abletonlink.NewLink(120.0)
    defer link.Destroy()

    // Enable Link networking
    link.Enable(true)
    defer link.Enable(false)

    // Set up callbacks
    link.SetNumPeersCallback(func(numPeers uint64) {
        fmt.Printf("Connected peers: %d\n", numPeers)
    })

    link.SetTempoCallback(func(tempo float64) {
        fmt.Printf("Tempo changed: %.2f BPM\n", tempo)
    })

    // Create session state for timeline manipulation
    state := abletonlink.NewSessionState()
    defer state.Destroy()

    // Main loop
    for {
        link.CaptureAppSessionState(state)
        
        currentTime := link.ClockMicros()
        beat := state.BeatAtTime(currentTime, 4.0)
        tempo := state.Tempo()
        
        fmt.Printf("Beat: %.2f, Tempo: %.2f BPM\n", beat, tempo)
        
        time.Sleep(1 * time.Second)
    }
}
```

## API Reference

### Link Instance Management

```go
// Create and destroy Link instances
link := abletonlink.NewLink(bpm float64) *Link
link.Destroy()

// Network control
link.Enable(enable bool)
link.IsEnabled() bool

// Peer information
link.NumPeers() uint64

// Clock access
link.ClockMicros() int64
```

### Session State Operations

```go
// Create session state
state := abletonlink.NewSessionState() *SessionState
state.Destroy()

// Capture/commit from different thread contexts
link.CaptureAudioSessionState(state)   // Audio thread only
link.CommitAudioSessionState(state)    // Audio thread only
link.CaptureAppSessionState(state)     // Any thread
link.CommitAppSessionState(state)      // Any thread
```

### Timeline Manipulation

```go
// Tempo operations
state.Tempo() float64
state.SetTempo(bpm float64, atTime int64)

// Beat/time conversions
state.BeatAtTime(time int64, quantum float64) float64
state.PhaseAtTime(time int64, quantum float64) float64
state.TimeAtBeat(beat float64, quantum float64) int64

// Beat mapping
state.RequestBeatAtTime(beat float64, time int64, quantum float64)
state.ForceBeatAtTime(beat float64, time uint64, quantum float64)
```

### Transport Control

```go
// Transport state
state.IsPlaying() bool
state.SetIsPlaying(isPlaying bool, time uint64)
state.TimeForIsPlaying() uint64

// Convenience functions
state.RequestBeatAtStartPlayingTime(beat float64, quantum float64)
state.SetIsPlayingAndRequestBeatAtTime(isPlaying bool, time uint64, beat float64, quantum float64)
```

### Callbacks

```go
// Set callback functions
link.SetNumPeersCallback(func(uint64))    // Peer count changes
link.SetTempoCallback(func(float64))      // Tempo changes
link.SetStartStopCallback(func(bool))     // Transport state changes
```

## Thread Safety

Ableton Link provides separate methods for different thread contexts:

- **Audio Thread**: Use `CaptureAudioSessionState` and `CommitAudioSessionState`
  - Realtime-safe, non-blocking
  - Should only be called from audio processing thread

- **Application Thread**: Use `CaptureAppSessionState` and `CommitAppSessionState`  
  - Thread-safe but may block
  - Safe to call from any non-audio thread

Do not mix audio and application thread methods concurrently.

## Examples

Complete examples are available in the `examples/` directory:

```bash
cd examples
go run basic_link.go
```

## Building from Source

The build system automatically handles the Ableton Link C library:

1. Downloads and builds Ableton Link as a git submodule
2. Configures CGO to link against the built library
3. Generates Go bindings automatically

### Build Targets

```bash
make deps         # Build C dependencies only
make generate     # Run go generate
make build        # Build Go module
make build-static # Build with static linking (Linux/Windows)
make examples     # Build examples
make test         # Run tests
make clean        # Clean build artifacts
make install      # Full build and install
```

## Platform Support

- **macOS**: Full support with dynamic linking
- **Linux**: Full support with dynamic and static linking  
- **Windows**: Full support with dynamic and static linking

## License

This project follows the same licensing terms as Ableton Link (GPL v2+).

For proprietary software integration, contact [link-devs@ableton.com](mailto:link-devs@ableton.com).

## Contributing

Contributions are welcome. Please ensure all tests pass and follow Go conventions.

## Support

For issues specific to these Go bindings, please file an issue on GitHub.
For Ableton Link questions, refer to the [official Link repository](https://github.com/Ableton/link).