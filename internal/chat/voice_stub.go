//go:build !voice

package chat

import (
	"errors"
	"os"
)

// VoiceConfig holds voice-related configuration (stub version)
type VoiceConfig struct {
	WakeWord                string
	TTSEndpoint            string
	TTSVoice               string
	TTSModel               string
	WhisperModel           string
	SampleRate             int
	FrameLength            int
	EnergyThreshold        int
	SpeechTimeoutSeconds   float64
	MaxRecordingSeconds    int
	WakeWordExclusionTime  float64
	PostWakewordDelay      float64
	Enabled                bool
}

// VoiceManager handles voice interaction (stub version)
type VoiceManager struct {
	config *VoiceConfig
}

// NewVoiceConfig creates voice configuration from environment variables (stub)
func NewVoiceConfig() *VoiceConfig {
	return &VoiceConfig{
		WakeWord:                "matey",
		TTSEndpoint:            "http://localhost:8000/v1/audio/speech",
		TTSVoice:               "alloy",
		TTSModel:               "tts-1",
		WhisperModel:           "base",
		SampleRate:             16000,
		FrameLength:            1024,
		EnergyThreshold:        300,
		SpeechTimeoutSeconds:   2.0,
		MaxRecordingSeconds:    30,
		WakeWordExclusionTime:  1.0,
		PostWakewordDelay:      0.5,
		Enabled:                false, // Always disabled in stub
	}
}

// NewVoiceManager creates a new voice manager (stub)
func NewVoiceManager(config *VoiceConfig) (*VoiceManager, error) {
	return &VoiceManager{
		config: config,
	}, nil
}

// Start begins voice processing (stub)
func (vm *VoiceManager) Start() error {
	return errors.New("voice features not compiled in - build with -tags voice")
}

// Stop stops voice processing (stub)
func (vm *VoiceManager) Stop() {
	// no-op
}

// SetCallbacks sets the callback functions (stub)
func (vm *VoiceManager) SetCallbacks(onWakeWord func(), onTranscript func(string), onTTSReady func([]byte)) {
	// no-op
}

// TextToSpeech converts text to speech (stub)
func (vm *VoiceManager) TextToSpeech(text string) error {
	return errors.New("voice features not compiled in - build with -tags voice")
}

// PlayAudio plays audio data (stub)
func (vm *VoiceManager) PlayAudio(audioData []byte) error {
	return errors.New("voice features not compiled in - build with -tags voice")
}

// getEnvDefault gets environment variable with default value (stub)
func getEnvDefault(key, defaultValue string) string {
	// This function is used by other parts of the system too
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}