package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/openapi"
)


func (h *ProxyHandler) discoverServerTools(serverName string) ([]openapi.ToolSpec, error) {
	h.logger.Info("Discovering tools from server %s via internal proxy methods", serverName)

	// Create tools/list request
	toolsRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      h.getNextRequestID(),
		"method":  "tools/list",
	}

	// Check if server exists using K8s-native discovery
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		h.logger.Warning("Server connection %s not found: %v", serverName, err)
		return nil, fmt.Errorf("server connection %s not found: %w", serverName, err)
	}

	// Determine the transport protocol from connection
	protocol := conn.Protocol
	if protocol == "" {
		protocol = "http" // K8s-native default
	}

	// Route based on protocol
	h.logger.Info("Server %s using protocol: %s", serverName, protocol)

	// Retry logic with exponential backoff
	maxRetries := 3
	baseTimeout := constants.ToolDiscoveryTimeout

	for attempt := 1; attempt <= maxRetries; attempt++ {
		h.logger.Debug("Tool discovery attempt %d/%d for server %s (protocol: %s)", attempt, maxRetries, serverName, protocol)
		timeout := time.Duration(attempt) * baseTimeout // 10s, 20s, 30s

		var response map[string]interface{}
		var err error

		switch protocol {
		case "sse":
			// Use SSE discovery
			response, err = h.sendSSEToolsRequestWithRetry(serverName, toolsRequest, timeout, attempt)
		case "http":
			// Use HTTP discovery
			response, err = h.sendHTTPToolsRequestWithRetry(serverName, toolsRequest, timeout, attempt)
		case "stdio":
			// STDIO transport no longer supported
			h.logger.Warning("STDIO transport no longer supported for server %s", serverName)
			return nil, fmt.Errorf("STDIO transport no longer supported for server %s", serverName)
		default:
			h.logger.Warning("Unknown protocol %s for server %s", protocol, serverName)

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
				h.logger.Info("Successfully discovered %d tools from %s: %v", len(specs), serverName, toolNames)

				return specs, nil
			}
			if parseErr != nil {
				h.logger.Warning("Failed to parse tools response from %s on attempt %d: %v", serverName, attempt, parseErr)
				err = parseErr
			}
		}

		// Log the failure and decide whether to retry
		isTimeout := strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "i/o timeout")
		isConnectionError := strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host")

		if attempt < maxRetries && (isTimeout || isConnectionError) {
			waitTime := time.Duration(attempt*constants.ToolDiscoveryRetryMultiplier) * time.Second // 2s, 4s wait between retries
			h.logger.Warning("Tool discovery attempt %d/%d failed for %s (%v), retrying in %v", attempt, maxRetries, serverName, err, waitTime)
			time.Sleep(waitTime)

			continue
		}

		// Final attempt failed or non-retryable error
		h.logger.Warning("Tool discovery failed for %s after %d attempts: %v, using generic fallback", serverName, attempt, err)

		break
	}

	// All retries failed - return error instead of generic fallback

	return nil, fmt.Errorf("failed to discover tools after %d attempts", maxRetries)
}

func (h *ProxyHandler) sendSSEToolsRequestWithRetry(serverName string, requestPayload map[string]interface{}, timeout time.Duration, attempt int) (map[string]interface{}, error) {
	h.logger.Debug("Attempting enhanced SSE request to %s (attempt %d, timeout %v)", serverName, attempt, timeout)

	return h.sendOptimalSSERequest(serverName, requestPayload)
}

func (h *ProxyHandler) sendHTTPToolsRequestWithRetry(serverName string, requestPayload map[string]interface{}, timeout time.Duration, attempt int) (map[string]interface{}, error) {
	h.logger.Debug("Attempting HTTP request to %s (attempt %d, timeout %v)", serverName, attempt, timeout)
	
	// Get connection using K8s-native discovery
	mcpConn, connErr := h.ConnectionManager.GetConnection(serverName)
	if connErr != nil {
		return nil, connErr
	}
	
	if mcpConn.HTTPConnection == nil {
		return nil, fmt.Errorf("no HTTP connection available for server %s", serverName)
	}

	return h.sendHTTPRequestWithSession(mcpConn.HTTPConnection, mcpConn.HTTPConnection.SessionID, requestPayload)
}

func (h *ProxyHandler) parseToolsResponse(serverName string, response map[string]interface{}) ([]openapi.ToolSpec, error) {
	h.logger.Debug("Parsing tools response for %s: %v", serverName, response)

	// Check for JSON-RPC error
	if errResp, ok := response["error"].(map[string]interface{}); ok {

		return nil, fmt.Errorf("server returned error: %v", errResp)
	}

	// Parse the tools from the response
	var specs []openapi.ToolSpec
	if result, ok := response["result"].(map[string]interface{}); ok {
		h.logger.Debug("Found result object for %s: %v", serverName, result)
		if tools, ok := result["tools"].([]interface{}); ok {
			h.logger.Debug("Found tools array for %s with %d tools", serverName, len(tools))
			for i, tool := range tools {
				if toolMap, ok := tool.(map[string]interface{}); ok {
					spec := openapi.ToolSpec{Type: "function"}
					if name, ok := toolMap["name"].(string); ok {
						spec.Name = name
					} else {
						h.logger.Warning("Tool %d in %s missing name field: %v", i, serverName, toolMap)

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
					h.logger.Warning("Tool %d in %s is not a map: %v", i, serverName, tool)
				}
			}
		} else {
			h.logger.Warning("No 'tools' array found in result for %s. Result keys: %v", serverName, getKeys(result))
		}
	} else {
		h.logger.Warning("No 'result' object found in response for %s. Response keys: %v", serverName, getKeys(response))
	}

	h.logger.Debug("Parsed %d tools for %s: %v", len(specs), serverName, getToolNames(specs))

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
