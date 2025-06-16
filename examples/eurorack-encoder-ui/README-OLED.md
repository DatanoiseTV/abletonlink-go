# Eurorack-Link Bridge with OLED Display

An enhanced version of the Eurorack-Link Bridge featuring a 128x64 OLED display (SSD1306) and rotary encoder interface for hardware control without needing a computer terminal.

## Features

- **OLED Display**: 128x64 pixel SSD1306 display via I2C
- **Rotary Encoder**: Navigation with A/B pins and integrated button
- **Hardware Buttons**: Dedicated Back and Enter buttons
- **Custom Clock Output**: Configurable clock divider with 11 preset options
- **Real-time Control**: Adjust tempo, view peers, check status
- **All Original Features**: Maintains full functionality of the base Eurorack bridge

## Hardware Requirements

### OLED Display (SSD1306)
- **Connection**: I2C bus (SDA/SCL)
- **Resolution**: 128x64 pixels
- **Address**: Default I2C address (usually 0x3C or 0x3D)

### Rotary Encoder
- **Type**: Quadrature encoder with integrated push button
- **Pins**: A, B, and Button (with pull-up resistors)

### Additional GPIO Pins

**Encoder & Controls:**
- GPIO 25: Encoder A pin
- GPIO 26: Encoder B pin  
- GPIO 27: Encoder button
- GPIO 5: Back button
- GPIO 6: Enter button
- GPIO 13: Custom clock output

**Original Bridge Pins:**
- All original GPIO assignments remain the same

### Wiring Diagram

```
OLED Display (SSD1306):
  VCC  → 3.3V
  GND  → GND
  SDA  → GPIO 2 (I2C SDA)
  SCL  → GPIO 3 (I2C SCL)

Rotary Encoder:
  A    → GPIO 25 (with pull-up)
  B    → GPIO 26 (with pull-up)
  SW   → GPIO 27 (with pull-up)
  VCC  → 3.3V
  GND  → GND

Buttons:
  Back   → GPIO 5 (pull-up to 3.3V)
  Enter  → GPIO 6 (pull-up to 3.3V)

Custom Output:
  Clock  → GPIO 13 (buffered to Eurorack levels)
```

## Menu System

### Main Menu
1. **Tempo** - Adjust Link session tempo (60-200 BPM)
2. **Custom Clock** - Configure custom output divider
3. **Peers** - View connected Link devices
4. **Status** - System status and transport state
5. **Settings** - Configuration options

### Custom Clock Dividers
- 24 PPQN (MIDI clock equivalent)
- 16 PPQN
- 8 PPQN  
- 4 PPQN
- 2 PPQN
- 1 PPQN (quarter notes)
- 1/2 Note (half notes)
- 1/8 Note (eighth notes)
- 1/16 Note (sixteenth notes)
- 1/32 Note (thirty-second notes)

## Controls

### Rotary Encoder
- **Rotate**: Navigate menus or adjust values
- **Press**: Enter submenu or edit mode
- **Press (in edit)**: Save changes

### Hardware Buttons
- **Back**: Return to previous menu or cancel edit
- **Enter**: Same as encoder press (alternative)

### Display Navigation
- Selected items are highlighted with `>` prefix
- Edit mode shows current value with save prompt
- Real-time updates for tempo, peers, and status

## Usage

### Basic Operation
```bash
# Start with OLED interface
./eurorack_bridge -oled

# OLED with real-time priority
./eurorack_bridge -oled -rt

# OLED with external sync mode
./eurorack_bridge -oled -enable-external-sync

# Test without hardware (dry-run)
./eurorack_bridge -oled -dry-run
```

### Menu Navigation
1. Power on - main menu appears
2. Rotate encoder to navigate
3. Press encoder to select
4. Use Back button to return
5. Settings are saved automatically

### Tempo Adjustment
1. Select "Tempo" from main menu
2. Press encoder to enter edit mode
3. Rotate to adjust BPM (60-200)
4. Press encoder to save

### Custom Clock Setup
1. Select "Custom Clock" from main menu
2. Rotate to choose divider type
3. Press to confirm selection
4. Output appears on GPIO 13

## Dependencies

### Go Modules
```go
periph.io/x/conn/v3      // I2C communication
periph.io/x/devices/v3   // SSD1306 driver
periph.io/x/host/v3      // Hardware initialization
```

### System Requirements
- Raspberry Pi with I2C enabled
- Go 1.21 or later
- GPIO access permissions

### Enable I2C
```bash
# Enable I2C interface
sudo raspi-config
# Interface Options → I2C → Enable

# Verify I2C devices
i2cdetect -y 1
```

## Build Instructions

```bash
# Install dependencies
go mod tidy

# Build for Raspberry Pi
go build -o eurorack_bridge .

# Or use build script
chmod +x build.sh
./build.sh
```

## Installation as Service

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

```bash
sudo systemctl enable eurorack-link
sudo systemctl start eurorack-link
```

## Troubleshooting

### OLED Display Issues
```bash
# Check I2C connection
i2cdetect -y 1

# Verify display address (usually 0x3C)
# Check wiring: SDA→GPIO2, SCL→GPIO3
```

### Encoder Problems
```bash
# Check pull-up resistors on A, B, SW pins
# Verify GPIO permissions
# Test with multimeter for proper voltage levels
```

### GPIO Permissions
```bash
# Add user to gpio group
sudo usermod -a -G gpio $USER
# Log out and back in
```

## Circuit Protection

- **Input Protection**: Use voltage dividers for Eurorack signals
- **Output Buffering**: Buffer 3.3V outputs to Eurorack levels
- **Pull-up Resistors**: 10kΩ on encoder and button pins
- **Decoupling**: Add 100nF capacitors near ICs

## Performance Notes

- Display updates at 10 FPS (100ms refresh)
- Encoder polling at 1ms for responsive control
- Hardware interrupts for precise clock timing
- Real-time priority recommended for stable operation

This enhanced version provides a complete standalone Eurorack interface without requiring a connected computer for operation.