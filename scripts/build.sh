#!/bin/bash
# Build script for matey

set -e

# Get the directory of this script
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$DIR/.."

# Ensure all dependencies are downloaded
go mod download

# Build for the current platform
echo "Building matey..."
go build -o bin/matey ./cmd/matey

echo "Build complete. Binary available at bin/matey"
echo "To install locally: sudo cp bin/matey /usr/local/bin/"
