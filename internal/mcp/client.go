package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// MCPClient represents a client for connecting to MCP servers via proxy
type MCPClient struct {
	proxyURL   string
	httpClient *http.Client
	apiKey     string
	retryCount int
}

// NewMCPClient creates a new MCP client
func NewMCPClient(proxyURL string) *MCPClient {
	return &MCPClient{
		proxyURL: proxyURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // Increased timeout for tool discovery
		},
		apiKey:     os.Getenv("MCP_API_KEY"),
		retryCount: 2, // Reduced retry count
	}
}

// MCPRequest represents a request to an MCP server
type MCPRequest struct {
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

// MCPResponse represents a response from an MCP server
type MCPResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  *MCPError   `json:"error,omitempty"`
}

// MCPError represents an error from an MCP server
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ServerInfo represents information about an MCP server
type ServerInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Tools       []Tool `json:"tools"`
}

// ListServers lists all available MCP servers
func (c *MCPClient) ListServers(ctx context.Context) ([]ServerInfo, error) {
	// For now, we'll hardcode the known servers since we know the endpoint structure
	// In a real implementation, this would query the proxy for available servers
	servers := []ServerInfo{
		{
			Name:        "matey",
			Version:     "0.0.4",
			Description: "Kubernetes MCP server orchestration",
		},
		// Add other known servers here as needed
	}
	
	return servers, nil
}

// GetServerTools gets all tools for a specific server
func (c *MCPClient) GetServerTools(ctx context.Context, serverName string) ([]Tool, error) {
	// Use proper MCP JSON-RPC format
	mcpReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}
	
	resp, err := c.makeMCPRequest(ctx, fmt.Sprintf("/servers/%s", serverName), mcpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get tools for server %s: %w", serverName, err)
	}
	
	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error: %s", resp.Error.Message)
	}
	
	// Parse the tools from the MCP response
	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}
	
	toolsArray, ok := resultMap["tools"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("tools not found in response")
	}
	
	var tools []Tool
	toolsBytes, err := json.Marshal(toolsArray)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tools: %w", err)
	}
	
	if err := json.Unmarshal(toolsBytes, &tools); err != nil {
		return nil, fmt.Errorf("failed to parse tools: %w", err)
	}
	
	return tools, nil
}

// CallTool calls a specific tool on an MCP server
func (c *MCPClient) CallTool(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) (*ToolResult, error) {
	// Try direct tool call first (preferred method)
	if result, err := c.callToolDirect(ctx, serverName, toolName, arguments); err == nil {
		return result, nil
	}

	// If direct call fails, try MCP protocol format as fallback
	return c.callToolMCP(ctx, serverName, toolName, arguments)
}

// callToolDirect calls a tool using direct proxy endpoint (e.g., /get_current_glucose)
func (c *MCPClient) callToolDirect(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) (*ToolResult, error) {
	endpoint := fmt.Sprintf("/%s", toolName)
	
	// Create HTTP request directly to the proxy endpoint
	reqBytes, err := json.Marshal(arguments)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal arguments: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.proxyURL+endpoint, bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	
	// Add authentication if API key is available
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call tool %s on server %s: %w", toolName, serverName, err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("HTTP error %d: %s", httpResp.StatusCode, string(respBytes))
	}

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Try to parse as ToolResult first
	var result ToolResult
	if err := json.Unmarshal(respBytes, &result); err == nil {
		return &result, nil
	}

	// If that fails, try to parse as plain text and convert to ToolResult
	resultText := string(respBytes)
	return &ToolResult{
		Content: []Content{{Type: "text", Text: resultText}},
		IsError: false,
	}, nil
}

// callToolMCP calls a tool using MCP protocol format
func (c *MCPClient) callToolMCP(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) (*ToolResult, error) {
	// Use proper MCP JSON-RPC format
	mcpReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}
	
	// Use the correct server endpoint
	endpoint := fmt.Sprintf("/servers/%s", serverName)
	resp, err := c.makeMCPRequest(ctx, endpoint, mcpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call tool %s on server %s: %w", toolName, serverName, err)
	}
	
	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error: %s", resp.Error.Message)
	}
	
	var result ToolResult
	
	// Handle the response format from MCP protocol endpoints
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool result: %w", err)
	}
	
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tool result: %w", err)
	}
	
	return &result, nil
}

// makeRequest makes an HTTP request to the MCP proxy with retry logic
func (c *MCPClient) makeRequest(ctx context.Context, endpoint string, req MCPRequest) (*MCPResponse, error) {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < c.retryCount; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.proxyURL+endpoint, bytes.NewBuffer(reqBytes))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		
		// Add authentication if API key is available
		if c.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		}

		httpResp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			if attempt < c.retryCount-1 {
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond) // Reduced sleep time
				continue
			}
			return nil, fmt.Errorf("failed to make request after %d attempts: %w", c.retryCount, err)
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode != http.StatusOK {
			respBytes, _ := io.ReadAll(httpResp.Body)
			lastErr = fmt.Errorf("HTTP error %d: %s", httpResp.StatusCode, string(respBytes))
			if attempt < c.retryCount-1 && httpResp.StatusCode >= 500 {
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond) // Reduced sleep time
				continue
			}
			return nil, lastErr
		}

		respBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		var resp MCPResponse
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}

		return &resp, nil
	}

	return nil, lastErr
}

// makeMCPRequest makes a proper MCP JSON-RPC request
func (c *MCPClient) makeMCPRequest(ctx context.Context, endpoint string, req map[string]interface{}) (*MCPResponse, error) {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < c.retryCount; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.proxyURL+endpoint, bytes.NewBuffer(reqBytes))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		
		// Add authentication if API key is available
		if c.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		}

		httpResp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			if attempt < c.retryCount-1 {
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("failed to make request after %d attempts: %w", c.retryCount, err)
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode != http.StatusOK {
			respBytes, _ := io.ReadAll(httpResp.Body)
			lastErr = fmt.Errorf("HTTP error %d: %s", httpResp.StatusCode, string(respBytes))
			if attempt < c.retryCount-1 && httpResp.StatusCode >= 500 {
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
				continue
			}
			return nil, lastErr
		}

		respBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		// Parse as MCP JSON-RPC response
		var mcpResp map[string]interface{}
		if err := json.Unmarshal(respBytes, &mcpResp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal MCP response: %w", err)
		}

		// Convert to MCPResponse format
		resp := &MCPResponse{}
		if result, ok := mcpResp["result"]; ok {
			resp.Result = result
		}
		if errorData, ok := mcpResp["error"]; ok {
			if errorMap, ok := errorData.(map[string]interface{}); ok {
				resp.Error = &MCPError{
					Code:    int(errorMap["code"].(float64)),
					Message: errorMap["message"].(string),
				}
			}
		}

		return resp, nil
	}

	return nil, lastErr
}

// ToolCallResult represents the result of calling an MCP tool
type ToolCallResult struct {
	ServerName string
	ToolName   string
	Arguments  map[string]interface{}
	Result     *ToolResult
	Error      error
}

// ExecuteToolCall executes a tool call and returns formatted result
func (c *MCPClient) ExecuteToolCall(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) string {
	result, err := c.CallTool(ctx, serverName, toolName, arguments)
	if err != nil {
		return fmt.Sprintf("❌ Error calling %s.%s: %v", serverName, toolName, err)
	}
	
	if result.IsError {
		return fmt.Sprintf("❌ Tool error from %s.%s: %s", serverName, toolName, result.Content[0].Text)
	}
	
	var output strings.Builder
	output.WriteString(fmt.Sprintf("✅ **%s.%s** executed successfully:\n\n", serverName, toolName))
	
	for _, content := range result.Content {
		output.WriteString(content.Text)
		output.WriteString("\n")
	}
	
	return output.String()
}

// GetAvailableTools returns a formatted list of all available tools
func (c *MCPClient) GetAvailableTools(ctx context.Context) string {
	servers, err := c.ListServers(ctx)
	if err != nil {
		return fmt.Sprintf("❌ Error listing servers: %v", err)
	}
	
	var output strings.Builder
	output.WriteString("## Available MCP Tools:\n\n")
	
	for _, server := range servers {
		tools, err := c.GetServerTools(ctx, server.Name)
		if err != nil {
			output.WriteString(fmt.Sprintf("❌ Error getting tools for %s: %v\n", server.Name, err))
			continue
		}
		
		if len(tools) == 0 {
			continue
		}
		
		output.WriteString(fmt.Sprintf("### %s\n", server.Name))
		if server.Description != "" {
			output.WriteString(fmt.Sprintf("*%s*\n\n", server.Description))
		}
		
		for _, tool := range tools {
			output.WriteString(fmt.Sprintf("- **%s**: %s\n", tool.Name, tool.Description))
		}
		output.WriteString("\n")
	}
	
	return output.String()
}

// ParseToolCall parses a tool call from AI response
func ParseToolCall(text string) (serverName, toolName string, arguments map[string]interface{}, found bool) {
	// Look for patterns like: CALL matey.matey_ps {"watch": true}
	// or: EXECUTE dexcom.get_glucose_data {"days": 7}
	
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		if strings.HasPrefix(line, "CALL ") || strings.HasPrefix(line, "EXECUTE ") {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			
			toolPath := parts[1]
			dotIndex := strings.Index(toolPath, ".")
			if dotIndex == -1 {
				continue
			}
			
			serverName = toolPath[:dotIndex]
			toolName = toolPath[dotIndex+1:]
			
			// Parse arguments if present
			if len(parts) > 2 {
				argsJSON := strings.Join(parts[2:], " ")
				if err := json.Unmarshal([]byte(argsJSON), &arguments); err != nil {
					arguments = map[string]interface{}{}
				}
			} else {
				arguments = map[string]interface{}{}
			}
			
			found = true
			return
		}
	}
	
	return "", "", nil, false
}