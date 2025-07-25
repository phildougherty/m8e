package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
)

// MCPHTTPConnection represents a persistent HTTP connection to an MCP server
type MCPHTTPConnection struct {
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

func (h *ProxyHandler) getServerConnection(serverName string) (*MCPHTTPConnection, error) {
	h.ConnectionMutex.RLock()
	conn, exists := h.ServerConnections[serverName]
	h.ConnectionMutex.RUnlock()

	if exists {
		if h.isConnectionHealthy(conn) {
			conn.mu.Lock()
			conn.LastUsed = time.Now()
			conn.mu.Unlock()
			h.logger.Debug("Reusing healthy connection for %s", serverName)

			return conn, nil
		}
		h.logger.Info("Existing connection for %s found unhealthy or uninitialized. Attempting to recreate.", serverName)
		h.ConnectionMutex.Lock()
		delete(h.ServerConnections, serverName)
		h.ConnectionMutex.Unlock()
	}

	h.logger.Info("Creating new HTTP connection for server: %s", serverName)
	serverConfig, cfgExists := h.Manager.config.Servers[serverName]
	if !cfgExists {

		return nil, fmt.Errorf("configuration for server '%s' not found", serverName)
	}

	// Ensure server is configured for HTTP
	if serverConfig.Protocol != "http" && serverConfig.HttpPort == 0 {
		isHTTPInArgs := false
		for _, arg := range serverConfig.Args {
			if strings.Contains(strings.ToLower(arg), "http") || strings.Contains(arg, "--port") {
				isHTTPInArgs = true

				break
			}
		}
		if !isHTTPInArgs {

			return nil, fmt.Errorf("server '%s' is not configured for HTTP transport ('protocol: http' or 'http_port' missing)", serverName)
		}
		h.logger.Warning("Server %s: 'protocol: http' or 'http_port' not explicit in YAML. Relying on command args for HTTP mode configuration.", serverName)
	}

	baseURL := h.getServerHTTPURL(serverName, serverConfig)
	if strings.Contains(baseURL, "INVALID_PORT_CONFIG_ERROR") {

		return nil, fmt.Errorf("cannot create connection for '%s' due to invalid port configuration", serverName)
	}

	newConn := &MCPHTTPConnection{
		ServerName:   serverName,
		BaseURL:      baseURL,
		LastUsed:     time.Now(),
		Healthy:      true,
		Capabilities: make(map[string]interface{}),
		ServerInfo:   make(map[string]interface{}),
	}

	maxRetries := 3
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := h.initializeHTTPConnection(newConn)
		if err == nil {
			h.ConnectionMutex.Lock()
			h.ServerConnections[serverName] = newConn
			h.ConnectionMutex.Unlock()
			h.logger.Info("Successfully created and initialized HTTP connection for %s.", serverName)

			return newConn, nil
		}
		lastErr = err
		h.logger.Warning("Initialization attempt %d/%d for %s failed: %v.", attempt, maxRetries, newConn.ServerName, err)
		if attempt < maxRetries {
			var waitDuration time.Duration
			if strings.Contains(strings.ToLower(err.Error()), "connection refused") || strings.Contains(err.Error(), "no such host") {
				waitDuration = time.Duration(attempt*constants.RetryBackoffMultiplier+constants.RetryBackoffBase) * time.Second
				h.logger.Info("Server %s might be starting (connection refused), waiting %v before retry %d...", newConn.ServerName, waitDuration, attempt+1)
			} else if strings.Contains(strings.ToLower(err.Error()), "timeout") {
				waitDuration = time.Duration(attempt*2+1) * time.Second
				h.logger.Info("Server %s initialization timed out, waiting %v before retry %d...", newConn.ServerName, waitDuration, attempt+1)
			} else {
				waitDuration = time.Duration(attempt) * time.Second
				h.logger.Info("General initialization error for %s, waiting %v before retry %d...", newConn.ServerName, waitDuration, attempt+1)
			}
			select {
			case <-time.After(waitDuration):
			case <-h.ctx.Done():

				return nil, fmt.Errorf("proxy shutting down during connection retry for %s: %w", serverName, h.ctx.Err())
			}
		}
	}

	return nil, fmt.Errorf("failed to establish and initialize HTTP connection for %s after %d attempts: %w", serverName, maxRetries, lastErr)
}

func (h *ProxyHandler) initializeHTTPConnection(conn *MCPHTTPConnection) error {
	conn.mu.Lock()
	conn.Initialized = false
	conn.SessionID = ""
	conn.mu.Unlock()

	h.logger.Info("Attempting to initialize HTTP MCP session for %s at %s", conn.ServerName, conn.BaseURL)

	requestID := h.getNextRequestID()
	initRequestPayload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"clientInfo": map[string]interface{}{
				"name":    "matey-proxy",
				"version": "1.1.0",
			},
			"capabilities": map[string]interface{}{},
		},
	}

	resp, err := h.doHTTPRequest(conn, initRequestPayload, constants.HTTPInitTimeout)
	if err != nil {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()

		return fmt.Errorf("HTTP initialize POST to %s failed: %w", conn.BaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	rawResponseData, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()

		return fmt.Errorf("failed to read initialize response body from %s: %w", conn.BaseURL, readErr)
	}

	h.logger.Debug("Raw Initialize HTTP response from %s (Status: %d, Content-Type: %s): %s", conn.ServerName, resp.StatusCode, resp.Header.Get("Content-Type"), string(rawResponseData))

	conn.mu.Lock()
	serverSessionID := resp.Header.Get("Mcp-Session-Id")
	if serverSessionID != "" {
		conn.SessionID = serverSessionID
		h.logger.Info("Server %s provided Mcp-Session-Id: %s", conn.ServerName, conn.SessionID)
	}
	conn.mu.Unlock()

	if resp.StatusCode != http.StatusOK {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()

		return fmt.Errorf("HTTP initialize request to %s failed with status %d: %s", conn.BaseURL, resp.StatusCode, string(rawResponseData))
	}

	contentType := resp.Header.Get("Content-Type")
	var responseJSONData []byte
	if strings.HasPrefix(contentType, "text/event-stream") {
		h.logger.Info("Server %s responded with text/event-stream for initialize. Reading first 'data:' event.", conn.ServerName)
		scanner := bufio.NewScanner(bytes.NewReader(rawResponseData))
		eventDataFound := false
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				responseJSONData = []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				eventDataFound = true

				break
			}
		}
		if !eventDataFound {
			conn.mu.Lock()
			conn.Healthy = false
			conn.mu.Unlock()

			return fmt.Errorf("SSE stream from %s for initialize, but no 'data:' event parsed. Body: %s", conn.ServerName, string(rawResponseData))
		}
	} else if strings.HasPrefix(contentType, "application/json") {
		responseJSONData = rawResponseData
	} else {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()

		return fmt.Errorf("unexpected Content-Type '%s' from %s for initialize. Body: %s", contentType, conn.ServerName, string(rawResponseData))
	}

	var responseMap map[string]interface{}
	if err := json.Unmarshal(responseJSONData, &responseMap); err != nil {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()

		return fmt.Errorf("failed to parse initialize JSON from %s (Content-Type: %s): %w. Data: %s", conn.ServerName, contentType, err, string(responseJSONData))
	}

	h.logger.Debug("Parsed Initialize JSON response from %s: %+v", conn.ServerName, responseMap)

	if errResp, ok := responseMap["error"].(map[string]interface{}); ok {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()

		return fmt.Errorf("initialize error from %s: code=%v, message=%v, data=%v",
			conn.ServerName, errResp["code"], errResp["message"], errResp["data"])
	}

	result, ok := responseMap["result"].(map[string]interface{})
	if !ok {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()

		return fmt.Errorf("initialize response from %s missing 'result' or not an object. Parsed: %+v", conn.ServerName, responseMap)
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

	initializedNotificationPayload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	}

	if err := h.sendHTTPNotification(conn, initializedNotificationPayload); err != nil {
		h.logger.Warning("Failed to send 'initialized' notification to %s: %v. Session continues.", conn.ServerName, err)
	}

	h.logger.Info("HTTP MCP session initialized successfully for %s.", conn.ServerName)

	return nil
}

func (h *ProxyHandler) doHTTPRequest(conn *MCPHTTPConnection, requestPayload map[string]interface{}, timeout time.Duration) (*http.Response, error) {
	requestData, err := json.Marshal(requestPayload)
	if err != nil {

		return nil, fmt.Errorf("marshal request for %s: %w", conn.ServerName, err)
	}

	targetURL := conn.BaseURL
	h.logger.Debug("Request to %s (%s): %s", conn.ServerName, targetURL, string(requestData))

	reqCtx, cancel := context.WithTimeout(h.ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, "POST", targetURL, bytes.NewBuffer(requestData))
	if err != nil {
		cancel()

		return nil, fmt.Errorf("create HTTP request for %s: %w", conn.ServerName, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	conn.mu.Lock()
	sessionIDForRequest := conn.SessionID
	conn.mu.Unlock()

	if sessionIDForRequest != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionIDForRequest)
	}

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		cancel()
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()

		return nil, fmt.Errorf("HTTP POST to %s failed: %w", targetURL, err)
	}

	conn.mu.Lock()
	conn.LastUsed = time.Now()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
		conn.Healthy = true
	}
	conn.mu.Unlock()

	return resp, nil
}

func (h *ProxyHandler) sendHTTPJsonRequest(conn *MCPHTTPConnection, requestPayload map[string]interface{}, timeout time.Duration) (map[string]interface{}, error) {
	requestData, err := json.Marshal(requestPayload)
	if err != nil {

		return nil, fmt.Errorf("marshal request for %s: %w", conn.ServerName, err)
	}

	targetURL := conn.BaseURL
	h.logger.Debug("Request to %s (%s): %s", conn.ServerName, targetURL, string(requestData))

	reqCtx, cancel := context.WithTimeout(h.ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, "POST", targetURL, bytes.NewBuffer(requestData))
	if err != nil {

		return nil, fmt.Errorf("create HTTP request for %s: %w", conn.ServerName, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

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

		return nil, fmt.Errorf("HTTP POST to %s failed: %w", targetURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	conn.mu.Lock()
	conn.LastUsed = time.Now()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
		conn.Healthy = true
	}
	conn.mu.Unlock()

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

		return nil, fmt.Errorf("HTTP request to %s failed with status %d: %s", targetURL, resp.StatusCode, string(bodyBytes))
	}

	// Read and parse response
	responseData, err := io.ReadAll(resp.Body)
	if err != nil {

		return nil, fmt.Errorf("failed to read response from %s: %w", targetURL, err)
	}

	h.logger.Debug("Raw response from %s: %s", conn.ServerName, string(responseData))

	var responseMap map[string]interface{}
	if err := json.Unmarshal(responseData, &responseMap); err != nil {

		return nil, fmt.Errorf("failed to parse JSON response from %s: %w. Data: %s", targetURL, err, string(responseData))
	}

	return responseMap, nil
}

func (h *ProxyHandler) sendHTTPNotification(conn *MCPHTTPConnection, notificationPayload map[string]interface{}) error {
	resp, err := h.doHTTPRequest(conn, notificationPayload, constants.HTTPNotificationTimeout)
	if err != nil {

		return fmt.Errorf("sending notification to %s failed: %w", conn.ServerName, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, constants.HTTPErrorBufferSize))
		h.logger.Warning("HTTP notification to %s received non-202/200 status %d: %s", conn.ServerName, resp.StatusCode, string(bodyBytes))
	} else {
		h.logger.Debug("HTTP notification to %s sent, status %d.", conn.ServerName, resp.StatusCode)
	}

	return nil
}

func (h *ProxyHandler) isConnectionHealthy(conn *MCPHTTPConnection) bool {
	conn.mu.Lock()
	if !conn.Healthy {
		conn.mu.Unlock()
		h.logger.Debug("Skipping health check for %s; already marked unhealthy.", conn.ServerName)

		return false
	}
	conn.mu.Unlock()

	pingRequestMap := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      h.getNextRequestID(),
		"method":  "ping",
	}

	h.logger.Debug("Performing health check ping to %s at %s", conn.ServerName, conn.BaseURL)
	_, err := h.sendHTTPJsonRequest(conn, pingRequestMap, constants.PingTimeout)
	if err != nil {
		h.logger.Warning("Health check ping to %s FAILED: %v", conn.ServerName, err)
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()

		return false
	}

	h.logger.Debug("Health check ping to %s SUCCEEDED.", conn.ServerName)

	return true
}

func (h *ProxyHandler) forwardHTTPRequest(conn *MCPHTTPConnection, requestData []byte, timeout time.Duration) (map[string]interface{}, error) {
	targetURL := conn.BaseURL
	h.logger.Debug("Forwarding request to %s (%s): %s", conn.ServerName, targetURL, string(requestData))

	reqCtx, cancel := context.WithTimeout(h.ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, "POST", targetURL, bytes.NewBuffer(requestData))
	if err != nil {

		return nil, fmt.Errorf("create HTTP request for %s: %w", conn.ServerName, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	conn.mu.Lock()
	if conn.SessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", conn.SessionID)
	}
	conn.mu.Unlock()

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()

		return nil, fmt.Errorf("HTTP POST to %s failed: %w", targetURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	conn.mu.Lock()
	conn.LastUsed = time.Now()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		conn.Healthy = true
	}
	conn.mu.Unlock()

	// Read and parse response
	responseData, err := io.ReadAll(resp.Body)
	if err != nil {

		return nil, fmt.Errorf("failed to read response from %s: %w", targetURL, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		conn.mu.Lock()
		conn.Healthy = false
		conn.mu.Unlock()

		return nil, fmt.Errorf("HTTP request to %s failed with status %d: %s", targetURL, resp.StatusCode, string(responseData))
	}

	h.logger.Debug("Raw response from %s: %s", conn.ServerName, string(responseData))

	var responseMap map[string]interface{}
	if err := json.Unmarshal(responseData, &responseMap); err != nil {

		return nil, fmt.Errorf("failed to parse JSON response from %s: %w. Data: %s", targetURL, err, string(responseData))
	}

	return responseMap, nil
}

func (h *ProxyHandler) maintainHttpConnections() {
	h.ConnectionMutex.Lock()
	defer h.ConnectionMutex.Unlock()

	for serverName, conn := range h.ServerConnections {
		if conn == nil {
			continue
		}
		// Clean up old HTTP connections
		maxIdleTime := constants.IdleTimeoutDefault
		if time.Since(conn.LastUsed) > maxIdleTime {
			h.logger.Info("Removing idle HTTP connection to %s (idle for %v)",
				serverName, time.Since(conn.LastUsed))
			delete(h.ServerConnections, serverName)
		}
	}

	// Force HTTP client to close idle connections periodically
	h.httpClient.CloseIdleConnections()
}

func (h *ProxyHandler) getConnectionHealthStatus(conn *MCPHTTPConnection) string {
	if conn.Healthy && conn.Initialized {
		status := "Active & Initialized"
		if conn.SessionID != "" {
			status += " (Session ID: " + conn.SessionID + ")"
		}

		return status
	} else if conn.Initialized {
		status := "Initialized but Unhealthy"
		if conn.SessionID != "" {
			status += " (Session ID: " + conn.SessionID + ")"
		}

		return status
	} else if conn.Healthy {

		return "Contactable but MCP Session Not Initialized"
	}

	return "Unhealthy / MCP Session Not Initialized"
}

// establishInitialHTTPConnections proactively establishes HTTP connections to all configured HTTP servers
func (h *ProxyHandler) establishInitialHTTPConnections() {
	if h.Manager == nil || h.Manager.config == nil {

		return
	}

	// Wait a moment for containers to fully start up
	time.Sleep(constants.ContainerStartupWait)

	h.logger.Info("Establishing initial HTTP connections to configured servers")

	for serverName, serverConfig := range h.Manager.config.Servers {
		// Only establish connections for HTTP servers
		if serverConfig.Protocol == "http" || serverConfig.HttpPort > 0 {
			go func(name string, cfg config.ServerConfig) {
				// Check if server is likely to be running
				instance, exists := h.Manager.GetServerInstance(name)
				if !exists {
					h.logger.Debug("Server %s not found in manager instances, skipping initial connection", name)

					return
				}

				// Skip connection check for system servers
				// Kubernetes will handle server lifecycle
				_ = instance // Avoid unused variable warning

				h.logger.Debug("Attempting to establish initial HTTP connection to %s", name)
				_, err := h.getServerConnection(name)
				if err != nil {
					h.logger.Debug("Initial connection to %s failed: %v", name, err)
				} else {
					h.logger.Info("Successfully established initial HTTP connection to %s", name)
				}
			}(serverName, serverConfig)
		}
	}
}

// ensureHTTPConnectionsEstablished ensures HTTP connections are established for all configured HTTP servers
// This can be called on-demand (e.g., from API endpoints) to refresh connections
func (h *ProxyHandler) ensureHTTPConnectionsEstablished() {
	if h.Manager == nil || h.Manager.config == nil {

		return
	}

	h.logger.Debug("Ensuring HTTP connections are established for all configured servers")

	for serverName, serverConfig := range h.Manager.config.Servers {
		// Only establish connections for HTTP servers
		if serverConfig.Protocol == "http" || serverConfig.HttpPort > 0 {
			// Check if we already have a healthy connection
			h.ConnectionMutex.RLock()
			conn, exists := h.ServerConnections[serverName]
			h.ConnectionMutex.RUnlock()

			if exists && h.isConnectionHealthy(conn) {
				h.logger.Debug("HTTP connection to %s already exists and is healthy", serverName)

				continue
			}

			// Establish connection if we don't have one or it's unhealthy
			go func(name string, cfg config.ServerConfig) {
				// Check if server is likely to be running
				instance, exists := h.Manager.GetServerInstance(name)
				if !exists {
					h.logger.Debug("Server %s not found in manager instances, skipping connection", name)

					return
				}

				// Skip connection check for system servers
				// Kubernetes will handle server lifecycle
				_ = instance // Avoid unused variable warning

				h.logger.Debug("Establishing HTTP connection to %s", name)
				_, err := h.getServerConnection(name)
				if err != nil {
					h.logger.Debug("Connection to %s failed: %v", name, err)
				} else {
					h.logger.Debug("Successfully established HTTP connection to %s", name)
				}
			}(serverName, serverConfig)
		}
	}
}
