#!/bin/bash

# Voice System Debug Test Script
# This script helps test voice key bindings and diagnose terminal compatibility issues

echo "🎤 Voice System Debug Test"
echo "========================="
echo ""

# Check if voice-enabled build exists
if [ ! -f "./bin/matey" ]; then
    echo "❌ Voice-enabled build not found. Run: make build-voice"
    exit 1
fi

# Set up environment for testing
export VOICE_ENABLED=true
export TTS_ENDPOINT=http://${TTS_HOST:-localhost}:8000/v1/audio/speech

echo "✅ Environment configured:"
echo "   VOICE_ENABLED: $VOICE_ENABLED"
echo "   TTS_ENDPOINT: $TTS_ENDPOINT"
echo ""

echo "📋 Testing Instructions:"
echo "   1. This will start matey chat with enhanced key debugging"
echo "   2. Press Ctrl+V to enable voice mode"
echo "   3. Try these keys and watch for debug messages:"
echo "      - F1 (most likely to work)"
echo "      - Ctrl+T (good alternative)"
echo "      - Ctrl+M (may work)"
echo "      - Ctrl+Space (least likely)"
echo "   4. All key presses will be logged when voice mode is active"
echo "   5. Exit with Ctrl+C when done"
echo ""

echo "🚀 Starting matey chat with debug mode..."
echo "   (If this hangs, check if you're in a compatible terminal)"
echo ""

# Start matey chat
./bin/matey chat