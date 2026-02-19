#!/bin/bash

# Build script for Icecast Stream example

set -e

echo "Building Icecast Streamer..."

# Build the example
echo "Building..."
go build -o icecast_stream .

echo "Build complete! Run with: ./icecast_stream"
