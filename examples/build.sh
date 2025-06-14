#!/bin/bash

# Build script for MIDI-Link Bridge example
# 
# Requirements:
# - rtmidi library (install with: brew install rtmidi on macOS)
# - Ableton Link (built automatically from submodule)

set -e

echo "Building MIDI-Link Bridge..."
echo "Checking dependencies..."

# Check for rtmidi
if ! pkg-config --exists rtmidi; then
    echo "Error: rtmidi not found. Please install:"
    echo "  macOS: brew install rtmidi"
    echo "  Ubuntu: sudo apt-get install librtmidi-dev"
    echo "  Other: https://github.com/thestk/rtmidi"
    exit 1
fi

echo "rtmidi found: $(pkg-config --modversion rtmidi)"

# Build the example
echo "Building..."
go build -o midi_bridge midi_bridge.go

echo "Build complete! Run with: ./midi_bridge"