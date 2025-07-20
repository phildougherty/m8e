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
	stopRecording bool
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

// Start begins voice processing (simplified - no wake word detection)
func (vm *VoiceManager) Start() error {
	if !vm.config.Enabled {
		return nil
	}

	// Initialize PortAudio for manual recording
	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize PortAudio: %w", err)
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

	vm.cancel()
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

	vm.mutex.Lock()
	defer vm.mutex.Unlock()

	if vm.isRecording {
		// If already recording, stop it early
		vm.stopRecording = true
		return nil
	}

	// Start new recording
	go vm.handleManualRecording()
	return nil
}

// StopRecording stops the current recording session early
func (vm *VoiceManager) StopRecording() {
	vm.mutex.Lock()
	defer vm.mutex.Unlock()
	vm.stopRecording = true
}

// handleManualRecording handles manual recording with user feedback
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

	// Record audio with shorter duration (3 seconds)
	audioData, err := vm.recordAudio()
	if err != nil {
		log.Printf("Error recording audio: %v", err)
		// Send error to UI
		if vm.onTranscript != nil {
			vm.onTranscript("ERROR: " + err.Error())
		}
		return
	}

	if len(audioData) == 0 {
		if vm.onTranscript != nil {
			vm.onTranscript("ERROR: No audio recorded")
		}
		return
	}

	// Send processing message to UI
	if vm.onTranscript != nil {
		vm.onTranscript("PROCESSING: Transcribing audio...")
	}

	// Transcribe audio
	transcript, err := vm.transcribeAudio(audioData)
	if err != nil {
		log.Printf("Error transcribing audio: %v", err)
		if vm.onTranscript != nil {
			vm.onTranscript("ERROR: " + err.Error())
		}
		return
	}

	if transcript != "" && vm.onTranscript != nil {
		vm.onTranscript(transcript)
	} else if vm.onTranscript != nil {
		vm.onTranscript("ERROR: No speech detected")
	}
}


// recordAudio records audio with Voice Activity Detection and early stopping
func (vm *VoiceManager) recordAudio() ([]float32, error) {
	maxDuration := 10 * time.Second // Maximum recording time
	silenceThreshold := float32(vm.config.EnergyThreshold) * 0.1 // Lower threshold for silence detection
	silenceDuration := time.Duration(1.5 * float64(time.Second)) // Stop after 1.5 seconds of silence
	
	audioData := make([]float32, 0)
	var mutex sync.Mutex
	
	// Get default input device
	inputDevice, err := portaudio.DefaultInputDevice()
	if err != nil {
		return nil, fmt.Errorf("failed to get input device: %w", err)
	}
	
	// Create recording stream
	inputParams := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   inputDevice,
			Channels: 1,
			Latency:  inputDevice.DefaultLowInputLatency,
		},
		SampleRate:      float64(vm.config.SampleRate),
		FramesPerBuffer: vm.config.FrameLength,
	}
	
	// Voice Activity Detection variables
	lastSpeechTime := time.Now()
	speechDetected := false
	
	stream, err := portaudio.OpenStream(inputParams, func(in []float32) {
		mutex.Lock()
		defer mutex.Unlock()
		
		// Append input to buffer
		audioData = append(audioData, in...)
		
		// Calculate energy for VAD
		energy := vm.calculateEnergy(in)
		
		if energy > silenceThreshold {
			lastSpeechTime = time.Now()
			speechDetected = true
		}
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open recording stream: %w", err)
	}
	defer stream.Close()
	
	// Start recording
	if err := stream.Start(); err != nil {
		return nil, fmt.Errorf("failed to start recording: %w", err)
	}
	
	// Reset stop flag
	vm.mutex.Lock()
	vm.stopRecording = false
	vm.mutex.Unlock()
	
	startTime := time.Now()
	
	// Record with VAD and early stopping
	for {
		time.Sleep(100 * time.Millisecond) // Check every 100ms
		
		// Check if user manually stopped recording
		vm.mutex.RLock()
		shouldStop := vm.stopRecording
		vm.mutex.RUnlock()
		
		if shouldStop {
			break
		}
		
		// Check maximum duration
		if time.Since(startTime) > maxDuration {
			break
		}
		
		// Check for silence after speech was detected
		if speechDetected && time.Since(lastSpeechTime) > silenceDuration {
			break
		}
	}
	
	// Stop recording
	stream.Stop()
	
	mutex.Lock()
	result := make([]float32, len(audioData))
	copy(result, audioData)
	mutex.Unlock()
	
	return result, nil
}

// calculateEnergy calculates RMS energy of audio frame for VAD
func (vm *VoiceManager) calculateEnergy(samples []float32) float32 {
	if len(samples) == 0 {
		return 0
	}
	
	var sum float32
	for _, sample := range samples {
		sum += sample * sample
	}
	return sum / float32(len(samples))
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

// saveAudioToWAV saves audio data to a proper WAV file
func (vm *VoiceManager) saveAudioToWAV(audioData []float32) (string, error) {
	tempDir := os.TempDir()
	tempFile := filepath.Join(tempDir, fmt.Sprintf("voice_%d.wav", time.Now().UnixNano()))
	
	file, err := os.Create(tempFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// WAV file parameters
	sampleRate := uint32(vm.config.SampleRate)
	numChannels := uint16(1)
	bitsPerSample := uint16(16)
	byteRate := sampleRate * uint32(numChannels) * uint32(bitsPerSample) / 8
	blockAlign := numChannels * bitsPerSample / 8
	dataSize := uint32(len(audioData)) * uint32(bitsPerSample) / 8

	// Write WAV header
	// RIFF chunk
	file.WriteString("RIFF")
	writeUint32(file, 36+dataSize) // ChunkSize
	file.WriteString("WAVE")
	
	// fmt chunk
	file.WriteString("fmt ")
	writeUint32(file, 16)          // Subchunk1Size
	writeUint16(file, 1)           // AudioFormat (PCM)
	writeUint16(file, numChannels) // NumChannels
	writeUint32(file, sampleRate)  // SampleRate
	writeUint32(file, byteRate)    // ByteRate
	writeUint16(file, blockAlign)  // BlockAlign
	writeUint16(file, bitsPerSample) // BitsPerSample
	
	// data chunk
	file.WriteString("data")
	writeUint32(file, dataSize)
	
	// Convert float32 to 16-bit PCM and write
	for _, sample := range audioData {
		// Clamp to [-1, 1] and convert to 16-bit
		if sample > 1.0 {
			sample = 1.0
		} else if sample < -1.0 {
			sample = -1.0
		}
		pcmSample := int16(sample * 32767)
		writeInt16(file, pcmSample)
	}
	
	return tempFile, nil
}

// Helper functions for binary writing
func writeUint32(file *os.File, value uint32) {
	data := []byte{
		byte(value),
		byte(value >> 8),
		byte(value >> 16),
		byte(value >> 24),
	}
	file.Write(data)
}

func writeUint16(file *os.File, value uint16) {
	data := []byte{
		byte(value),
		byte(value >> 8),
	}
	file.Write(data)
}

func writeInt16(file *os.File, value int16) {
	data := []byte{
		byte(value),
		byte(value >> 8),
	}
	file.Write(data)
}

// callWhisper calls Whisper for transcription
func (vm *VoiceManager) callWhisper(audioFile string) (string, error) {
	// Try different whisper implementations in order of preference
	whisperCommands := [][]string{
		// OpenAI whisper (most common) - output to file and read it
		{"whisper", audioFile, "--model", vm.config.WhisperModel, "--output_format", "txt", "--output_dir", "/tmp", "--verbose", "False"},
		// Alternative whisper command formats  
		{"whisper", audioFile, "--model", vm.config.WhisperModel, "--verbose", "False"},
		// Python module invocation
		{"python3", "-m", "whisper", audioFile, "--model", vm.config.WhisperModel, "--verbose", "False"},
		// Whisper.cpp if available
		{"whisper-cpp", audioFile},
	}

	var lastError error
	for i, cmdArgs := range whisperCommands {
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		
		// Redirect both stdout and stderr to null to hide all output
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err == nil {
			cmd.Stdout = devNull
			cmd.Stderr = devNull
			defer devNull.Close()
		}
		
		// For the first command that outputs to file, run it and read the file
		if i == 0 {
			err := cmd.Run()
			if err == nil {
				// Read the output file
				baseName := strings.TrimSuffix(filepath.Base(audioFile), filepath.Ext(audioFile))
				outputFile := filepath.Join("/tmp", baseName+".txt")
				if content, readErr := os.ReadFile(outputFile); readErr == nil {
					os.Remove(outputFile) // Clean up
					result := strings.TrimSpace(string(content))
					if result != "" {
						return result, nil
					}
				}
			}
			lastError = err
			continue
		}
		
		// For other commands, capture output normally but silently
		output, err := cmd.Output()
		if err == nil && len(output) > 0 {
			result := strings.TrimSpace(string(output))
			if result != "" {
				return result, nil
			}
		}
		lastError = err
	}

	// If all whisper commands failed, provide helpful error message
	return "", fmt.Errorf("whisper transcription failed - install whisper with: pipx install openai-whisper\nLast error: %w", lastError)
}

// TextToSpeech converts text to speech using configured TTS endpoint
func (vm *VoiceManager) TextToSpeech(text string) error {
	if !vm.config.Enabled {
		return nil
	}

	// Filter and clean text for TTS
	cleanText := vm.filterTextForTTS(text)
	if cleanText == "" {
		return nil // Nothing to speak
	}

	payload := map[string]interface{}{
		"model": vm.config.TTSModel,
		"voice": vm.config.TTSVoice,
		"input": cleanText,
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

// filterTextForTTS cleans text for better TTS by removing markdown and expanding acronyms
func (vm *VoiceManager) filterTextForTTS(text string) string {
	// Remove markdown formatting
	text = vm.removeMarkdown(text)
	
	// Expand common acronyms and abbreviations
	text = vm.expandAcronyms(text)
	
	// Clean up extra whitespace
	text = strings.TrimSpace(text)
	
	return text
}

// removeMarkdown strips markdown formatting from text
func (vm *VoiceManager) removeMarkdown(text string) string {
	// Remove code blocks (```...```)
	text = strings.ReplaceAll(text, "```", "")
	
	// Remove inline code (`...`)
	for strings.Contains(text, "`") {
		start := strings.Index(text, "`")
		if start == -1 {
			break
		}
		end := strings.Index(text[start+1:], "`")
		if end == -1 {
			break
		}
		end += start + 1
		// Replace with just the content (without backticks)
		text = text[:start] + text[start+1:end] + text[end+1:]
	}
	
	// Remove bold/italic markers (**text**, *text*)
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "*", "")
	
	// Remove headers (# ## ###)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			// Remove # markers but keep the text
			lines[i] = strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	text = strings.Join(lines, "\n")
	
	// Remove links [text](url) -> text
	for strings.Contains(text, "](") {
		start := strings.LastIndex(text[:strings.Index(text, "](")+1], "[")
		if start == -1 {
			break
		}
		end := strings.Index(text[start:], ")")
		if end == -1 {
			break
		}
		end += start
		
		linkText := text[start+1:strings.Index(text[start:], "]")+start]
		text = text[:start] + linkText + text[end+1:]
	}
	
	// Remove remaining brackets and parentheses that might be leftover
	text = strings.ReplaceAll(text, "[", "")
	text = strings.ReplaceAll(text, "]", "")
	
	return text
}

// expandAcronyms expands common internet acronyms and abbreviations for better TTS
func (vm *VoiceManager) expandAcronyms(text string) string {
	// Common acronyms and their expansions
	acronyms := map[string]string{
		"etc":   "etcetera",
		"etc.":  "etcetera",
		"tldr":  "too long didn't read",
		"tl;dr": "too long didn't read",
		"wtf":   "what the heck", 
		"omg":   "oh my god",
		"lol":   "laughing out loud",
		"lmao":  "laughing my butt off",
		"rofl":  "rolling on floor laughing",
		"brb":   "be right back",
		"btw":   "by the way",
		"fyi":   "for your information",
		"imho":  "in my humble opinion",
		"imo":   "in my opinion",
		"afaik": "as far as I know",
		"iirc":  "if I recall correctly",
		"ianal": "I am not a lawyer",
		"ftfy":  "fixed that for you",
		"smh":   "shaking my head",
		"tbh":   "to be honest",
		"ngl":   "not gonna lie",
		"idk":   "I don't know",
		"irl":   "in real life",
		"diy":   "do it yourself",
		"faq":   "frequently asked questions",
		"asap":  "as soon as possible",
		"aka":   "also known as",
		"i.e.":  "that is",
		"e.g.":  "for example",
		"vs":    "versus",
		"vs.":   "versus",
		"w/":    "with",
		"w/o":   "without",
		"b/c":   "because",
		"thru":  "through",
		"ur":    "your",
		"u":     "you",
		"pls":   "please",
		"plz":   "please",
		"thx":   "thanks",
		"ty":    "thank you",
		"np":    "no problem",
		"nvm":   "never mind",
		"rn":    "right now",
		"atm":   "at the moment",
		"2day":  "today",
		"2morrow": "tomorrow",
		"b4":    "before",
		"gr8":   "great",
		"n8":    "night",
		"m8":    "mate",
		"k8s":   "kubernetes",
		"ai":    "artificial intelligence",
		"ml":    "machine learning",
		"api":   "application programming interface",
		"ui":    "user interface",
		"ux":    "user experience",
		"db":    "database",
		"cli":   "command line interface",
		"gui":   "graphical user interface",
		"os":    "operating system",
		"vm":    "virtual machine",
		"vps":   "virtual private server",
		"ssh":   "secure shell",
		"http":  "hypertext transfer protocol",
		"https": "hypertext transfer protocol secure",
		"url":   "uniform resource locator",
		"uri":   "uniform resource identifier",
		"json":  "javascript object notation",
		"yaml":  "yet another markup language",
		"xml":   "extensible markup language",
		"html":  "hypertext markup language",
		"css":   "cascading style sheets",
		"js":    "javascript",
		"ts":    "typescript",
		"sql":   "structured query language",
		"crud":  "create read update delete",
		"rest":  "representational state transfer",
		"jwt":   "json web token",
		"oauth": "open authorization",
		"2fa":   "two factor authentication",
		"mfa":   "multi factor authentication",
		"vpn":   "virtual private network",
		"dns":   "domain name system",
		"cdn":   "content delivery network",
		"aws":   "amazon web services",
		"gcp":   "google cloud platform",
		"ci":    "continuous integration",
		"cd":    "continuous deployment",
		"devops": "development operations",
		"sre":   "site reliability engineering",
		"tdd":   "test driven development",
		"bdd":   "behavior driven development",
		"mvp":   "minimum viable product",
		"poc":   "proof of concept",
		"wip":   "work in progress",
		"pr":    "pull request",
		"mr":    "merge request",
		"repo":  "repository",
		"gh":    "github",
		"gl":    "gitlab",
	}
	
	// Split into words and check each one
	words := strings.Fields(text)
	for i, word := range words {
		// Check if word (lowercase) is in our acronym map
		lowerWord := strings.ToLower(strings.Trim(word, ".,!?;:"))
		if expansion, exists := acronyms[lowerWord]; exists {
			// Preserve punctuation
			punctuation := ""
			if len(word) > len(lowerWord) {
				punctuation = word[len(lowerWord):]
			}
			words[i] = expansion + punctuation
		}
	}
	
	return strings.Join(words, " ")
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