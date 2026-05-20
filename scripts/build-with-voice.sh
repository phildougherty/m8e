#!/bin/bash

# Build script for m8e with voice features enabled
# This script ensures PortAudio is installed and builds with voice support

set -e

echo "🎤 Building m8e with voice features..."

# Check for PortAudio installation
echo "📦 Checking PortAudio installation..."

# Function to check if portaudio is installed
check_portaudio() {
    if command -v pkg-config >/dev/null 2>&1; then
        if pkg-config --exists portaudio-2.0; then
            echo "✅ PortAudio found: $(pkg-config --modversion portaudio-2.0)"
            return 0
        fi
    fi
    return 1
}

# Function to install PortAudio on different systems
install_portaudio() {
    echo "⚠️  PortAudio not found. Attempting to install..."
    
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        # Linux
        if command -v apt-get >/dev/null 2>&1; then
            echo "🐧 Installing on Ubuntu/Debian..."
            sudo apt-get update
            sudo apt-get install -y portaudio19-dev pkg-config
        elif command -v yum >/dev/null 2>&1; then
            echo "🐧 Installing on CentOS/RHEL..."
            sudo yum install -y portaudio-devel pkgconfig
        elif command -v dnf >/dev/null 2>&1; then
            echo "🐧 Installing on Fedora..."
            sudo dnf install -y portaudio-devel pkgconf
        else
            echo "❌ Unsupported Linux distribution. Please install portaudio19-dev manually."
            exit 1
        fi
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        if command -v brew >/dev/null 2>&1; then
            echo "🍎 Installing on macOS with Homebrew..."
            brew install portaudio pkg-config
        else
            echo "❌ Homebrew not found. Please install Homebrew and portaudio manually."
            echo "   brew install portaudio pkg-config"
            exit 1
        fi
    else
        echo "❌ Unsupported operating system: $OSTYPE"
        exit 1
    fi
}

# Check and install PortAudio if needed
if ! check_portaudio; then
    install_portaudio
    
    # Verify installation
    if ! check_portaudio; then
        echo "❌ PortAudio installation failed or not detected."
        exit 1
    fi
fi

echo "🔊 Installing audio tools..."

# Install audio tools for playback
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    if command -v apt-get >/dev/null 2>&1; then
        sudo apt-get install -y mpg123 alsa-utils
    elif command -v yum >/dev/null 2>&1; then
        sudo yum install -y mpg123 alsa-utils
    elif command -v dnf >/dev/null 2>&1; then
        sudo dnf install -y mpg123 alsa-utils
    fi
elif [[ "$OSTYPE" == "darwin"* ]]; then
    if command -v brew >/dev/null 2>&1; then
        brew install mpg123
    fi
fi

echo "🗣️  Checking for Whisper..."

# Check for Whisper installation
if command -v whisper >/dev/null 2>&1; then
    echo "✅ Whisper found: $(whisper --version 2>/dev/null || echo 'installed')"
elif command -v faster-whisper >/dev/null 2>&1; then
    echo "✅ Faster Whisper found"
else
    echo "⚠️  Whisper not found. Install with:"
    echo "   pip install openai-whisper"
    echo "   # or"
    echo "   pip install faster-whisper"
fi

echo "🔧 Building m8e with voice support..."

# Update Go dependencies
echo "📦 Updating Go dependencies..."
go mod tidy

# Build with voice tag
echo "🛠️  Compiling with voice features..."
go build -tags voice -o matey ./cmd/matey

if [ $? -eq 0 ]; then
    echo "✅ Build successful! m8e compiled with voice features."
    echo ""
    echo "🎯 Usage:"
    echo "   export VOICE_ENABLED=true"
    echo "   export TTS_ENDPOINT=http://localhost:8000/v1/audio/speech"
    echo "   ./matey chat"
    echo ""
    echo "📖 For more information, see VOICE_INTEGRATION.md"
else
    echo "❌ Build failed!"
    exit 1
fi