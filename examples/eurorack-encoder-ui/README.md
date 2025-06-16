# Eurorack-Link Bridge

A bridge between Eurorack modular synthesizers and Ableton Link, using Raspberry Pi GPIO for clock and transport synchronization.

## Features

- **Bidirectional sync**: Link can be master or slave to external Eurorack clock
- **Multiple clock divisions**: 1, 2, 4, and 24 PPQN outputs
- **Transport sync**: Start, stop, and reset triggers
- **Real-time performance**: Optional real-time priority for precise timing
- **Console UI**: Live monitoring of GPIO activity and Link session
- **Eurorack compatible**: 5V tolerant inputs, proper trigger pulse widths

## GPIO Pin Configuration

### Default Pin Assignment (BCM numbering)

**Inputs:**
- GPIO 2 (SDA): External clock input
- GPIO 3 (SCL): Start trigger input  
- GPIO 4: Stop trigger input
- GPIO 17: Reset trigger input

**Outputs:**
- GPIO 18 (PWM0): 1 PPQN clock output
- GPIO 19 (PWM1): 2 PPQN clock output
- GPIO 20: 4 PPQN clock output
- GPIO 21: 24 PPQN clock output (MIDI clock equivalent)
- GPIO 22: Start trigger output
- GPIO 23: Stop trigger output
- GPIO 24: Reset trigger output

### Hardware Requirements

**Input Protection:**
- Use voltage dividers or level shifters for Eurorack levels (±12V to 3.3V)
- Add input protection diodes
- Pull-down resistors (10kΩ) are configured in software

**Output Buffering:**
- Use buffer circuits to convert 3.3V to Eurorack levels (5V/12V)
- Consider using 74HC series buffers or dedicated Eurorack interface modules
- Ensure adequate current drive for connected modules

**Power Supply:**
- Clean, stable power supply for Raspberry Pi
- Consider linear regulators for audio applications
- Proper grounding between Eurorack and Pi

## Usage

### Basic Operation

```bash
# Start with default settings
./eurorack_bridge

# Enable external sync mode (Eurorack controls Link)
./eurorack_bridge -enable-external-sync

# Start with custom tempo
./eurorack_bridge -tempo 140

# Enable console UI for monitoring
./eurorack_bridge -cui

# Enable real-time priority (requires privileges)
./eurorack_bridge -rt

# Dry run mode (simulate without GPIO hardware)
./eurorack_bridge -dry-run -cui
```

### Configuration

Pin assignments can be customized using a JSON configuration file:

```json
{
  "gpio_pins": {
    "ClockIn": 2,
    "StartIn": 3,
    "StopIn": 4,
    "ResetIn": 17,
    "Clock1PPQN": 18,
    "Clock2PPQN": 19,
    "Clock4PPQN": 20,
    "Clock24PPQN": 21,
    "StartOut": 22,
    "StopOut": 23,
    "ResetOut": 24
  },
  "external_sync_enabled": false,
  "quantize_to_bar": false,
  "beats_per_bar": 4,
  "initial_tempo": 120.0
}
```

Use with: `./eurorack_bridge -config config.json`

## Operation Modes

### Link Master Mode (Default)
- Link session controls the timing
- GPIO outputs sync to Link timeline
- External triggers affect Link transport state
- Multiple devices can join the Link session

### External Sync Mode
- External clock input controls Link tempo
- Link session follows external timing
- Useful for syncing to hardware sequencers
- Clock input expects 24 PPQN (like MIDI clock)

## Console UI

The console UI (`-cui` flag) provides real-time monitoring:

- **System Status**: Sync mode, transport state, tempo
- **Clock & Timing**: Beat visualization, bar counter, phase
- **GPIO Status**: Pin assignments and activity indicators  
- **Link Network**: Connected peers, session status
- **Log Messages**: Real-time event logging

### Keyboard Controls
- **Space**: Toggle Link transport start/stop
- **R**: Send reset pulse and return to beat 0
- **H**: Show/hide help
- **Q**: Quit application

## Timing Specifications

- **Clock Resolution**: 1ms internal timing resolution
- **Pulse Width**: 10ms trigger pulses (Eurorack standard)
- **Input Threshold**: 2V rising edge detection
- **Output Levels**: 3.3V (requires external buffering for Eurorack)
- **Latency**: Sub-millisecond with real-time priority

## Installation

### Prerequisites

```bash
# Install Go dependencies
go mod tidy

# Enable GPIO access (add user to gpio group)
sudo usermod -a -G gpio $USER

# For real-time priority support
sudo usermod -a -G audio $USER
```

### Build

```bash
go build -o eurorack_bridge .
```

### Running as Service

Create a systemd service for automatic startup:

```ini
[Unit]
Description=Eurorack Link Bridge
After=network.target

[Service]
Type=simple
User=pi
WorkingDirectory=/home/pi/eurorack-link
ExecStart=/home/pi/eurorack-link/eurorack_bridge -rt
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

## Safety Notes

- **Voltage Protection**: Eurorack operates at ±12V - use proper level shifting
- **Current Limiting**: GPIO pins have limited current drive capability
- **Ground Loops**: Ensure proper grounding between systems
- **ESD Protection**: Use ESD-safe handling procedures
- **Power Sequencing**: Power on Raspberry Pi before connecting to Eurorack

## Troubleshooting

### GPIO Permission Issues
```bash
# Check GPIO group membership
groups $USER

# If not in gpio group:
sudo usermod -a -G gpio $USER
# Then log out and back in
```

### Real-time Priority Issues
```bash
# Check audio group membership  
groups $USER

# Configure limits (add to /etc/security/limits.conf):
@audio - rtprio 99
@audio - memlock unlimited
```

### Clock Timing Issues
- Use `--rt` flag for better timing precision
- Check system load and background processes
- Verify stable power supply
- Consider using external hardware clock source

## Hardware Interface Examples

### Simple Level Shifter (Input)
```
Eurorack Signal (±12V) → Voltage Divider → RPi GPIO (3.3V)
```

### Buffer Circuit (Output)  
```
RPi GPIO (3.3V) → 74HC14 Buffer → Eurorack Level (5V)
```

### Commercial Solutions
- Expert Sleepers FH-2 (with custom firmware)
- Polyend Poly 2 (with CV expansion)
- DIY PCBs with proper level conversion

## License

This project is part of the abletonlink-go library and follows the same license terms.