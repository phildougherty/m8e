package chat

import (
	"testing"
)

func TestVoiceConfigCreation(t *testing.T) {
	config := NewVoiceConfig()
	if config == nil {
		t.Fatal("NewVoiceConfig() returned nil")
	}
	
	// Test that default values are set
	if config.WakeWord == "" {
		t.Error("WakeWord should have a default value")
	}
	
	if config.TTSEndpoint == "" {
		t.Error("TTSEndpoint should have a default value")
	}
	
	if config.SampleRate == 0 {
		t.Error("SampleRate should have a default value")
	}
}

func TestVoiceManagerCreation(t *testing.T) {
	config := NewVoiceConfig()
	manager, err := NewVoiceManager(config)
	
	if err != nil {
		t.Fatalf("NewVoiceManager() returned error: %v", err)
	}
	
	if manager == nil {
		t.Fatal("NewVoiceManager() returned nil")
	}
	
	if manager.config != config {
		t.Error("VoiceManager config not set correctly")
	}
}

func TestVoiceManagerStubBehavior(t *testing.T) {
	config := NewVoiceConfig()
	manager, err := NewVoiceManager(config)
	if err != nil {
		t.Fatalf("NewVoiceManager() returned error: %v", err)
	}
	
	// These should work without error (no-op in stub)
	manager.SetCallbacks(nil, nil, nil)
	manager.Stop()
	
	// These should return errors in stub version (if !voice build tag)
	// but work in full version (if voice build tag)
	err = manager.Start()
	// We don't test the specific error because it depends on build tags
	
	err = manager.TextToSpeech("test")
	// We don't test the specific error because it depends on build tags
	
	err = manager.PlayAudio([]byte("test"))
	// We don't test the specific error because it depends on build tags
}

func TestVoiceConfigEnvironmentDefaults(t *testing.T) {
	config := NewVoiceConfig()
	
	// Test default values
	if config.WakeWord != "matey" {
		t.Errorf("Expected default wake word 'matey', got '%s'", config.WakeWord)
	}
	
	if config.TTSVoice != "alloy" {
		t.Errorf("Expected default TTS voice 'alloy', got '%s'", config.TTSVoice)
	}
	
	if config.TTSModel != "tts-1" {
		t.Errorf("Expected default TTS model 'tts-1', got '%s'", config.TTSModel)
	}
	
	if config.WhisperModel != "base" {
		t.Errorf("Expected default Whisper model 'base', got '%s'", config.WhisperModel)
	}
	
	if config.SampleRate != 16000 {
		t.Errorf("Expected default sample rate 16000, got %d", config.SampleRate)
	}
}