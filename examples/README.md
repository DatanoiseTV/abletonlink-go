# Examples

This directory contains examples demonstrating various uses of the Ableton Link Go bindings.

## Examples

### basic_link.go
A simple example showing basic Link functionality including:
- Creating and managing Link instances
- Setting up callbacks for tempo and peer changes
- Capturing and modifying session state
- Transport control

### midi_bridge.go
A comprehensive MIDI-Link bridge that provides bidirectional synchronization between MIDI clock and Ableton Link:
- Creates virtual MIDI ports for integration with other applications
- Converts MIDI clock to Link tempo and vice versa
- Synchronizes transport state (start/stop/continue)
- Quantizes transport changes to bar boundaries
- Supports both MIDI → Link and Link → MIDI directions

## Running Examples

### Prerequisites

For the MIDI bridge example, you'll need:
- A MIDI-capable system (Core MIDI on macOS, ALSA on Linux)
- The gomidi library with rtmidi driver support

### Installation

```bash
cd examples
go mod tidy
```

### Basic Example

```bash
go run basic_link.go
```

### MIDI Bridge Example

```bash
go run midi_bridge.go
```

The MIDI bridge will create virtual MIDI ports:
- "Link-MIDI Bridge In" - for receiving MIDI clock and transport
- "Link-MIDI Bridge Out" - for sending MIDI clock and transport

## MIDI Bridge Usage

### Connecting MIDI Applications

1. **Start the bridge:**
   ```bash
   go run midi_bridge.go
   ```

2. **Connect MIDI applications:**
   - Set your DAW/sequencer to send MIDI clock to "Link-MIDI Bridge In"
   - Set applications to receive MIDI clock from "Link-MIDI Bridge Out"

3. **The bridge will automatically:**
   - Sync incoming MIDI tempo to Ableton Link
   - Send MIDI clock based on Link tempo
   - Synchronize transport state bidirectionally
   - Quantize transport changes to bar boundaries

### Features

#### Bidirectional Tempo Sync
- **MIDI → Link**: Analyzes incoming MIDI clock timing to detect tempo changes
- **Link → MIDI**: Generates MIDI clock at Link's current tempo
- Configurable tempo tolerance to avoid jitter

#### Transport Synchronization
- **MIDI Start** → Link transport start (quantized to next bar)
- **MIDI Stop** → Link transport stop (immediate)
- **MIDI Continue** → Link transport continue
- **Link Start/Stop** → Corresponding MIDI messages

#### Quantization
- Transport start commands are quantized to bar boundaries
- Configurable beats per bar (default: 4)
- Helps maintain musical timing across applications

#### Virtual MIDI Ports
- Creates virtual MIDI input and output ports
- Other applications can connect as if bridge were hardware
- Cross-platform support (macOS, Linux)

### Configuration

Key parameters can be modified in the source:

```go
const (
    midiClocksPerQuarterNote = 24     // Standard MIDI clock resolution
    virtualPortName = "Link-MIDI Bridge"  // MIDI port name
    tempoTolerance = 0.1              // BPM tolerance for changes
    defaultQuantum = 4.0              // Quarter note quantum
)

// In NewMIDILinkBridge:
bridge.quantizeToBar = true          // Enable bar quantization
bridge.beatsPerBar = 4               // 4/4 time signature
```

### Use Cases

1. **DAW Synchronization**: Sync multiple DAWs via Link while maintaining MIDI clock compatibility
2. **Hardware Integration**: Connect hardware sequencers/drum machines via MIDI to Link network
3. **Legacy Support**: Bridge older MIDI-only applications to modern Link-enabled software
4. **Live Performance**: Maintain sync between Link-enabled apps and MIDI hardware

### Troubleshooting

#### No Virtual Ports Created
- Ensure you have appropriate MIDI drivers installed
- On Linux, ensure ALSA is properly configured
- Check that no other application is using the same port name

#### Tempo Drift
- Adjust `tempoTolerance` if tempo changes too frequently
- Check MIDI clock stability from source application
- Verify system audio latency settings

#### Transport Sync Issues
- Ensure both applications support the transport messages being sent
- Check quantization settings if timing seems off
- Verify that Link start/stop sync is enabled in connected applications

### Platform Support

- **macOS**: Full support via Core MIDI
- **Linux**: Full support via ALSA
- **Windows**: Limited virtual port support (consider loopMIDI)

### Dependencies

The MIDI bridge requires:
- `gitlab.com/gomidi/midi/v2` - MIDI library
- rtmidi driver (automatically included)
- Platform MIDI support (Core MIDI, ALSA, etc.)

Install with:
```bash
go get gitlab.com/gomidi/midi/v2
```