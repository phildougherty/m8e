package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/ai"
)

// MockProvider implements the ai.Provider interface for testing
type MockProvider struct {
	name            string
	available       bool
	supportedModels []string
	defaultModel    string
	responses       map[string][]ai.StreamResponse
	currentResponse []ai.StreamResponse
}

func NewMockProvider(name string) *MockProvider {
	return &MockProvider{
		name:            name,
		available:       true,
		supportedModels: []string{"test-model-1", "test-model-2"},
		defaultModel:    "test-model-1",
		responses:       make(map[string][]ai.StreamResponse),
		currentResponse: []ai.StreamResponse{},
	}
}

func (m *MockProvider) Name() string {
	return m.name
}

func (m *MockProvider) StreamChat(ctx context.Context, messages []ai.Message, options ai.StreamOptions) (<-chan ai.StreamResponse, error) {
	ch := make(chan ai.StreamResponse, 10)
	
	go func() {
		defer close(ch)
		
		// Get the last user message to determine response
		var lastUserMessage string
		for _, msg := range messages {
			if msg.Role == "user" {
				lastUserMessage = msg.Content
			}
		}
		
		// Get predefined response or generate a default one
		responses, exists := m.responses[lastUserMessage]
		if !exists {
			responses = m.currentResponse
			if len(responses) == 0 {
				// Default response
				responses = []ai.StreamResponse{
					{Content: "Mock response for: " + lastUserMessage, Finished: false},
					{Content: "", Finished: true},
				}
			}
		}
		
		for _, response := range responses {
			select {
			case ch <- response:
			case <-ctx.Done():
				return
			}
			
			// Small delay to simulate streaming
			time.Sleep(10 * time.Millisecond)
		}
	}()
	
	return ch, nil
}

func (m *MockProvider) SupportedModels() []string {
	return m.supportedModels
}

func (m *MockProvider) ValidateConfig() error {
	return nil
}

func (m *MockProvider) IsAvailable() bool {
	return m.available
}

func (m *MockProvider) DefaultModel() string {
	return m.defaultModel
}

// SetResponse sets a predefined response for a specific input
func (m *MockProvider) SetResponse(input string, responses []ai.StreamResponse) {
	m.responses[input] = responses
}

// SetCurrentResponse sets the response for any input
func (m *MockProvider) SetCurrentResponse(responses []ai.StreamResponse) {
	m.currentResponse = responses
}

// MockAIManager creates a mock AI manager for testing
func createMockAIManager() *ai.Manager {
	config := ai.Config{
		DefaultProvider: "mock",
		Providers: map[string]ai.ProviderConfig{
			"mock": {
				APIKey:       "test-key",
				DefaultModel: "test-model",
				MaxTokens:    4096,
				Temperature:  0.7,
				Timeout:      30 * time.Second,
			},
		},
		FallbackProviders: []string{},
	}
	
	return ai.NewManager(config)
}

// createTestDashboardModel creates a dashboard model for testing
func createTestDashboardModel() *DashboardModel {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &DashboardModel{
		aiManager:     createMockAIManager(),
		currentTab:    TabChat,
		chatHistory:   []ChatMessage{},
		configState:   ConfigStateList,
		workflowState: WorkflowStateList,
		ctx:           ctx,
		cancel:        cancel,
		lastUpdate:    time.Now(),
	}
}

func TestEnhancePromptWithConfigContext(t *testing.T) {
	model := createTestDashboardModel()
	
	// Add some test configurations
	model.selectedConfig = "test-server"
	
	// Add some test workflows
	model.workflows = []WorkflowInfo{
		{
			Name:      "test-workflow",
			Namespace: "default",
			Phase:     "Running",
			Enabled:   true,
			StepCount: 3,
		},
	}
	
	userMessage := "Create a new MCP server"
	enhancedPrompt := model.enhancePromptWithConfigContext(userMessage)
	
	// Check that the enhanced prompt contains expected context
	if !strings.Contains(enhancedPrompt, "You are Matey") {
		t.Error("Enhanced prompt should contain Matey introduction")
	}
	
	if !strings.Contains(enhancedPrompt, "Current Workflows:") {
		t.Error("Enhanced prompt should contain workflow context")
	}
	
	if !strings.Contains(enhancedPrompt, "test-workflow") {
		t.Error("Enhanced prompt should contain specific workflow info")
	}
	
	if !strings.Contains(enhancedPrompt, "Available Configuration Templates:") {
		t.Error("Enhanced prompt should contain template information")
	}
	
	if !strings.Contains(enhancedPrompt, "MCPServer - MCP Server configuration") {
		t.Error("Enhanced prompt should contain specific template info")
	}
	
	if !strings.Contains(enhancedPrompt, "Guidelines:") {
		t.Error("Enhanced prompt should contain guidelines")
	}
	
	if !strings.Contains(enhancedPrompt, userMessage) {
		t.Error("Enhanced prompt should contain original user message")
	}
}

func TestHandleConfigurationCommand(t *testing.T) {
	model := createTestDashboardModel()
	
	tests := []struct {
		name        string
		command     string
		args        []string
		expectHandled bool
		expectContains string
	}{
		{
			name:           "generate config with valid args",
			command:        "/generate-config",
			args:           []string{"mcpserver", "my-server", "port=8080"},
			expectHandled:  true,
			expectContains: "Generating mcpserver configuration",
		},
		{
			name:           "generate config without args",
			command:        "/generate-config",
			args:           []string{},
			expectHandled:  true,
			expectContains: "Usage: /generate-config",
		},
		{
			name:           "validate config without selection",
			command:        "/validate-config",
			args:           []string{},
			expectHandled:  true,
			expectContains: "No configuration selected",
		},
		{
			name:           "suggest workflow with purpose",
			command:        "/suggest-workflow",
			args:           []string{"monitor", "database", "health"},
			expectHandled:  true,
			expectContains: "Suggesting workflow configuration",
		},
		{
			name:           "suggest workflow without purpose",
			command:        "/suggest-workflow",
			args:           []string{},
			expectHandled:  true,
			expectContains: "Usage: /suggest-workflow",
		},
		{
			name:           "unknown command",
			command:        "/unknown-command",
			args:           []string{},
			expectHandled:  false,
			expectContains: "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, handled := model.handleConfigurationCommand(tt.command, tt.args)
			
			if handled != tt.expectHandled {
				t.Errorf("Expected handled=%v, got %v", tt.expectHandled, handled)
			}
			
			if tt.expectContains != "" && !strings.Contains(response, tt.expectContains) {
				t.Errorf("Expected response to contain '%s', got: %s", tt.expectContains, response)
			}
		})
	}
}

func TestGenerateConfigWithAI(t *testing.T) {
	model := createTestDashboardModel()
	
	tests := []struct {
		name       string
		configType string
		args       []string
		expectType string
		expectName string
	}{
		{
			name:       "basic mcpserver",
			configType: "mcpserver",
			args:       []string{"my-server"},
			expectType: "mcpserver",
			expectName: "my-server",
		},
		{
			name:       "mcpserver with parameters",
			configType: "mcpserver",
			args:       []string{"web-server", "port=8080", "replicas=3"},
			expectType: "mcpserver",
			expectName: "web-server",
		},
		{
			name:       "workflow without name",
			configType: "workflow",
			args:       []string{"schedule=daily"},
			expectType: "workflow",
			expectName: "example-workflow",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, handled := model.generateConfigWithAI(tt.configType, tt.args)
			
			if !handled {
				t.Error("Expected generateConfigWithAI to be handled")
			}
			
			if !strings.Contains(response, tt.expectType) {
				t.Errorf("Expected response to contain config type '%s'", tt.expectType)
			}
		})
	}
}

func TestProcessEnhancedChatMessage(t *testing.T) {
	model := createTestDashboardModel()
	
	tests := []struct {
		name           string
		message        string
		expectResponse bool
		expectContains string
	}{
		{
			name:           "configuration command",
			message:        "/generate-config mcpserver test-server",
			expectResponse: true,
			expectContains: "Generating mcpserver configuration",
		},
		{
			name:           "natural language server creation",
			message:        "create a new server configuration",
			expectResponse: true,
			expectContains: "Generating MCPServer configuration",
		},
		{
			name:           "natural language workflow creation",
			message:        "create a workflow for monitoring",
			expectResponse: true,
			expectContains: "Suggesting workflow configuration",
		},
		{
			name:           "validation request",
			message:        "validate this configuration",
			expectResponse: true,
			expectContains: "Processing your request",
		},
		{
			name:           "optimization request",
			message:        "optimize my configuration",
			expectResponse: true,
			expectContains: "Processing your request",
		},
		{
			name:           "general question",
			message:        "what is kubernetes?",
			expectResponse: true,
			expectContains: "Processing your request",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := model.processEnhancedChatMessage(tt.message)
			
			hasResponse := response != ""
			if hasResponse != tt.expectResponse {
				t.Errorf("Expected response=%v, got response present=%v", tt.expectResponse, hasResponse)
			}
			
			if tt.expectContains != "" && !strings.Contains(response, tt.expectContains) {
				t.Errorf("Expected response to contain '%s', got: %s", tt.expectContains, response)
			}
		})
	}
}

func TestGetSmartSuggestions(t *testing.T) {
	model := createTestDashboardModel()
	
	// Test with empty state
	suggestions := model.getSmartSuggestions()
	if len(suggestions) == 0 {
		t.Error("Expected smart suggestions for empty state")
	}
	
	// Check for specific suggestions
	found := false
	for _, suggestion := range suggestions {
		if strings.Contains(suggestion, "MCPServer") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected suggestion to create MCPServer")
	}
	
	// Test with workflows but none enabled
	model.workflows = []WorkflowInfo{
		{Name: "test-workflow", Enabled: false},
	}
	
	suggestions = model.getSmartSuggestions()
	found = false
	for _, suggestion := range suggestions {
		if strings.Contains(suggestion, "Enable workflows") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected suggestion to enable workflows")
	}
}

func TestConfigurationContextBuilding(t *testing.T) {
	model := createTestDashboardModel()
	
	// Add some test configurations (would be used in real implementation)
	_ = []ConfigResource{
		{
			Name:      "test-server",
			Namespace: "default",
			Kind:      "MCPServer",
			Valid:     true,
			Modified:  time.Now(),
		},
		{
			Name:      "invalid-config",
			Namespace: "default", 
			Kind:      "MCPMemory",
			Valid:     false,
			Errors:    []string{"missing required field"},
			Modified:  time.Now(),
		},
	}
	
	// Mock the config resources (in a real implementation, this would be stored in the model)
	model.selectedConfig = "test-server"
	
	// Add workflows
	model.workflows = []WorkflowInfo{
		{Name: "backup-workflow", Phase: "Running", Enabled: true, StepCount: 5},
		{Name: "monitoring-workflow", Phase: "Paused", Enabled: false, StepCount: 3},
	}
	
	prompt := model.enhancePromptWithConfigContext("help me optimize my setup")
	
	// Since getConfigResources() returns empty slice in tests, we expect no "Current Configurations:" section
	// But we should still have other context sections
	
	if !strings.Contains(prompt, "Current Workflows:") {
		t.Error("Context should include workflow section")
	}
	
	if !strings.Contains(prompt, "backup-workflow") {
		t.Error("Context should include specific workflow names")
	}
	
	if !strings.Contains(prompt, "Available Configuration Templates:") {
		t.Error("Context should include available templates")
	}
	
	if !strings.Contains(prompt, "Guidelines:") {
		t.Error("Context should include guidelines")
	}
}

func TestAIMessageSending(t *testing.T) {
	model := createTestDashboardModel()
	
	// Test sending a message
	cmd := model.sendAIMessage("test message", "config")
	
	// Check that command was returned
	if cmd == nil {
		t.Error("Expected sendAIMessage to return a command")
	}
	
	// Check that streaming is started
	if !model.streaming {
		t.Error("Expected streaming to be started")
	}
	
	// Check artifact type
	if model.artifactType != "config" {
		t.Errorf("Expected artifact type 'config', got '%s'", model.artifactType)
	}
}

// Benchmark tests for performance
func BenchmarkEnhancePromptWithConfigContext(b *testing.B) {
	model := createTestDashboardModel()
	
	// Add some test data
	model.workflows = []WorkflowInfo{
		{Name: "test-workflow-1", Phase: "Running", Enabled: true, StepCount: 3},
		{Name: "test-workflow-2", Phase: "Paused", Enabled: false, StepCount: 5},
	}
	
	message := "Create a new MCP server configuration"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.enhancePromptWithConfigContext(message)
	}
}

func BenchmarkProcessEnhancedChatMessage(b *testing.B) {
	model := createTestDashboardModel()
	
	messages := []string{
		"create a new server",
		"validate my configuration", 
		"optimize the setup",
		"/generate-config mcpserver test",
		"what is the status of my workflows?",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		message := messages[i%len(messages)]
		model.processEnhancedChatMessage(message)
	}
}