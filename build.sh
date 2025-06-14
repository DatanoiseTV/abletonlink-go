#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LINK_DIR="$SCRIPT_DIR/vendor/link"
BUILD_DIR="$LINK_DIR/build"

echo "Building Ableton Link..."

# Check if submodule is initialized
if [ ! -f "$LINK_DIR/CMakeLists.txt" ]; then
    echo "Initializing git submodules..."
    cd "$SCRIPT_DIR"
    git submodule update --init --recursive
fi

# Initialize Link's own submodules (asio-standalone)
echo "Initializing Link submodules..."
cd "$LINK_DIR"
git submodule update --init --recursive

# Create build directory
mkdir -p "$BUILD_DIR"

# Configure and build
cd "$BUILD_DIR"

# Configure with CMake
cmake .. \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
    -DLINK_BUILD_JACK=OFF \
    -DLINK_BUILD_VCV_RACK_PLUGIN=OFF

# Build the abl_link C extension
cmake --build . --target abl_link --parallel

echo "Ableton Link built successfully at: $BUILD_DIR"
echo "Library location: $BUILD_DIR/extensions/abl_link/libabl_link.a"