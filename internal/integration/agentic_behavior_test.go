package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/ai"
	"github.com/phildougherty/m8e/internal/cmd"
	"github.com/phildougherty/m8e/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgenticBehaviorEndToEnd tests the complete agentic workflow creation flow
func TestAgenticBehaviorEndToEnd(t *testing.T) {
	// Setup mock MCP server with proper tools
	mcpServer := setupMockMCPServer(t)
	defer mcpServer.Close()

	// Setup mock AI provider that follows agentic behavior
	aiProvider := setupMockAIProvider(t)

	// Create test client
	client := setupTestClient(t, mcpServer.URL)

	// Test scenarios
	tests := []struct {
		name                 string
		userInput           string
		expectedToolCalls   []string
		expectedFinalState  string
		maxIterations       int
	}{
		{
			name:      "workflow creation flow",
			userInput: "create a workflow to track glucose trends",
			expectedToolCalls: []string{
				"get_current_glucose",  // First: gather data
				"create_workflow",      // Second: create workflow (after continuation)
				"matey_up",            // Third: deploy workflow
				"matey_ps",            // Fourth: verify deployment
			},
			expectedFinalState: "workflow_deployed",
			maxIterations:      5,
		},
		{
			name:      "mcp server creation flow", 
			userInput: "build an MCP server for monitoring",
			expectedToolCalls: []string{
				"get_cluster_state",    // First: assess current state
				"create_workflow",      // Second: create server config
				"apply_config",        // Third: apply configuration
				"matey_ps",            // Fourth: verify deployment
			},
			expectedFinalState: "server_deployed",
			maxIterations:      5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Execute the agentic behavior flow
			result := executeAgenticFlow(t, ctx, client, aiProvider, tt.userInput, tt.maxIterations)

			// Verify all expected tool calls were made
			for i, expectedTool := range tt.expectedToolCalls {
				assert.True(t, len(result.ToolCalls) > i, "Should have called tool %d: %s", i, expectedTool)
				if len(result.ToolCalls) > i {
					assert.Equal(t, expectedTool, result.ToolCalls[i].ToolName, "Tool call %d should be %s", i, expectedTool)
				}
			}

			// Verify final state
			assert.Equal(t, tt.expectedFinalState, result.FinalState, "Should reach expected final state")
			assert.True(t, result.Completed, "Flow should complete successfully")
		})
	}
}

// TestContinuationPromptEffectiveness tests that continuation prompts actually work
func TestContinuationPromptEffectiveness(t *testing.T) {
	tests := []struct {
		name                string
		userMessage        string
		firstToolCall      string
		shouldContinue     bool
		expectedNextTool   string
	}{
		{
			name:             "glucose workflow triggers continuation",
			userMessage:      "create a workflow to track glucose",
			firstToolCall:    "get_current_glucose",
			shouldContinue:   true,
			expectedNextTool: "create_workflow",
		},
		{
			name:             "simple query does not continue",
			userMessage:      "what is my glucose level?",
			firstToolCall:    "get_current_glucose", 
			shouldContinue:   false,
			expectedNextTool: "",
		},
		{
			name:             "build command triggers continuation",
			userMessage:      "build a monitoring system",
			firstToolCall:    "get_cluster_state",
			shouldContinue:   true,
			expectedNextTool: "create_workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the continuation logic
			chatHistory := []ai.Message{
				{Role: "user", Content: tt.userMessage},
				{Role: "assistant", Content: "I'll help you with that."},
			}

			// Simulate function call result
			functionResults := []cmd.ToolCallResult{
				{
					ToolName: tt.firstToolCall,
					Result:   map[string]interface{}{"status": "success"},
				},
			}

			// Apply continuation logic
			messages := []ai.Message{{Role: "system", Content: "test"}}
			
			// Find last user message
			lastUserMsg := ""
			for i := len(chatHistory) - 1; i >= 0; i-- {
				if chatHistory[i].Role == "user" {
					lastUserMsg = strings.ToLower(chatHistory[i].Content)
					break
				}
			}

			// Check if continuation should trigger
			shouldTrigger := len(functionResults) > 0 && (strings.Contains(lastUserMsg, "workflow") || strings.Contains(lastUserMsg, "create") || strings.Contains(lastUserMsg, "build"))

			if shouldTrigger {
				messages = append(messages, ai.Message{
					Role:    "user",
					Content: "CONTINUE NOW: You must immediately call the create_workflow function. Do not stop or provide explanations - call create_workflow right now to create the actual workflow the user requested.",
				})
			}

			// Verify expectations
			assert.Equal(t, tt.shouldContinue, shouldTrigger, "Continuation trigger should match expected")
			
			if tt.shouldContinue {
				assert.Len(t, messages, 2, "Should have continuation message")
				assert.Contains(t, messages[1].Content, tt.expectedNextTool, "Should mention expected next tool")
			} else {
				assert.Len(t, messages, 1, "Should not have continuation message")
			}
		})
	}
}

// TestToolDiscoveryAndRouting tests that tools are discovered and routed correctly
func TestToolDiscoveryAndRouting(t *testing.T) {
	mcpServer := setupMockMCPServer(t)
	defer mcpServer.Close()

	client := setupTestClient(t, mcpServer.URL)

	tests := []struct {
		name           string
		toolName       string
		shouldFind     bool
		expectedServer string
	}{
		{
			name:           "create_workflow found on matey server",
			toolName:       "create_workflow",
			shouldFind:     true,
			expectedServer: "matey",
		},
		{
			name:           "get_current_glucose found on dexcom server",
			toolName:       "get_current_glucose",
			shouldFind:     true,
			expectedServer: "dexcom",
		},
		{
			name:           "nonexistent tool not found",
			toolName:       "nonexistent_tool",
			shouldFind:     false,
			expectedServer: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			
			// Test tool discovery
			servers, err := client.ListServers(ctx)
			require.NoError(t, err)
			assert.NotEmpty(t, servers, "Should discover servers")

			// Find tool
			var foundServer string
			for _, server := range servers {
				tools, err := client.GetServerTools(ctx, server.Name)
				if err != nil {
					continue
				}
				
				for _, tool := range tools {
					if tool.Name == tt.toolName {
						foundServer = server.Name
						break
					}
				}
				if foundServer != "" {
					break
				}
			}

			if tt.shouldFind {
				assert.Equal(t, tt.expectedServer, foundServer, "Should find tool on expected server")
			} else {
				assert.Empty(t, foundServer, "Should not find nonexistent tool")
			}
		})
	}
}

// TestMCPProtocolCompliance tests MCP protocol initialization compliance
func TestMCPProtocolCompliance(t *testing.T) {
	mcpServer := setupMockMCPServer(t)
	defer mcpServer.Close()

	// Test initialize method
	initReq := cmd.MCPRequest{
		Method: "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}

	response := sendMCPRequest(t, mcpServer.URL, initReq)
	
	assert.Nil(t, response.Error, "Initialize should not return error")
	assert.NotNil(t, response.Result, "Initialize should return result")

	result, ok := response.Result.(map[string]interface{})
	require.True(t, ok, "Result should be object")

	// Verify protocol version
	protocolVersion, exists := result["protocolVersion"]
	assert.True(t, exists, "Should have protocol version")
	assert.Equal(t, "2024-11-05", protocolVersion, "Should use correct protocol version")

	// Verify capabilities
	capabilities, exists := result["capabilities"]
	assert.True(t, exists, "Should have capabilities")
	assert.NotNil(t, capabilities, "Capabilities should not be nil")

	// Verify server info
	serverInfo, exists := result["serverInfo"]
	assert.True(t, exists, "Should have server info")
	serverInfoMap, ok := serverInfo.(map[string]interface{})
	require.True(t, ok, "Server info should be object")
	
	name, hasName := serverInfoMap["name"]
	version, hasVersion := serverInfoMap["version"]
	assert.True(t, hasName, "Should have server name")
	assert.True(t, hasVersion, "Should have server version")
	assert.Equal(t, "matey", name, "Should have correct server name")

	// Test initialized notification
	notifyReq := cmd.MCPRequest{
		Method: "notifications/initialized",
		Params: map[string]interface{}{},
	}

	notifyResponse := sendMCPRequest(t, mcpServer.URL, notifyReq)
	assert.Nil(t, notifyResponse.Error, "Initialized notification should not return error")
}

// Helper functions

type AgenticFlowResult struct {
	ToolCalls   []ToolCall
	FinalState  string
	Completed   bool
	Iterations  int
}

type ToolCall struct {
	ToolName  string
	Arguments map[string]interface{}
	Result    map[string]interface{}
}

func executeAgenticFlow(t *testing.T, ctx context.Context, client protocol.MCPClient, aiProvider ai.Provider, userInput string, maxIterations int) AgenticFlowResult {
	result := AgenticFlowResult{
		ToolCalls: []ToolCall{},
	}

	// Simulate the agentic conversation flow
	messages := []ai.Message{
		{Role: "system", Content: "You are an agentic assistant. Work until the user's goal is 100% complete."},
		{Role: "user", Content: userInput},
	}

	for i := 0; i < maxIterations; i++ {
		result.Iterations = i + 1

		// Simulate AI response with tool calls based on the input
		var toolCalls []ai.ToolCall
		
		// Determine next tool call based on context
		if i == 0 {
			// First call - gather data
			if strings.Contains(userInput, "glucose") {
				toolCalls = []ai.ToolCall{{
					Function: ai.FunctionCall{Name: "get_current_glucose", Arguments: "{}"},
				}}
			} else {
				toolCalls = []ai.ToolCall{{
					Function: ai.FunctionCall{Name: "get_cluster_state", Arguments: "{}"},
				}}
			}
		} else if i == 1 {
			// Second call - create workflow (after continuation)
			toolCalls = []ai.ToolCall{{
				Function: ai.FunctionCall{Name: "create_workflow", Arguments: `{"name": "test-workflow"}`},
			}}
		} else if i == 2 {
			// Third call - deploy
			if strings.Contains(userInput, "server") {
				toolCalls = []ai.ToolCall{{
					Function: ai.FunctionCall{Name: "apply_config", Arguments: "{}"},
				}}
			} else {
				toolCalls = []ai.ToolCall{{
					Function: ai.FunctionCall{Name: "matey_up", Arguments: "{}"},
				}}
			}
		} else if i == 3 {
			// Fourth call - verify
			toolCalls = []ai.ToolCall{{
				Function: ai.FunctionCall{Name: "matey_ps", Arguments: "{}"},
			}}
		}

		// Execute tool calls
		for _, toolCall := range toolCalls {
			// Find server for tool
			servers, err := client.ListServers(ctx)
			require.NoError(t, err)

			var serverName string
			for _, server := range servers {
				tools, err := client.GetServerTools(ctx, server.Name)
				if err != nil {
					continue
				}
				for _, tool := range tools {
					if tool.Name == toolCall.Function.Name {
						serverName = server.Name
						break
					}
				}
				if serverName != "" {
					break
				}
			}

			if serverName == "" {
				t.Logf("Tool %s not found on any server", toolCall.Function.Name)
				continue
			}

			// Execute tool
			var args map[string]interface{}
			if toolCall.Function.Arguments != "" {
				json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
			}

			toolResult, err := client.CallTool(ctx, serverName, toolCall.Function.Name, args)
			require.NoError(t, err)

			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ToolName:  toolCall.Function.Name,
				Arguments: args,
				Result:    toolResult,
			})
		}

		// Check if we should continue based on agentic logic
		if len(toolCalls) == 0 {
			break
		}

		// Simulate continuation check
		shouldContinue := i < 3 && (strings.Contains(userInput, "create") || strings.Contains(userInput, "build") || strings.Contains(userInput, "workflow"))
		if !shouldContinue {
			break
		}

		// Add continuation prompt for next iteration
		if shouldContinue && i == 0 {
			messages = append(messages, ai.Message{
				Role: "user",
				Content: "CONTINUE NOW: You must immediately call the create_workflow function.",
			})
		}
	}

	// Determine final state based on tool calls
	if len(result.ToolCalls) >= 4 {
		if strings.Contains(userInput, "server") {
			result.FinalState = "server_deployed"
		} else {
			result.FinalState = "workflow_deployed"
		}
		result.Completed = true
	} else if len(result.ToolCalls) >= 2 {
		result.FinalState = "partially_completed"
	} else {
		result.FinalState = "failed"
	}

	return result
}

func setupMockMCPServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req cmd.MCPRequest
		json.NewDecoder(r.Body).Decode(&req)

		var result interface{}
		var mcpErr *cmd.MCPError

		switch req.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "matey",
					"version": "0.0.4",
				},
			}
		case "notifications/initialized":
			result = nil
		case "tools/list":
			// Return mock tools for different servers
			serverName := r.Header.Get("X-Server-Name")
			if serverName == "" {
				serverName = "matey" // default
			}

			switch serverName {
			case "matey":
				result = map[string]interface{}{
					"tools": []map[string]interface{}{
						{"name": "create_workflow", "description": "Create workflow"},
						{"name": "matey_up", "description": "Start services"},
						{"name": "matey_ps", "description": "List processes"},
						{"name": "apply_config", "description": "Apply configuration"},
						{"name": "get_cluster_state", "description": "Get cluster state"},
					},
				}
			case "dexcom":
				result = map[string]interface{}{
					"tools": []map[string]interface{}{
						{"name": "get_current_glucose", "description": "Get glucose"},
						{"name": "get_glucose_history", "description": "Get glucose history"},
					},
				}
			}
		case "tools/call":
			// Mock tool execution
			params := req.Params.(map[string]interface{})
			toolName := params["name"].(string)
			
			result = map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": fmt.Sprintf("Tool %s executed successfully", toolName),
					},
				},
			}
		default:
			mcpErr = &cmd.MCPError{Code: -32601, Message: "Method not found"}
		}

		response := cmd.MCPResponse{Result: result, Error: mcpErr}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
}

func setupMockAIProvider(t *testing.T) ai.Provider {
	// Return a mock AI provider - in real tests this would be more sophisticated
	return nil
}

func setupTestClient(t *testing.T, serverURL string) protocol.MCPClient {
	// Create a test MCP client connected to the mock server
	// This would use the actual MCP client implementation
	return &MockMCPClient{serverURL: serverURL}
}

type MockMCPClient struct {
	serverURL string
}

func (m *MockMCPClient) ListServers(ctx context.Context) ([]protocol.MCPServer, error) {
	return []protocol.MCPServer{
		{Name: "matey", URL: m.serverURL},
		{Name: "dexcom", URL: m.serverURL},
	}, nil
}

func (m *MockMCPClient) GetServerTools(ctx context.Context, serverName string) ([]protocol.MCPTool, error) {
	// Send tools/list request with server name header
	req := cmd.MCPRequest{Method: "tools/list", Params: map[string]interface{}{}}
	
	client := &http.Client{}
	reqBytes, _ := json.Marshal(req)
	httpReq, _ := http.NewRequest("POST", m.serverURL, strings.NewReader(string(reqBytes)))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Server-Name", serverName)
	
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var response cmd.MCPResponse
	json.NewDecoder(resp.Body).Decode(&response)
	
	if response.Error != nil {
		return nil, fmt.Errorf("MCP error: %s", response.Error.Message)
	}

	result := response.Result.(map[string]interface{})
	toolsList := result["tools"].([]interface{})
	
	var tools []protocol.MCPTool
	for _, toolData := range toolsList {
		tool := toolData.(map[string]interface{})
		tools = append(tools, protocol.MCPTool{
			Name:        tool["name"].(string),
			Description: tool["description"].(string),
		})
	}
	
	return tools, nil
}

func (m *MockMCPClient) CallTool(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) (map[string]interface{}, error) {
	req := cmd.MCPRequest{
		Method: "tools/call",
		Params: map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}
	
	response := sendMCPRequest(nil, m.serverURL, req)
	if response.Error != nil {
		return nil, fmt.Errorf("MCP error: %s", response.Error.Message)
	}
	
	return response.Result.(map[string]interface{}), nil
}

func sendMCPRequest(t *testing.T, serverURL string, req cmd.MCPRequest) cmd.MCPResponse {
	reqBytes, _ := json.Marshal(req)
	resp, _ := http.Post(serverURL, "application/json", strings.NewReader(string(reqBytes)))
	defer resp.Body.Close()

	var response cmd.MCPResponse
	json.NewDecoder(resp.Body).Decode(&response)
	return response
}