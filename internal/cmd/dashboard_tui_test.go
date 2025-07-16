package cmd

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/phildougherty/m8e/internal/ai"
)

func TestDashboardModelInit(t *testing.T) {
	model := createTestDashboardModel()
	
	cmd := model.Init()
	if cmd == nil {
		t.Error("Init should return a command")
	}
	
	// Test that the model has proper defaults
	if model.currentTab != TabChat {
		t.Errorf("Expected default tab to be Chat, got %v", model.currentTab)
	}
	
	if model.configState != ConfigStateList {
		t.Errorf("Expected default config state to be List, got %v", model.configState)
	}
	
	if model.workflowState != WorkflowStateList {
		t.Errorf("Expected default workflow state to be List, got %v", model.workflowState)
	}
}

func TestHandleKeyMsg(t *testing.T) {
	model := createTestDashboardModel()
	
	tests := []struct {
		name     string
		key      string
		tab      Tab
		expectCmd bool
	}{
		{
			name:      "ctrl+c quits",
			key:       "ctrl+c",
			tab:       TabChat,
			expectCmd: true,
		},
		{
			name:      "tab switches tabs",
			key:       "tab",
			tab:       TabChat,
			expectCmd: false,
		},
		{
			name:      "f1 shows help",
			key:       "f1",
			tab:       TabChat,
			expectCmd: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model.currentTab = tt.tab
			keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}
			
			_, cmd := model.handleKeyMsg(keyMsg)
			
			hasCmd := cmd != nil
			if hasCmd != tt.expectCmd {
				t.Errorf("Expected command=%v, got command present=%v", tt.expectCmd, hasCmd)
			}
		})
	}
}

func TestHandleChatKeys(t *testing.T) {
	model := createTestDashboardModel()
	
	tests := []struct {
		name        string
		key         string
		inputText   string
		expectCmd   bool
		expectInput string
	}{
		{
			name:        "enter with text sends message",
			key:         "enter",
			inputText:   "hello",
			expectCmd:   true,
			expectInput: "", // Input is cleared after sending
		},
		{
			name:        "enter without text does nothing",
			key:         "enter",
			inputText:   "",
			expectCmd:   false,
			expectInput: "",
		},
		{
			name:        "backspace removes character",
			key:         "backspace",
			inputText:   "hello",
			expectCmd:   false,
			expectInput: "hell",
		},
		{
			name:        "backspace on empty does nothing",
			key:         "backspace",
			inputText:   "",
			expectCmd:   false,
			expectInput: "",
		},
		{
			name:        "character adds to input",
			key:         "a",
			inputText:   "hello",
			expectCmd:   false,
			expectInput: "helloa",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model.inputText = tt.inputText
			keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}
			
			_, cmd := model.handleChatKeys(keyMsg)
			
			hasCmd := cmd != nil
			if hasCmd != tt.expectCmd {
				t.Errorf("Expected command=%v, got command present=%v", tt.expectCmd, hasCmd)
			}
			
			if model.inputText != tt.expectInput {
				t.Errorf("Expected input='%s', got '%s'", tt.expectInput, model.inputText)
			}
		})
	}
}

func TestHandleConfigKeys(t *testing.T) {
	model := createTestDashboardModel()
	
	// Test config list keys
	model.configState = ConfigStateList
	model.selectedConfig = "test-config"
	
	tests := []struct {
		name          string
		key           string
		state         ConfigState
		expectNewState ConfigState
		expectCmd     bool
	}{
		{
			name:          "enter switches to edit",
			key:           "enter",
			state:         ConfigStateList,
			expectNewState: ConfigStateEdit,
			expectCmd:     false,
		},
		{
			name:          "n switches to new",
			key:           "n",
			state:         ConfigStateList,
			expectNewState: ConfigStateNew,
			expectCmd:     false,
		},
		{
			name:          "r refreshes list",
			key:           "r",
			state:         ConfigStateList,
			expectNewState: ConfigStateList,
			expectCmd:     true,
		},
		{
			name:          "escape returns to list from edit",
			key:           "escape",
			state:         ConfigStateEdit,
			expectNewState: ConfigStateList,
			expectCmd:     false,
		},
		{
			name:          "escape returns to list from new",
			key:           "escape",
			state:         ConfigStateNew,
			expectNewState: ConfigStateList,
			expectCmd:     false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model.configState = tt.state
			keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}
			
			_, cmd := model.handleConfigKeys(keyMsg)
			
			if model.configState != tt.expectNewState {
				t.Errorf("Expected state=%v, got %v", tt.expectNewState, model.configState)
			}
			
			hasCmd := cmd != nil
			if hasCmd != tt.expectCmd {
				t.Errorf("Expected command=%v, got command present=%v", tt.expectCmd, hasCmd)
			}
		})
	}
}

func TestHandleWorkflowKeys(t *testing.T) {
	model := createTestDashboardModel()
	model.workflowState = WorkflowStateList
	model.selectedWorkflow = "test-workflow"
	
	tests := []struct {
		name          string
		key           string
		state         WorkflowState
		expectNewState WorkflowState
		expectCmd     bool
	}{
		{
			name:          "enter switches to edit",
			key:           "enter",
			state:         WorkflowStateList,
			expectNewState: WorkflowStateEdit,
			expectCmd:     false,
		},
		{
			name:          "m switches to monitor",
			key:           "m",
			state:         WorkflowStateList,
			expectNewState: WorkflowStateMonitor,
			expectCmd:     false,
		},
		{
			name:          "d switches to design from edit",
			key:           "d",
			state:         WorkflowStateEdit,
			expectNewState: WorkflowStateDesign,
			expectCmd:     false,
		},
		{
			name:          "escape returns to list from edit",
			key:           "escape",
			state:         WorkflowStateEdit,
			expectNewState: WorkflowStateList,
			expectCmd:     false,
		},
		{
			name:          "escape returns to edit from design",
			key:           "escape",
			state:         WorkflowStateDesign,
			expectNewState: WorkflowStateEdit,
			expectCmd:     false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model.workflowState = tt.state
			keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}
			
			_, cmd := model.handleWorkflowKeys(keyMsg)
			
			if model.workflowState != tt.expectNewState {
				t.Errorf("Expected state=%v, got %v", tt.expectNewState, model.workflowState)
			}
			
			hasCmd := cmd != nil
			if hasCmd != tt.expectCmd {
				t.Errorf("Expected command=%v, got command present=%v", tt.expectCmd, hasCmd)
			}
		})
	}
}

func TestSendMessage(t *testing.T) {
	model := createTestDashboardModel()
	
	// Test sending when streaming
	model.streaming = true
	model.inputText = "test message"
	
	cmd := model.sendMessage()
	if cmd != nil {
		t.Error("Should not send message when streaming")
	}
	
	// Test sending empty message
	model.streaming = false
	model.inputText = ""
	
	cmd = model.sendMessage()
	if cmd != nil {
		t.Error("Should not send empty message")
	}
	
	// Test sending valid message
	model.inputText = "hello world"
	initialLength := len(model.chatHistory)
	
	cmd = model.sendMessage()
	if cmd == nil {
		t.Error("Should return command for valid message")
	}
	
	// Check that message was added to history
	if len(model.chatHistory) != initialLength+1 {
		t.Errorf("Expected chat history to increase by 1")
	}
	
	// Check that input was cleared
	if model.inputText != "" {
		t.Errorf("Expected input to be cleared, got '%s'", model.inputText)
	}
	
	// Check message content
	lastMessage := model.chatHistory[len(model.chatHistory)-1]
	if lastMessage.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", lastMessage.Role)
	}
	if lastMessage.Content != "hello world" {
		t.Errorf("Expected content 'hello world', got '%s'", lastMessage.Content)
	}
}

func TestHandleCommand(t *testing.T) {
	model := createTestDashboardModel()
	
	tests := []struct {
		name      string
		command   string
		expectCmd bool
	}{
		{
			name:      "help command",
			command:   "/help",
			expectCmd: false,
		},
		{
			name:      "provider command without args",
			command:   "/provider",
			expectCmd: false, // addSystemMessage returns nil
		},
		{
			name:      "model command without args",
			command:   "/model",
			expectCmd: false, // addSystemMessage returns nil
		},
		{
			name:      "list servers command",
			command:   "/list servers",
			expectCmd: true,
		},
		{
			name:      "status command",
			command:   "/status",
			expectCmd: true,
		},
		{
			name:      "validate command",
			command:   "/validate",
			expectCmd: false, // addSystemMessage returns nil for error case
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := model.handleCommand(tt.command)
			
			hasCmd := cmd != nil
			if hasCmd != tt.expectCmd {
				t.Errorf("Expected command=%v, got command present=%v", tt.expectCmd, hasCmd)
			}
		})
	}
}

func TestSwitchTab(t *testing.T) {
	model := createTestDashboardModel()
	
	// Test tab switching cycle
	model.currentTab = TabChat
	model.switchTab()
	if model.currentTab != TabMonitoring {
		t.Errorf("Expected TabMonitoring, got %v", model.currentTab)
	}
	
	model.switchTab()
	if model.currentTab != TabConfig {
		t.Errorf("Expected TabConfig, got %v", model.currentTab)
	}
	
	model.switchTab()
	if model.currentTab != TabWorkflows {
		t.Errorf("Expected TabWorkflows, got %v", model.currentTab)
	}
	
	model.switchTab()
	if model.currentTab != TabChat {
		t.Errorf("Expected TabChat, got %v", model.currentTab)
	}
}

func TestNavigateWorkflowList(t *testing.T) {
	model := createTestDashboardModel()
	
	// Test with empty workflow list
	result := model.navigateWorkflowList(1)
	if result != model {
		t.Error("Expected model to be returned")
	}
	
	// Add test workflows
	model.workflows = []WorkflowInfo{
		{Name: "workflow-1"},
		{Name: "workflow-2"},
		{Name: "workflow-3"},
	}
	
	// Test navigation down
	model.selectedWorkflow = "workflow-1"
	model.navigateWorkflowList(1)
	if model.selectedWorkflow != "workflow-2" {
		t.Errorf("Expected workflow-2, got %s", model.selectedWorkflow)
	}
	
	// Test navigation up
	model.navigateWorkflowList(-1)
	if model.selectedWorkflow != "workflow-1" {
		t.Errorf("Expected workflow-1, got %s", model.selectedWorkflow)
	}
	
	// Test wrapping down
	model.selectedWorkflow = "workflow-3"
	model.navigateWorkflowList(1)
	if model.selectedWorkflow != "workflow-1" {
		t.Errorf("Expected workflow-1 (wrapped), got %s", model.selectedWorkflow)
	}
	
	// Test wrapping up
	model.selectedWorkflow = "workflow-1"
	model.navigateWorkflowList(-1)
	if model.selectedWorkflow != "workflow-3" {
		t.Errorf("Expected workflow-3 (wrapped), got %s", model.selectedWorkflow)
	}
}

func TestMessageUpdate(t *testing.T) {
	model := createTestDashboardModel()
	
	// Test window size message
	windowMsg := tea.WindowSizeMsg{Width: 100, Height: 50}
	_, cmd := model.Update(windowMsg)
	
	if model.width != 100 || model.height != 50 {
		t.Errorf("Expected dimensions 100x50, got %dx%d", model.width, model.height)
	}
	if cmd != nil {
		t.Error("Window size message should not return command")
	}
	
	// Test config list message
	configs := []ConfigResource{
		{Name: "test-config", Kind: "MCPServer", Valid: true},
	}
	configMsg := configListMsg(configs)
	_, cmd = model.Update(configMsg)
	
	if cmd != nil {
		t.Error("Config list message should not return command")
	}
	
	// Test workflow list message
	workflows := []WorkflowInfo{
		{Name: "test-workflow", Phase: "Running"},
	}
	workflowMsg := workflowListMsg(workflows)
	_, cmd = model.Update(workflowMsg)
	
	if len(model.workflows) != 1 {
		t.Errorf("Expected 1 workflow, got %d", len(model.workflows))
	}
	if model.workflows[0].Name != "test-workflow" {
		t.Errorf("Expected test-workflow, got %s", model.workflows[0].Name)
	}
	if cmd != nil {
		t.Error("Workflow list message should not return command")
	}
	
	// Test error message
	errorMsg := errorMsg(fmt.Errorf("test error"))
	_, cmd = model.Update(errorMsg)
	
	if model.err == nil || model.err.Error() != "test error" {
		t.Errorf("Expected error 'test error', got %v", model.err)
	}
	if cmd != nil {
		t.Error("Error message should not return command")
	}
}

func TestChatStreamHandling(t *testing.T) {
	model := createTestDashboardModel()
	
	// Add an AI message to chat history
	model.chatHistory = append(model.chatHistory, ChatMessage{
		Role:      "assistant",
		Content:   "",
		Streaming: true,
	})
	
	// Test stream message
	stream := make(chan ai.StreamResponse, 1)
	stream <- ai.StreamResponse{Content: "Hello", Finished: false}
	
	streamMsg := chatStreamMsg{
		StreamResponse: ai.StreamResponse{Content: "Hello", Finished: false},
		stream:        stream,
	}
	
	model.streaming = true
	_, cmd := model.Update(streamMsg)
	
	// Check that content was added to last message
	if len(model.chatHistory) == 0 {
		t.Fatal("Expected chat history to have messages")
	}
	
	lastMessage := &model.chatHistory[len(model.chatHistory)-1]
	if !strings.Contains(lastMessage.Content, "Hello") {
		t.Errorf("Expected message content to contain 'Hello', got '%s'", lastMessage.Content)
	}
	
	if cmd == nil {
		t.Error("Expected command to continue streaming")
	}
	
	// Test completion message
	completeMsg := chatCompleteMsg{}
	_, cmd = model.Update(completeMsg)
	
	if model.streaming {
		t.Error("Expected streaming to be false after completion")
	}
	
	if cmd != nil {
		t.Error("Complete message should not return command")
	}
}

func TestGetHelpText(t *testing.T) {
	model := createTestDashboardModel()
	
	helpText := model.getHelpText()
	
	expectedSections := []string{
		"Provider Commands:",
		"Configuration Commands:",
		"Examples:",
		"Keyboard Shortcuts:",
		"/generate-config",
		"/validate-config",
		"/suggest-workflow",
		"/optimize-config",
		"/explain-config",
	}
	
	for _, section := range expectedSections {
		if !strings.Contains(helpText, section) {
			t.Errorf("Help text should contain '%s'", section)
		}
	}
}

// Integration test for full message flow
func TestFullMessageFlow(t *testing.T) {
	model := createTestDashboardModel()
	
	// Set up initial state
	model.inputText = "/generate-config mcpserver test-server"
	
	// Send message
	cmd := model.sendMessage()
	// For configuration commands, streaming might be started without returning a command
	if cmd == nil && !model.streaming {
		t.Fatal("Expected command from sendMessage or streaming to be started")
	}
	
	// Check that input was cleared
	if model.inputText != "" {
		t.Error("Expected input to be cleared")
	}
	
	// Check that message was added to history
	if len(model.chatHistory) == 0 {
		t.Fatal("Expected message in chat history")
	}
	
	// Find the user message (might not be the last one if a system message was added)
	var userMessage *ChatMessage
	for i := range model.chatHistory {
		if model.chatHistory[i].Role == "user" {
			userMessage = &model.chatHistory[i]
		}
	}
	
	if userMessage == nil {
		t.Fatal("Expected user message in chat history")
	}
	
	if !strings.Contains(userMessage.Content, "/generate-config") {
		t.Error("Expected message to contain command")
	}
}

// Benchmark tests
func BenchmarkHandleKeyMsg(b *testing.B) {
	model := createTestDashboardModel()
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.handleKeyMsg(keyMsg)
	}
}

func BenchmarkSendMessage(b *testing.B) {
	model := createTestDashboardModel()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.inputText = "test message"
		model.sendMessage()
	}
}

func BenchmarkUpdate(b *testing.B) {
	model := createTestDashboardModel()
	windowMsg := tea.WindowSizeMsg{Width: 100, Height: 50}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.Update(windowMsg)
	}
}