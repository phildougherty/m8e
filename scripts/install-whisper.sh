#!/bin/bash

# Whisper Installation Script for Voice Features
# This script installs the correct OpenAI Whisper CLI tool

echo "🗣️  Installing OpenAI Whisper for Voice Features"
echo "=============================================="
echo ""

# Check if pipx is available
if ! command -v pipx &> /dev/null; then
    echo "📦 Installing pipx first..."
    if command -v brew &> /dev/null; then
        # macOS with Homebrew
        brew install pipx
        pipx ensurepath
    elif command -v apt &> /dev/null; then
        # Ubuntu/Debian
        sudo apt update && sudo apt install -y pipx
        pipx ensurepath
    else
        echo "❌ Please install pipx manually first"
        echo "   macOS: brew install pipx"
        echo "   Ubuntu: apt install pipx"
        exit 1
    fi
fi

echo "✅ pipx is available"
echo ""

# Remove any incorrect installations
echo "🧹 Cleaning up any incorrect whisper installations..."
pipx uninstall faster-whisper 2>/dev/null || true
pipx uninstall whisper 2>/dev/null || true

# Install correct OpenAI Whisper
echo "📦 Installing OpenAI Whisper CLI..."
pipx install openai-whisper

echo ""
echo "🔍 Verifying installation..."

# Test whisper command
if command -v whisper &> /dev/null; then
    echo "✅ Whisper CLI installed successfully"
    whisper --help | head -5
    echo ""
    echo "✅ Installation complete!"
    echo ""
    echo "🎤 You can now use voice features:"
    echo "   1. Build with voice: make build-voice"
    echo "   2. Set environment: export VOICE_ENABLED=true"
    echo "   3. Start chat: ./bin/matey chat"
    echo "   4. Enable voice: Ctrl+V"
    echo "   5. Test with: /voice-check"
else
    echo "❌ Installation failed. Whisper command not found in PATH"
    echo "💡 Try adding ~/.local/bin to your PATH:"
    echo "   echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.bashrc"
    echo "   source ~/.bashrc"
    exit 1
fi