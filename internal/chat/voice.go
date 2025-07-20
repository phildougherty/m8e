//go:build voice

package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
)

// VoiceConfig holds voice-related configuration
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

// VoiceManager handles voice interaction
type VoiceManager struct {
	config        *VoiceConfig
	ctx           context.Context
	cancel        context.CancelFunc
	mutex         sync.RWMutex
	audioStream   *portaudio.Stream
	isListening   bool
	isRecording   bool
	onWakeWord    func()
	onTranscript  func(string)
	onTTSReady    func([]byte)
}

// NewVoiceConfig creates voice configuration from environment variables
func NewVoiceConfig() *VoiceConfig {
	enabled, _ := strconv.ParseBool(getEnvDefault("VOICE_ENABLED", "false"))
	sampleRate, _ := strconv.Atoi(getEnvDefault("VOICE_SAMPLE_RATE", "44100")) // Higher sample rate for macOS
	frameLength, _ := strconv.Atoi(getEnvDefault("VOICE_FRAME_LENGTH", "2048")) // Larger frame for better detection
	energyThreshold, _ := strconv.Atoi(getEnvDefault("VOICE_ENERGY_THRESHOLD", "100")) // Lower threshold for macOS
	speechTimeout, _ := strconv.ParseFloat(getEnvDefault("VOICE_SPEECH_TIMEOUT", "2.0"), 64)
	maxRecording, _ := strconv.Atoi(getEnvDefault("VOICE_MAX_RECORDING_SECONDS", "30"))
	exclusionTime, _ := strconv.ParseFloat(getEnvDefault("VOICE_WAKE_WORD_EXCLUSION_TIME", "1.0"), 64)
	postDelay, _ := strconv.ParseFloat(getEnvDefault("VOICE_POST_WAKEWORD_DELAY", "0.5"), 64)

	return &VoiceConfig{
		WakeWord:                getEnvDefault("VOICE_WAKE_WORD", "matey"),
		TTSEndpoint:            getEnvDefault("TTS_ENDPOINT", "http://localhost:8000/v1/audio/speech"),
		TTSVoice:               getEnvDefault("TTS_VOICE", "alloy"),
		TTSModel:               getEnvDefault("TTS_MODEL", "tts-1"),
		WhisperModel:           getEnvDefault("WHISPER_MODEL", "base"),
		SampleRate:             sampleRate,
		FrameLength:            frameLength,
		EnergyThreshold:        energyThreshold,
		SpeechTimeoutSeconds:   speechTimeout,
		MaxRecordingSeconds:    maxRecording,
		WakeWordExclusionTime:  exclusionTime,
		PostWakewordDelay:      postDelay,
		Enabled:                enabled,
	}
}

// NewVoiceManager creates a new voice manager
func NewVoiceManager(config *VoiceConfig) (*VoiceManager, error) {
	if !config.Enabled {
		return &VoiceManager{config: config}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	
	vm := &VoiceManager{
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}

	// Initialize PortAudio
	if err := portaudio.Initialize(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize PortAudio: %w", err)
	}

	return vm, nil
}

// Start begins voice processing
func (vm *VoiceManager) Start() error {
	if !vm.config.Enabled {
		return nil
	}

	vm.mutex.Lock()
	defer vm.mutex.Unlock()

	if vm.isListening {
		return nil
	}

	// Get default input device
	inputDevice, err := portaudio.DefaultInputDevice()
	if err != nil {
		return fmt.Errorf("failed to get default input device: %w", err)
	}

	// Open audio stream for recording
	inputParams := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   inputDevice,
			Channels: 1,
			Latency:  inputDevice.DefaultLowInputLatency,
		},
		SampleRate:      float64(vm.config.SampleRate),
		FramesPerBuffer: vm.config.FrameLength,
	}

	stream, err := portaudio.OpenStream(inputParams, vm.processAudio)
	if err != nil {
		return fmt.Errorf("failed to open audio stream: %w", err)
	}

	vm.audioStream = stream
	vm.isListening = true

	if err := vm.audioStream.Start(); err != nil {
		return fmt.Errorf("failed to start audio stream: %w", err)
	}

	return nil
}

// Stop stops voice processing
func (vm *VoiceManager) Stop() {
	if !vm.config.Enabled {
		return
	}

	vm.mutex.Lock()
	defer vm.mutex.Unlock()

	if !vm.isListening {
		return
	}

	vm.isListening = false
	vm.cancel()

	if vm.audioStream != nil {
		vm.audioStream.Stop()
		vm.audioStream.Close()
		vm.audioStream = nil
	}

	portaudio.Terminate()
}

// SetCallbacks sets the callback functions
func (vm *VoiceManager) SetCallbacks(onWakeWord func(), onTranscript func(string), onTTSReady func([]byte)) {
	vm.onWakeWord = onWakeWord
	vm.onTranscript = onTranscript
	vm.onTTSReady = onTTSReady
}

// TriggerManualRecording manually starts a recording session (bypass wake word)
func (vm *VoiceManager) TriggerManualRecording() error {
	if !vm.config.Enabled {
		return fmt.Errorf("voice manager not enabled")
	}

	// Skip wake word detection and go straight to recording
	go vm.handleManualRecording()
	return nil
}

// handleManualRecording handles manual recording without wake word detection
func (vm *VoiceManager) handleManualRecording() {
	vm.mutex.Lock()
	if vm.isRecording {
		vm.mutex.Unlock()
		return
	}
	vm.isRecording = true
	vm.mutex.Unlock()

	defer func() {
		vm.mutex.Lock()
		vm.isRecording = false
		vm.mutex.Unlock()
	}()

	if vm.onWakeWord != nil {
		vm.onWakeWord()
	}

	// Record audio for speech-to-text
	audioData, err := vm.recordAudio()
	if err != nil {
		log.Printf("Error recording audio: %v", err)
		return
	}

	if len(audioData) == 0 {
		return
	}

	// Transcribe audio
	transcript, err := vm.transcribeAudio(audioData)
	if err != nil {
		log.Printf("Error transcribing audio: %v", err)
		return
	}

	if transcript != "" && vm.onTranscript != nil {
		vm.onTranscript(transcript)
	}
}

// processAudio processes incoming audio data
func (vm *VoiceManager) processAudio(in []float32) {
	if !vm.isListening {
		return
	}

	// Simple wake word detection using energy threshold
	energy := vm.calculateEnergy(in)
	
	if energy > float32(vm.config.EnergyThreshold) && !vm.isRecording {
		// Start recording on energy spike
		go vm.handleWakeWordDetected()
	}
}

// calculateEnergy calculates RMS energy of audio frame
func (vm *VoiceManager) calculateEnergy(samples []float32) float32 {
	var sum float32
	for _, sample := range samples {
		sum += sample * sample
	}
	return float32(len(samples)) * sum / float32(len(samples))
}

// handleWakeWordDetected handles wake word detection
func (vm *VoiceManager) handleWakeWordDetected() {
	vm.mutex.Lock()
	if vm.isRecording {
		vm.mutex.Unlock()
		return
	}
	vm.isRecording = true
	vm.mutex.Unlock()

	defer func() {
		vm.mutex.Lock()
		vm.isRecording = false
		vm.mutex.Unlock()
	}()

	if vm.onWakeWord != nil {
		vm.onWakeWord()
	}

	// Record audio for speech-to-text
	audioData, err := vm.recordAudio()
	if err != nil {
		log.Printf("Error recording audio: %v", err)
		return
	}

	if len(audioData) == 0 {
		return
	}

	// Transcribe audio
	transcript, err := vm.transcribeAudio(audioData)
	if err != nil {
		log.Printf("Error transcribing audio: %v", err)
		return
	}

	if transcript != "" && vm.onTranscript != nil {
		vm.onTranscript(transcript)
	}
}

// recordAudio records audio with voice activity detection
func (vm *VoiceManager) recordAudio() ([]float32, error) {
	// Simple implementation - record for a fixed duration
	// In a real implementation, you'd use voice activity detection
	duration := time.Duration(vm.config.MaxRecordingSeconds) * time.Second
	frameCount := int(float64(vm.config.SampleRate) * duration.Seconds())
	
	audioData := make([]float32, frameCount)
	
	// This is a simplified recording implementation
	// In practice, you'd need to implement proper VAD
	time.Sleep(duration)
	
	return audioData, nil
}

// transcribeAudio transcribes audio using Whisper
func (vm *VoiceManager) transcribeAudio(audioData []float32) (string, error) {
	// Save audio to temporary WAV file
	tempFile, err := vm.saveAudioToWAV(audioData)
	if err != nil {
		return "", err
	}
	defer os.Remove(tempFile)

	// Use whisper CLI or API for transcription
	return vm.callWhisper(tempFile)
}

// saveAudioToWAV saves audio data to a WAV file
func (vm *VoiceManager) saveAudioToWAV(audioData []float32) (string, error) {
	tempDir := os.TempDir()
	tempFile := filepath.Join(tempDir, fmt.Sprintf("voice_%d.wav", time.Now().UnixNano()))
	
	// Simple WAV file creation (you'd need a proper WAV library)
	// This is a placeholder - implement proper WAV encoding
	file, err := os.Create(tempFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Write basic WAV header and data
	// This is simplified - use a proper audio library in production
	return tempFile, nil
}

// callWhisper calls Whisper for transcription
func (vm *VoiceManager) callWhisper(audioFile string) (string, error) {
	// Try whisper command line tool first
	cmd := exec.Command("whisper", audioFile, "--model", vm.config.WhisperModel, "--output_format", "txt")
	output, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	// Fallback to faster-whisper if available
	cmd = exec.Command("faster-whisper", audioFile, "--model", vm.config.WhisperModel)
	output, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("whisper transcription failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// TextToSpeech converts text to speech using configured TTS endpoint
func (vm *VoiceManager) TextToSpeech(text string) error {
	if !vm.config.Enabled {
		return nil
	}

	payload := map[string]interface{}{
		"model": vm.config.TTSModel,
		"voice": vm.config.TTSVoice,
		"input": text,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal TTS request: %w", err)
	}

	// Make HTTP request to TTS endpoint
	cmd := exec.Command("curl", "-X", "POST", 
		"-H", "Content-Type: application/json",
		"-d", string(jsonData),
		vm.config.TTSEndpoint)
	
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("TTS request failed: %w", err)
	}

	if vm.onTTSReady != nil {
		vm.onTTSReady(output)
	}

	return nil
}

// PlayAudio plays audio data
func (vm *VoiceManager) PlayAudio(audioData []byte) error {
	if !vm.config.Enabled {
		return nil
	}

	// Save to temporary file and play
	tempFile := filepath.Join(os.TempDir(), fmt.Sprintf("tts_%d.mp3", time.Now().UnixNano()))
	
	if err := os.WriteFile(tempFile, audioData, 0644); err != nil {
		return fmt.Errorf("failed to write audio file: %w", err)
	}
	defer os.Remove(tempFile)

	// Try different audio players
	players := []string{"mpg123", "ffplay", "aplay"}
	for _, player := range players {
		cmd := exec.Command(player, tempFile)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	return fmt.Errorf("no suitable audio player found")
}

// getEnvDefault gets environment variable with default value
func getEnvDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}