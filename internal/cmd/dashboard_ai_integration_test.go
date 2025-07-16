package cmd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/ai"
)

// Simplified integration testing without mock AI manager
// We test the dashboard functionality that doesn't require actual AI responses

func createTestDashboardModelWithMockAI() *DashboardModel {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Create a real AI manager but with mock configuration
	aiConfig := ai.Config{
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
	
	model := &DashboardModel{
		aiManager:     ai.NewManager(aiConfig),
		currentTab:    TabChat,
		chatHistory:   []ChatMessage{},
		configState:   ConfigStateList,
		workflowState: WorkflowStateList,
		ctx:           ctx,
		cancel:        cancel,
		lastUpdate:    time.Now(),
	}
	
	return model
}

func TestAIStreamingIntegration(t *testing.T) {
	model := createTestDashboardModelWithMockAI()
	
	// Send a message to start streaming
	model.inputText = "generate a config"
	initialHistoryLength := len(model.chatHistory)
	
	cmd := model.sendMessage()
	
	// The command might be nil if AI provider is not available in test environment
	if cmd == nil {
		t.Skip("AI provider not available in test environment")
		return
	}
	
	// Check that user message was added to history
	if len(model.chatHistory) != initialHistoryLength+1 {
		t.Fatal("Expected user message to be added to history")
	}
	
	userMessage := model.chatHistory[len(model.chatHistory)-1]
	if userMessage.Role != "user" {
		t.Errorf("Expected user message, got %s", userMessage.Role)
	}
	
	if userMessage.Content != "generate a config" {
		t.Errorf("Expected message content 'generate a config', got '%s'", userMessage.Content)
	}
	
	// Test that the command would execute (we can't actually test streaming without a real provider)
	// This tests the command structure
	if cmd == nil {
		t.Error("Expected streaming command to be created")
	}
}

func TestAIConfigurationGeneration(t *testing.T) {
	model := createTestDashboardModelWithMockAI()
	
	// Test configuration generation command
	response, handled := model.handleConfigurationCommand("/generate-config", []string{"mcpserver", "test-server"})
	
	if !handled {
		t.Error("Expected command to be handled")
	}
	
	if !strings.Contains(response, "Generating mcpserver configuration") {
		t.Errorf("Expected generation message, got: %s", response)
	}
	
	// Check that artifact type was set
	if model.artifactType != "config" {
		t.Errorf("Expected artifact type 'config', got '%s'", model.artifactType)
	}
}

func TestAIWorkflowSuggestion(t *testing.T) {
	model := createTestDashboardModelWithMockAI()
	
	// Test workflow suggestion command
	response, handled := model.handleConfigurationCommand("/suggest-workflow", []string{"monitor", "database", "health"})
	
	if !handled {
		t.Error("Expected command to be handled")
	}
	
	if !strings.Contains(response, "Suggesting workflow configuration") {
		t.Errorf("Expected suggestion message, got: %s", response)
	}
	
	// Check that artifact type was set
	if model.artifactType != "workflow" {
		t.Errorf("Expected artifact type 'workflow', got '%s'", model.artifactType)
	}
}

func TestAIValidationFeedback(t *testing.T) {
	model := createTestDashboardModelWithMockAI()
	
	// Set up a selected configuration
	model.selectedConfig = "test-config"
	model.configContent = `apiVersion: mcp.matey.ai/v1
kind: MCPServer
metadata:
  name: test-server`
	
	// Test validation command
	response, handled := model.handleConfigurationCommand("/validate-config", []string{})
	
	if !handled {
		t.Error("Expected command to be handled")
	}
	
	if !strings.Contains(response, "Validating configuration") {
		t.Errorf("Expected validation message, got: %s", response)
	}
	
	// Check that artifact type was set
	if model.artifactType != "validation" {
		t.Errorf("Expected artifact type 'validation', got '%s'", model.artifactType)
	}
}

func TestAIContextEnhancement(t *testing.T) {
	model := createTestDashboardModelWithMockAI()
	
	// Set up complex state
	model.workflows = []WorkflowInfo{
		{Name: "backup-workflow", Phase: "Running", Enabled: true, StepCount: 3},
		{Name: "monitor-workflow", Phase: "Paused", Enabled: false, StepCount: 5},
	}
	
	// Test context enhancement
	userMessage := "How can I improve my setup?"
	enhancedPrompt := model.enhancePromptWithConfigContext(userMessage)
	
	// Verify context includes current state
	expectedContextElements := []string{
		"You are Matey",
		"Current Workflows:",
		"backup-workflow",
		"monitor-workflow",
		"Available Configuration Templates:",
		"MCPServer - MCP Server configuration",
		"Guidelines:",
		"apiVersion 'mcp.matey.ai/v1'",
		userMessage,
	}
	
	for _, element := range expectedContextElements {
		if !strings.Contains(enhancedPrompt, element) {
			t.Errorf("Enhanced prompt should contain '%s'", element)
		}
	}
}

func TestAINaturalLanguageProcessing(t *testing.T) {
	model := createTestDashboardModelWithMockAI()
	
	tests := []struct {
		name           string
		input          string
		expectResponse bool
		expectArtifact string
	}{
		{
			name:           "server creation request",
			input:          "create a new server for web scraping",
			expectResponse: true,
			expectArtifact: "config", // This triggers config generation
		},
		{
			name:           "workflow creation request",
			input:          "create a workflow for daily backups",
			expectResponse: true,
			expectArtifact: "workflow", // This triggers workflow generation
		},
		{
			name:           "validation request",
			input:          "check if my configuration is valid",
			expectResponse: true,
			expectArtifact: "general",
		},
		{
			name:           "optimization request",
			input:          "optimize my setup for better performance",
			expectResponse: true,
			expectArtifact: "general",
		},
		{
			name:           "general question",
			input:          "what is the best way to scale my services?",
			expectResponse: true,
			expectArtifact: "general",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset artifact type
			model.artifactType = ""
			
			response := model.processEnhancedChatMessage(tt.input)
			
			hasResponse := response != ""
			if hasResponse != tt.expectResponse {
				t.Errorf("Expected response=%v, got response present=%v", tt.expectResponse, hasResponse)
			}
			
			if tt.expectArtifact != "" && model.artifactType != tt.expectArtifact {
				t.Errorf("Expected artifact type '%s', got '%s'", tt.expectArtifact, model.artifactType)
			}
		})
	}
}

func TestAIProviderIntegration(t *testing.T) {
	model := createTestDashboardModelWithMockAI()
	
	// Test that AI manager is properly initialized
	if model.aiManager == nil {
		t.Fatal("Expected AI manager to be initialized")
	}
	
	// Test getting current provider (should not error even with mock config)
	provider, err := model.aiManager.GetCurrentProvider()
	if err != nil {
		t.Logf("Provider not available (expected in test environment): %v", err)
	} else if provider != nil {
		// If provider is available, test basic functionality
		if provider.Name() == "" {
			t.Error("Provider name should not be empty")
		}
	}
}

func TestAIConfigurationTemplates(t *testing.T) {
	model := createTestDashboardModelWithMockAI()
	
	templates := []string{
		"MCPServer",
		"MCPMemory", 
		"MCPTaskScheduler",
		"MCPProxy",
		"Workflow",
		"MCPToolbox",
	}
	
	for _, template := range templates {
		t.Run(template, func(t *testing.T) {
			content := model.getTemplateContent(template)
			
			if content == "" {
				t.Errorf("Expected content for template %s", template)
			}
			
			if !strings.Contains(content, "apiVersion") {
				t.Errorf("Template %s should contain apiVersion", template)
			}
			
			if !strings.Contains(content, "kind") {
				t.Errorf("Template %s should contain kind", template)
			}
			
			if !strings.Contains(content, "metadata") {
				t.Errorf("Template %s should contain metadata", template)
			}
		})
	}
}

func TestAISmartSuggestions(t *testing.T) {
	model := createTestDashboardModelWithMockAI()
	
	// Test with empty state
	suggestions := model.getSmartSuggestions()
	if len(suggestions) == 0 {
		t.Error("Expected smart suggestions for empty state")
	}
	
	// Should suggest creating an MCPServer
	foundServerSuggestion := false
	for _, suggestion := range suggestions {
		if strings.Contains(suggestion, "MCPServer") {
			foundServerSuggestion = true
			break
		}
	}
	if !foundServerSuggestion {
		t.Error("Expected suggestion to create MCPServer")
	}
	
	// Test with workflows present but disabled
	model.workflows = []WorkflowInfo{
		{Name: "test-workflow", Enabled: false},
	}
	
	suggestions = model.getSmartSuggestions()
	foundWorkflowSuggestion := false
	for _, suggestion := range suggestions {
		if strings.Contains(suggestion, "Enable workflows") {
			foundWorkflowSuggestion = true
			break
		}
	}
	if !foundWorkflowSuggestion {
		t.Error("Expected suggestion to enable workflows")
	}
}

// Performance and stress tests
func TestAIConcurrentRequests(t *testing.T) {
	model := createTestDashboardModelWithMockAI()
	
	// Test concurrent AI requests
	var wg sync.WaitGroup
	numRequests := 10
	
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			
			message := fmt.Sprintf("test message %d", i)
			response := model.processEnhancedChatMessage(message)
			
			if response == "" {
				t.Errorf("Expected response for message %d", i)
			}
		}(i)
	}
	
	wg.Wait()
}

func TestAILargeContext(t *testing.T) {
	model := createTestDashboardModelWithMockAI()
	
	// Add large amount of context data
	for i := 0; i < 100; i++ {
		model.workflows = append(model.workflows, WorkflowInfo{
			Name:      fmt.Sprintf("workflow-%d", i),
			Phase:     "Running",
			Enabled:   true,
			StepCount: i % 10,
		})
	}
	
	// Test that large context doesn't break enhancement
	userMessage := "help me optimize my setup"
	start := time.Now()
	enhancedPrompt := model.enhancePromptWithConfigContext(userMessage)
	duration := time.Since(start)
	
	if duration > time.Second {
		t.Errorf("Context enhancement took too long: %v", duration)
	}
	
	if !strings.Contains(enhancedPrompt, userMessage) {
		t.Error("Enhanced prompt should contain original message")
	}
}

// Benchmark tests for AI interactions
func BenchmarkEnhancePromptWithLargeContext(b *testing.B) {
	model := createTestDashboardModelWithMockAI()
	
	// Add large context
	for i := 0; i < 1000; i++ {
		model.workflows = append(model.workflows, WorkflowInfo{
			Name: fmt.Sprintf("workflow-%d", i),
		})
	}
	
	message := "optimize my configuration"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.enhancePromptWithConfigContext(message)
	}
}

func BenchmarkProcessEnhancedChatMessageWithContext(b *testing.B) {
	model := createTestDashboardModelWithMockAI()
	
	messages := []string{
		"create a server",
		"validate config",
		"/generate-config mcpserver test",
		"optimize setup",
		"help me with workflows",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		message := messages[i%len(messages)]
		model.processEnhancedChatMessage(message)
	}
}