package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/discovery"
)

// mcpResponseRecorder captures HTTP responses for MCP tool calls
type mcpResponseRecorder struct {
	statusCode int
	body       []byte
	headers    http.Header
}

func (r *mcpResponseRecorder) Header() http.Header {
	if r.headers == nil {
		r.headers = make(http.Header)
	}

	return r.headers
}

func (r *mcpResponseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
}

func (r *mcpResponseRecorder) Write(body []byte) (int, error) {
	r.body = append(r.body, body...)

	return len(body), nil
}

// sendHTTPToolCall sends a tools/call request via HTTP
func (h *ProxyHandler) sendHTTPToolCall(conn *discovery.MCPConnection, request map[string]interface{}) (map[string]interface{}, error) {
	if conn.HTTPConnection == nil {
		return nil, fmt.Errorf("no HTTP connection available")
	}

	return h.sendHTTPRequestWithSession(conn.HTTPConnection, h.generateStringID(), request)
}

// SendHTTPToolCall sends a tools/call request via HTTP (exported for proxy endpoints)
func (h *ProxyHandler) SendHTTPToolCall(conn *discovery.MCPConnection, request map[string]interface{}) (map[string]interface{}, error) {
	return h.sendHTTPToolCall(conn, request)
}

// sendSSEToolCall sends a tools/call request via SSE
func (h *ProxyHandler) sendSSEToolCall(conn *discovery.MCPConnection, request map[string]interface{}) (map[string]interface{}, error) {
	if conn.SSEConnection == nil {
		return nil, fmt.Errorf("no SSE connection available")
	}

	// Get session endpoint
	sessionEndpoint, err := h.establishSSESession(conn.SSEConnection)
	if err != nil {
		return nil, fmt.Errorf("failed to establish SSE session: %w", err)
	}

	sessionURL := conn.SSEConnection.BaseURL + sessionEndpoint
	return h.sendSSERequestAndWaitForResponse(sessionURL, request)
}

// SendSSEToolCall sends a tools/call request via SSE (exported for proxy endpoints)
func (h *ProxyHandler) SendSSEToolCall(conn *discovery.MCPConnection, request map[string]interface{}) (map[string]interface{}, error) {
	return h.sendSSEToolCall(conn, request)
}

// SendStreamableHTTPToolCall sends a tools/call request via streamable HTTP
func (h *ProxyHandler) SendStreamableHTTPToolCall(serverName string, request map[string]interface{}) (map[string]interface{}, error) {
	conn, err := h.getStreamableHTTPConnection(serverName)
	if err != nil {
		return nil, fmt.Errorf("failed to get streamable HTTP connection: %w", err)
	}

	return h.sendStreamableHTTPRequest(conn, request, constants.HTTPExtendedTimeout)
}

func (h *ProxyHandler) handleDirectToolCall(w http.ResponseWriter, r *http.Request, toolName string) {
	h.logger.Info("=== DIRECT TOOL CALL DEBUG: Starting handleDirectToolCall for %s ===", toolName)
	
	// Authenticate
	apiKeyToCheck := h.APIKey
	if h.Manager != nil && h.Manager.config != nil && h.Manager.config.ProxyAuth.Enabled {
		apiKeyToCheck = h.Manager.config.ProxyAuth.APIKey
	}

	if apiKeyToCheck != "" {
		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != apiKeyToCheck {
			h.corsError(w, "Unauthorized", http.StatusUnauthorized)

			return
		}
	}

	h.logger.Info("Handling direct tool call: %s", toolName)

	// Parse request body as tool arguments
	var arguments map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&arguments); err != nil {
		h.logger.Error("Failed to decode request body for tool %s: %v", toolName, err)
		h.corsError(w, "Invalid request body", http.StatusBadRequest)

		return
	}

	// Find which server has this tool using K8s-native approach
	serverName, found := h.FindServerForTool(toolName)
	if !found {
		h.logger.Warning("Tool %s not found in any server", toolName)
		h.corsError(w, "Tool not found", http.StatusNotFound)

		return
	}

	h.logger.Info("Routing tool %s to server %s", toolName, serverName)

	mcpRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      h.generateStringID(),
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}

	// Forward to the appropriate server using K8s-native connection management
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		h.logger.Error("No connection available for server %s: %v", serverName, err)
		h.corsError(w, "Server not available", http.StatusServiceUnavailable)
		return
	}

	// Send MCP request based on protocol
	var response map[string]interface{}
	switch conn.Protocol {
	case "http":
		response, err = h.sendHTTPToolCall(conn, mcpRequest)
	case "sse":
		response, err = h.sendSSEToolCall(conn, mcpRequest)
	default:
		h.logger.Error("Unsupported protocol %s for server %s", conn.Protocol, serverName)
		h.corsError(w, "Unsupported protocol", http.StatusInternalServerError)
		return
	}

	if err != nil {
		h.logger.Error("Failed to execute tool %s on server %s: %v", toolName, serverName, err)
		h.corsError(w, "Tool execution failed", http.StatusInternalServerError)
		return
	}

	// Parse and format the MCP response
	if response != nil {
		// Check for MCP error
		if mcpError, hasError := response["error"].(map[string]interface{}); hasError {
			errorResponse := map[string]interface{}{
				"error": mcpError["message"],
			}
			if data, hasData := mcpError["data"]; hasData {
				errorResponse["details"] = data
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(errorResponse)

			return
		}

		// Check if the client expects JSON-RPC format
		userAgent := r.Header.Get("User-Agent")
		accept := r.Header.Get("Accept")
		
		// Check if this is a standard MCP client that expects JSON-RPC
		if strings.Contains(accept, "application/json") || 
		   strings.Contains(userAgent, "MCP") || 
		   strings.Contains(userAgent, "claude") || 
		   strings.Contains(userAgent, "curl") {
			h.logger.Info("Client expects JSON-RPC format - returning full response")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
			return
		}

		// Extract and format the successful result for OpenWebUI - return clean text
		if result, exists := response["result"]; exists {
			h.logger.Info("Found result in response")
			if resultMap, ok := result.(map[string]interface{}); ok {
				h.logger.Info("Result is a map")
				if content, exists := resultMap["content"]; exists {
					h.logger.Info("Found content in result: %+v", content)
					// Process the content for OpenWebUI - extract text from MCP content array
					cleanResult := h.processMCPContent(content)
					h.logger.Info("processMCPContent returned: %+v (type: %T)", cleanResult, cleanResult)
					
					// For OpenWebUI, we want just the text content, not JSON
					if cleanText, ok := cleanResult.(string); ok {
						h.logger.Info("Successfully converted to string: %s", cleanText)
						w.Header().Set("Content-Type", "text/plain")
						_, _ = w.Write([]byte(cleanText))
						return
					} else {
						h.logger.Warning("cleanResult is not a string, type: %T", cleanResult)
					}
				} else {
					h.logger.Warning("No content found in result")
				}
			} else {
				h.logger.Warning("Result is not a map, type: %T", result)
			}
		} else {
			h.logger.Warning("No result found in response")
		}

		// Fallback to original response if formatting fails
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	} else {
		h.corsError(w, "No response from server", http.StatusInternalServerError)
	}
}

// processMCPContent processes MCP content for OpenWebUI compatibility
func (h *ProxyHandler) processMCPContent(content interface{}) interface{} {
	h.logger.Info("processMCPContent called with: %+v (type: %T)", content, content)
	
	if contentArray, ok := content.([]interface{}); ok {
		h.logger.Info("Content is an array with %d items", len(contentArray))
		var textParts []string
		for i, item := range contentArray {
			h.logger.Info("Processing item %d: %+v (type: %T)", i, item, item)
			if itemMap, ok := item.(map[string]interface{}); ok {
				if itemType, ok := itemMap["type"].(string); ok {
					h.logger.Info("Item type: %s", itemType)
					switch itemType {
					case "text":
						if text, ok := itemMap["text"].(string); ok {
							h.logger.Info("Found text: %s", text)
							textParts = append(textParts, text)
						}
					case "image":
						if data, ok := itemMap["data"].(string); ok {
							if mimeType, ok := itemMap["mimeType"].(string); ok {
								imageURL := fmt.Sprintf("data:%s;base64,%s", mimeType, data)
								h.logger.Info("Found image: %s", imageURL)
								textParts = append(textParts, imageURL)
							}
						}
					// For other types, we skip them for OpenWebUI simplicity
					}
				}
			}
		}

		// Join all text parts with newlines for OpenWebUI
		if len(textParts) > 0 {
			result := strings.Join(textParts, "\n")
			h.logger.Info("Returning joined text: %s", result)
			return result
		}
		h.logger.Info("No text parts found, returning original content")
	} else {
		h.logger.Warning("Content is not an array, type: %T", content)
	}

	return content
}

func (h *ProxyHandler) handleServerForward(w http.ResponseWriter, r *http.Request, serverName string, instance *ServerInstance) {
	// Authentication check - validate before processing the request
	if !h.authenticateRequest(w, r, serverName, instance) {

		return // Authentication failed, response already sent
	}

	w.Header().Set("Content-Type", "application/json")

	// Read request body ONCE and store it
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("Failed to read request body for %s: %v", serverName, err)
		h.sendMCPError(w, nil, -32700, "Error reading request body")

		return
	}

	// Parse JSON payload from the stored body
	var requestPayload map[string]interface{}
	if err := json.Unmarshal(body, &requestPayload); err != nil {
		h.logger.Error("Invalid JSON in request for %s: %v. Body: %s", serverName, err, string(body))
		h.sendMCPError(w, nil, -32700, "Invalid JSON in request")

		return
	}

	reqIDVal := requestPayload["id"]
	reqMethodVal, _ := requestPayload["method"].(string)

	// ONLY handle proxy-specific standard methods, NOT server methods
	if isProxyStandardMethod(reqMethodVal) {
		h.handleProxyStandardMethod(w, r, requestPayload, reqIDVal, reqMethodVal)

		return
	}

	// FORWARD ALL OTHER METHODS TO THE ACTUAL MCP SERVERS
	// Get server config
	serverConfig, exists := h.Manager.config.Servers[serverName]
	if !exists {
		h.logger.Error("Server config not found for %s", serverName)
		h.sendMCPError(w, reqIDVal, -32602, "Server configuration not found")

		return
	}

	// Determine transport protocol
	protocolType := serverConfig.Protocol
	if protocolType == "" {
		protocolType = "stdio" // default
	}

	h.logger.Info("Forwarding request to server '%s' using '%s' transport: Method=%s, ID=%v",
		serverName, protocolType, reqMethodVal, reqIDVal)

	// Route based on transport protocol - pass the original body bytes
	switch protocolType {
	case "http":
		h.handleHTTPServerRequestWithBody(w, r, serverName, instance, body, reqIDVal, reqMethodVal)
	case "sse":
		h.handleSSEServerRequest(w, r, serverName, instance, requestPayload, reqIDVal, reqMethodVal)
	case "stdio":
		if serverConfig.StdioHosterPort > 0 {
			h.handleSocatSTDIOServerRequest(w, r, serverName, requestPayload, reqIDVal, reqMethodVal)
		} else {
			h.handleSTDIOServerRequest(w, r, serverName, requestPayload, reqIDVal, reqMethodVal)
		}
	default:
		h.logger.Error("Unsupported transport protocol '%s' for server %s", protocolType, serverName)
		h.sendMCPError(w, reqIDVal, -32602, fmt.Sprintf("Unsupported transport protocol: %s", protocolType))
	}
}

