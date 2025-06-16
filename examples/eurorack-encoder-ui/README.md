# Eurorack-Link Bridge with OLED Display

A bidirectional bridge between Eurorack modular synthesizers and Ableton Link, featuring an optional hardware UI with 128x64 OLED display and rotary encoder control. Uses Raspberry Pi GPIO for clock and transport synchronization.

## Features

- **Bidirectional sync**: Link can be master or slave to external Eurorack clock
- **Multiple clock divisions**: 1, 2, 4, and 24 PPQN outputs
- **Transport sync**: Start, stop, and reset triggers
- **OLED Hardware UI**: 128x64 SSD1306 display with rotary encoder interface
- **Custom clock output**: Configurable dividers (1/32 to 24 PPQN) with dedicated GPIO output
- **Real-time controls**: Adjust tempo, view peers, monitor status via hardware interface
- **Console UI**: Optional text-based interface with keyboard controls
- **Real-time performance**: Optional real-time priority for precise timing
- **Eurorack compatible**: 5V tolerant inputs, proper trigger pulse widths
- **Static builds**: Self-contained binaries for easy deployment

## GPIO Pin Configuration

### Default Pin Assignment (BCM numbering)

**Eurorack I/O:**
- GPIO 2: External clock input (conflicts with I2C - see note below)
- GPIO 3: Start trigger input (conflicts with I2C - see note below)
- GPIO 4: Stop trigger input
- GPIO 17: Reset trigger input
- GPIO 18 (PWM0): 1 PPQN clock output
- GPIO 19 (PWM1): 2 PPQN clock output
- GPIO 20: 4 PPQN clock output
- GPIO 21: 24 PPQN clock output (MIDI clock equivalent)
- GPIO 22: Start trigger output
- GPIO 23: Stop trigger output
- GPIO 24: Reset trigger output

**OLED Display (I2C):**
- GPIO 2 (SDA): I2C data line for OLED
- GPIO 3 (SCL): I2C clock line for OLED

**Encoder & Controls:**
- GPIO 25: Rotary encoder A pin
- GPIO 26: Rotary encoder B pin
- GPIO 27: Rotary encoder button
- GPIO 5: Back button
- GPIO 6: Enter button
- GPIO 13: Custom clock output (configurable divider)

> **Note**: GPIO 2 and 3 are shared between I2C (for OLED) and Eurorack inputs. 
> When using OLED mode, these pins cannot be used for external clock/start inputs.
> Consider using GPIO 4 and 17 for essential triggers, or use a custom pin configuration.

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
# Start basic bridge (no UI)
./eurorack_bridge

# OLED display with encoder interface
./eurorack_bridge -oled

# Console text UI with keyboard controls
./eurorack_bridge -cui

# OLED mode with real-time priority
./eurorack_bridge -oled -rt

# External sync mode (GPIO clock controls Link)
./eurorack_bridge -enable-external-sync -oled

# Custom initial tempo
./eurorack_bridge -tempo 140 -oled

# Test without GPIO hardware
./eurorack_bridge -dry-run -oled

# Show detailed help
./eurorack_bridge -help
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
# Quick build
go build -o eurorack_bridge .

# Use build script (recommended)
./build.sh

# Cross-compile for Raspberry Pi from another machine
GOOS=linux GOARCH=arm64 go build -o eurorack_bridge .
```

The build script automatically detects Raspberry Pi architecture. Note that static builds are not possible due to the CGO dependency on the Ableton Link C++ library.

### Running as Service

Create a systemd service for automatic startup:

```ini
# /etc/systemd/system/eurorack-link.service
[Unit]
Description=Eurorack Link Bridge with OLED
After=network.target

[Service]
Type=simple
User=pi
WorkingDirectory=/home/pi/eurorack-link
ExecStart=/home/pi/eurorack-link/eurorack_bridge -oled -rt
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Enable and start the service:
```bash
sudo systemctl enable eurorack-link
sudo systemctl start eurorack-link
sudo systemctl status eurorack-link
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
- Use `-rt` flag for better timing precision
- Check system load and background processes
- Verify stable power supply
- Consider using external hardware clock source

### OLED Display Issues
```bash
# Enable I2C interface
sudo raspi-config
# Interface Options → I2C → Enable

# Check I2C devices
i2cdetect -y 1

# Verify OLED connection (should show device at 0x3C or 0x3D)
# Common addresses: 0x3C (60), 0x3D (61)

# Test with verbose logging
./eurorack_bridge -oled -dry-run

# Check I2C permissions
ls -la /dev/i2c-*
```

The OLED initialization includes enhanced debugging that will:
- Try multiple I2C bus numbers automatically
- Scan for available I2C devices
- Report detailed error information
- Test multiple common SSD1306 addresses

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