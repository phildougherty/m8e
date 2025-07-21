package chat

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestApprovalMode_String(t *testing.T) {
	tests := []struct {
		name     string
		mode     ApprovalMode
		expected string
	}{
		{"DEFAULT mode", DEFAULT, "DEFAULT"},
		{"AUTO_EDIT mode", AUTO_EDIT, "AUTO_EDIT"},
		{"YOLO mode", YOLO, "YOLO"},
		{"Unknown mode", ApprovalMode(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.mode.String()
			if result != tt.expected {
				t.Errorf("ApprovalMode.String() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestApprovalMode_GetModeIndicatorNoEmoji(t *testing.T) {
	tests := []struct {
		name     string
		mode     ApprovalMode
		expected string
	}{
		{"DEFAULT mode", DEFAULT, "MANUAL"},
		{"AUTO_EDIT mode", AUTO_EDIT, "AUTO"},
		{"YOLO mode", YOLO, "YOLO"},
		{"Unknown mode", ApprovalMode(999), "MANUAL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.mode.GetModeIndicatorNoEmoji()
			if result != tt.expected {
				t.Errorf("ApprovalMode.GetModeIndicatorNoEmoji() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestApprovalMode_Description(t *testing.T) {
	tests := []struct {
		name     string
		mode     ApprovalMode
		expected string
	}{
		{"DEFAULT mode", DEFAULT, "Ask for confirmation before executing actions"},
		{"AUTO_EDIT mode", AUTO_EDIT, "Auto-approve safe operations, confirm destructive ones"},
		{"YOLO mode", YOLO, "Auto-approve all function calls without confirmation"},
		{"Unknown mode", ApprovalMode(999), "Unknown mode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.mode.Description()
			if result != tt.expected {
				t.Errorf("ApprovalMode.Description() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestApprovalMode_ShouldConfirm(t *testing.T) {
	tests := []struct {
		name         string
		mode         ApprovalMode
		functionName string
		expected     bool
	}{
		{"YOLO never confirms", YOLO, "dangerous_function", false},
		{"YOLO never confirms safe", YOLO, "get_status", false},
		{"AUTO_EDIT confirms dangerous", AUTO_EDIT, "dangerous_function", true},
		{"AUTO_EDIT allows safe get_status", AUTO_EDIT, "get_status", false},
		{"AUTO_EDIT allows safe list_services", AUTO_EDIT, "list_services", false},
		{"AUTO_EDIT allows safe get_logs", AUTO_EDIT, "get_logs", false},
		{"AUTO_EDIT allows safe get_metrics", AUTO_EDIT, "get_metrics", false},
		{"DEFAULT always confirms", DEFAULT, "get_status", true},
		{"DEFAULT always confirms dangerous", DEFAULT, "dangerous_function", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.mode.ShouldConfirm(tt.functionName)
			if result != tt.expected {
				t.Errorf("ApprovalMode.ShouldConfirm(%s) = %v, want %v", tt.functionName, result, tt.expected)
			}
		})
	}
}

func TestTermChatMessage(t *testing.T) {
	now := time.Now()
	msg := TermChatMessage{
		Role:      "user",
		Content:   "Hello, world!",
		Timestamp: now,
	}

	if msg.Role != "user" {
		t.Errorf("Expected Role to be 'user', got %s", msg.Role)
	}
	if msg.Content != "Hello, world!" {
		t.Errorf("Expected Content to be 'Hello, world!', got %s", msg.Content)
	}
	if msg.Timestamp != now {
		t.Errorf("Expected Timestamp to be %v, got %v", now, msg.Timestamp)
	}
}

func TestFunctionConfirmation(t *testing.T) {
	callbackCalled := false
	confirmation := FunctionConfirmation{
		FunctionName: "test_function",
		Arguments:    `{"arg1": "value1"}`,
		Callback: func(approved bool) {
			callbackCalled = true
			if !approved {
				t.Errorf("Expected callback to be called with true")
			}
		},
	}

	if confirmation.FunctionName != "test_function" {
		t.Errorf("Expected FunctionName to be 'test_function', got %s", confirmation.FunctionName)
	}
	if confirmation.Arguments != `{"arg1": "value1"}` {
		t.Errorf("Expected Arguments to match, got %s", confirmation.Arguments)
	}

	// Test callback
	confirmation.Callback(true)
	if !callbackCalled {
		t.Errorf("Expected callback to be called")
	}
}

func TestGetUIProgram(t *testing.T) {
	// Test initial state
	if GetUIProgram() != nil {
		t.Errorf("Expected GetUIProgram() to return nil initially")
	}

	// Test setting global program
	mockProgram := &tea.Program{}
	uiProgram = mockProgram

	if GetUIProgram() != mockProgram {
		t.Errorf("Expected GetUIProgram() to return the set program")
	}

	// Clean up
	uiProgram = nil
}

func TestMessageTypes(t *testing.T) {
	// Test AiStreamMsg
	streamMsg := AiStreamMsg{Content: "streaming content"}
	if streamMsg.Content != "streaming content" {
		t.Errorf("Expected AiStreamMsg.Content to be 'streaming content', got %s", streamMsg.Content)
	}

	// Test AiResponseMsg
	responseMsg := AiResponseMsg{Content: "response content"}
	if responseMsg.Content != "response content" {
		t.Errorf("Expected AiResponseMsg.Content to be 'response content', got %s", responseMsg.Content)
	}

	// Test userInputMsg
	inputMsg := userInputMsg{
		input:       "user input",
		userDisplay: "display text",
	}
	if inputMsg.input != "user input" {
		t.Errorf("Expected userInputMsg.input to be 'user input', got %s", inputMsg.input)
	}
	if inputMsg.userDisplay != "display text" {
		t.Errorf("Expected userInputMsg.userDisplay to be 'display text', got %s", inputMsg.userDisplay)
	}

	// Test voiceTranscriptMsg
	voiceMsg := voiceTranscriptMsg{transcript: "voice transcript"}
	if voiceMsg.transcript != "voice transcript" {
		t.Errorf("Expected voiceTranscriptMsg.transcript to be 'voice transcript', got %s", voiceMsg.transcript)
	}

	// Test voiceTTSReadyMsg
	audioData := []byte{1, 2, 3, 4}
	ttsMsg := voiceTTSReadyMsg{audioData: audioData}
	if len(ttsMsg.audioData) != 4 {
		t.Errorf("Expected voiceTTSReadyMsg.audioData length to be 4, got %d", len(ttsMsg.audioData))
	}

	// Test contextUpdateMsg
	contextMsg := contextUpdateMsg{
		action: "added",
		path:   "/test/path",
		stats:  map[string]interface{}{"files": 5},
	}
	if contextMsg.action != "added" {
		t.Errorf("Expected contextUpdateMsg.action to be 'added', got %s", contextMsg.action)
	}
	if contextMsg.path != "/test/path" {
		t.Errorf("Expected contextUpdateMsg.path to be '/test/path', got %s", contextMsg.path)
	}
	if contextMsg.stats["files"] != 5 {
		t.Errorf("Expected contextUpdateMsg.stats['files'] to be 5, got %v", contextMsg.stats["files"])
	}
}

func TestColorConstants(t *testing.T) {
	// Test that color constants are defined and have expected string representations
	colorTests := []struct {
		name     string
		color    string
		expected string
	}{
		{"ArmyGreen", string(ArmyGreen), "58"},
		{"LightGreen", string(LightGreen), "64"},
		{"Brown", string(Brown), "94"},
		{"Yellow", string(Yellow), "226"},
		{"GoldYellow", string(GoldYellow), "220"},
		{"Red", string(Red), "196"},
		{"Tan", string(Tan), "180"},
	}

	for _, tt := range colorTests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.color != tt.expected {
				t.Errorf("Expected %s color to be %s, got %s", tt.name, tt.expected, tt.color)
			}
		})
	}
}

func TestTermChatStructure(t *testing.T) {
	// Test that TermChat has expected field structure (basic validation)
	tc := &TermChat{
		chatHistory:     make([]TermChatMessage, 0),
		functionResults: make(map[string]string),
		verboseMode:     false,
		approvalMode:    DEFAULT,
		maxTurns:        25,
		currentTurns:    0,
	}

	if tc.chatHistory == nil {
		t.Error("Expected chatHistory to be initialized")
	}
	if tc.functionResults == nil {
		t.Error("Expected functionResults to be initialized")
	}
	if tc.verboseMode != false {
		t.Error("Expected verboseMode to be false")
	}
	if tc.approvalMode != DEFAULT {
		t.Error("Expected approvalMode to be DEFAULT")
	}
	if tc.maxTurns != 25 {
		t.Errorf("Expected maxTurns to be 25, got %d", tc.maxTurns)
	}
	if tc.currentTurns != 0 {
		t.Errorf("Expected currentTurns to be 0, got %d", tc.currentTurns)
	}
}

func TestChatUIStructure(t *testing.T) {
	// Test that ChatUI has expected field structure (basic validation)
	ui := &ChatUI{
		input:             "",
		cursor:            0,
		viewport:          make([]string, 0),
		viewportOffset:    0,
		inputFocused:      true,
		ready:             false,
		loading:           false,
		spinnerFrame:      0,
		confirmationMode:  false,
		aiResponseStartIdx: 0,
	}

	if ui.input != "" {
		t.Error("Expected input to be empty string")
	}
	if ui.cursor != 0 {
		t.Error("Expected cursor to be 0")
	}
	if ui.viewport == nil {
		t.Error("Expected viewport to be initialized")
	}
	if ui.viewportOffset != 0 {
		t.Error("Expected viewportOffset to be 0")
	}
	if !ui.inputFocused {
		t.Error("Expected inputFocused to be true")
	}
	if ui.ready {
		t.Error("Expected ready to be false")
	}
	if ui.loading {
		t.Error("Expected loading to be false")
	}
}