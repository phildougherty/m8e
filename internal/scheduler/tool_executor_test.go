package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolExecutor_NewToolExecutor(t *testing.T) {
	logger := logr.Discard()
	executor := NewToolExecutor("http://localhost:8080", "test-api-key", logger)

	assert.NotNil(t, executor)
	assert.Equal(t, "http://localhost:8080", executor.mcpProxyURL)
	assert.Equal(t, "test-api-key", executor.mcpProxyAPIKey)
	assert.NotNil(t, executor.httpClient)
	assert.Equal(t, 5*time.Minute, executor.httpClient.Timeout)
	assert.Equal(t, logger, executor.logger)
}

func TestToolExecutor_ExecuteTool(t *testing.T) {
	logger := logr.Discard()

	tests := []struct {
		name           string
		request        *ToolExecutionRequest
		mockResponse   MCPToolResponse
		mockStatusCode int
		expectedResult *ToolExecutionResult
		wantErr        bool
	}{
		{
			name: "successful tool execution",
			request: &ToolExecutionRequest{
				Tool: "test-tool",
				Parameters: map[string]interface{}{
					"param1": "value1",
					"param2": 42,
				},
				Timeout: 30 * time.Second,
			},
			mockResponse: MCPToolResponse{
				Success: true,
				Result: map[string]interface{}{
					"output": "success",
					"data":   []string{"item1", "item2"},
				},
			},
			mockStatusCode: http.StatusOK,
			expectedResult: &ToolExecutionResult{
				Success: true,
				Output: map[string]interface{}{
					"output": "success",
					"data":   []interface{}{"item1", "item2"},
				},
			},
			wantErr: false,
		},
		{
			name: "tool execution with error response",
			request: &ToolExecutionRequest{
				Tool: "failing-tool",
				Parameters: map[string]interface{}{
					"param1": "value1",
				},
				Timeout: 30 * time.Second,
			},
			mockResponse: MCPToolResponse{
				Success: false,
				Error:   "Tool execution failed",
			},
			mockStatusCode: http.StatusOK,
			expectedResult: &ToolExecutionResult{
				Success: false,
				Error:   "Tool execution failed",
			},
			wantErr: false,
		},
		{
			name: "HTTP error response",
			request: &ToolExecutionRequest{
				Tool: "test-tool",
				Parameters: map[string]interface{}{
					"param1": "value1",
				},
				Timeout: 30 * time.Second,
			},
			mockResponse:   MCPToolResponse{},
			mockStatusCode: http.StatusInternalServerError,
			expectedResult: &ToolExecutionResult{
				Success: false,
			},
			wantErr: false,
		},
		{
			name: "tool execution without timeout",
			request: &ToolExecutionRequest{
				Tool: "test-tool",
				Parameters: map[string]interface{}{
					"param1": "value1",
				},
			},
			mockResponse: MCPToolResponse{
				Success: true,
				Result: map[string]interface{}{
					"output": "success",
				},
			},
			mockStatusCode: http.StatusOK,
			expectedResult: &ToolExecutionResult{
				Success: true,
				Output: map[string]interface{}{
					"output": "success",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method and headers
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))

				// Verify URL path
				expectedPath := fmt.Sprintf("/tools/%s/call", tt.request.Tool)
				assert.Equal(t, expectedPath, r.URL.Path)

				// Verify request body
				var toolCall MCPToolCall
				err := json.NewDecoder(r.Body).Decode(&toolCall)
				require.NoError(t, err)
				assert.Equal(t, tt.request.Tool, toolCall.Tool)
				assert.Equal(t, tt.request.Parameters, toolCall.Parameters)

				// Send response
				w.WriteHeader(tt.mockStatusCode)
				if tt.mockStatusCode == http.StatusOK {
					json.NewEncoder(w).Encode(tt.mockResponse)
				}
			}))
			defer server.Close()

			executor := NewToolExecutor(server.URL, "test-api-key", logger)
			result, err := executor.ExecuteTool(context.Background(), tt.request)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedResult.Success, result.Success)
				if tt.expectedResult.Success {
					assert.Equal(t, tt.expectedResult.Output, result.Output)
				} else {
					assert.Contains(t, result.Error, tt.expectedResult.Error)
				}
				assert.NotZero(t, result.Duration)
			}
		})
	}
}

func TestToolExecutor_ExecuteTool_WithTimeout(t *testing.T) {
	logger := logr.Discard()

	// Create mock server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(MCPToolResponse{
			Success: true,
			Result:  map[string]interface{}{"output": "success"},
		})
	}))
	defer server.Close()

	executor := NewToolExecutor(server.URL, "test-api-key", logger)

	// Test with timeout that should succeed
	request := &ToolExecutionRequest{
		Tool: "test-tool",
		Parameters: map[string]interface{}{
			"param1": "value1",
		},
		Timeout: 500 * time.Millisecond,
	}

	result, err := executor.ExecuteTool(context.Background(), request)
	assert.NoError(t, err)
	assert.True(t, result.Success)

	// Test with timeout that should fail
	request.Timeout = 100 * time.Millisecond
	result, err = executor.ExecuteTool(context.Background(), request)
	assert.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "context deadline exceeded")
}

func TestToolExecutor_ExecuteTool_WithoutAPIKey(t *testing.T) {
	logger := logr.Discard()

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify no authorization header
		assert.Empty(t, r.Header.Get("Authorization"))

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(MCPToolResponse{
			Success: true,
			Result:  map[string]interface{}{"output": "success"},
		})
	}))
	defer server.Close()

	executor := NewToolExecutor(server.URL, "", logger) // Empty API key

	request := &ToolExecutionRequest{
		Tool: "test-tool",
		Parameters: map[string]interface{}{
			"param1": "value1",
		},
	}

	result, err := executor.ExecuteTool(context.Background(), request)
	assert.NoError(t, err)
	assert.True(t, result.Success)
}

func TestToolExecutor_DiscoverTools(t *testing.T) {
	logger := logr.Discard()

	tests := []struct {
		name           string
		mockResponse   []string
		mockStatusCode int
		expectedTools  []string
		wantErr        bool
	}{
		{
			name: "successful discovery",
			mockResponse: []string{
				"tool1",
				"tool2",
				"tool3",
			},
			mockStatusCode: http.StatusOK,
			expectedTools: []string{
				"tool1",
				"tool2",
				"tool3",
			},
			wantErr: false,
		},
		{
			name:           "empty tools list",
			mockResponse:   []string{},
			mockStatusCode: http.StatusOK,
			expectedTools:  []string{},
			wantErr:        false,
		},
		{
			name:           "HTTP error",
			mockResponse:   nil,
			mockStatusCode: http.StatusInternalServerError,
			expectedTools:  nil,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method and headers
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
				assert.Equal(t, "/tools", r.URL.Path)

				// Send response
				w.WriteHeader(tt.mockStatusCode)
				if tt.mockStatusCode == http.StatusOK {
					json.NewEncoder(w).Encode(tt.mockResponse)
				}
			}))
			defer server.Close()

			executor := NewToolExecutor(server.URL, "test-api-key", logger)
			tools, err := executor.DiscoverTools(context.Background())

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedTools, tools)
			}
		})
	}
}

func TestToolExecutor_ValidateTool(t *testing.T) {
	logger := logr.Discard()

	tests := []struct {
		name          string
		toolName      string
		availableTools []string
		wantErr       bool
	}{
		{
			name:          "tool exists",
			toolName:      "existing-tool",
			availableTools: []string{"tool1", "existing-tool", "tool2"},
			wantErr:       false,
		},
		{
			name:          "tool does not exist",
			toolName:      "non-existent-tool",
			availableTools: []string{"tool1", "tool2", "tool3"},
			wantErr:       true,
		},
		{
			name:          "empty tools list",
			toolName:      "any-tool",
			availableTools: []string{},
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(tt.availableTools)
			}))
			defer server.Close()

			executor := NewToolExecutor(server.URL, "test-api-key", logger)
			err := executor.ValidateTool(context.Background(), tt.toolName)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "not found")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestToolExecutor_ExecuteStepWithContext(t *testing.T) {
	logger := logr.Discard()

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		var toolCall MCPToolCall
		err := json.NewDecoder(r.Body).Decode(&toolCall)
		require.NoError(t, err)

		// Verify enhanced parameters
		assert.Equal(t, "test-workflow", toolCall.Parameters["_workflow_name"])
		assert.Equal(t, "default", toolCall.Parameters["_workflow_namespace"])
		assert.Equal(t, "test-step", toolCall.Parameters["_step_name"])
		assert.Equal(t, float64(1), toolCall.Parameters["_attempt"])
		assert.Equal(t, "value1", toolCall.Parameters["param1"])

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(MCPToolResponse{
			Success: true,
			Result:  map[string]interface{}{"output": "success"},
		})
	}))
	defer server.Close()

	executor := NewToolExecutor(server.URL, "test-api-key", logger)

	stepContext := &StepContext{
		WorkflowName:      "test-workflow",
		WorkflowNamespace: "default",
		StepName:          "test-step",
		Attempt:           1,
		PreviousOutputs: map[string]interface{}{
			"previous-step": "previous-output",
		},
	}

	parameters := map[string]interface{}{
		"param1": "value1",
	}

	result, err := executor.ExecuteStepWithContext(
		context.Background(),
		stepContext,
		"test-tool",
		parameters,
		30*time.Second,
	)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "success", result.Output.(map[string]interface{})["output"])
}

func TestToolExecutor_HealthCheck(t *testing.T) {
	logger := logr.Discard()

	tests := []struct {
		name           string
		mockStatusCode int
		wantErr        bool
	}{
		{
			name:           "healthy",
			mockStatusCode: http.StatusOK,
			wantErr:        false,
		},
		{
			name:           "unhealthy",
			mockStatusCode: http.StatusInternalServerError,
			wantErr:        true,
		},
		{
			name:           "service unavailable",
			mockStatusCode: http.StatusServiceUnavailable,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/health", r.URL.Path)
				assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))

				w.WriteHeader(tt.mockStatusCode)
			}))
			defer server.Close()

			executor := NewToolExecutor(server.URL, "test-api-key", logger)
			err := executor.HealthCheck(context.Background())

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTemplateEngine_NewTemplateEngine(t *testing.T) {
	logger := logr.Discard()
	engine := NewTemplateEngine(logger)

	assert.NotNil(t, engine)
	assert.NotNil(t, engine.stepOutputs)
	assert.Equal(t, logger, engine.logger)
}

func TestTemplateEngine_SetStepOutput(t *testing.T) {
	logger := logr.Discard()
	engine := NewTemplateEngine(logger)

	output := map[string]interface{}{
		"result": "success",
		"count":  42,
	}

	engine.SetStepOutput("test-step", output)

	assert.Equal(t, output, engine.stepOutputs["test-step"])
}

func TestTemplateEngine_RenderParameters(t *testing.T) {
	logger := logr.Discard()
	engine := NewTemplateEngine(logger)

	// Set up step outputs
	engine.SetStepOutput("step1", map[string]interface{}{
		"result": "success",
		"count":  42,
	})
	engine.SetStepOutput("step2", "simple-output")

	tests := []struct {
		name       string
		parameters map[string]interface{}
		expected   map[string]interface{}
		wantErr    bool
	}{
		{
			name: "simple parameters without templates",
			parameters: map[string]interface{}{
				"param1": "value1",
				"param2": 42,
				"param3": true,
			},
			expected: map[string]interface{}{
				"param1": "value1",
				"param2": 42,
				"param3": true,
			},
			wantErr: false,
		},
		{
			name: "parameters with step output templates",
			parameters: map[string]interface{}{
				"previous_result": "{{ index .steps \"step1\" \"result\" }}",
				"count":           "{{ index .steps \"step1\" \"count\" }}",
				"simple_output":   "{{ index .steps \"step2\" }}",
			},
			expected: map[string]interface{}{
				"previous_result": "success",
				"count":           "42",
				"simple_output":   "simple-output",
			},
			wantErr: false,
		},
		{
			name: "parameters with sprig functions",
			parameters: map[string]interface{}{
				"uppercase": "{{ upper \"hello\" }}",
				"date":      "{{ now | date \"2006-01-02\" }}",
			},
			expected: map[string]interface{}{
				"uppercase": "HELLO",
				"date":      time.Now().Format("2006-01-02"),
			},
			wantErr: false,
		},
		{
			name: "nested parameters",
			parameters: map[string]interface{}{
				"config": map[string]interface{}{
					"name":  "{{ upper \"test\" }}",
					"value": "{{ index .steps \"step1\" \"count\" }}",
				},
				"list": []interface{}{
					"{{ upper \"item1\" }}",
					"{{ upper \"item2\" }}",
				},
			},
			expected: map[string]interface{}{
				"config": map[string]interface{}{
					"name":  "TEST",
					"value": "42",
				},
				"list": []interface{}{
					"ITEM1",
					"ITEM2",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid template",
			parameters: map[string]interface{}{
				"invalid": "{{ .nonexistent.field }}",
			},
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.RenderParameters(tt.parameters)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestTemplateEngine_RenderString(t *testing.T) {
	logger := logr.Discard()
	engine := NewTemplateEngine(logger)

	// Set up step outputs
	engine.SetStepOutput("step1", map[string]interface{}{
		"result": "success",
		"data":   []interface{}{"item1", "item2"},
	})

	tests := []struct {
		name        string
		templateStr string
		expected    interface{}
		wantErr     bool
	}{
		{
			name:        "plain string",
			templateStr: "plain text",
			expected:    "plain text",
			wantErr:     false,
		},
		{
			name:        "simple template",
			templateStr: "Result: {{ index .steps \"step1\" \"result\" }}",
			expected:    "Result: success",
			wantErr:     false,
		},
		{
			name:        "JSON template",
			templateStr: "{\"result\": \"{{ index .steps \"step1\" \"result\" }}\"}",
			expected: map[string]interface{}{
				"result": "success",
			},
			wantErr: false,
		},
		{
			name:        "array template",
			templateStr: "[\"{{ index .steps \"step1\" \"result\" }}\", \"other\"]",
			expected:    []interface{}{"success", "other"},
			wantErr:     false,
		},
		{
			name:        "sprig function",
			templateStr: "{{ upper \"hello\" }}",
			expected:    "HELLO",
			wantErr:     false,
		},
		{
			name:        "invalid template syntax",
			templateStr: "{{ .invalid syntax",
			expected:    nil,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.renderString(tt.templateStr)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestTemplateEngine_EvaluateCondition(t *testing.T) {
	logger := logr.Discard()
	engine := NewTemplateEngine(logger)

	// Set up step outputs
	engine.SetStepOutput("step1", map[string]interface{}{
		"success": true,
		"count":   5,
	})

	tests := []struct {
		name      string
		condition string
		expected  bool
		wantErr   bool
	}{
		{
			name:      "empty condition",
			condition: "",
			expected:  true,
			wantErr:   false,
		},
		{
			name:      "literal true",
			condition: "true",
			expected:  true,
			wantErr:   false,
		},
		{
			name:      "literal false",
			condition: "false",
			expected:  false,
			wantErr:   false,
		},
		{
			name:      "boolean from step output",
			condition: "{{ index .steps \"step1\" \"success\" }}",
			expected:  true,
			wantErr:   false,
		},
		{
			name:      "numeric condition",
			condition: "{{ index .steps \"step1\" \"count\" }}",
			expected:  true,
			wantErr:   false,
		},
		{
			name:      "string condition - yes",
			condition: "yes",
			expected:  true,
			wantErr:   false,
		},
		{
			name:      "string condition - no",
			condition: "no",
			expected:  false,
			wantErr:   false,
		},
		{
			name:      "string condition - on",
			condition: "on",
			expected:  true,
			wantErr:   false,
		},
		{
			name:      "string condition - off",
			condition: "off",
			expected:  false,
			wantErr:   false,
		},
		{
			name:      "string condition - 1",
			condition: "1",
			expected:  true,
			wantErr:   false,
		},
		{
			name:      "string condition - 0",
			condition: "0",
			expected:  false,
			wantErr:   false,
		},
		{
			name:      "invalid boolean value",
			condition: "invalid",
			expected:  false,
			wantErr:   true,
		},
		{
			name:      "template evaluation error",
			condition: "{{ .nonexistent.field }}",
			expected:  false,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.EvaluateCondition(tt.condition)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestStepContext_Structure(t *testing.T) {
	stepContext := &StepContext{
		WorkflowName:      "test-workflow",
		WorkflowNamespace: "default",
		StepName:          "test-step",
		Attempt:           3,
		PreviousOutputs: map[string]interface{}{
			"step1": "output1",
			"step2": "output2",
		},
	}

	assert.Equal(t, "test-workflow", stepContext.WorkflowName)
	assert.Equal(t, "default", stepContext.WorkflowNamespace)
	assert.Equal(t, "test-step", stepContext.StepName)
	assert.Equal(t, 3, stepContext.Attempt)
	assert.Len(t, stepContext.PreviousOutputs, 2)
	assert.Equal(t, "output1", stepContext.PreviousOutputs["step1"])
	assert.Equal(t, "output2", stepContext.PreviousOutputs["step2"])
}

func TestToolExecutionRequest_Structure(t *testing.T) {
	request := &ToolExecutionRequest{
		Tool: "test-tool",
		Parameters: map[string]interface{}{
			"param1": "value1",
			"param2": 42,
		},
		Timeout: 30 * time.Second,
	}

	assert.Equal(t, "test-tool", request.Tool)
	assert.Len(t, request.Parameters, 2)
	assert.Equal(t, "value1", request.Parameters["param1"])
	assert.Equal(t, 42, request.Parameters["param2"])
	assert.Equal(t, 30*time.Second, request.Timeout)
}

func TestToolExecutionResult_Structure(t *testing.T) {
	result := &ToolExecutionResult{
		Success: true,
		Output: map[string]interface{}{
			"result": "success",
			"count":  42,
		},
		Error:    "",
		Duration: 2 * time.Second,
	}

	assert.True(t, result.Success)
	assert.Len(t, result.Output, 2)
	assert.Equal(t, "success", result.Output["result"])
	assert.Equal(t, 42, result.Output["count"])
	assert.Equal(t, "", result.Error)
	assert.Equal(t, 2*time.Second, result.Duration)
}

func TestMCPToolCall_Structure(t *testing.T) {
	toolCall := &MCPToolCall{
		Tool: "test-tool",
		Parameters: map[string]interface{}{
			"param1": "value1",
			"param2": 42,
		},
	}

	assert.Equal(t, "test-tool", toolCall.Tool)
	assert.Len(t, toolCall.Parameters, 2)
	assert.Equal(t, "value1", toolCall.Parameters["param1"])
	assert.Equal(t, 42, toolCall.Parameters["param2"])
}

func TestMCPToolResponse_Structure(t *testing.T) {
	response := &MCPToolResponse{
		Success: true,
		Result: map[string]interface{}{
			"output": "success",
			"data":   []interface{}{"item1", "item2"},
		},
		Error: "",
	}

	assert.True(t, response.Success)
	assert.Len(t, response.Result, 2)
	assert.Equal(t, "success", response.Result["output"])
	assert.Equal(t, []interface{}{"item1", "item2"}, response.Result["data"])
	assert.Equal(t, "", response.Error)
}

func TestToolExecutor_URLTrimming(t *testing.T) {
	logger := logr.Discard()

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify URL path is correct
		assert.Equal(t, "/tools/test-tool/call", r.URL.Path)
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(MCPToolResponse{
			Success: true,
			Result:  map[string]interface{}{"output": "success"},
		})
	}))
	defer server.Close()

	// Test with trailing slash
	executor := NewToolExecutor(server.URL+"/", "test-api-key", logger)

	request := &ToolExecutionRequest{
		Tool: "test-tool",
		Parameters: map[string]interface{}{
			"param1": "value1",
		},
	}

	result, err := executor.ExecuteTool(context.Background(), request)
	assert.NoError(t, err)
	assert.True(t, result.Success)
}

func BenchmarkToolExecutor_ExecuteTool(b *testing.B) {
	logger := logr.Discard()

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(MCPToolResponse{
			Success: true,
			Result:  map[string]interface{}{"output": "success"},
		})
	}))
	defer server.Close()

	executor := NewToolExecutor(server.URL, "test-api-key", logger)

	request := &ToolExecutionRequest{
		Tool: "test-tool",
		Parameters: map[string]interface{}{
			"param1": "value1",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := executor.ExecuteTool(context.Background(), request)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTemplateEngine_RenderParameters(b *testing.B) {
	logger := logr.Discard()
	engine := NewTemplateEngine(logger)

	// Set up step outputs
	engine.SetStepOutput("step1", map[string]interface{}{
		"result": "success",
		"count":  42,
	})

	parameters := map[string]interface{}{
		"param1":         "{{ index .steps \"step1\" \"result\" }}",
		"param2":         "{{ index .steps \"step1\" \"count\" }}",
		"param3":         "{{ upper \"hello\" }}",
		"nested_param": map[string]interface{}{
			"inner": "{{ index .steps \"step1\" \"result\" }}",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.RenderParameters(parameters)
		if err != nil {
			b.Fatal(err)
		}
	}
}