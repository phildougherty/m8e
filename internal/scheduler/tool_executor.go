// internal/scheduler/tool_executor.go
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/Masterminds/sprig/v3"
	"text/template"
	"bytes"
)

type ToolExecutor struct {
	mcpProxyURL    string
	mcpProxyAPIKey string
	httpClient     *http.Client
	logger         logr.Logger
}

type ToolExecutionRequest struct {
	Tool       string                 `json:"tool"`
	Parameters map[string]interface{} `json:"parameters"`
	Timeout    time.Duration          `json:"timeout"`
}

type ToolExecutionResult struct {
	Success  bool                   `json:"success"`
	Output   map[string]interface{} `json:"output,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Duration time.Duration          `json:"duration"`
}

type MCPToolCall struct {
	Tool       string                 `json:"tool"`
	Parameters map[string]interface{} `json:"parameters"`
}

type MCPToolResponse struct {
	Success bool                   `json:"success"`
	Result  map[string]interface{} `json:"result,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

func NewToolExecutor(mcpProxyURL, mcpProxyAPIKey string, logger logr.Logger) *ToolExecutor {
	return &ToolExecutor{
		mcpProxyURL:    mcpProxyURL,
		mcpProxyAPIKey: mcpProxyAPIKey,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		logger: logger,
	}
}

func (te *ToolExecutor) ExecuteTool(ctx context.Context, req *ToolExecutionRequest) (*ToolExecutionResult, error) {
	startTime := time.Now()
	
	te.logger.Info("Executing MCP tool", "tool", req.Tool, "parameters", req.Parameters)

	// Create context with timeout
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	// Prepare the MCP tool call
	toolCall := MCPToolCall{
		Tool:       req.Tool,
		Parameters: req.Parameters,
	}

	// Execute the tool call
	response, err := te.callMCPTool(ctx, &toolCall)
	duration := time.Since(startTime)

	if err != nil {
		te.logger.Error(err, "MCP tool execution failed", "tool", req.Tool, "duration", duration)
		return &ToolExecutionResult{
			Success:  false,
			Error:    err.Error(),
			Duration: duration,
		}, nil
	}

	if !response.Success {
		te.logger.Info("MCP tool returned error", "tool", req.Tool, "error", response.Error, "duration", duration)
		return &ToolExecutionResult{
			Success:  false,
			Error:    response.Error,
			Duration: duration,
		}, nil
	}

	te.logger.Info("MCP tool execution succeeded", "tool", req.Tool, "duration", duration)
	return &ToolExecutionResult{
		Success:  true,
		Output:   response.Result,
		Duration: duration,
	}, nil
}

func (te *ToolExecutor) callMCPTool(ctx context.Context, toolCall *MCPToolCall) (*MCPToolResponse, error) {
	// Prepare HTTP request
	reqBody, err := json.Marshal(toolCall)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool call: %w", err)
	}

	url := fmt.Sprintf("%s/tools/%s/call", strings.TrimRight(te.mcpProxyURL, "/"), toolCall.Tool)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if te.mcpProxyAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+te.mcpProxyAPIKey)
	}

	// Execute HTTP request
	resp, err := te.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP proxy returned status %d", resp.StatusCode)
	}

	// Parse response
	var mcpResp MCPToolResponse
	if err := json.NewDecoder(resp.Body).Decode(&mcpResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &mcpResp, nil
}

func (te *ToolExecutor) DiscoverTools(ctx context.Context) ([]string, error) {
	url := fmt.Sprintf("%s/tools", strings.TrimRight(te.mcpProxyURL, "/"))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	if te.mcpProxyAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+te.mcpProxyAPIKey)
	}

	// Execute HTTP request
	resp, err := te.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP proxy returned status %d", resp.StatusCode)
	}

	// Parse response
	var tools []string
	if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return tools, nil
}

func (te *ToolExecutor) ValidateTool(ctx context.Context, toolName string) error {
	tools, err := te.DiscoverTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover tools: %w", err)
	}

	for _, tool := range tools {
		if tool == toolName {
			return nil
		}
	}

	return fmt.Errorf("tool %q not found in available tools: %v", toolName, tools)
}

// TemplateEngine handles parameter templating with step outputs
type TemplateEngine struct {
	stepOutputs map[string]interface{}
	logger      logr.Logger
}

func NewTemplateEngine(logger logr.Logger) *TemplateEngine {
	return &TemplateEngine{
		stepOutputs: make(map[string]interface{}),
		logger:      logger,
	}
}

func (te *TemplateEngine) SetStepOutput(stepName string, output interface{}) {
	te.stepOutputs[stepName] = output
}

func (te *TemplateEngine) RenderParameters(parameters map[string]interface{}) (map[string]interface{}, error) {
	rendered := make(map[string]interface{})

	for key, value := range parameters {
		renderedValue, err := te.renderValue(value)
		if err != nil {
			return nil, fmt.Errorf("failed to render parameter %q: %w", key, err)
		}
		rendered[key] = renderedValue
	}

	return rendered, nil
}

func (te *TemplateEngine) renderValue(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		return te.renderString(v)
	case map[string]interface{}:
		rendered := make(map[string]interface{})
		for k, val := range v {
			renderedVal, err := te.renderValue(val)
			if err != nil {
				return nil, err
			}
			rendered[k] = renderedVal
		}
		return rendered, nil
	case []interface{}:
		rendered := make([]interface{}, len(v))
		for i, val := range v {
			renderedVal, err := te.renderValue(val)
			if err != nil {
				return nil, err
			}
			rendered[i] = renderedVal
		}
		return rendered, nil
	default:
		return value, nil
	}
}

func (te *TemplateEngine) renderString(templateStr string) (interface{}, error) {
	// Check if the string contains template syntax
	if !strings.Contains(templateStr, "{{") {
		return templateStr, nil
	}

	// Create template with sprig functions
	tmpl, err := template.New("parameter").Funcs(sprig.TxtFuncMap()).Parse(templateStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	// Prepare template data
	data := map[string]interface{}{
		"steps": te.stepOutputs,
	}

	// Render template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	result := buf.String()

	// Try to parse as JSON for complex objects
	var jsonResult interface{}
	if err := json.Unmarshal([]byte(result), &jsonResult); err == nil {
		return jsonResult, nil
	}

	return result, nil
}

func (te *TemplateEngine) EvaluateCondition(condition string) (bool, error) {
	if condition == "" {
		return true, nil // No condition means always execute
	}

	// Render the condition as a template
	rendered, err := te.renderString(condition)
	if err != nil {
		return false, fmt.Errorf("failed to render condition: %w", err)
	}

	// Convert to boolean
	switch v := rendered.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(v) {
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off", "":
			return false, nil
		default:
			return false, fmt.Errorf("invalid boolean value: %q", v)
		}
	case int, int32, int64:
		return v != 0, nil
	case float32, float64:
		return v != 0.0, nil
	default:
		return false, fmt.Errorf("condition must evaluate to a boolean, got %T", v)
	}
}

// StepContext provides context information for step execution
type StepContext struct {
	WorkflowName      string
	WorkflowNamespace string
	StepName          string
	Attempt           int
	PreviousOutputs   map[string]interface{}
}

// ExecuteStepWithContext executes a single step with full context
func (te *ToolExecutor) ExecuteStepWithContext(ctx context.Context, stepCtx *StepContext, tool string, parameters map[string]interface{}, timeout time.Duration) (*ToolExecutionResult, error) {
	// Add context information to parameters
	enhancedParams := make(map[string]interface{})
	for k, v := range parameters {
		enhancedParams[k] = v
	}

	// Add metadata
	enhancedParams["_workflow_name"] = stepCtx.WorkflowName
	enhancedParams["_workflow_namespace"] = stepCtx.WorkflowNamespace
	enhancedParams["_step_name"] = stepCtx.StepName
	enhancedParams["_attempt"] = stepCtx.Attempt

	req := &ToolExecutionRequest{
		Tool:       tool,
		Parameters: enhancedParams,
		Timeout:    timeout,
	}

	return te.ExecuteTool(ctx, req)
}

// HealthCheck verifies the MCP proxy is accessible
func (te *ToolExecutor) HealthCheck(ctx context.Context) error {
	url := fmt.Sprintf("%s/health", strings.TrimRight(te.mcpProxyURL, "/"))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	if te.mcpProxyAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+te.mcpProxyAPIKey)
	}

	resp, err := te.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MCP proxy health check failed with status %d", resp.StatusCode)
	}

	return nil
}