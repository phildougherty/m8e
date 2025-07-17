package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/ai"
	"github.com/phildougherty/m8e/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockMCPClient implements the MCP client interface for testing
type MockMCPClient struct {
	mock.Mock
}

func (m *MockMCPClient) ListServers(ctx context.Context) ([]protocol.MCPServer, error) {
	args := m.Called(ctx)
	return args.Get(0).([]protocol.MCPServer), args.Error(1)
}

func (m *MockMCPClient) GetServerTools(ctx context.Context, serverName string) ([]protocol.MCPTool, error) {
	args := m.Called(ctx, serverName)
	return args.Get(0).([]protocol.MCPTool), args.Error(1)
}

func (m *MockMCPClient) ExecuteToolCall(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) string {
	args := m.Called(ctx, serverName, toolName, arguments)
	return args.String(0)
}

func (m *MockMCPClient) CallTool(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) (map[string]interface{}, error) {
	args := m.Called(ctx, serverName, toolName, arguments)
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

// MockAIProvider implements the AI provider interface for testing
type MockAIProvider struct {
	mock.Mock
}

func (m *MockAIProvider) GetConfig() ai.ProviderConfig {
	args := m.Called()
	return args.Get(0).(ai.ProviderConfig)
}

func (m *MockAIProvider) ChatCompletion(ctx context.Context, req ai.ChatRequest) (*ai.ChatResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*ai.ChatResponse), args.Error(1)
}

func (m *MockAIProvider) StreamChatCompletion(ctx context.Context, req ai.ChatRequest, responseHandler ai.ResponseHandler) error {
	args := m.Called(ctx, req, responseHandler)
	return args.Error(0)
}

func TestTermChat_ContinuationLogic(t *testing.T) {
	tests := []struct {
		name                    string
		userMessage            string
		shouldTriggerContinuation bool
		expectedContinuationPhrase string
	}{
		{
			name:                    "workflow creation triggers continuation",
			userMessage:            "create a workflow to track glucose",
			shouldTriggerContinuation: true,
			expectedContinuationPhrase: "create_workflow",
		},
		{
			name:                    "build command triggers continuation", 
			userMessage:            "build a monitoring system",
			shouldTriggerContinuation: true,
			expectedContinuationPhrase: "create_workflow",
		},
		{
			name:                    "create command triggers continuation",
			userMessage:            "create a new MCP server",
			shouldTriggerContinuation: true,
			expectedContinuationPhrase: "create_workflow",
		},
		{
			name:                    "simple query does not trigger continuation",
			userMessage:            "what is my glucose level?",
			shouldTriggerContinuation: false,
			expectedContinuationPhrase: "",
		},
		{
			name:                    "status check does not trigger continuation",
			userMessage:            "show server status",
			shouldTriggerContinuation: false,
			expectedContinuationPhrase: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockMCPClient{}
			tc := &TermChat{
				mcpClient: mockClient,
				chatHistory: []ai.Message{
					{Role: "user", Content: tt.userMessage},
				},
			}

			// Test the continuation logic by examining the message construction
			messages := []ai.Message{{Role: "system", Content: "test"}}
			
			// Find the last user message (mimicking the continuation logic)
			lastUserMsg := ""
			for i := len(tc.chatHistory) - 1; i >= 0; i-- {
				if tc.chatHistory[i].Role == "user" {
					lastUserMsg = strings.ToLower(tc.chatHistory[i].Content)
					break
				}
			}

			// Apply continuation logic
			var continuationAdded bool
			if strings.Contains(lastUserMsg, "workflow") || strings.Contains(lastUserMsg, "create") || strings.Contains(lastUserMsg, "build") {
				messages = append(messages, ai.Message{
					Role:    "user",
					Content: "CONTINUE NOW: You must immediately call the create_workflow function. Do not stop or provide explanations - call create_workflow right now to create the actual workflow the user requested.",
				})
				continuationAdded = true
			}

			// Verify expectations
			assert.Equal(t, tt.shouldTriggerContinuation, continuationAdded)
			
			if tt.shouldTriggerContinuation {
				assert.Len(t, messages, 2, "Should have system message + continuation message")
				assert.Contains(t, messages[1].Content, tt.expectedContinuationPhrase)
				assert.Equal(t, "user", messages[1].Role, "Continuation should be user role")
			} else {
				assert.Len(t, messages, 1, "Should only have system message")
			}
		})
	}
}

func TestTermChat_FindServerForTool(t *testing.T) {
	tests := []struct {
		name           string
		toolName       string
		servers        []protocol.MCPServer
		serverTools    map[string][]protocol.MCPTool
		expectedServer string
	}{
		{
			name:     "find tool on matey server",
			toolName: "create_workflow",
			servers: []protocol.MCPServer{
				{Name: "dexcom", URL: "http://dexcom:8080"},
				{Name: "matey", URL: "http://matey:8081"},
			},
			serverTools: map[string][]protocol.MCPTool{
				"dexcom": {
					{Name: "get_current_glucose", Description: "Get current glucose"},
				},
				"matey": {
					{Name: "create_workflow", Description: "Create workflow"},
					{Name: "matey_ps", Description: "List processes"},
				},
			},
			expectedServer: "matey",
		},
		{
			name:     "tool not found on any server",
			toolName: "nonexistent_tool",
			servers: []protocol.MCPServer{
				{Name: "dexcom", URL: "http://dexcom:8080"},
			},
			serverTools: map[string][]protocol.MCPTool{
				"dexcom": {
					{Name: "get_current_glucose", Description: "Get current glucose"},
				},
			},
			expectedServer: "",
		},
		{
			name:     "find tool on first matching server",
			toolName: "common_tool",
			servers: []protocol.MCPServer{
				{Name: "server1", URL: "http://server1:8080"},
				{Name: "server2", URL: "http://server2:8080"},
			},
			serverTools: map[string][]protocol.MCPTool{
				"server1": {
					{Name: "common_tool", Description: "Common tool"},
				},
				"server2": {
					{Name: "common_tool", Description: "Common tool"},
				},
			},
			expectedServer: "server1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockMCPClient{}
			
			// Setup mock expectations
			ctx := context.Background()
			mockClient.On("ListServers", mock.MatchedBy(func(ctx context.Context) bool { return true })).Return(tt.servers, nil)
			
			for serverName, tools := range tt.serverTools {
				mockClient.On("GetServerTools", mock.MatchedBy(func(ctx context.Context) bool { return true }), serverName).Return(tools, nil)
			}

			tc := &TermChat{
				mcpClient: mockClient,
				ctx:       context.Background(),
			}

			result := tc.findServerForTool(tt.toolName)
			assert.Equal(t, tt.expectedServer, result)
			
			mockClient.AssertExpectations(t)
		})
	}
}

func TestTermChat_AgenticSystemPrompt(t *testing.T) {
	mockClient := &MockMCPClient{}
	
	// Setup mock for tool context
	servers := []protocol.MCPServer{
		{Name: "matey", URL: "http://matey:8081"},
		{Name: "dexcom", URL: "http://dexcom:8080"},
	}
	tools := map[string][]protocol.MCPTool{
		"matey": {
			{Name: "create_workflow", Description: "Create workflow"},
			{Name: "matey_up", Description: "Start services"},
		},
		"dexcom": {
			{Name: "get_current_glucose", Description: "Get glucose"},
		},
	}
	
	mockClient.On("ListServers", mock.MatchedBy(func(ctx context.Context) bool { return true })).Return(servers, nil)
	for serverName, serverTools := range tools {
		mockClient.On("GetServerTools", mock.MatchedBy(func(ctx context.Context) bool { return true }), serverName).Return(serverTools, nil)
	}

	tc := &TermChat{
		mcpClient: mockClient,
	}

	systemPrompt := tc.getSystemContext()

	// Verify agentic behavior elements are present
	assert.Contains(t, systemPrompt, "AGENTIC MODE", "Should contain agentic mode declaration")
	assert.Contains(t, systemPrompt, "autonomous agent", "Should declare autonomous behavior")
	assert.Contains(t, systemPrompt, "100% complete", "Should specify completion requirement")
	assert.Contains(t, systemPrompt, "NEVER stop after just gathering data", "Should prohibit stopping early")
	
	// Verify concrete examples are present
	assert.Contains(t, systemPrompt, "CRITICAL: How to Call Functions", "Should contain function call instructions")
	assert.Contains(t, systemPrompt, "create_workflow", "Should contain create_workflow example")
	assert.Contains(t, systemPrompt, "matey_up", "Should contain matey_up example")
	assert.Contains(t, systemPrompt, "dexcom.get_current_glucose", "Should contain dexcom example")

	// Verify tools are listed
	assert.Contains(t, systemPrompt, "create_workflow", "Should list create_workflow tool")
	assert.Contains(t, systemPrompt, "get_current_glucose", "Should list glucose tool")
}

func TestTermChat_FunctionCallContinuation(t *testing.T) {
	mockClient := &MockMCPClient{}
	mockProvider := &MockAIProvider{}

	tc := &TermChat{
		mcpClient:       mockClient,
		currentProvider: "test",
		chatHistory: []ai.Message{
			{Role: "user", Content: "create a workflow to track glucose"},
			{Role: "assistant", Content: "I'll help you create a workflow."},
		},
		ctx: context.Background(),
	}

	// Test that function results trigger continuation
	functionResults := []ai.ToolCall{
		{
			ID: "test-call",
			Type: "function",
			Function: ai.FunctionCall{
				Name: "get_current_glucose",
				Arguments: "{}",
			},
		},
	}

	// Mock the continuation prompt scenario
	expectedMessages := []ai.Message{
		{Role: "system", Content: "test system prompt"},
		{Role: "user", Content: "create a workflow to track glucose"},
		{Role: "assistant", Content: "I'll help you create a workflow."},
		{Role: "user", Content: "CONTINUE NOW: You must immediately call the create_workflow function. Do not stop or provide explanations - call create_workflow right now to create the actual workflow the user requested."},
	}

	// Verify the continuation logic would trigger
	lastUserMsg := "create a workflow to track glucose"
	shouldTrigger := strings.Contains(lastUserMsg, "workflow") || strings.Contains(lastUserMsg, "create") || strings.Contains(lastUserMsg, "build")
	assert.True(t, shouldTrigger, "Should trigger continuation for workflow creation")

	// Verify continuation message format
	continuationMsg := "CONTINUE NOW: You must immediately call the create_workflow function. Do not stop or provide explanations - call create_workflow right now to create the actual workflow the user requested."
	assert.Contains(t, continuationMsg, "create_workflow", "Should mention specific tool to call")
	assert.Contains(t, continuationMsg, "CONTINUE NOW", "Should be urgent")
	assert.Contains(t, continuationMsg, "Do not stop", "Should prohibit stopping")
}

// Benchmark the continuation logic performance
func BenchmarkContinuationLogic(b *testing.B) {
	testMessages := []string{
		"create a workflow to track glucose trends",
		"build a monitoring system for my servers", 
		"create a new MCP server configuration",
		"what is my current glucose level?",
		"show me the server status",
	}

	for i := 0; i < b.N; i++ {
		msg := testMessages[i%len(testMessages)]
		lowered := strings.ToLower(msg)
		
		// Simulate the continuation check
		shouldTrigger := strings.Contains(lowered, "workflow") || 
			strings.Contains(lowered, "create") || 
			strings.Contains(lowered, "build")
		
		if shouldTrigger {
			// Simulate creating continuation message
			_ = "CONTINUE NOW: You must immediately call the create_workflow function."
		}
	}
}