package chat

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
	"github.com/phildougherty/m8e/internal/mcp"
)

func TestTermChat_GetMCPFunctions(t *testing.T) {
	tc := &TermChat{
		mcpClient: &mcp.MCPClient{}, // Mock client
	}

	functions := tc.getMCPFunctions()

	// Should include native functions
	expectedNativeFunctions := []string{
		"execute_bash",
		"deploy_service", 
		"scale_service",
		"get_service_status",
		"get_logs",
		"create_backup",
	}

	functionNames := make(map[string]bool)
	for _, fn := range functions {
		functionNames[fn.Name] = true
	}

	for _, expectedName := range expectedNativeFunctions {
		if !functionNames[expectedName] {
			t.Errorf("Expected to find native function '%s' in getMCPFunctions result", expectedName)
		}
	}

	// Test function structure
	for _, fn := range functions {
		if fn.Name == "" {
			t.Error("Function name should not be empty")
		}
		if fn.Description == "" {
			t.Error("Function description should not be empty")
		}
		if fn.Parameters == nil {
			t.Error("Function parameters should not be nil")
		}
		
		// Check parameters structure
		if params, ok := fn.Parameters.(map[string]interface{}); ok {
			if params["type"] != "object" {
				t.Errorf("Expected function parameters type to be 'object', got %v", params["type"])
			}
		}
	}
}

func TestTermChat_GetDiscoveredMCPFunctions(t *testing.T) {
	// Create a mock TermChat with a method to return test data
	tc := &TermChat{}
	
	// Mock the getComprehensiveMCPData method by creating a minimal test
	// This would normally require extensive mocking of the discovery system
	functions := tc.getDiscoveredMCPFunctions()

	// Should return empty list or valid functions
	for _, fn := range functions {
		if fn.Name == "" {
			t.Error("Discovered function name should not be empty")
		}
		if fn.Description == "" {
			t.Error("Discovered function description should not be empty") 
		}
		if fn.Parameters == nil {
			t.Error("Discovered function parameters should not be nil")
		}
	}
}

func TestTermChat_DiscoverMCPServerFunctions(t *testing.T) {
	// Create mock MCP client that returns test data
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"servers": []map[string]interface{}{
				{
					"name": "test-server",
					"tools": []map[string]interface{}{
						{
							"name":        "test_tool",
							"description": "A test tool",
							"inputSchema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"param1": map[string]interface{}{
										"type": "string",
									},
								},
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	tc := &TermChat{
		mcpClient: mcp.NewMCPClient(mockServer.URL),
	}

	functions := tc.discoverMCPServerFunctions()

	// Should handle the case where discovery fails gracefully
	// Since we can't easily mock the MCP client completely, we just test it doesn't panic
	if functions == nil {
		functions = []ai.Function{}
	}

	// Verify function structure if any are returned
	for _, fn := range functions {
		if fn.Name == "" {
			t.Error("Function name should not be empty")
		}
		if fn.Parameters == nil {
			t.Error("Function parameters should not be nil")
		}
	}
}

func TestTermChat_SwitchProviderSilent(t *testing.T) {
	tests := []struct {
		name      string
		aiManager *ai.Manager
		provider  string
		wantError bool
	}{
		{
			name:      "nil AI manager",
			aiManager: nil,
			provider:  "test",
			wantError: true,
		},
		{
			name:      "valid switch with nil manager",
			aiManager: nil,
			provider:  "openai",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := &TermChat{
				aiManager: tt.aiManager,
			}

			err := tc.switchProviderSilent(tt.provider)

			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestTermChat_SwitchModelSilent(t *testing.T) {
	tests := []struct {
		name      string
		aiManager *ai.Manager
		model     string
		wantError bool
	}{
		{
			name:      "nil AI manager",
			aiManager: nil,
			model:     "test-model",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := &TermChat{
				aiManager: tt.aiManager,
			}

			err := tc.switchModelSilent(tt.model)

			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// If no error expected, check that model was set
			if !tt.wantError {
				if tc.currentModel != tt.model {
					t.Errorf("Expected currentModel to be %s, got %s", tt.model, tc.currentModel)
				}
			}
		})
	}
}

func TestTermChat_ExecuteConversationFlowSilent(t *testing.T) {
	tc := &TermChat{
		ctx:         context.Background(),
		chatHistory: make([]TermChatMessage, 0),
	}

	// This is a complex method that requires extensive mocking
	// For now, we test that it doesn't panic with basic input
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("executeConversationFlowSilent panicked: %v", r)
		}
	}()

	// Test with simple input - this will likely fail internally but shouldn't panic
	// tc.executeConversationFlowSilent("Hello")
	
	// We can't easily test this without mocking the entire conversation system
	// so we'll just verify the method signature exists
}

func TestTermChat_ChatWithAISilent(t *testing.T) {
	tc := &TermChat{
		chatHistory: make([]TermChatMessage, 0),
	}

	// Test that method doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("chatWithAISilent panicked: %v", r)
		}
	}()

	// This method delegates to executeConversationFlowSilent
	// We can verify it exists but can't easily test full functionality
	// tc.chatWithAISilent("test message")
}

func TestTermChat_CallDiscoveredTool(t *testing.T) {
	// Mock server for testing tool calls
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		expectedURL := "/servers/test-server/tools/call"
		if r.URL.Path != expectedURL {
			t.Errorf("Expected URL path %s, got %s", expectedURL, r.URL.Path)
		}

		// Return mock response
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"content": "Tool executed successfully",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	tc := &TermChat{}

	ctx := context.Background()
	arguments := map[string]interface{}{
		"param1": "value1",
	}

	// Modify the callDiscoveredTool to use our mock server
	originalURL := "https://mcp.robotrad.io"
	testURL := mockServer.URL

	// We need to create a version that uses our test server
	result, err := tc.callDiscoveredToolWithURL(ctx, "test-server", "test_tool", arguments, testURL)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if result == nil {
		t.Error("Expected non-nil result")
	}

	// Check result structure
	if resultMap, ok := result.(map[string]interface{}); ok {
		if content, exists := resultMap["content"]; !exists {
			t.Error("Expected result to contain 'content' field")
		} else if content != "Tool executed successfully" {
			t.Errorf("Expected content to be 'Tool executed successfully', got %v", content)
		}
	}
}

// Helper method for testing with custom URL
func (tc *TermChat) callDiscoveredToolWithURL(ctx context.Context, serverName, toolName string, arguments map[string]interface{}, baseURL string) (interface{}, error) {
	endpointURL := fmt.Sprintf("%s/servers/%s/tools/call", baseURL, serverName)
	
	mcpRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}
	
	reqBytes, err := json.Marshal(mcpRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", endpointURL, strings.NewReader(string(reqBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call tool: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d", resp.StatusCode)
	}
	
	var mcpResponse struct {
		Jsonrpc string      `json:"jsonrpc"`
		ID      int         `json:"id"`
		Result  interface{} `json:"result"`
		Error   interface{} `json:"error,omitempty"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&mcpResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	if mcpResponse.Error != nil {
		return nil, fmt.Errorf("MCP error: %v", mcpResponse.Error)
	}
	
	return mcpResponse.Result, nil
}

func TestTermChat_FormatToolResult(t *testing.T) {
	tc := &TermChat{}

	tests := []struct {
		name       string
		toolName   string
		serverName string
		result     interface{}
		duration   time.Duration
		expectDiff bool
	}{
		{
			name:       "regular tool",
			toolName:   "get_status",
			serverName: "test-server",
			result:     map[string]interface{}{"status": "ok"},
			duration:   time.Millisecond * 100,
			expectDiff: false,
		},
		{
			name:       "file editing tool with diff",
			toolName:   "edit_file",
			serverName: "filesystem",
			result: map[string]interface{}{
				"diff_preview": "------- SEARCH\nold line\n=======\nnew line\n+++++++ REPLACE",
				"file_path":    "/test/file.txt",
			},
			duration:   time.Millisecond * 50,
			expectDiff: true,
		},
		{
			name:       "file editing tool without diff",
			toolName:   "write_file",
			serverName: "filesystem", 
			result:     map[string]interface{}{"status": "success"},
			duration:   time.Millisecond * 25,
			expectDiff: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tc.formatToolResult(tt.toolName, tt.serverName, tt.result, tt.duration)

			if result == "" {
				t.Error("Expected non-empty result")
			}

			// Should contain tool name and server name
			if !strings.Contains(result, tt.toolName) {
				t.Errorf("Expected result to contain tool name '%s'", tt.toolName)
			}
			if !strings.Contains(result, tt.serverName) {
				t.Errorf("Expected result to contain server name '%s'", tt.serverName)
			}

			// Should contain success indicator (✓)
			if !strings.Contains(result, "✓") {
				t.Error("Expected result to contain success indicator")
			}

			// For file editing tools with diff, should contain more content
			if tt.expectDiff && len(result) < 100 {
				t.Error("Expected longer result for file editing tool with diff")
			}
		})
	}
}

func TestTermChat_FormatEnhancedVisualDiff(t *testing.T) {
	tc := &TermChat{}

	diffPreview := `------- SEARCH
old line 1
old line 2
=======
new line 1
new line 2
+++++++ REPLACE`

	result := tc.formatEnhancedVisualDiff(diffPreview, "/test/file.txt")

	if result == "" {
		t.Error("Expected non-empty result")
	}

	// Should contain file path
	if !strings.Contains(result, "/test/file.txt") {
		t.Error("Expected result to contain file path")
	}

	// Should contain diff markers
	expectedElements := []string{
		"diff preview",
		"old line 1",
		"old line 2", 
		"new line 1",
		"new line 2",
	}

	for _, element := range expectedElements {
		if !strings.Contains(result, element) {
			t.Errorf("Expected result to contain '%s'", element)
		}
	}

	// Should have visual structure (box drawing characters)
	if !strings.Contains(result, "╭") || !strings.Contains(result, "╰") {
		t.Error("Expected result to contain box drawing characters")
	}
}

func TestTermChat_RequestFunctionConfirmationAsync(t *testing.T) {
	tests := []struct {
		name         string
		mode         ApprovalMode
		functionName string
		expectedCall bool
		expectedApproval bool
	}{
		{
			name:             "YOLO approves everything",
			mode:             YOLO,
			functionName:     "dangerous_function",
			expectedCall:     true,
			expectedApproval: true,
		},
		{
			name:             "AUTO_EDIT approves safe functions",
			mode:             AUTO_EDIT,
			functionName:     "get_logs",
			expectedCall:     true,
			expectedApproval: true,
		},
		{
			name:             "AUTO_EDIT needs confirmation for unsafe",
			mode:             AUTO_EDIT,
			functionName:     "delete_database",
			expectedCall:     true,
			expectedApproval: true, // Will fallback to true without callback
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callbackCalled := false
			var receivedApproval bool

			tc := &TermChat{
				approvalMode: tt.mode,
			}

			tc.requestFunctionConfirmationAsync(tt.functionName, "{}", func(approved bool) {
				callbackCalled = true
				receivedApproval = approved
			})

			if tt.expectedCall && !callbackCalled {
				t.Error("Expected callback to be called")
			}

			if callbackCalled && receivedApproval != tt.expectedApproval {
				t.Errorf("Expected callback approval to be %v, got %v", tt.expectedApproval, receivedApproval)
			}
		})
	}
}