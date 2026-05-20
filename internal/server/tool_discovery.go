package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/discovery"
	"github.com/phildougherty/m8e/internal/openapi"
)

func (h *ProxyHandler) discoverServerTools(ctx context.Context, serverName string) ([]openapi.ToolSpec, error) {
	h.Logger.Info("Discovering tools from server %s via internal proxy methods", serverName)

	// Create tools/list request
	toolsRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      h.getNextRequestID(),
		"method":  "tools/list",
	}

	// Check if server exists using K8s-native discovery
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		h.Logger.Warning("Server connection %s not found: %v", serverName, err)
		return nil, fmt.Errorf("server connection %s not found: %w", serverName, err)
	}

	// Determine the transport protocol from connection
	protocol := conn.Protocol
	if protocol == "" {
		protocol = "http" // K8s-native default
	}

	// Route based on protocol
	h.Logger.Info("Server %s using protocol: %s", serverName, protocol)

	// Retry logic with exponential backoff
	maxRetries := 3
	baseTimeout := constants.ToolDiscoveryTimeout

	for attempt := 1; attempt <= maxRetries; attempt++ {
		h.Logger.Debug("Tool discovery attempt %d/%d for server %s (protocol: %s)", attempt, maxRetries, serverName, protocol)
		timeout := time.Duration(attempt) * baseTimeout // 10s, 20s, 30s

		var response map[string]interface{}
		var err error

		switch protocol {
		case "sse":
			// Use SSE discovery
			response, err = h.sendSSEToolsRequestWithRetry(ctx, serverName, toolsRequest, timeout, attempt)
		case "http":
			// Use HTTP discovery
			response, err = h.sendHTTPToolsRequestWithRetry(ctx, serverName, toolsRequest, timeout, attempt)
		case "stdio":
			// STDIO transport no longer supported
			h.Logger.Warning("STDIO transport no longer supported for server %s", serverName)
			return nil, fmt.Errorf("STDIO transport no longer supported for server %s", serverName)
		default:
			h.Logger.Warning("Unknown protocol %s for server %s", protocol, serverName)

			return nil, fmt.Errorf("unknown protocol %s for server %s", protocol, serverName)
		}

		if err == nil {
			// Success - parse and return tools
			specs, parseErr := h.parseToolsResponse(serverName, response)
			if parseErr == nil && len(specs) > 0 {
				toolNames := make([]string, len(specs))
				for i, spec := range specs {
					toolNames[i] = spec.Name
				}
				h.Logger.Info("Successfully discovered %d tools from %s: %v", len(specs), serverName, toolNames)

				return specs, nil
			}
			if parseErr != nil {
				h.Logger.Warning("Failed to parse tools response from %s on attempt %d: %v", serverName, attempt, parseErr)
				err = parseErr
			}
		}

		// Log the failure and decide whether to retry
		isTimeout := strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "i/o timeout")
		isConnectionError := strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host")

		if attempt < maxRetries && (isTimeout || isConnectionError) {
			waitTime := time.Duration(attempt*constants.ToolDiscoveryRetryMultiplier) * time.Second // 2s, 4s wait between retries
			h.Logger.Warning("Tool discovery attempt %d/%d failed for %s (%v), retrying in %v", attempt, maxRetries, serverName, err, waitTime)
			select {
			case <-time.After(waitTime):
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			continue
		}

		// Final attempt failed or non-retryable error
		h.Logger.Warning("Tool discovery failed for %s after %d attempts: %v, using generic fallback", serverName, attempt, err)

		break
	}

	// All retries failed - return error instead of generic fallback

	return nil, fmt.Errorf("failed to discover tools after %d attempts", maxRetries)
}

func (h *ProxyHandler) sendSSEToolsRequestWithRetry(ctx context.Context, serverName string, requestPayload map[string]interface{}, timeout time.Duration, attempt int) (map[string]interface{}, error) {
	h.Logger.Debug("Attempting enhanced SSE request to %s (attempt %d, timeout %v)", serverName, attempt, timeout)

	_ = ctx // ctx is plumbed through but sendOptimalSSERequest does not yet take one
	return h.sendOptimalSSERequest(serverName, requestPayload)
}

func (h *ProxyHandler) sendHTTPToolsRequestWithRetry(ctx context.Context, serverName string, requestPayload map[string]interface{}, timeout time.Duration, attempt int) (map[string]interface{}, error) {
	h.Logger.Debug("Attempting HTTP request to %s (attempt %d, timeout %v)", serverName, attempt, timeout)

	// Get connection using K8s-native discovery
	mcpConn, connErr := h.ConnectionManager.GetConnection(serverName)
	if connErr != nil {
		return nil, connErr
	}

	if mcpConn.HTTPConnection == nil {
		return nil, fmt.Errorf("no HTTP connection available for server %s", serverName)
	}

	return h.sendHTTPRequestWithSession(ctx, mcpConn.HTTPConnection, mcpConn.HTTPConnection.SessionID, requestPayload)
}

func (h *ProxyHandler) parseToolsResponse(serverName string, response map[string]interface{}) ([]openapi.ToolSpec, error) {
	h.Logger.Debug("Parsing tools response for %s: %v", serverName, response)

	// Check for JSON-RPC error
	if errResp, ok := response["error"].(map[string]interface{}); ok {

		return nil, fmt.Errorf("server returned error: %v", errResp)
	}

	// Parse the tools from the response
	var specs []openapi.ToolSpec
	if result, ok := response["result"].(map[string]interface{}); ok {
		h.Logger.Debug("Found result object for %s: %v", serverName, result)
		if tools, ok := result["tools"].([]interface{}); ok {
			h.Logger.Debug("Found tools array for %s with %d tools", serverName, len(tools))
			for i, tool := range tools {
				if toolMap, ok := tool.(map[string]interface{}); ok {
					spec := openapi.ToolSpec{Type: "function"}
					if name, ok := toolMap["name"].(string); ok {
						spec.Name = name
					} else {
						h.Logger.Warning("Tool %d in %s missing name field: %v", i, serverName, toolMap)

						continue
					}

					if desc, ok := toolMap["description"].(string); ok {
						spec.Description = desc
					} else {
						spec.Description = fmt.Sprintf("Tool from %s server", serverName)
					}

					if inputSchema, ok := toolMap["inputSchema"].(map[string]interface{}); ok {
						spec.Parameters = inputSchema
					} else {
						spec.Parameters = map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
							"required":   []string{},
						}
					}

					specs = append(specs, spec)
				} else {
					h.Logger.Warning("Tool %d in %s is not a map: %v", i, serverName, tool)
				}
			}
		} else {
			h.Logger.Warning("No 'tools' array found in result for %s. Result keys: %v", serverName, getKeys(result))
		}
	} else {
		h.Logger.Warning("No 'result' object found in response for %s. Response keys: %v", serverName, getKeys(response))
	}

	h.Logger.Debug("Parsed %d tools for %s: %v", len(specs), serverName, getToolNames(specs))

	if len(specs) == 0 {

		return nil, fmt.Errorf("no tools found in response")
	}

	return specs, nil
}

// Helper functions for debugging
func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}

func getToolNames(specs []openapi.ToolSpec) []string {
	names := make([]string, len(specs))
	for i, spec := range specs {
		names[i] = spec.Name
	}

	return names
}

// Tool discovery cache orchestration. The cache state itself lives in
// *ToolCache; this is the proxy-side wiring that drives refreshes.

// toolDiscoveryCacheTTL is how long a discovered tool set is trusted before the
// next lookup forces a refresh.
const toolDiscoveryCacheTTL = 10 * time.Minute

// toolDiscoveryLoop periodically discovers tools from connected services
func (h *ProxyHandler) toolDiscoveryLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-h.lifecycle.done():
			return
		case <-ticker.C:
			h.discoverTools()
		}
	}
}

// discoverTools discovers available tools from all connected services and
// replaces the tool cache with the fresh result.
func (h *ProxyHandler) discoverTools() {
	h.Logger.Debug("Discovering tools from connected services")

	connections := h.ConnectionManager.GetAllConnections()
	h.Logger.Debug("Got %d connections from ConnectionManager", len(connections))

	freshCache := make(map[string]string)

	connectedCount := 0
	for serverName, conn := range connections {
		h.Logger.Debug("Checking server %s with status %s", serverName, conn.Status)
		if conn.Status != "connected" {
			h.Logger.Debug("Skipping server %s - not connected (status: %s)", serverName, conn.Status)
			continue
		}
		connectedCount++

		// Discover tools based on protocol
		h.Logger.Debug("Discovering tools for connected server %s", serverName)
		tools := h.discoverK8sServerTools(serverName, conn)
		h.Logger.Debug("Server %s returned %d tools", serverName, len(tools))

		for toolName, owningServer := range tools {
			h.Logger.Debug("Adding tool %s -> %s to cache", toolName, owningServer)
			freshCache[toolName] = owningServer
		}
	}

	h.toolCache.replace(freshCache, toolDiscoveryCacheTTL)
	h.Logger.Info("Discovered %d tools from %d connected servers (out of %d total)",
		len(freshCache), connectedCount, len(connections))
}

// DiscoverServerTools discovers tools from a specific server (public method for cmd/proxy.go)
func (h *ProxyHandler) DiscoverServerTools(serverName string) ([]Tool, error) {
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		h.Logger.Warning("No connection for %s: %v", serverName, err)
		return nil, fmt.Errorf("no connection available for server %s: %w", serverName, err)
	}

	// Note: For tool discovery, we don't have access to the original HTTP request
	// The OAuth token should have already been set when the connection was first used

	h.Logger.Info("Connection status for %s: protocol=%s, status=%s",
		serverName, conn.Protocol, conn.Status)

	// Check if the connection is healthy before attempting tool discovery
	if conn.Status != "connected" {
		h.Logger.Warning("Server %s is not connected (status: %s) - this may be due to authentication failure", serverName, conn.Status)
		return nil, fmt.Errorf("server %s is not connected (status: %s)", serverName, conn.Status)
	}

	// Make MCP tools/list call to discover actual tools
	tools, err := h.makeToolsListRequest(serverName, conn)
	if err != nil {
		h.Logger.Warning("Failed to discover tools for %s: %v", serverName, err)
		// Return the error - no placeholder tools
		return nil, fmt.Errorf("tool discovery failed for %s: %w", serverName, err)
	}

	return tools, nil
}

// FindServerForTool finds which server has a specific tool using cached discovery
func (h *ProxyHandler) FindServerForTool(toolName string) (string, bool) {
	// Check if cache needs refresh (expired or empty)
	cacheEmpty, cacheExpired, cacheSize := h.toolCache.state()

	h.Logger.Debug("FindServerForTool: toolName=%s, cacheEmpty=%v, cacheExpired=%v, cacheSize=%d",
		toolName, cacheEmpty, cacheExpired, cacheSize)

	if cacheEmpty || cacheExpired {
		h.Logger.Info("Tool cache is empty or expired, refreshing...")
		h.discoverTools()

		// Check cache size after refresh
		newCacheSize := h.toolCache.size()
		h.Logger.Info("Tool cache refreshed: old size=%d, new size=%d", cacheSize, newCacheSize)
	}

	// Now check the unified cache
	serverName, found := h.toolCache.lookup(toolName)
	if found {
		h.Logger.Debug("Found tool %s in server %s via unified cache", toolName, serverName)
		return serverName, true
	}

	h.Logger.Warning("Tool %s not found in unified cache of %d tools", toolName, h.toolCache.size())

	// Debug: Print all cached tools
	h.Logger.Debug("Available tools in cache: %v", h.toolCache.snapshot())

	return "", false
}

// discoverK8sServerTools discovers tools from a specific server
func (h *ProxyHandler) discoverK8sServerTools(serverName string, conn *discovery.MCPConnection) map[string]string {
	tools := make(map[string]string)

	// Only attempt tool discovery if the server has the "tools" capability
	hasToolsCapability := false
	for _, capability := range conn.Capabilities {
		if capability == "tools" {
			hasToolsCapability = true
			break
		}
	}

	if !hasToolsCapability {
		h.Logger.Debug("Server %s does not have 'tools' capability", serverName)
		return tools
	}

	// Make actual MCP tools/list call to discover real tools
	h.Logger.Debug("Discovering tools for server %s using protocol %s", serverName, conn.Protocol)

	actualTools, err := h.makeToolsListRequest(serverName, conn)
	if err != nil {
		h.Logger.Warning("Failed to discover tools for server %s: %v", serverName, err)
		// Don't add placeholder tools on error - return empty map
		return tools
	}

	// Add real tools to cache
	for _, tool := range actualTools {
		tools[tool.Name] = serverName
		h.Logger.Debug("Discovered tool %s from server %s", tool.Name, serverName)
	}

	h.Logger.Info("Successfully discovered %d tools from server %s", len(tools), serverName)
	return tools
}
