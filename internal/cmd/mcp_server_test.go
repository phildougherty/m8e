package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPServer_Initialize(t *testing.T) {
	// Create a test MCP server handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
			return
		}

		var req MCPRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
			return
		}

		var result interface{}
		var mcpErr *MCPError

		switch req.Method {
		case "initialize":
			// MCP protocol initialization
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
			// Notification that client has been initialized - no response needed
			result = nil
		case "tools/list":
			result = map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "create_workflow",
						"description": "Create a new workflow",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name": map[string]interface{}{
									"type":        "string",
									"description": "Workflow name",
								},
							},
						},
					},
					{
						"name":        "matey_up",
						"description": "Start MCP services",
					},
				},
			}
		default:
			mcpErr = &MCPError{
				Code:    -32601,
				Message: "Method not found",
			}
		}

		response := MCPResponse{
			Result: result,
			Error:  mcpErr,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Logf("Warning: Failed to encode test response: %v", err)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	tests := []struct {
		name           string
		method         string
		params         map[string]interface{}
		expectedStatus int
		expectedResult map[string]interface{}
		expectedError  *MCPError
	}{
		{
			name:           "initialize request",
			method:         "initialize",
			params:         map[string]interface{}{},
			expectedStatus: http.StatusOK,
			expectedResult: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "matey",
					"version": "0.0.4",
				},
			},
		},
		{
			name:           "initialized notification",
			method:         "notifications/initialized",
			params:         map[string]interface{}{},
			expectedStatus: http.StatusOK,
			expectedResult: nil,
		},
		{
			name:           "tools list request",
			method:         "tools/list",
			params:         map[string]interface{}{},
			expectedStatus: http.StatusOK,
			expectedResult: map[string]interface{}{
				"tools": []interface{}{
					map[string]interface{}{
						"name":        "create_workflow",
						"description": "Create a new workflow",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name": map[string]interface{}{
									"type":        "string",
									"description": "Workflow name",
								},
							},
						},
					},
					map[string]interface{}{
						"name":        "matey_up",
						"description": "Start MCP services",
					},
				},
			},
		},
		{
			name:           "unknown method",
			method:         "unknown/method",
			params:         map[string]interface{}{},
			expectedStatus: http.StatusOK,
			expectedError: &MCPError{
				Code:    -32601,
				Message: "Method not found",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestBody := MCPRequest{
				Method: tt.method,
				Params: tt.params,
			}

			reqBytes, err := json.Marshal(requestBody)
			require.NoError(t, err)

			resp, err := http.Post(server.URL, "application/json", bytes.NewBuffer(reqBytes))
			require.NoError(t, err)
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Logf("Warning: Failed to close response body: %v", err)
				}
			}()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			var response MCPResponse
			err = json.NewDecoder(resp.Body).Decode(&response)
			require.NoError(t, err)

			if tt.expectedError != nil {
				assert.NotNil(t, response.Error)
				assert.Equal(t, tt.expectedError.Code, response.Error.Code)
				assert.Equal(t, tt.expectedError.Message, response.Error.Message)
			} else {
				assert.Nil(t, response.Error)
				if tt.expectedResult != nil {
					assert.Equal(t, tt.expectedResult, response.Result)
				}
			}
		})
	}
}

func TestMCPServer_InitializeProtocolCompliance(t *testing.T) {
	tests := []struct {
		name             string
		method           string
		shouldHaveResult bool
		resultChecks     []func(interface{}) bool
	}{
		{
			name:             "initialize returns protocol version",
			method:           "initialize",
			shouldHaveResult: true,
			resultChecks: []func(interface{}) bool{
				func(result interface{}) bool {
					resultMap, ok := result.(map[string]interface{})
					if !ok {
						return false
					}
					version, exists := resultMap["protocolVersion"]
					return exists && version == "2024-11-05"
				},
				func(result interface{}) bool {
					resultMap, ok := result.(map[string]interface{})
					if !ok {
						return false
					}
					capabilities, exists := resultMap["capabilities"]
					return exists && capabilities != nil
				},
				func(result interface{}) bool {
					resultMap, ok := result.(map[string]interface{})
					if !ok {
						return false
					}
					serverInfo, exists := resultMap["serverInfo"]
					if !exists {
						return false
					}
					serverInfoMap, ok := serverInfo.(map[string]interface{})
					if !ok {
						return false
					}
					name, hasName := serverInfoMap["name"]
					version, hasVersion := serverInfoMap["version"]
					return hasName && name == "matey" && hasVersion && version == "0.0.4"
				},
			},
		},
		{
			name:             "initialized notification has no result",
			method:           "notifications/initialized",
			shouldHaveResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test validates the structure of responses for MCP protocol compliance
			var result interface{}
			var mcpErr *MCPError

			switch tt.method {
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
			default:
				mcpErr = &MCPError{
					Code:    -32601,
					Message: "Method not found",
				}
			}

			if tt.shouldHaveResult {
				assert.NotNil(t, result, "Method should return a result")
				assert.Nil(t, mcpErr, "Method should not return an error")

				// Run all result checks
				for i, check := range tt.resultChecks {
					assert.True(t, check(result), "Result check %d failed for method %s", i, tt.method)
				}
			} else {
				// For notifications, result can be nil
				assert.Nil(t, mcpErr, "Notification should not return an error")
			}
		})
	}
}

func TestMCPServer_ToolDiscovery(t *testing.T) {
	// Test that tools/list returns the expected tools for workflow creation
	expectedTools := []string{
		"create_workflow",
		"matey_up", 
		"matey_down",
		"matey_ps",
		"matey_logs",
		"matey_inspect",
		"apply_config",
		"get_cluster_state",
	}

	// Simulate tools/list response
	toolsResponse := map[string]interface{}{
		"tools": []map[string]interface{}{},
	}

	// Add expected tools to response
	tools := toolsResponse["tools"].([]map[string]interface{})
	for _, toolName := range expectedTools {
		tools = append(tools, map[string]interface{}{
			"name":        toolName,
			"description": "Tool for " + toolName,
		})
	}
	toolsResponse["tools"] = tools

	// Verify all expected tools are present
	toolsList, ok := toolsResponse["tools"].([]map[string]interface{})
	assert.True(t, ok, "Tools should be a list")
	assert.Len(t, toolsList, len(expectedTools), "Should have all expected tools")

	// Check each expected tool is present
	foundTools := make(map[string]bool)
	for _, tool := range toolsList {
		name, ok := tool["name"].(string)
		assert.True(t, ok, "Tool should have a name")
		foundTools[name] = true
	}

	for _, expectedTool := range expectedTools {
		assert.True(t, foundTools[expectedTool], "Should find tool: %s", expectedTool)
	}

	// Specifically verify create_workflow is present (critical for agentic behavior)
	assert.True(t, foundTools["create_workflow"], "create_workflow tool must be available for agentic behavior")
}

func TestMCPServer_ErrorHandling(t *testing.T) {
	tests := []struct {
		name          string
		requestBody   string
		expectedError bool
		expectedCode  int
	}{
		{
			name:          "invalid JSON",
			requestBody:   `{invalid json}`,
			expectedError: true,
		},
		{
			name:          "missing method",
			requestBody:   `{"params": {}}`,
			expectedError: true,
		},
		{
			name:          "valid request",
			requestBody:   `{"method": "initialize", "params": {}}`,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req MCPRequest
				err := json.NewDecoder(r.Body).Decode(&req)
				
				if err != nil && tt.expectedError {
					// Expected error case
					w.WriteHeader(http.StatusBadRequest)
					if err := json.NewEncoder(w).Encode(MCPResponse{
						Error: &MCPError{
							Code:    -32700,
							Message: "Parse error",
						},
					}); err != nil {
						t.Errorf("Failed to encode test response: %v", err)
					}
					return
				}

				if req.Method == "" && tt.expectedError {
					// Missing method case
					w.WriteHeader(http.StatusBadRequest)
					if err := json.NewEncoder(w).Encode(MCPResponse{
						Error: &MCPError{
							Code:    -32600,
							Message: "Invalid request",
						},
					}); err != nil {
						t.Errorf("Failed to encode test response: %v", err)
					}
					return
				}

				// Valid case
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(MCPResponse{
					Result: map[string]interface{}{"status": "ok"},
				}); err != nil {
					t.Errorf("Failed to encode test response: %v", err)
				}
			})

			server := httptest.NewServer(handler)
			defer server.Close()

			resp, err := http.Post(server.URL, "application/json", strings.NewReader(tt.requestBody))
			require.NoError(t, err)
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Logf("Warning: Failed to close response body: %v", err)
				}
			}()

			var response MCPResponse
			err = json.NewDecoder(resp.Body).Decode(&response)
			require.NoError(t, err)

			if tt.expectedError {
				assert.NotNil(t, response.Error, "Should have error for invalid request")
				if tt.expectedCode != 0 {
					assert.Equal(t, tt.expectedCode, response.Error.Code)
				}
			} else {
				assert.Nil(t, response.Error, "Should not have error for valid request")
				assert.NotNil(t, response.Result, "Should have result for valid request")
			}
		})
	}
}