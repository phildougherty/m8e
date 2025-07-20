//go:build voice

package chat

import (
	"fmt"
	"os/exec"
	"strings"
)

// CheckVoiceSystem checks if all voice dependencies are available
func CheckVoiceSystem() string {
	var report strings.Builder
	
	report.WriteString("🎤 Voice System Check\n")
	report.WriteString("====================\n\n")
	
	// Check environment variables
	report.WriteString("📋 Environment Variables:\n")
	config := NewVoiceConfig()
	report.WriteString(fmt.Sprintf("  VOICE_ENABLED: %t\n", config.Enabled))
	report.WriteString(fmt.Sprintf("  VOICE_WAKE_WORD: %s\n", config.WakeWord))
	report.WriteString(fmt.Sprintf("  TTS_ENDPOINT: %s\n", config.TTSEndpoint))
	report.WriteString(fmt.Sprintf("  WHISPER_MODEL: %s\n", config.WhisperModel))
	report.WriteString(fmt.Sprintf("  VOICE_SAMPLE_RATE: %d\n", config.SampleRate))
	report.WriteString(fmt.Sprintf("  VOICE_ENERGY_THRESHOLD: %d\n", config.EnergyThreshold))
	report.WriteString("\n")
	
	// Check Whisper availability
	report.WriteString("🗣️  Speech-to-Text (Whisper):\n")
	whisperCommands := []string{"whisper", "python3 -m whisper", "whisper-cpp"}
	whisperFound := false
	
	for _, cmdStr := range whisperCommands {
		parts := strings.Split(cmdStr, " ")
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.Args = append(cmd.Args, "--help")
		
		if err := cmd.Run(); err == nil {
			report.WriteString(fmt.Sprintf("  ✅ %s: Available\n", cmdStr))
			whisperFound = true
		} else {
			report.WriteString(fmt.Sprintf("  ❌ %s: Not found\n", cmdStr))
		}
	}
	
	if !whisperFound {
		report.WriteString("  💡 Install with: pipx install openai-whisper\n")
	}
	report.WriteString("\n")
	
	// Check audio tools
	report.WriteString("🔊 Audio Playback:\n")
	audioPlayers := []string{"mpg123", "aplay", "ffplay"}
	
	for _, player := range audioPlayers {
		cmd := exec.Command("which", player)
		if err := cmd.Run(); err == nil {
			report.WriteString(fmt.Sprintf("  ✅ %s: Available\n", player))
		} else {
			report.WriteString(fmt.Sprintf("  ❌ %s: Not found\n", player))
		}
	}
	report.WriteString("\n")
	
	// Check TTS endpoint
	report.WriteString("🎵 Text-to-Speech:\n")
	if config.TTSEndpoint == "" {
		report.WriteString("  ❌ TTS_ENDPOINT not set\n")
		report.WriteString("  💡 Set with: export TTS_ENDPOINT=http://your-tts-server:8000/v1/audio/speech\n")
	} else {
		report.WriteString(fmt.Sprintf("  📡 TTS_ENDPOINT: %s\n", config.TTSEndpoint))
		report.WriteString("  💡 Test manually with curl to verify TTS service is running\n")
	}
	report.WriteString("\n")
	
	// System recommendations
	report.WriteString("💡 Recommendations:\n")
	if !whisperFound {
		report.WriteString("  • Install Whisper: pipx install openai-whisper\n")
	}
	if config.TTSEndpoint == "" {
		report.WriteString("  • Set TTS endpoint environment variable\n")
	}
	report.WriteString("  • Grant microphone permission to Terminal in System Preferences\n")
	report.WriteString("  • Use F1 or Ctrl+T for voice triggers (most reliable on macOS)\n")
	
	return report.String()
}