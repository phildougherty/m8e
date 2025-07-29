#!/bin/bash

# macOS-specific voice setup script for m8e
# This script sets optimal voice settings for macOS systems

echo "🍎 Setting up voice features for macOS..."

# Export macOS-optimized voice settings
export VOICE_ENABLED=true
export VOICE_SAMPLE_RATE=44100           # Higher sample rate works better on macOS
export VOICE_FRAME_LENGTH=2048           # Larger frame size for better detection
export VOICE_ENERGY_THRESHOLD=50        # Lower threshold for sensitive detection
export VOICE_SPEECH_TIMEOUT=3.0         # Longer timeout for comfortable speaking
export VOICE_WAKE_WORD=matey             # Clear wake word
export VOICE_POST_WAKEWORD_DELAY=0.3     # Shorter delay for responsiveness

# TTS settings (user should customize)
export TTS_ENDPOINT=http://localhost:8000/v1/audio/speech
export TTS_VOICE=alloy
export TTS_MODEL=tts-1

# Whisper settings
export WHISPER_MODEL=base

echo "✅ macOS voice settings configured:"
echo "   Sample Rate: $VOICE_SAMPLE_RATE Hz"
echo "   Energy Threshold: $VOICE_ENERGY_THRESHOLD"
echo "   Frame Length: $VOICE_FRAME_LENGTH"
echo "   Wake Word: '$VOICE_WAKE_WORD'"
echo ""
echo "🎤 Voice Controls:"
echo "   Ctrl+V      - Toggle voice mode"
echo "   Ctrl+Space  - Push-to-talk recording"
echo "   Ctrl+M      - Manual voice trigger (bypass wake word)"
echo ""
echo "💡 Tips for macOS:"
echo "   - Grant microphone permission to Terminal in System Preferences"
echo "   - Use Ctrl+M for manual voice trigger if wake word detection is unreliable"
echo "   - Adjust VOICE_ENERGY_THRESHOLD if too sensitive/insensitive"
echo ""
echo "🚀 Ready to run:"
echo "   source $0  # to apply settings to current shell"
echo "   ./bin/matey chat"