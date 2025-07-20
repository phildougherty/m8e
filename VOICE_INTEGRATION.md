# Voice Integration for m8e Chat UI

This document describes the voice integration features added to the m8e chat UI system, providing wakeword detection, speech-to-text, and configurable TTS capabilities.

## Features

### 🎤 Wakeword Detection
- Configurable wake word (default: "matey")
- Real-time audio monitoring using PortAudio
- Energy-based detection with configurable thresholds
- Visual feedback in the chat UI when wake word is detected

### 🗣️ Speech-to-Text
- Whisper-based transcription using external whisper command
- Support for faster-whisper as fallback
- Configurable model selection (base, small, medium, large)
- Voice Activity Detection (VAD) for automatic recording end
- Real-time transcription processing

### 🔊 Text-to-Speech
- Configurable TTS endpoint (OpenAI-compatible API)
- Environment variable configuration for endpoint, voice, and model
- Automatic playback of AI responses
- Support for multiple audio players (mpg123, aplay, ffplay)

### 🎛️ UI Integration
- Seamless integration with existing BubbleTea UI
- Keyboard shortcuts for voice control (Ctrl+V to toggle)
- Push-to-talk mode (Spacebar when voice enabled)
- Visual indicators for voice status and activity
- Real-time feedback for recording and transcription

## Environment Variables

```bash
# Voice System Enable/Disable
VOICE_ENABLED=true                    # Enable voice features (default: false)

# Wake Word Configuration
VOICE_WAKE_WORD=matey                 # Wake word to detect (default: "matey")

# TTS Configuration
TTS_ENDPOINT=http://localhost:8000/v1/audio/speech  # TTS API endpoint
TTS_VOICE=alloy                       # Voice to use (default: "alloy")
TTS_MODEL=tts-1                       # TTS model (default: "tts-1")

# Speech Recognition
WHISPER_MODEL=base                    # Whisper model (base, small, medium, large)

# Audio Settings
VOICE_SAMPLE_RATE=16000               # Audio sample rate (default: 16000)
VOICE_FRAME_LENGTH=1024               # Audio frame length (default: 1024)
VOICE_ENERGY_THRESHOLD=300            # Voice detection threshold (default: 300)
VOICE_SPEECH_TIMEOUT=2.0              # Silence timeout in seconds (default: 2.0)
VOICE_MAX_RECORDING_SECONDS=30        # Max recording duration (default: 30)
VOICE_WAKE_WORD_EXCLUSION_TIME=1.0    # Time to exclude after wake word (default: 1.0)
VOICE_POST_WAKEWORD_DELAY=0.5         # Delay after wake word detection (default: 0.5)
```

## Installation & Dependencies

### System Dependencies

#### Ubuntu/Debian
```bash
sudo apt update
sudo apt install -y portaudio19-dev alsa-utils mpg123 whisper
```

#### macOS
```bash
brew install portaudio mpg123 openai-whisper
```

#### Go Dependencies
The voice integration requires the PortAudio Go binding:
```bash
go get github.com/gordonklaus/portaudio
```

### Whisper Installation
For speech-to-text functionality, install Whisper:

```bash
# Option 1: OpenAI Whisper (Python)
pip install openai-whisper

# Option 2: Faster Whisper (recommended for performance)
pip install faster-whisper

# Option 3: Whisper.cpp (C++ implementation)
git clone https://github.com/ggerganov/whisper.cpp.git
cd whisper.cpp
make
```

### TTS Service
Set up a compatible TTS service:

#### Option 1: Local TTS with Kokoro or similar
```bash
# Example with local TTS service running on port 8000
export TTS_ENDPOINT=http://localhost:8000/v1/audio/speech
```

#### Option 2: OpenAI TTS API
```bash
export TTS_ENDPOINT=https://api.openai.com/v1/audio/speech
export OPENAI_API_KEY=your_api_key_here
```

#### Option 3: Custom TTS Endpoint
```bash
export TTS_ENDPOINT=http://your-tts-server:8000/v1/audio/speech
```

## Usage

### Starting the Chat with Voice
1. Set environment variables:
   ```bash
   export VOICE_ENABLED=true
   export TTS_ENDPOINT=http://localhost:8000/v1/audio/speech
   ```

2. Start the chat:
   ```bash
   ./matey chat
   ```

3. Toggle voice mode:
   - Press `Ctrl+V` to enable/disable voice features
   - Use spacebar for push-to-talk mode

### Voice Interaction Flow
1. **Wake Word Detection**: Say "matey" (or configured wake word)
2. **Recording**: System starts recording automatically
3. **Transcription**: Speech is converted to text using Whisper
4. **Processing**: Text is sent to AI for processing
5. **TTS Response**: AI response is converted to speech and played

### Keyboard Shortcuts
- `Ctrl+V`: Toggle voice mode on/off
- `Spacebar`: Push-to-talk (when voice enabled)
- `Ctrl+C`: Exit chat
- `ESC`: Cancel current operation

## Architecture

### Components
```
Voice Manager (voice.go)
├── VoiceConfig - Configuration management
├── VoiceManager - Core voice processing
├── Wake Word Detection - PortAudio-based energy detection
├── Speech-to-Text - Whisper integration
├── Text-to-Speech - HTTP API integration
└── Audio Playback - Multi-player support

UI Integration (ui_*.go)
├── Voice Message Types - BubbleTea message system
├── Keyboard Handlers - Voice control shortcuts
├── Visual Feedback - Status indicators and notifications
└── Response Integration - Automatic TTS of AI responses
```

### Message Flow
```
[Wake Word] → [Audio Recording] → [Whisper STT] → [AI Processing] → [TTS] → [Audio Playback]
     ↓              ↓                   ↓              ↓           ↓          ↓
[UI Feedback] [Recording UI] [Transcript UI] [Response UI] [TTS UI] [Playback]
```

## Configuration Examples

### Basic Setup
```bash
# Minimal voice setup
export VOICE_ENABLED=true
export TTS_ENDPOINT=http://localhost:8000/v1/audio/speech
./matey chat
```

### Advanced Setup
```bash
# Advanced voice configuration
export VOICE_ENABLED=true
export VOICE_WAKE_WORD=matey
export TTS_ENDPOINT=http://localhost:8000/v1/audio/speech
export TTS_VOICE=alloy
export TTS_MODEL=tts-1-hd
export WHISPER_MODEL=medium
export VOICE_ENERGY_THRESHOLD=400
export VOICE_SPEECH_TIMEOUT=3.0
./matey chat
```

### Production Setup with Remote TTS
```bash
# Production setup with external TTS service
export VOICE_ENABLED=true
export TTS_ENDPOINT=https://your-tts-api.com/v1/audio/speech
export TTS_VOICE=custom_voice
export WHISPER_MODEL=large
export VOICE_ENERGY_THRESHOLD=200
./matey chat
```

## Troubleshooting

### Common Issues

#### Audio Permission Issues
```bash
# Linux: Add user to audio group
sudo usermod -a -G audio $USER
# Logout and login again

# macOS: Grant microphone permission to terminal
# System Preferences → Security & Privacy → Microphone
```

#### PortAudio Not Found
```bash
# Ubuntu/Debian
sudo apt install portaudio19-dev

# macOS
brew install portaudio

# Then rebuild
go build ./cmd/matey
```

#### Whisper Command Not Found
```bash
# Install whisper
pip install openai-whisper

# Or faster-whisper
pip install faster-whisper

# Verify installation
which whisper
which faster-whisper
```

#### TTS Service Not Responding
```bash
# Test TTS endpoint
curl -X POST http://localhost:8000/v1/audio/speech \
  -H "Content-Type: application/json" \
  -d '{"model":"tts-1","voice":"alloy","input":"test"}'
```

#### Audio Playback Issues
```bash
# Install audio players
sudo apt install mpg123 alsa-utils  # Linux
brew install mpg123                 # macOS

# Test audio playback
mpg123 test.mp3
aplay test.wav
```

### Debug Mode
Enable debug logging for voice components:
```bash
export LOG_LEVEL=debug
./matey chat
```

## Security Considerations

### Audio Privacy
- Audio is processed locally by default
- Whisper transcription can run locally
- Consider privacy implications of external TTS services

### Network Security
- TTS endpoints should use HTTPS in production
- Validate TTS service certificates
- Consider rate limiting for external services

### Configuration Security
- Store sensitive API keys in environment variables
- Use secure credential management in production
- Rotate API keys regularly

## Performance Tips

### Audio Processing
- Use faster-whisper for better performance
- Adjust energy threshold based on environment
- Consider CPU vs accuracy trade-offs with Whisper models

### TTS Optimization
- Cache frequently used responses
- Use streaming TTS when available
- Consider local TTS for reduced latency

### Memory Management
- Voice processing is done in separate goroutines
- Audio buffers are automatically managed
- Consider memory limits for long conversations

## Integration with m8e

The voice system integrates seamlessly with m8e's existing features:

### MCP Protocol
- Voice commands can trigger MCP server actions
- Function calls work with voice input
- Tool responses can be spoken via TTS

### Kubernetes Operations
- Voice control for cluster management
- Spoken status reports and alerts
- Natural language infrastructure commands

### AI Providers
- Works with all configured AI providers
- Supports streaming responses with TTS
- Maintains conversation context across voice interactions

## Future Enhancements

### Planned Features
- Voice command macros and shortcuts
- Multi-language support
- Custom wake word training
- Voice-controlled function approvals
- Audio streaming for real-time responses

### Experimental Features
- Voice emotion detection
- Speaker identification
- Background noise filtering
- Voice activity improvement

## Contributing

To contribute to the voice integration:

1. Test your changes with various audio setups
2. Ensure cross-platform compatibility
3. Add appropriate error handling
4. Update documentation for new features
5. Consider accessibility implications

## License

Voice integration follows the same license as the main m8e project (GNU AGPL v3.0).