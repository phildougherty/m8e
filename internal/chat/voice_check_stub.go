//go:build !voice

package chat

// CheckVoiceSystem checks if all voice dependencies are available (stub)
func CheckVoiceSystem() string {
	return `Voice System Check
====================

Voice features not compiled in
Build with voice support: make build-voice
Requires PortAudio: brew install portaudio (macOS) or apt install portaudio19-dev (Linux)
`
}