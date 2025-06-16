#!/bin/bash

# Build script for Eurorack Bridge with OLED UI

set -e

echo "Building Eurorack-Link Bridge with OLED UI..."

# Check if we're on a Raspberry Pi
if [[ $(uname -m) == "arm"* ]] || [[ $(uname -m) == "aarch64" ]]; then
    echo "Detected ARM architecture - building for Raspberry Pi"
    # Note: Cannot use CGO_ENABLED=0 because abletonlink-go requires CGO for C++ Link library
    GOOS=linux GOARCH=arm64 go build -o eurorack_bridge .
else
    echo "Building for current architecture (dry-run mode available)"
    go build -o eurorack_bridge .
fi

echo "Build complete! Run with: ./eurorack_bridge"
echo ""
echo "Usage examples:"
echo "  ./eurorack_bridge                          # Basic mode"
echo "  ./eurorack_bridge -oled                    # OLED display with encoder UI"
echo "  ./eurorack_bridge -cui                     # Console text UI"
echo "  ./eurorack_bridge -oled -rt                # OLED mode with real-time priority"
echo "  ./eurorack_bridge -dry-run -oled           # Test OLED mode without GPIO hardware"
echo "  ./eurorack_bridge -enable-external-sync    # External clock controls Link tempo"
echo "  ./eurorack_bridge -tempo 140               # Set initial tempo"
echo "  ./eurorack_bridge -config config.json      # Use custom GPIO configuration"
echo ""
echo "OLED Mode Features:"
echo "  - 128x64 SSD1306 OLED display"
echo "  - Rotary encoder navigation"
echo "  - Hardware buttons (Back/Enter)"
echo "  - Custom clock output with configurable dividers"
echo "  - Real-time tempo, peer count, and status display"
echo ""
echo "For hardware setup, see README.md and README-OLED.md for pin configurations."