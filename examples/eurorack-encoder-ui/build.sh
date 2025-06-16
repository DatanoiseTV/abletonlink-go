#!/bin/bash

# Build script for Eurorack Bridge

set -e

echo "Building Eurorack-Link Bridge..."

# Check if we're on a Raspberry Pi
if [[ $(uname -m) == "arm"* ]] || [[ $(uname -m) == "aarch64" ]]; then
    echo "Detected ARM architecture - building for Raspberry Pi"
    GOOS=linux GOARCH=arm64 go build -o eurorack_bridge .
else
    echo "Building for current architecture (dry-run mode available)"
    go build -o eurorack_bridge .
fi

echo "Build complete! Run with: ./eurorack_bridge"
echo ""
echo "Usage examples:"
echo "  ./eurorack_bridge -cui                    # Start with console UI"
echo "  ./eurorack_bridge -dry-run -cui          # Test without GPIO hardware"
echo "  ./eurorack_bridge -enable-external-sync  # External clock mode"
echo "  ./eurorack_bridge -rt                    # Real-time priority"
echo ""
echo "For hardware setup, see README.md for GPIO pin configurations."