package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/phildougherty/m8e/internal/constants"
)

// MCPStreamableHTTPConnection represents a streamable HTTP connection to an MCP server
// This is designed for clients like Gemini CLI that expect streaming HTTP responses
type MCPStreamableHTTPConnection struct {
	ServerName   string
	BaseURL      string
	LastUsed     time.Time
	Initialized  bool
	Healthy      bool
	Capabilities map[string]interface{}
	ServerInfo   map[string]interface{}
	SessionID    string
	mu           sync.Mutex
}

// StreamableHTTPConnections holds streamable HTTP connections
var (
	StreamableHTTPConnections      = make(map[string]*MCPStreamableHTTPConnection)
	StreamableHTTPConnectionsMutex sync.RWMutex
)

func (h *ProxyHandler) getStreamableHTTPConnection(serverName string) (*MCPStreamableHTTPConnection, error) {
	StreamableHTTPConnectionsMutex.RLock()
	conn, exists := StreamableHTTPConnections[serverName]
	StreamableHTTPConnectionsMutex.RUnlock()

	if exists {
		if h.isStreamableHTTPConnectionHealthy(conn) {
			conn.mu.Lock()
			conn.LastUsed = time.Now()
			conn.mu.Unlock()
			h.logger.Debug("Reusing healthy streamable HTTP connection for %s", serverName)
			return conn, nil
		}
		h.logger.Info("Existing streamable HTTP connection for %s found unhealthy. Recreating.", serverName)
		StreamableHTTPConnectionsMutex.Lock()
		delete(StreamableHTTPConnections, serverName)
		StreamableHTTPConnectionsMutex.Unlock()
	}

	h.logger.Info("Creating new streamable HTTP connection for server: %s", serverName)
	
	// Get discovered connection instead of config file
	discoveredConn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		return nil, fmt.Errorf("discovered connection for server '%s' not found: %w", serverName, err)
	}

	// Ensure server is configured for HTTP
	if discoveredConn.Protocol != "http" {
		return nil, fmt.Errorf("server '%s' is not configured for HTTP transport (protocol: %s)", serverName, discoveredConn.Protocol)
	}

	if discoveredConn.HTTPConnection == nil {
		return nil, fmt.Errorf("server '%s' does not have an HTTP connection", serverName)
	}

	baseURL := discoveredConn.HTTPConnection.BaseURL
	if baseURL == "" {
		return nil, fmt.Errorf("server '%s' has no base URL configured", serverName)
	}

	newConn := &MCPStreamableHTTPConnection{
		ServerName:   serverName,
		BaseURL:      baseURL,
		LastUsed:     time.Now(),
		Healthy:      true,
		Capabilities: make(map[string]interface{}),
		ServerInfo:   make(map[string]interface{}),
	}

	// Initialize connection
	if err := h.initializeStreamableHTTPConnection(newConn); err != nil {
		return nil, fmt.Errorf("failed to initialize streamable HTTP connection: %w", err)
	}

	StreamableHTTPConnectionsMutex.Lock()
	StreamableHTTPConnections[serverName] = newConn
	StreamableHTTPConnectionsMutex.Unlock()

	h.logger.Info("Successfully created and initialized streamable HTTP connection for %s", serverName)
	return newConn, nil
}

func (h *ProxyHandler) initializeStreamableHTTPConnection(conn *MCPStreamableHTTPConnection) error {
	conn.mu.Lock()
	conn.Initialized = false
	conn.SessionID = ""
	conn.mu.Unlock()

	h.logger.Info("Attempting to initialize streamable HTTP MCP session for %s at %s", conn.ServerName, conn.BaseURL)

	requestID := h.getNextRequestID()
	initRequestPayload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]interface{}{
				"name":    "matey-proxy-streamable",
				"version": "1.1.0",
			},
			"capabilities": map[string]interface{}{},
		},
	}

	resp, err := h.doStreamableHTTPRequest(conn, initRequestPayload, constants.HTTPInitTimeout)
	if err != nil {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()
		return fmt.Errorf("streamable HTTP initialize POST to %s failed: %w", conn.BaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Handle streaming response
	response, err := h.readStreamableHTTPResponse(resp)
	if err != nil {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()
		return fmt.Errorf("failed to read streamable HTTP initialize response from %s: %w", conn.BaseURL, err)
	}

	if resp.StatusCode != http.StatusOK {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()
		return fmt.Errorf("streamable HTTP initialize request to %s failed with status %d", conn.BaseURL, resp.StatusCode)
	}

	// Extract session ID from headers
	conn.mu.Lock()
	serverSessionID := resp.Header.Get("Mcp-Session-Id")
	if serverSessionID != "" {
		conn.SessionID = serverSessionID
		h.logger.Info("Server %s provided Mcp-Session-Id: %s", conn.ServerName, conn.SessionID)
	}
	conn.mu.Unlock()

	// Parse initialize response
	if errResp, ok := response["error"].(map[string]interface{}); ok {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()
		return fmt.Errorf("initialize error from %s: code=%v, message=%v", conn.ServerName, errResp["code"], errResp["message"])
	}

	result, ok := response["result"].(map[string]interface{})
	if !ok {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()
		return fmt.Errorf("initialize response from %s missing 'result'", conn.ServerName)
	}

	conn.mu.Lock()
	if caps, ok := result["capabilities"].(map[string]interface{}); ok {
		conn.Capabilities = caps
	}
	if sInfo, ok := result["serverInfo"].(map[string]interface{}); ok {
		conn.ServerInfo = sInfo
	}
	conn.Initialized = true
	conn.Healthy = true
	conn.mu.Unlock()

	// Send initialized notification
	initializedNotificationPayload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	}

	if err := h.sendStreamableHTTPNotification(conn, initializedNotificationPayload); err != nil {
		h.logger.Warning("Failed to send 'initialized' notification to %s: %v. Session continues.", conn.ServerName, err)
	}

	h.logger.Info("Streamable HTTP MCP session initialized successfully for %s", conn.ServerName)
	return nil
}

func (h *ProxyHandler) doStreamableHTTPRequest(conn *MCPStreamableHTTPConnection, requestPayload map[string]interface{}, timeout time.Duration) (*http.Response, error) {
	requestData, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal request for %s: %w", conn.ServerName, err)
	}

	targetURL := conn.BaseURL
	h.logger.Debug("Streamable HTTP request to %s (%s): %s", conn.ServerName, targetURL, string(requestData))

	reqCtx, cancel := context.WithTimeout(h.ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, "POST", targetURL, bytes.NewBuffer(requestData))
	if err != nil {
		return nil, fmt.Errorf("create streamable HTTP request for %s: %w", conn.ServerName, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	// Key difference: Request streaming response
	httpReq.Header.Set("X-Streaming", "true")
	httpReq.Header.Set("Cache-Control", "no-cache")

	conn.mu.Lock()
	sessionIDForRequest := conn.SessionID
	conn.mu.Unlock()

	if sessionIDForRequest != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionIDForRequest)
	}

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()
		return nil, fmt.Errorf("streamable HTTP POST to %s failed: %w", targetURL, err)
	}

	conn.mu.Lock()
	conn.LastUsed = time.Now()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
		conn.Healthy = true
	}
	conn.mu.Unlock()

	return resp, nil
}

func (h *ProxyHandler) readStreamableHTTPResponse(resp *http.Response) (map[string]interface{}, error) {
	// Check if response is chunked or streaming
	if resp.Header.Get("Transfer-Encoding") == "chunked" || resp.Header.Get("X-Streaming") == "true" {
		return h.readChunkedJSONResponse(resp)
	}

	// Fallback to regular JSON response
	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}
	return response, nil
}

func (h *ProxyHandler) readChunkedJSONResponse(resp *http.Response) (map[string]interface{}, error) {
	scanner := bufio.NewScanner(resp.Body)
	var jsonData []byte

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Try to parse each line as JSON
		var lineData map[string]interface{}
		if err := json.Unmarshal(line, &lineData); err == nil {
			// This is a complete JSON object
			return lineData, nil
		}

		// Otherwise, accumulate the line
		jsonData = append(jsonData, line...)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading chunked response: %w", err)
	}

	// Try to parse accumulated data
	if len(jsonData) > 0 {
		var response map[string]interface{}
		if err := json.Unmarshal(jsonData, &response); err != nil {
			return nil, fmt.Errorf("failed to parse accumulated JSON: %w", err)
		}
		return response, nil
	}

	return nil, fmt.Errorf("no valid JSON data found in chunked response")
}

func (h *ProxyHandler) sendStreamableHTTPRequest(conn *MCPStreamableHTTPConnection, requestPayload map[string]interface{}, timeout time.Duration) (map[string]interface{}, error) {
	resp, err := h.doStreamableHTTPRequest(conn, requestPayload, timeout)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Handle session ID updates
	newSessionID := resp.Header.Get("Mcp-Session-Id")
	if newSessionID != "" {
		conn.mu.Lock()
		if newSessionID != conn.SessionID {
			h.logger.Info("Server %s updated Mcp-Session-Id from '%s' to '%s'", conn.ServerName, conn.SessionID, newSessionID)
			conn.SessionID = newSessionID
		}
		conn.mu.Unlock()
	}

	// Check for non-OK status
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, constants.HTTPResponseBufferSize))
		return nil, fmt.Errorf("streamable HTTP request to %s failed with status %d: %s", conn.BaseURL, resp.StatusCode, string(bodyBytes))
	}

	// Read and parse streaming response
	response, err := h.readStreamableHTTPResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to read streamable HTTP response from %s: %w", conn.BaseURL, err)
	}

	return response, nil
}

func (h *ProxyHandler) sendStreamableHTTPNotification(conn *MCPStreamableHTTPConnection, notificationPayload map[string]interface{}) error {
	resp, err := h.doStreamableHTTPRequest(conn, notificationPayload, constants.HTTPNotificationTimeout)
	if err != nil {
		return fmt.Errorf("sending streamable HTTP notification to %s failed: %w", conn.ServerName, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, constants.HTTPErrorBufferSize))
		h.logger.Warning("Streamable HTTP notification to %s received non-202/200 status %d: %s", conn.ServerName, resp.StatusCode, string(bodyBytes))
	} else {
		h.logger.Debug("Streamable HTTP notification to %s sent, status %d", conn.ServerName, resp.StatusCode)
	}

	return nil
}

func (h *ProxyHandler) isStreamableHTTPConnectionHealthy(conn *MCPStreamableHTTPConnection) bool {
	conn.mu.Lock()
	if !conn.Healthy {
		conn.mu.Unlock()
		h.logger.Debug("Skipping health check for %s; already marked unhealthy", conn.ServerName)
		return false
	}
	conn.mu.Unlock()

	pingRequestMap := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      h.getNextRequestID(),
		"method":  "ping",
	}

	h.logger.Debug("Performing streamable HTTP health check ping to %s at %s", conn.ServerName, conn.BaseURL)
	_, err := h.sendStreamableHTTPRequest(conn, pingRequestMap, constants.PingTimeout)
	if err != nil {
		h.logger.Warning("Streamable HTTP health check ping to %s FAILED: %v", conn.ServerName, err)
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()
		return false
	}

	h.logger.Debug("Streamable HTTP health check ping to %s SUCCEEDED", conn.ServerName)
	return true
}

func (h *ProxyHandler) maintainStreamableHTTPConnections() {
	StreamableHTTPConnectionsMutex.Lock()
	defer StreamableHTTPConnectionsMutex.Unlock()

	for serverName, conn := range StreamableHTTPConnections {
		if conn == nil {
			continue
		}
		// Clean up old connections
		maxIdleTime := constants.IdleTimeoutDefault
		if time.Since(conn.LastUsed) > maxIdleTime {
			h.logger.Info("Removing idle streamable HTTP connection to %s (idle for %v)",
				serverName, time.Since(conn.LastUsed))
			delete(StreamableHTTPConnections, serverName)
		}
	}
}

