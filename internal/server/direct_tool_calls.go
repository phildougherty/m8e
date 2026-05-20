package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/discovery"
	"github.com/phildougherty/m8e/internal/protocol"
)

// sendHTTPToolCall sends a tools/call request via HTTP
func (h *ProxyHandler) sendHTTPToolCall(conn *discovery.MCPConnection, request map[string]interface{}) (map[string]interface{}, error) {
	if conn.HTTPConnection == nil {
		return nil, fmt.Errorf("no HTTP connection available")
	}

	return h.sendHTTPRequestWithSession(context.Background(), conn.HTTPConnection, h.generateStringID(), request)
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
	ctx := context.Background()
	sessionEndpoint, err := h.establishSSESession(ctx, conn.SSEConnection)
	if err != nil {
		return nil, fmt.Errorf("failed to establish SSE session: %w", err)
	}

	sessionURL := conn.SSEConnection.BaseURL + sessionEndpoint
	return h.sendSSERequestAndWaitForResponseWithAuth(ctx, sessionURL, conn.SSEConnection.AuthToken, request)
}

// SendSSEToolCall sends a tools/call request via SSE (exported for proxy endpoints)
func (h *ProxyHandler) SendSSEToolCall(conn *discovery.MCPConnection, request map[string]interface{}) (map[string]interface{}, error) {
	return h.sendSSEToolCall(conn, request)
}

// SendStreamableHTTPToolCall sends a tools/call request via streamable HTTP
func (h *ProxyHandler) SendStreamableHTTPToolCall(serverName string, request map[string]interface{}) (map[string]interface{}, error) {
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		return nil, fmt.Errorf("failed to get streamable HTTP connection: %w", err)
	}

	if conn.StreamableHTTPConnection == nil {
		return nil, fmt.Errorf("no streamable HTTP connection available for server %s", serverName)
	}

	// Use the K8s-native streamable HTTP request handling
	return h.sendStreamableHTTPRequestWithSession(context.Background(), conn.StreamableHTTPConnection, "", request)
}

func (h *ProxyHandler) handleDirectToolCall(w http.ResponseWriter, r *http.Request, toolName string) {
	h.Logger.Info("=== DIRECT TOOL CALL DEBUG: Starting handleDirectToolCall for %s ===", toolName)

	// Instrument: this is the production direct-tool-call path reached from
	// ServeHTTP. Record the final status on the way out; resolve the server
	// label once FindServerForTool maps the tool to a backend.
	start := time.Now()
	rec := newStatusRecorder(w)
	w = rec
	metricServer := toolName
	connOpened := false
	defer func() {
		if connOpened {
			h.metrics.ConnectionClosed(metricServer)
		}
		h.recordProxyRequest(metricServer, r.Method, rec.status, start)
	}()

	// Authenticate
	apiKeyToCheck := h.APIKey

	if apiKeyToCheck != "" {
		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != apiKeyToCheck {
			h.corsError(w, "Unauthorized", http.StatusUnauthorized)

			return
		}
	}

	h.Logger.Info("Handling direct tool call: %s", toolName)

	// Parse request body as tool arguments
	var arguments map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&arguments); err != nil {
		h.Logger.Error("Failed to decode request body for tool %s: %v", toolName, err)
		h.corsError(w, "Invalid request body", http.StatusBadRequest)

		return
	}

	// Find which server has this tool using K8s-native approach
	serverName, found := h.FindServerForTool(toolName)
	if !found {
		h.Logger.Warning("Tool %s not found in any server", toolName)
		h.corsError(w, "Tool not found", http.StatusNotFound)

		return
	}
	metricServer = serverName
	h.metrics.ConnectionOpened(serverName)
	connOpened = true

	h.Logger.Info("Routing tool %s to server %s", toolName, serverName)

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
		h.Logger.Error("No connection available for server %s: %v", serverName, err)
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
		h.Logger.Error("Unsupported protocol %s for server %s", conn.Protocol, serverName)
		h.corsError(w, "Unsupported protocol", http.StatusInternalServerError)
		return
	}

	if err != nil {
		h.Logger.Error("Failed to execute tool %s on server %s: %v", toolName, serverName, err)
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
			h.Logger.Info("Client expects JSON-RPC format - returning full response")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
			return
		}

		// Extract and format the successful result for OpenWebUI - return clean text
		if result, exists := response["result"]; exists {
			h.Logger.Info("Found result in response")
			if resultMap, ok := result.(map[string]interface{}); ok {
				h.Logger.Info("Result is a map")
				if content, exists := resultMap["content"]; exists {
					h.Logger.Info("Found content in result: %+v", content)
					// Process the content for OpenWebUI - extract text from MCP content array
					cleanResult := h.processMCPContent(content)
					h.Logger.Info("processMCPContent returned: %+v (type: %T)", cleanResult, cleanResult)

					// For OpenWebUI, we want just the text content, not JSON
					if cleanText, ok := cleanResult.(string); ok {
						h.Logger.Info("Successfully converted to string: %s", cleanText)
						w.Header().Set("Content-Type", "text/plain")
						_, _ = w.Write([]byte(cleanText))
						return
					} else {
						h.Logger.Warning("cleanResult is not a string, type: %T", cleanResult)
					}
				} else {
					h.Logger.Warning("No content found in result")
				}
			} else {
				h.Logger.Warning("Result is not a map, type: %T", result)
			}
		} else {
			h.Logger.Warning("No result found in response")
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
	h.Logger.Info("processMCPContent called with: %+v (type: %T)", content, content)

	if contentArray, ok := content.([]interface{}); ok {
		h.Logger.Info("Content is an array with %d items", len(contentArray))
		var textParts []string
		for i, item := range contentArray {
			h.Logger.Info("Processing item %d: %+v (type: %T)", i, item, item)
			if itemMap, ok := item.(map[string]interface{}); ok {
				if itemType, ok := itemMap["type"].(string); ok {
					h.Logger.Info("Item type: %s", itemType)
					switch itemType {
					case "text":
						if text, ok := itemMap["text"].(string); ok {
							h.Logger.Info("Found text: %s", text)
							textParts = append(textParts, text)
						}
					case "image":
						if data, ok := itemMap["data"].(string); ok {
							if mimeType, ok := itemMap["mimeType"].(string); ok {
								imageURL := fmt.Sprintf("data:%s;base64,%s", mimeType, data)
								h.Logger.Info("Found image: %s", imageURL)
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
			h.Logger.Info("Returning joined text: %s", result)
			return result
		}
		h.Logger.Info("No text parts found, returning original content")
	} else {
		h.Logger.Warning("Content is not an array, type: %T", content)
	}

	return content
}

func (h *ProxyHandler) handleServerForward(w http.ResponseWriter, r *http.Request, serverName string) {
	// Instrument: this is the production server-routing path reached from
	// ServeHTTP. Record the final status on the way out and bracket the
	// active-connection gauge around the in-flight request to this server.
	start := time.Now()
	rec := newStatusRecorder(w)
	w = rec
	h.metrics.ConnectionOpened(serverName)
	defer func() {
		h.metrics.ConnectionClosed(serverName)
		h.recordProxyRequest(serverName, r.Method, rec.status, start)
	}()

	// Handle DELETE requests for session termination
	if r.Method == "DELETE" {
		h.handleSessionTerminationInline(w, r, serverName)
		return
	}

	// Authentication check - validate before processing the request
	if !h.authenticateRequest(w, r, serverName) {

		return // Authentication failed, response already sent
	}

	w.Header().Set("Content-Type", "application/json")

	// Read request body ONCE and store it
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.Logger.Error("Failed to read request body for %s: %v", serverName, err)
		h.sendMCPError(w, nil, -32700, "Error reading request body")

		return
	}

	// Parse JSON payload from the stored body
	var requestPayload map[string]interface{}
	if err := json.Unmarshal(body, &requestPayload); err != nil {
		h.Logger.Error("Invalid JSON in request for %s: %v. Body: %s", serverName, err, string(body))
		h.sendMCPError(w, nil, -32700, "Invalid JSON in request")

		return
	}

	reqIDVal := requestPayload["id"]
	reqMethodVal, _ := requestPayload["method"].(string)

	// If this body is a JSON-RPC RESPONSE (no method, has id and result|error),
	// it's the client replying to a server-initiated request (sampling/
	// createMessage, elicitation/create, roots/list) that the proxy forwarded
	// to the client mid-stream. Route it to the pending wait registered by
	// the in-flight bidirectional relay rather than forwarding it as a method
	// call to the backend.
	_, hasResult := requestPayload["result"]
	_, hasError := requestPayload["error"]
	if reqMethodVal == "" && reqIDVal != nil && (hasResult || hasError) {
		if h.routeClientReply(w, r, requestPayload, body) {

			return
		}
		// If we couldn't route it (no matching pending wait), fall through to
		// the normal error path below — the server side wasn't waiting.
		h.Logger.Warning("Received JSON-RPC response without matching pending request: id=%v", reqIDVal)
		w.WriteHeader(http.StatusAccepted)

		return
	}

	// ONLY handle proxy-specific standard methods, NOT server methods
	if isProxyStandardMethod(reqMethodVal) {
		h.handleProxyStandardMethod(w, r, requestPayload, reqIDVal, reqMethodVal)

		return
	}

	// Handle notification-related methods first
	switch reqMethodVal {
	case "resources/subscribe":
		h.handleResourceSubscribe(w, r, serverName, requestPayload)
		return
	case "resources/unsubscribe":
		h.handleResourceUnsubscribe(w, r, serverName, requestPayload)
		return
	case "tools/list":
		// Check if client wants change notifications
		if h.supportsNotifications(r) {
			clientID := h.getClientID(r)
			sessionID := r.Header.Get("Mcp-Session-Id")
			notifyFunc := func(notification *protocol.ChangeNotification) error {
				return h.sendChangeNotificationToClient(clientID, notification)
			}
			h.changeNotificationManager.SubscribeToToolChanges(clientID, sessionID, notifyFunc)
			h.Logger.Debug("Client %s subscribed to tool changes for server %s", clientID, serverName)
		}
		// Continue to forward the request
	case "prompts/list":
		// Check if client wants change notifications
		if h.supportsNotifications(r) {
			clientID := h.getClientID(r)
			sessionID := r.Header.Get("Mcp-Session-Id")
			notifyFunc := func(notification *protocol.ChangeNotification) error {
				return h.sendChangeNotificationToClient(clientID, notification)
			}
			h.changeNotificationManager.SubscribeToPromptChanges(clientID, sessionID, notifyFunc)
			h.Logger.Debug("Client %s subscribed to prompt changes for server %s", clientID, serverName)
		}
		// Continue to forward the request
	}

	// FORWARD ALL OTHER METHODS TO THE ACTUAL MCP SERVERS
	// Get server connection using K8s-native discovery
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		h.Logger.Error("Server connection not found for %s: %v", serverName, err)
		h.sendMCPError(w, reqIDVal, -32602, "Server not available")

		return
	}

	// Determine transport protocol from connection
	protocolType := conn.Protocol
	if protocolType == "" {
		protocolType = "http" // K8s-native default
	}

	h.Logger.Info("Forwarding request to server '%s' using '%s' transport: Method=%s, ID=%v",
		serverName, protocolType, reqMethodVal, reqIDVal)

	// Route based on transport protocol - pass the original body bytes
	switch protocolType {
	case "http":
		h.handleHTTPServerRequestWithBody(w, r, serverName, body, reqIDVal, reqMethodVal)
	case "sse":
		h.handleSSEServerRequest(w, r, serverName, requestPayload, reqIDVal, reqMethodVal)
	case "stdio":
		h.Logger.Error("STDIO transport no longer supported for server %s", serverName)
		h.sendMCPError(w, reqIDVal, -32602, "STDIO transport no longer supported")
	default:
		h.Logger.Error("Unsupported transport protocol '%s' for server %s", protocolType, serverName)
		h.sendMCPError(w, reqIDVal, -32602, fmt.Sprintf("Unsupported transport protocol: %s", protocolType))
	}
}

// handleSessionTerminationInline handles DELETE requests for session termination
func (h *ProxyHandler) handleSessionTerminationInline(w http.ResponseWriter, r *http.Request, serverName string) {
	clientSessionID := r.Header.Get("Mcp-Session-Id")
	if clientSessionID == "" {
		h.corsError(w, "Mcp-Session-Id header required for session termination (DELETE)", http.StatusBadRequest)
		return
	}

	h.Logger.Info("Received DELETE request to terminate session '%s' for server '%s'", clientSessionID, serverName)

	// Ask the backend server to terminate its session using K8s-native discovery
	_, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		h.Logger.Warning("Cannot terminate session: No connection to server '%s' (%v)", serverName, err)
		h.corsError(w, "Server not connected via proxy", http.StatusBadGateway)
		return
	}

	// In K8s-native proxy, session termination is handled by the connection manager
	// Sessions are managed per-protocol connection type

	h.Logger.Info("Session '%s' terminated for server '%s'", clientSessionID, serverName)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":     "session_terminated",
		"session_id": clientSessionID,
		"server":     serverName,
	}); err != nil {
		h.Logger.Error("Failed to encode session termination response: %v", err)
	}
}

// HandleDirectToolCall handles direct tool calls without MCP protocol overhead
func (h *ProxyHandler) HandleDirectToolCall(w http.ResponseWriter, r *http.Request, toolName string) {
	// Instrument the request: wrap w so the metric records the FINAL status
	// sent to the client, and record on the way out (defer) so auth failures,
	// 404s and error paths are all counted. The server label is resolved once
	// the tool is mapped to a backend server; until then it is the tool name.
	start := time.Now()
	rec := newStatusRecorder(w)
	w = rec
	metricServer := toolName
	defer func() {
		h.recordProxyRequest(metricServer, r.Method, rec.status, start)
	}()

	// Check authentication
	if !h.checkAuth(r) {
		h.corsError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Set CORS headers
	h.setCORSHeaders(w)

	// Handle OPTIONS requests
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Find which server has this tool. Use FindServerForTool, not a bare
	// toolCache.lookup: the cache is cold at startup and expires on a TTL,
	// and FindServerForTool refreshes it on a miss. A bare lookup here meant
	// the first direct tool call after startup (or after TTL expiry) 404'd
	// even for tools that exist.
	serverName, found := h.FindServerForTool(toolName)
	if !found {
		h.corsError(w, "Tool not found", http.StatusNotFound)
		return
	}
	metricServer = serverName

	// Bracket the active-connection gauge around the in-flight tool call
	// against this server, mirroring the request lifecycle.
	h.metrics.ConnectionOpened(serverName)
	defer h.metrics.ConnectionClosed(serverName)

	// Parse request body
	var arguments map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&arguments); err != nil {
		h.corsError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Create MCP tools/call request
	mcpRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
		"id": h.getNextRequestID(),
	}

	// Send request to the server
	payload, err := json.Marshal(mcpRequest)
	if err != nil {
		h.corsError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Forward to server using optimal connection
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		h.corsError(w, "Server not available", http.StatusServiceUnavailable)
		return
	}

	// Handle based on protocol
	switch conn.Protocol {
	case "http":
		h.forwardDirectHTTPToolCall(w, r, conn, payload)
	case "http-stream":
		h.forwardDirectStreamableHTTPToolCall(w, r, conn, payload)
	case "sse":
		h.forwardDirectSSEToolCall(w, r, conn, payload)
	default:
		h.corsError(w, "Unsupported protocol for direct tool calls", http.StatusNotImplemented)
	}
}

// forwardDirectHTTPToolCall forwards a direct tool call via plain HTTP. It
// mirrors forwardDirectStreamableHTTPToolCall but targets conn.HTTPConnection
// and does not negotiate streaming. (Previously this returned 501 — direct
// tool calls only worked against streamable-HTTP and SSE servers.)
func (h *ProxyHandler) forwardDirectHTTPToolCall(w http.ResponseWriter, r *http.Request, conn *discovery.MCPConnection, payload []byte) {
	if conn.HTTPConnection == nil {
		h.corsError(w, "HTTP connection not available", http.StatusServiceUnavailable)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), "POST", conn.HTTPConnection.BaseURL, bytes.NewReader(payload))
	if err != nil {
		h.corsError(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if sessionID := conn.HTTPConnection.SessionID; sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	if token := conn.HTTPConnection.AuthToken; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := conn.HTTPConnection.Client.Do(req)
	if err != nil {
		h.corsError(w, fmt.Sprintf("Failed to execute tool: %v", err), http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			h.Logger.Warning("Failed to close response body: %v", err)
		}
	}()

	// Adopt a server-rotated session id, matching the streamable variant.
	if newSessionID := resp.Header.Get("Mcp-Session-Id"); newSessionID != "" && newSessionID != conn.HTTPConnection.SessionID {
		h.Logger.Info("Server %s updated Mcp-Session-Id for tool call", conn.Name)
		conn.HTTPConnection.SessionID = newSessionID
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.corsError(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	// Process for OpenWebUI if needed, mirroring the streamable path.
	if h.openWebUI.shouldProcess(r, responseBody) {
		if processedResponse := h.openWebUI.processResponse(responseBody); processedResponse != nil {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(processedResponse)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(responseBody)
}

// forwardDirectStreamableHTTPToolCall forwards a direct tool call via streamable HTTP
func (h *ProxyHandler) forwardDirectStreamableHTTPToolCall(w http.ResponseWriter, r *http.Request, conn *discovery.MCPConnection, payload []byte) {
	if conn.StreamableHTTPConnection == nil {
		h.corsError(w, "Streamable HTTP connection not available", http.StatusServiceUnavailable)
		return
	}

	// Create request to streamable HTTP server
	req, err := http.NewRequest("POST", conn.StreamableHTTPConnection.BaseURL, bytes.NewReader(payload))
	if err != nil {
		h.corsError(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// Set headers for tool call
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Streaming", "true")

	// Add session ID if available
	sessionID := conn.StreamableHTTPConnection.SessionID
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	// Make the request
	resp, err := conn.StreamableHTTPConnection.Client.Do(req)
	if err != nil {
		h.corsError(w, fmt.Sprintf("Failed to execute tool: %v", err), http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			h.Logger.Warning("Failed to close response body: %v", err)
		}
	}()

	// Update session ID if provided
	if newSessionID := resp.Header.Get("Mcp-Session-Id"); newSessionID != "" {
		if newSessionID != conn.StreamableHTTPConnection.SessionID {
			h.Logger.Info("Server %s updated Mcp-Session-Id for tool call", conn.Name)
			conn.StreamableHTTPConnection.SessionID = newSessionID
		}
	}

	// Handle response based on whether it's streaming or not
	if resp.Header.Get("Transfer-Encoding") == "chunked" || resp.Header.Get("X-Streaming") == "true" {
		h.forwardStreamingResponse(w, resp)
	} else {
		// Read and return response
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			h.corsError(w, "Failed to read response", http.StatusInternalServerError)
			return
		}

		// Process for OpenWebUI if needed
		if h.openWebUI.shouldProcess(r, responseBody) {
			processedResponse := h.openWebUI.processResponse(responseBody)
			if processedResponse != nil {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(processedResponse)
				return
			}
		}

		// Return raw JSON response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(responseBody)
	}
}

// forwardDirectSSEToolCall forwards a direct tool call via SSE
func (h *ProxyHandler) forwardDirectSSEToolCall(w http.ResponseWriter, r *http.Request, conn *discovery.MCPConnection, payload []byte) {
	// Implementation similar to sendOptimalSSERequest
	response, err := h.sendOptimalSSERequest(conn.Name, payload)
	if err != nil {
		h.corsError(w, fmt.Sprintf("Failed to execute tool: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.Logger.Warning("Failed to encode response: %v", err)
	}
}

// sendOptimalSSERequest with proper MCP protocol implementation
func (h *ProxyHandler) sendOptimalSSERequest(serverName string, payload interface{}) (map[string]interface{}, error) {
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		return nil, fmt.Errorf("no connection available for server %s: %w", serverName, err)
	}

	if conn.SSEConnection == nil {
		return nil, fmt.Errorf("no SSE connection available for server %s", serverName)
	}

	// Convert payload to request map
	var request map[string]interface{}
	switch p := payload.(type) {
	case []byte:
		if err := json.Unmarshal(p, &request); err != nil {
			return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
		}
	case map[string]interface{}:
		request = p
	default:
		return nil, fmt.Errorf("unsupported payload type: %T", payload)
	}

	// Use the proper MCP tools discovery implementation
	tools, err := h.makeSSEToolsListRequest(context.Background(), conn, request)
	if err != nil {
		return nil, err
	}

	// Convert tools to response format
	result := map[string]interface{}{
		"tools": tools,
	}

	return map[string]interface{}{
		"jsonrpc": "2.0",
		"result":  result,
		"id":      request["id"],
	}, nil
}

// isProxyStandardMethod checks if a method is a standard proxy method.
// No standard MCP protocol methods are handled by the proxy itself - they are
// all forwarded to the backend servers.
func isProxyStandardMethod(method string) bool {
	return false
}

// routeClientReply attempts to deliver a JSON-RPC response payload from the
// client to a pending server-initiated request currently held by the
// bidirectional relay. Returns true if delivered (response 202 to the
// client) and false if no matching pending wait was found.
func (h *ProxyHandler) routeClientReply(w http.ResponseWriter, r *http.Request, payload map[string]interface{}, body []byte) bool {
	if h.activeChannels == nil {

		return false
	}
	idKeyVal := idKey(payload["id"])
	sessionID := r.Header.Get("Mcp-Session-Id")
	cc := h.activeChannels.get(sessionID)
	delivered := false
	if cc != nil {
		delivered = cc.resolvePending(idKeyVal, json.RawMessage(body))
	}
	if !delivered {
		delivered = h.activeChannels.resolveAny(idKeyVal, json.RawMessage(body))
	}
	if delivered {
		w.WriteHeader(http.StatusAccepted)

		return true
	}

	return false
}
