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
			Timeout: 20 * time.Minute, // Extended timeout for complex operations like execute_agent
		},
		apiKey:     os.Getenv("MCP_API_KEY"),
		retryCount: 4, // Increased retry count for better reliability
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
	// Try to discover servers from the discovery endpoint first
	servers, err := c.discoverServersFromDiscovery(ctx)
	if err != nil {
		// Fallback to OpenAPI discovery if discovery endpoint fails
		servers, err := c.discoverServersFromOpenAPI(ctx)
		if err != nil {
			// Final fallback to hardcoded server
			servers := []ServerInfo{
				{
					Name:        "matey",
					Version:     "0.0.4",
					Description: "Kubernetes MCP server orchestration",
				},
			}
			return servers, nil
		}
		return servers, nil
	}
	
	return servers, nil
}

// discoverServersFromOpenAPI discovers servers and their tools from the OpenAPI endpoint
func (c *MCPClient) discoverServersFromOpenAPI(ctx context.Context) ([]ServerInfo, error) {
	// Fetch OpenAPI spec
	req, err := http.NewRequestWithContext(ctx, "GET", c.proxyURL+"/openapi.json", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OpenAPI spec: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d", resp.StatusCode)
	}
	
	var openAPISpec map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&openAPISpec); err != nil {
		return nil, fmt.Errorf("failed to decode OpenAPI spec: %w", err)
	}
	
	// Extract tools from OpenAPI paths and organize by server
	paths, ok := openAPISpec["paths"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no paths found in OpenAPI spec")
	}
	
	// Map to collect tools by server, with deduplication
	serverToolsMap := make(map[string]map[string]Tool)
	
	for path, pathSpec := range paths {
		if pathSpecMap, ok := pathSpec.(map[string]interface{}); ok {
			if postSpec, ok := pathSpecMap["post"].(map[string]interface{}); ok {
				toolName := strings.TrimPrefix(path, "/")
				
				// Skip *_default tools if they're placeholders
				if strings.HasSuffix(toolName, "_default") {
					continue
				}
				
				tool := Tool{
					Name:        toolName,
					Description: extractDescription(postSpec),
					InputSchema: extractInputSchema(postSpec, openAPISpec),
				}
				
				// Determine server name from tool name and OpenAPI spec
				serverName := extractServerName(tool.Name, postSpec)
				
				// Initialize server map if not exists
				if serverToolsMap[serverName] == nil {
					serverToolsMap[serverName] = make(map[string]Tool)
				}
				
				// Add tool (this naturally deduplicates by tool name)
				serverToolsMap[serverName][tool.Name] = tool
			}
		}
	}
	
	// Convert map to server list
	var servers []ServerInfo
	for serverName, toolsMap := range serverToolsMap {
		// Convert tool map to slice
		var tools []Tool
		for _, tool := range toolsMap {
			tools = append(tools, tool)
		}
		
		servers = append(servers, ServerInfo{
			Name:        serverName,
			Version:     "1.0.0",
			Description: fmt.Sprintf("MCP server: %s", serverName),
			Tools:       tools,
		})
	}
	
	return servers, nil
}

// extractServerName extracts server name from tool name or OpenAPI tags
func extractServerName(toolName string, postSpec map[string]interface{}) string {
	// Handle special cases first
	if strings.HasSuffix(toolName, "_default") {
		// Extract server name from *_default pattern
		return strings.TrimSuffix(toolName, "_default")
	}
	
	// Try to extract server name from OpenAPI tags
	if tags, ok := postSpec["tags"].([]interface{}); ok {
		for _, tag := range tags {
			if tagStr, ok := tag.(string); ok {
				// Skip generic tags like "MCP Proxy" and "mcp-tools"
				if tagStr != "MCP Proxy" && tagStr != "mcp-tools" {
					return tagStr
				}
			}
		}
	}
	
	// Try to extract from operationId (e.g., "filesystem_create_directory")
	if operationId, ok := postSpec["operationId"].(string); ok {
		parts := strings.Split(operationId, "_")
		if len(parts) > 1 {
			return parts[0]
		}
	}
	
	
	// Group all tools that don't have clear server identification as "proxy-tools"
	return "proxy-tools"
}

// discoverServersFromDiscovery discovers servers using the /discovery endpoint
func (c *MCPClient) discoverServersFromDiscovery(ctx context.Context) ([]ServerInfo, error) {
	// Fetch discovery endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", c.proxyURL+"/discovery", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery request: %w", err)
	}
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch discovery endpoint: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d from discovery endpoint", resp.StatusCode)
	}
	
	var discoveryResp struct {
		DiscoveredServers []struct {
			Name string `json:"name"`
		} `json:"discovered_servers"`
		ConnectionStatus map[string]struct {
			Connected bool `json:"connected"`
		} `json:"connection_status"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&discoveryResp); err != nil {
		return nil, fmt.Errorf("failed to decode discovery response: %w", err)
	}
	
	var servers []ServerInfo
	
	// Create ServerInfo for each discovered server
	for _, discoveredServer := range discoveryResp.DiscoveredServers {
		serverName := discoveredServer.Name
		
		// Check if server is connected
		connectionInfo, exists := discoveryResp.ConnectionStatus[serverName]
		if !exists || !connectionInfo.Connected {
			// Skip disconnected servers
			continue
		}
		
		// Get tools for this server
		tools, err := c.getServerToolsFromIndividualAPI(ctx, serverName)
		if err != nil {
			// If we can't get tools, still include the server but with empty tools
			tools = []Tool{}
		}
		
		servers = append(servers, ServerInfo{
			Name:        serverName,
			Version:     "1.0.0",
			Description: fmt.Sprintf("MCP server: %s", serverName),
			Tools:       tools,
		})
	}
	
	return servers, nil
}

// getServerToolsFromIndividualAPI gets tools for a specific server from its individual OpenAPI spec
func (c *MCPClient) getServerToolsFromIndividualAPI(ctx context.Context, serverName string) ([]Tool, error) {
	// Fetch individual server OpenAPI spec
	req, err := http.NewRequestWithContext(ctx, "GET", c.proxyURL+"/"+serverName+"/openapi.json", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for server %s: %w", serverName, err)
	}
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch server %s spec: %w", serverName, err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d for server %s", resp.StatusCode, serverName)
	}
	
	var openAPISpec map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&openAPISpec); err != nil {
		return nil, fmt.Errorf("failed to decode server %s spec: %w", serverName, err)
	}
	
	// Extract tools from server's OpenAPI paths
	paths, ok := openAPISpec["paths"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no paths found in server %s spec", serverName)
	}
	
	var tools []Tool
	for path, pathSpec := range paths {
		if pathSpecMap, ok := pathSpec.(map[string]interface{}); ok {
			if postSpec, ok := pathSpecMap["post"].(map[string]interface{}); ok {
				toolName := strings.TrimPrefix(path, "/")
				
				tool := Tool{
					Name:        toolName,
					Description: extractDescription(postSpec),
					InputSchema: extractInputSchema(postSpec, openAPISpec),
				}
				
				tools = append(tools, tool)
			}
		}
	}
	
	return tools, nil
}

// discoverToolsFromOpenAPI discovers tools from the OpenAPI endpoint
func (c *MCPClient) discoverToolsFromOpenAPI(ctx context.Context) ([]Tool, error) {
	// Fetch OpenAPI spec
	req, err := http.NewRequestWithContext(ctx, "GET", c.proxyURL+"/openapi.json", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OpenAPI spec: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d", resp.StatusCode)
	}
	
	var openAPISpec map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&openAPISpec); err != nil {
		return nil, fmt.Errorf("failed to decode OpenAPI spec: %w", err)
	}
	
	// Extract tools from OpenAPI paths
	paths, ok := openAPISpec["paths"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no paths found in OpenAPI spec")
	}
	
	var tools []Tool
	for path, pathSpec := range paths {
		if pathSpecMap, ok := pathSpec.(map[string]interface{}); ok {
			if postSpec, ok := pathSpecMap["post"].(map[string]interface{}); ok {
				tool := Tool{
					Name:        strings.TrimPrefix(path, "/"),
					Description: extractDescription(postSpec),
					InputSchema: extractInputSchema(postSpec, openAPISpec),
				}
				tools = append(tools, tool)
			}
		}
	}
	
	return tools, nil
}

// extractDescription extracts description from OpenAPI operation
func extractDescription(postSpec map[string]interface{}) string {
	if desc, ok := postSpec["description"].(string); ok {
		return desc
	}
	if summary, ok := postSpec["summary"].(string); ok {
		return summary
	}
	return ""
}

// extractInputSchema extracts input schema from OpenAPI operation
func extractInputSchema(postSpec map[string]interface{}, openAPISpec map[string]interface{}) interface{} {
	if reqBody, ok := postSpec["requestBody"].(map[string]interface{}); ok {
		if content, ok := reqBody["content"].(map[string]interface{}); ok {
			if appJson, ok := content["application/json"].(map[string]interface{}); ok {
				if schema, ok := appJson["schema"].(map[string]interface{}); ok {
					// Check if this is a $ref reference
					if ref, isRef := schema["$ref"].(string); isRef {
						// Resolve the reference
						return resolveRef(ref, openAPISpec)
					}
					return schema
				}
			}
		}
	}
	
	// Return a valid empty schema for OpenAI API compatibility
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{},
	}
}

// resolveRef resolves a $ref reference in the OpenAPI spec
func resolveRef(ref string, openAPISpec map[string]interface{}) interface{} {
	// Handle #/components/schemas/SomeName format
	if strings.HasPrefix(ref, "#/components/schemas/") {
		schemaName := strings.TrimPrefix(ref, "#/components/schemas/")
		if components, ok := openAPISpec["components"].(map[string]interface{}); ok {
			if schemas, ok := components["schemas"].(map[string]interface{}); ok {
				if schema, ok := schemas[schemaName]; ok {
					return schema
				}
			}
		}
	}
	
	// Fallback - return a basic object schema
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{},
	}
}

// GetServerTools gets all tools for a specific server
func (c *MCPClient) GetServerTools(ctx context.Context, serverName string) ([]Tool, error) {
	// First try to get tools from server discovery (which filters properly)
	servers, err := c.discoverServersFromOpenAPI(ctx)
	if err == nil && len(servers) > 0 {
		for _, server := range servers {
			if server.Name == serverName {
				return server.Tools, nil
			}
		}
	}
	
	// Fallback to MCP JSON-RPC format if OpenAPI discovery fails
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
	
	var mcpTools []Tool
	toolsBytes, err := json.Marshal(toolsArray)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tools: %w", err)
	}
	
	if err := json.Unmarshal(toolsBytes, &mcpTools); err != nil {
		return nil, fmt.Errorf("failed to parse tools: %w", err)
	}
	
	return mcpTools, nil
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

	// Try to parse as JSON-RPC response first
	var jsonRPCResp map[string]interface{}
	if err := json.Unmarshal(respBytes, &jsonRPCResp); err == nil {
		// Check if this is a JSON-RPC response with result
		if result, ok := jsonRPCResp["result"]; ok {
			// Marshal the result part and parse as ToolResult
			resultBytes, err := json.Marshal(result)
			if err == nil {
				var toolResult ToolResult
				if err := json.Unmarshal(resultBytes, &toolResult); err == nil {
					return &toolResult, nil
				}
			}
		}
	}

	// Try to parse as ToolResult directly (legacy compatibility)
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
	
	// Use the correct server/tool endpoint format
	endpoint := fmt.Sprintf("/servers/%s/%s", serverName, toolName)
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
				// Exponential backoff: 1s, 2s, 4s, 8s...
				backoffDelay := time.Duration(1<<uint(attempt)) * time.Second
				time.Sleep(backoffDelay)
				continue
			}
			return nil, fmt.Errorf("failed to make request after %d attempts: %w", c.retryCount, err)
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode != http.StatusOK {
			respBytes, _ := io.ReadAll(httpResp.Body)
			lastErr = fmt.Errorf("HTTP error %d: %s", httpResp.StatusCode, string(respBytes))
			if attempt < c.retryCount-1 && httpResp.StatusCode >= 500 {
				// Exponential backoff: 1s, 2s, 4s, 8s...
				backoffDelay := time.Duration(1<<uint(attempt)) * time.Second
				time.Sleep(backoffDelay)
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
				// Exponential backoff: 1s, 2s, 4s, 8s...
				backoffDelay := time.Duration(1<<uint(attempt)) * time.Second
				time.Sleep(backoffDelay)
				continue
			}
			return nil, fmt.Errorf("failed to make request after %d attempts: %w", c.retryCount, err)
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode != http.StatusOK {
			respBytes, _ := io.ReadAll(httpResp.Body)
			lastErr = fmt.Errorf("HTTP error %d: %s", httpResp.StatusCode, string(respBytes))
			if attempt < c.retryCount-1 && httpResp.StatusCode >= 500 {
				// Exponential backoff: 1s, 2s, 4s, 8s...
				backoffDelay := time.Duration(1<<uint(attempt)) * time.Second
				time.Sleep(backoffDelay)
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
		return fmt.Sprintf("ERROR calling %s.%s: %v", serverName, toolName, err)
	}
	
	if result.IsError {
		return fmt.Sprintf("Tool error from %s.%s: %s", serverName, toolName, result.Content[0].Text)
	}
	
	var output strings.Builder
	output.WriteString(fmt.Sprintf("**%s.%s** executed successfully:\n\n", serverName, toolName))
	
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
		return fmt.Sprintf("ERROR listing servers: %v", err)
	}
	
	var output strings.Builder
	output.WriteString("## Available MCP Tools:\n\n")
	
	for _, server := range servers {
		tools, err := c.GetServerTools(ctx, server.Name)
		if err != nil {
			output.WriteString(fmt.Sprintf("ERROR getting tools for %s: %v\n", server.Name, err))
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