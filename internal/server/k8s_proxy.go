// internal/server/k8s_proxy.go
package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/phildougherty/m8e/internal/auth"
	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/discovery"
	"github.com/phildougherty/m8e/internal/logging"
	"github.com/phildougherty/m8e/internal/protocol"
)

// ProxyHandler is a proxy handler
type ProxyHandler struct {
	// Core K8s components
	ServiceDiscovery      *discovery.K8sServiceDiscovery
	ConnectionManager     *discovery.DynamicConnectionManager
	Config                *config.ComposeConfig
	Logger                *logging.Logger
	logger                *logging.Logger // compatibility field
	
	// Existing functionality maintained
	APIKey                string
	EnableAPI             bool
	ProxyStarted          time.Time
	GlobalRequestID       int
	GlobalIDMutex         sync.Mutex
	
	// Legacy connection management (for compatibility with existing API handlers)
	ServerConnections     map[string]*MCPHTTPConnection
	SSEConnections        map[string]*MCPSSEConnection
	SSEMutex              sync.RWMutex
	StdioConnections      map[string]*MCPSTDIOConnection
	StdioMutex            sync.RWMutex
	Manager               *Manager
	
	// Enhanced SSE connections (for compatibility)
	EnhancedSSEConnections map[string]*EnhancedMCPSSEConnection
	EnhancedSSEMutex       sync.RWMutex
	
	// Protocol managers - same as before
	subscriptionManager       *protocol.SubscriptionManager
	changeNotificationManager *protocol.ChangeNotificationManager
	standardHandler           *protocol.StandardMethodHandler
	
	// Authentication - same as before
	authServer                *auth.AuthorizationServer
	authMiddleware            *auth.AuthenticationMiddleware
	resourceMeta              *auth.ResourceMetadataHandler
	oauthEnabled              bool
	
	// Context and lifecycle
	ctx                       context.Context
	cancel                    context.CancelFunc
	wg                        sync.WaitGroup
	
	// Tool discovery and caching
	toolCache                 map[string]string
	toolCacheMu               sync.RWMutex
	cacheExpiry               time.Time
	connectionStats           map[string]*ConnectionStats
	ConnectionMutex           sync.RWMutex
	
	// HTTP client for compatibility
	httpClient                *http.Client
	sseClient                 *http.Client
}

// ConnectionStats tracks connection performance
type ConnectionStats struct {
	TotalRequests  int64
	FailedRequests int64
	TimeoutErrors  int64
	LastError      time.Time
	LastSuccess    time.Time
	mu             sync.RWMutex
}

// MCP protocol types already defined in other files

// NewProxyHandler creates a new proxy handler
func NewProxyHandler(cfg *config.ComposeConfig, namespace, apiKey string) (*ProxyHandler, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	logLevel := "info"
	if cfg != nil && cfg.Logging.Level != "" {
		logLevel = cfg.Logging.Level
	}
	logger := logging.NewLogger(logLevel)

	// Create service discovery
	serviceDiscovery, err := discovery.NewK8sServiceDiscovery(namespace, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create service discovery: %w", err)
	}

	// Create connection manager
	connectionManager := discovery.NewDynamicConnectionManager(serviceDiscovery, logger)

	handler := &ProxyHandler{
		ServiceDiscovery:          serviceDiscovery,
		ConnectionManager:         connectionManager,
		Config:                    cfg,
		Logger:                    logger,
		logger:                    logger, // compatibility field
		APIKey:                    apiKey,
		EnableAPI:                 true,
		ProxyStarted:              time.Now(),
		ServerConnections:         make(map[string]*MCPHTTPConnection),
		SSEConnections:            make(map[string]*MCPSSEConnection),
		StdioConnections:          make(map[string]*MCPSTDIOConnection),
		EnhancedSSEConnections:    make(map[string]*EnhancedMCPSSEConnection),
		Manager:                   nil, // TODO: Create K8s-native manager or stub
		httpClient:                &http.Client{Timeout: 30 * time.Second},
		sseClient:                 &http.Client{Timeout: 0}, // No timeout for SSE
		subscriptionManager:       protocol.NewSubscriptionManager(),
		changeNotificationManager: protocol.NewChangeNotificationManager(),
		standardHandler:           protocol.NewStandardMethodHandler(protocol.ServerInfo{}, protocol.CapabilitiesOpts{}, logger),
		ctx:                       ctx,
		cancel:                    cancel,
		toolCache:                 make(map[string]string),
		connectionStats:           make(map[string]*ConnectionStats),
	}

	// Setup authentication if enabled
	if err := handler.setupAuthentication(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to setup authentication: %w", err)
	}

	return handler, nil
}

// Start begins the proxy handler operation
func (h *ProxyHandler) Start() error {
	h.Logger.Info("Starting proxy handler")

	// Start service discovery
	if err := h.ServiceDiscovery.Start(); err != nil {
		return fmt.Errorf("failed to start service discovery: %w", err)
	}

	// Start connection manager
	if err := h.ConnectionManager.Start(); err != nil {
		return fmt.Errorf("failed to start connection manager: %w", err)
	}

	// Start tool discovery refresh
	go h.toolDiscoveryLoop()

	h.Logger.Info("Proxy handler started successfully")
	return nil
}

// Stop stops the proxy handler
func (h *ProxyHandler) Stop() {
	h.Logger.Info("Stopping proxy handler")
	
	h.cancel()
	h.wg.Wait()
	
	if h.ConnectionManager != nil {
		h.ConnectionManager.Stop()
	}
	
	if h.ServiceDiscovery != nil {
		h.ServiceDiscovery.Stop()
	}
}

// setupAuthentication configures authentication if enabled
func (h *ProxyHandler) setupAuthentication() error {
	if h.Config == nil {
		return nil
	}

	// Setup OAuth if enabled
	if h.Config.OAuth != nil && h.Config.OAuth.Enabled {
		h.oauthEnabled = true
		
		// Note: For now we're not setting up full OAuth in K8s-native mode
		// This would need to be adapted for K8s-native authentication
		h.Logger.Info("OAuth configuration detected but not fully implemented in K8s-native mode")
	}

	// Setup resource metadata handler
	if h.Config.RBAC != nil && h.Config.RBAC.Enabled {
		// For K8s-native mode, we'd use RBAC from Kubernetes itself
		h.Logger.Info("RBAC configuration detected but using K8s-native RBAC")
	}

	return nil
}

// HandleMCPRequest handles MCP requests using discovered services
func (h *ProxyHandler) HandleMCPRequest(w http.ResponseWriter, r *http.Request, serverName string) {
	// Get connection for the server
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		h.Logger.Error("Failed to get connection for server %s: %v", serverName, err)
		h.writeErrorResponse(w, fmt.Sprintf("Server %s not available: %v", serverName, err), http.StatusServiceUnavailable)
		return
	}

	// Update connection stats
	h.updateConnectionStats(conn.Name, true)

	// Route based on protocol
	switch conn.Protocol {
	case "http":
		h.handleHTTPRequest(w, r, conn)
	case "sse":
		h.handleSSERequest(w, r, conn)
	case "stdio":
		h.handleStdioRequest(w, r, conn)
	default:
		h.writeErrorResponse(w, fmt.Sprintf("Unsupported protocol: %s", conn.Protocol), http.StatusBadRequest)
	}
}

// handleHTTPRequest handles HTTP protocol requests
func (h *ProxyHandler) handleHTTPRequest(w http.ResponseWriter, r *http.Request, conn *discovery.MCPConnection) {
	if conn.HTTPConnection == nil {
		h.writeErrorResponse(w, "HTTP connection not available", http.StatusServiceUnavailable)
		return
	}

	// Forward request to the actual server
	h.forwardK8sHTTPRequest(w, r, conn.HTTPConnection)
}

// handleSSERequest handles SSE protocol requests
func (h *ProxyHandler) handleSSERequest(w http.ResponseWriter, r *http.Request, conn *discovery.MCPConnection) {
	if conn.SSEConnection == nil {
		h.writeErrorResponse(w, "SSE connection not available", http.StatusServiceUnavailable)
		return
	}

	// Forward request to SSE server
	h.forwardSSERequest(w, r, conn.SSEConnection)
}

// handleStdioRequest handles STDIO protocol requests
func (h *ProxyHandler) handleStdioRequest(w http.ResponseWriter, r *http.Request, conn *discovery.MCPConnection) {
	// STDIO in Kubernetes would require special handling
	h.writeErrorResponse(w, "STDIO protocol not supported in Kubernetes mode", http.StatusNotImplemented)
}

// forwardK8sHTTPRequest forwards an HTTP request to the target server
func (h *ProxyHandler) forwardK8sHTTPRequest(w http.ResponseWriter, r *http.Request, conn *discovery.MCPHTTPConnection) {
	// MCP servers expect requests to be sent to the root path
	targetURL := conn.BaseURL + "/"
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		h.writeErrorResponse(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// Make the request
	resp, err := conn.Client.Do(req)
	if err != nil {
		h.updateConnectionStats(conn.BaseURL, false)
		h.writeErrorResponse(w, "Failed to forward request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read response body first to check if we need to process it for OpenWebUI
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.Logger.Error("Failed to read response body: %v", err)
		return
	}

	// Check if this is a tools/call response that needs OpenWebUI processing
	if h.shouldProcessForOpenWebUI(r, responseBody) {
		h.Logger.Info("Processing MCP response for OpenWebUI compatibility")
		processedResponse := h.processResponseForOpenWebUI(responseBody)
		if processedResponse != nil {
			// Return plain text for OpenWebUI
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, err = w.Write(processedResponse)
			if err != nil {
				h.Logger.Error("Failed to write processed response: %v", err)
			}
			return
		}
	}

	// Copy response headers for non-OpenWebUI response
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	// Copy original response body for non-OpenWebUI or failed processing
	_, err = w.Write(responseBody)
	if err != nil {
		h.Logger.Error("Failed to copy response body: %v", err)
	}
}

// forwardSSERequest forwards an SSE request to the target server
func (h *ProxyHandler) forwardSSERequest(w http.ResponseWriter, r *http.Request, conn *discovery.MCPSSEConnection) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	// Create request to SSE endpoint
	req, err := http.NewRequest("GET", conn.BaseURL, nil)
	if err != nil {
		h.writeErrorResponse(w, "Failed to create SSE request", http.StatusInternalServerError)
		return
	}
	
	// Copy headers from original request
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	
	// Make the SSE request
	resp, err := conn.Client.Do(req)
	if err != nil {
		h.updateConnectionStats(conn.BaseURL, false)
		h.writeErrorResponse(w, "Failed to connect to SSE server", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	
	// Stream the SSE response
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeErrorResponse(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}
	
	buffer := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			w.Write(buffer[:n])
			flusher.Flush()
		}
		if err != nil {
			break
		}
	}
}

// API Methods - matching the old proxy interface

// GetDiscoveredServers returns information about discovered servers
func (h *ProxyHandler) GetDiscoveredServers() []discovery.ServiceEndpoint {
	return h.ServiceDiscovery.GetServices()
}

// GetConnectionStatus returns the status of all connections
func (h *ProxyHandler) GetConnectionStatus() map[string]discovery.ConnectionStatus {
	return h.ConnectionManager.GetConnectionStatus()
}

// RefreshConnections triggers a refresh of service discovery and connections
func (h *ProxyHandler) RefreshConnections() error {
	h.Logger.Info("Refreshing service discovery and connections")
	
	// Service discovery is automatic in Kubernetes, but we can trigger
	// a manual discovery to get immediate results
	_, err := h.ServiceDiscovery.DiscoverMCPServers()
	if err != nil {
		h.Logger.Error("Failed to refresh service discovery: %v", err)
		return err
	}

	return nil
}

// GetProxyInfo returns information about the proxy
func (h *ProxyHandler) GetProxyInfo() map[string]interface{} {
	services := h.GetDiscoveredServers()
	connections := h.GetConnectionStatus()
	
	return map[string]interface{}{
		"type":              "kubernetes-native",
		"started":           h.ProxyStarted,
		"uptime":            time.Since(h.ProxyStarted).String(),
		"discovered_servers": len(services),
		"active_connections": len(connections),
		"oauth_enabled":     h.oauthEnabled,
		"api_enabled":       h.EnableAPI,
		"services":          services,
		"connections":       connections,
	}
}

// Tool Discovery Methods

// toolDiscoveryLoop periodically discovers tools from connected services
func (h *ProxyHandler) toolDiscoveryLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.discoverTools()
		}
	}
}

// discoverTools discovers available tools from all connected services
func (h *ProxyHandler) discoverTools() {
	h.Logger.Debug("Discovering tools from connected services")

	connections := h.ConnectionManager.GetAllConnections()
	h.Logger.Debug("Got %d connections from ConnectionManager", len(connections))
	
	h.toolCacheMu.Lock()
	defer h.toolCacheMu.Unlock()

	// Clear existing cache
	h.toolCache = make(map[string]string)

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
		
		for toolName, serverName := range tools {
			h.Logger.Debug("Adding tool %s -> %s to cache", toolName, serverName)
			h.toolCache[toolName] = serverName
		}
	}

	h.cacheExpiry = time.Now().Add(10 * time.Minute)
	h.Logger.Info("Discovered %d tools from %d connected servers (out of %d total)", 
		len(h.toolCache), connectedCount, len(connections))
}

// Tool structure for OpenAPI generation
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// DiscoverServerTools discovers tools from a specific server (public method for cmd/proxy.go)
func (h *ProxyHandler) DiscoverServerTools(serverName string) ([]Tool, error) {
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		h.Logger.Warning("No connection for %s: %v", serverName, err)
		return nil, fmt.Errorf("no connection available for server %s: %w", serverName, err)
	}
	
	h.Logger.Info("Connection status for %s: protocol=%s, status=%s", 
		serverName, conn.Protocol, conn.Status)

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
	h.toolCacheMu.RLock()
	cacheEmpty := len(h.toolCache) == 0
	cacheExpired := time.Now().After(h.cacheExpiry)
	cacheSize := len(h.toolCache)
	h.toolCacheMu.RUnlock()
	
	h.Logger.Debug("FindServerForTool: toolName=%s, cacheEmpty=%v, cacheExpired=%v, cacheSize=%d", 
		toolName, cacheEmpty, cacheExpired, cacheSize)
	
	if cacheEmpty || cacheExpired {
		h.Logger.Info("Tool cache is empty or expired, refreshing...")
		h.discoverTools()
		
		// Check cache size after refresh
		h.toolCacheMu.RLock()
		newCacheSize := len(h.toolCache)
		h.toolCacheMu.RUnlock()
		h.Logger.Info("Tool cache refreshed: old size=%d, new size=%d", cacheSize, newCacheSize)
	}
	
	// Now check the unified cache
	h.toolCacheMu.RLock()
	serverName, found := h.toolCache[toolName]
	finalCacheSize := len(h.toolCache)
	h.toolCacheMu.RUnlock()
	
	if found {
		h.Logger.Debug("Found tool %s in server %s via unified cache", toolName, serverName)
		return serverName, true
	}
	
	h.Logger.Warning("Tool %s not found in unified cache of %d tools", toolName, finalCacheSize)
	
	// Debug: Print all cached tools
	h.toolCacheMu.RLock()
	h.Logger.Debug("Available tools in cache: %v", h.toolCache)
	h.toolCacheMu.RUnlock()
	
	return "", false
}

// findServerForTool method already exists in tool_discovery.go

// makeToolsListRequest makes an MCP tools/list request to discover tools
func (h *ProxyHandler) makeToolsListRequest(serverName string, conn *discovery.MCPConnection) ([]Tool, error) {
	// Create MCP tools/list request with string ID
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/list",
		"params":  map[string]interface{}{},
		"id":      h.generateStringID(),
	}

	// Send request based on protocol
	switch conn.Protocol {
	case "http":
		return h.makeHTTPToolsListRequest(conn, request)
	case "sse":
		return h.makeSSEToolsListRequest(conn, request)
	default:
		return nil, fmt.Errorf("unsupported protocol for tools discovery: %s", conn.Protocol)
	}
}

// makeHTTPToolsListRequest makes HTTP tools/list request with proper MCP session management
func (h *ProxyHandler) makeHTTPToolsListRequest(conn *discovery.MCPConnection, request map[string]interface{}) ([]Tool, error) {
	if conn.HTTPConnection == nil {
		return nil, fmt.Errorf("no HTTP connection available")
	}

	// Generate session ID for this request sequence
	sessionID := h.generateStringID()
	
	// Step 1: Send initialize request
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      h.generateStringID(),
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]interface{}{
				"name":    "matey-proxy",
				"version": "1.0.0",
			},
			"capabilities": map[string]interface{}{},
		},
	}
	
	initResponse, err := h.sendHTTPRequestWithSession(conn.HTTPConnection, sessionID, initRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize HTTP session: %w", err)
	}
	
	// Check initialize response
	if initResponse["error"] != nil {
		return nil, fmt.Errorf("initialize failed: %v", initResponse["error"])
	}
	
	// Step 2: Send initialized notification
	initializedNotif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]interface{}{},
	}
	
	err = h.sendHTTPNotificationWithSession(conn.HTTPConnection, sessionID, initializedNotif)
	if err != nil {
		return nil, fmt.Errorf("failed to send initialized notification: %w", err)
	}
	
	// Step 3: Send tools/list request
	toolsRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      h.generateStringID(),
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}
	
	toolsResponse, err := h.sendHTTPRequestWithSession(conn.HTTPConnection, sessionID, toolsRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to send tools/list request: %w", err)
	}
	
	return h.parseToolsFromMCPResponse(toolsResponse)
}

// makeSSEToolsListRequest makes SSE tools/list request with proper MCP session management
func (h *ProxyHandler) makeSSEToolsListRequest(conn *discovery.MCPConnection, request map[string]interface{}) ([]Tool, error) {
	if conn.SSEConnection == nil {
		return nil, fmt.Errorf("no SSE connection available")
	}
	
	h.Logger.Info("Starting SSE tools/list request for server %s", conn.Name)
	
	// Step 1: Establish SSE connection and get session endpoint
	sessionEndpoint, err := h.establishSSESession(conn.SSEConnection)
	if err != nil {
		return nil, fmt.Errorf("failed to establish SSE session: %w", err)
	}
	
	sessionURL := conn.SSEConnection.BaseURL + sessionEndpoint
	h.Logger.Info("Got session URL: %s", sessionURL)
	
	// Step 2: Send initialize request (don't wait for response)
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      h.generateStringID(),
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]interface{}{
				"name":    "matey-proxy",
				"version": "1.0.0",
			},
			"capabilities": map[string]interface{}{},
		},
	}
	
	err = h.sendSSENotification(sessionURL, initRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to send initialize request: %w", err)
	}
	
	// Step 3: Send initialized notification (don't wait for response)
	initializedNotif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]interface{}{},
	}
	
	err = h.sendSSENotification(sessionURL, initializedNotif)
	if err != nil {
		h.Logger.Warning("Failed to send initialized notification: %v (continuing anyway)", err)
	} else {
		h.Logger.Info("Sent initialized notification successfully")
	}
	
	// Give the server a moment to process
	time.Sleep(100 * time.Millisecond)
	
	// Step 4: Send tools/list request
	toolsRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      h.generateStringID(),
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}
	
	toolsResponse, err := h.sendSSERequestAndWaitForResponse(sessionURL, toolsRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to send tools/list request: %w", err)
	}
	
	h.Logger.Info("Got tools/list response: %v", toolsResponse)
	return h.parseToolsFromMCPResponse(toolsResponse)
}


// establishSSESession establishes an SSE session and returns the session endpoint
func (h *ProxyHandler) establishSSESession(sseConn *discovery.MCPSSEConnection) (string, error) {
	h.Logger.Debug("Establishing SSE session to: %s", sseConn.BaseURL)
	
	// Try GET first (some SSE servers expect GET for initial handshake)
	req, err := http.NewRequest("GET", sseConn.BaseURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create handshake request: %w", err)
	}
	
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	
	// Send handshake request
	resp, err := sseConn.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send handshake request: %w", err)
	}
	defer resp.Body.Close()
	
	h.Logger.Debug("SSE handshake response status: %d", resp.StatusCode)
	
	// Parse SSE stream for session endpoint
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		h.Logger.Debug("SSE handshake line: %s", line)
		
		// Handle empty lines
		if line == "" {
			continue
		}
		
		// Look for endpoint event
		if strings.HasPrefix(line, "event: endpoint") {
			// Next line should contain the session endpoint
			if scanner.Scan() {
				dataLine := scanner.Text()
				h.Logger.Debug("SSE handshake data line: %s", dataLine)
				if strings.HasPrefix(dataLine, "data: ") {
					endpoint := strings.TrimPrefix(dataLine, "data: ")
					h.Logger.Info("Found SSE session endpoint: %s", endpoint)
					return endpoint, nil
				}
			}
		}
		
		// Some servers may send session info in different formats
		if strings.HasPrefix(line, "data: ") {
			dataContent := strings.TrimPrefix(line, "data: ")
			h.Logger.Debug("SSE handshake data content: %s", dataContent)
			
			// Try to parse as JSON to see if it contains session info
			var sessionInfo map[string]interface{}
			if err := json.Unmarshal([]byte(dataContent), &sessionInfo); err == nil {
				if sessionEndpoint, ok := sessionInfo["endpoint"].(string); ok {
					h.Logger.Info("Found session endpoint in JSON: %s", sessionEndpoint)
					return sessionEndpoint, nil
				}
				if sessionPath, ok := sessionInfo["path"].(string); ok {
					h.Logger.Info("Found session path in JSON: %s", sessionPath)
					return sessionPath, nil
				}
			}
			
			// If it looks like a path, use it directly
			if strings.HasPrefix(dataContent, "/") {
				h.Logger.Info("Using data content as session endpoint: %s", dataContent)
				return dataContent, nil
			}
		}
	}
	
	return "", fmt.Errorf("no session endpoint found in SSE handshake response")
}


// parseSSEResponse parses SSE stream for MCP JSON-RPC response
func (h *ProxyHandler) parseSSEResponse(reader io.Reader) (map[string]interface{}, error) {
	scanner := bufio.NewScanner(reader)
	
	for scanner.Scan() {
		line := scanner.Text()
		h.Logger.Debug("SSE response line: %s", line)
		
		// Handle empty lines
		if line == "" {
			continue
		}
		
		// Look for SSE event: response format (proper MCP format)
		if strings.HasPrefix(line, "event: response") {
			h.Logger.Info("Found SSE event: response")
			// Next line should be the data
			if scanner.Scan() {
				dataLine := scanner.Text()
				h.Logger.Info("SSE data line: %s", dataLine)
				if strings.HasPrefix(dataLine, "data: ") {
					jsonData := strings.TrimPrefix(dataLine, "data: ")
					h.Logger.Info("SSE JSON data: %s", jsonData)
					
					var response MCPResponse
					if err := json.Unmarshal([]byte(jsonData), &response); err == nil {
						h.Logger.Debug("Successfully parsed SSE response with ID: %s", response.ID)
						// Convert to map for compatibility
						responseMap := make(map[string]interface{})
						responseMap["jsonrpc"] = response.JSONRPC
						responseMap["id"] = response.ID
						responseMap["result"] = response.Result
						responseMap["error"] = response.Error
						return responseMap, nil
					} else {
						h.Logger.Debug("Failed to parse SSE response as MCPResponse: %v", err)
					}
				}
			}
		}
		
		// Look for SSE event: message format (alternative format from mcp-compose)
		if strings.HasPrefix(line, "event: message") {
			h.Logger.Info("Found SSE event: message")
			// Next line should be the data
			if scanner.Scan() {
				dataLine := scanner.Text()
				h.Logger.Info("SSE message data line: %s", dataLine)
				if strings.HasPrefix(dataLine, "data: ") {
					jsonData := strings.TrimPrefix(dataLine, "data: ")
					h.Logger.Info("SSE message JSON data: %s", jsonData)
					
					var response map[string]interface{}
					if err := json.Unmarshal([]byte(jsonData), &response); err == nil {
						h.Logger.Debug("Successfully parsed SSE message response: %v", response)
						// Check if this is our tools/list response
						if response["id"] != nil && (response["result"] != nil || response["error"] != nil) {
							return response, nil
						}
					} else {
						h.Logger.Debug("Failed to parse SSE message response: %v", err)
					}
				}
			}
		}
		
		// Look for any other event formats
		if strings.HasPrefix(line, "event: ") && !strings.HasPrefix(line, "event: endpoint") {
			eventType := strings.TrimPrefix(line, "event: ")
			h.Logger.Debug("Found SSE event: %s", eventType)
			// Next line should be the data
			if scanner.Scan() {
				dataLine := scanner.Text()
				h.Logger.Debug("SSE event data line: %s", dataLine)
				if strings.HasPrefix(dataLine, "data: ") {
					jsonData := strings.TrimPrefix(dataLine, "data: ")
					h.Logger.Debug("SSE event JSON data: %s", jsonData)
					
					var response map[string]interface{}
					if err := json.Unmarshal([]byte(jsonData), &response); err == nil {
						h.Logger.Debug("Successfully parsed SSE event response: %v", response)
						// Check if this is our tools/list response
						if response["id"] != nil && (response["result"] != nil || response["error"] != nil) {
							return response, nil
						}
					} else {
						h.Logger.Debug("Failed to parse SSE event response: %v", err)
					}
				}
			}
		}
		
		// Fallback: handle legacy data: format
		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimPrefix(line, "data: ")
			h.Logger.Debug("Legacy SSE data: %s", jsonData)
			
			// Parse JSON-RPC response
			var response map[string]interface{}
			if err := json.Unmarshal([]byte(jsonData), &response); err != nil {
				h.Logger.Debug("Failed to parse legacy SSE data: %v", err)
				continue // Skip invalid JSON
			}
			
			h.Logger.Debug("Parsed legacy SSE response: %v", response)
			// Check if this is our tools/list response
			if response["id"] != nil && (response["result"] != nil || response["error"] != nil) {
				return response, nil
			}
		}
	}
	
	h.Logger.Info("No valid MCP response found in SSE stream")
	return nil, fmt.Errorf("no valid MCP response found in SSE stream")
}

// parseToolsFromMCPResponse parses tools from MCP response
func (h *ProxyHandler) parseToolsFromMCPResponse(response map[string]interface{}) ([]Tool, error) {
	result, ok := response["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid MCP response format")
	}

	toolsArray, ok := result["tools"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("no tools array in response")
	}

	var tools []Tool
	for _, toolItem := range toolsArray {
		toolMap, ok := toolItem.(map[string]interface{})
		if !ok {
			continue
		}

		tool := Tool{
			Name:        getString(toolMap, "name"),
			Description: getString(toolMap, "description"),
			Parameters:  getMap(toolMap, "inputSchema"),
		}

		if tool.Name != "" {
			tools = append(tools, tool)
		}
	}

	return tools, nil
}

// createPlaceholderTools creates placeholder tools based on server capabilities
func (h *ProxyHandler) createPlaceholderTools(serverName string, conn *discovery.MCPConnection) []Tool {
	var tools []Tool

	// Create placeholder tools based on capabilities
	for _, capability := range conn.Capabilities {
		switch capability {
		case "tools":
			tools = append(tools, Tool{
				Name:        fmt.Sprintf("%s_tool", serverName),
				Description: fmt.Sprintf("Generic tool from %s server", serverName),
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"input": map[string]interface{}{
							"type":        "string",
							"description": "Input for the tool",
						},
					},
				},
			})
		case "resources":
			tools = append(tools, Tool{
				Name:        fmt.Sprintf("%s_read_resource", serverName),
				Description: fmt.Sprintf("Read resources from %s server", serverName),
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"uri": map[string]interface{}{
							"type":        "string",
							"description": "Resource URI to read",
						},
					},
				},
			})
		}
	}

	// If no tools found, add a default one
	if len(tools) == 0 {
		tools = append(tools, Tool{
			Name:        fmt.Sprintf("%s_default", serverName),
			Description: fmt.Sprintf("Default endpoint for %s server", serverName),
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		})
	}

	return tools
}

// Helper functions for parsing JSON
func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

func getMap(m map[string]interface{}, key string) map[string]interface{} {
	if val, ok := m[key].(map[string]interface{}); ok {
		return val
	}
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
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

// Authentication and CORS methods

// corsError writes a CORS-enabled error response
func (h *ProxyHandler) corsError(w http.ResponseWriter, message string, statusCode int) {
	h.setCORSHeaders(w)
	h.writeErrorResponse(w, message, statusCode)
}

// CorsError writes a CORS-enabled error response (public method for cmd/proxy.go)
func (h *ProxyHandler) CorsError(w http.ResponseWriter, message string, statusCode int) {
	h.corsError(w, message, statusCode)
}

// SetCORSHeaders sets CORS headers for cross-origin requests (public method for cmd/proxy.go)
func (h *ProxyHandler) SetCORSHeaders(w http.ResponseWriter) {
	h.setCORSHeaders(w)
}

// setCORSHeaders sets CORS headers for cross-origin requests
func (h *ProxyHandler) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-MCP-Server, Mcp-Session-Id")
}

// CheckAuth validates API key authentication (public method for cmd/proxy.go)
func (h *ProxyHandler) CheckAuth(r *http.Request) bool {
	return h.checkAuth(r)
}

// checkAuth validates API key authentication
func (h *ProxyHandler) checkAuth(r *http.Request) bool {
	if h.APIKey == "" {
		return true // No auth required
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	return token == h.APIKey
}

// Helper methods

// updateConnectionStats updates connection statistics
func (h *ProxyHandler) updateConnectionStats(serverName string, success bool) {
	h.ConnectionMutex.Lock()
	defer h.ConnectionMutex.Unlock()

	if h.connectionStats[serverName] == nil {
		h.connectionStats[serverName] = &ConnectionStats{}
	}

	stats := h.connectionStats[serverName]
	stats.TotalRequests++

	if success {
		stats.LastSuccess = time.Now()
	} else {
		stats.FailedRequests++
		stats.LastError = time.Now()
	}
}

// writeErrorResponse writes an error response
func (h *ProxyHandler) writeErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	h.setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"error": map[string]interface{}{
			"code":    statusCode,
			"message": message,
		},
		"id": h.getNextRequestID(),
	}
	
	json.NewEncoder(w).Encode(response)
}

// getNextRequestID returns the next request ID
func (h *ProxyHandler) getNextRequestID() int {
	h.GlobalIDMutex.Lock()
	defer h.GlobalIDMutex.Unlock()
	h.GlobalRequestID++
	return h.GlobalRequestID
}

// generateStringID generates a unique string ID for MCP requests
func (h *ProxyHandler) generateStringID() string {
	h.GlobalIDMutex.Lock()
	defer h.GlobalIDMutex.Unlock()
	h.GlobalRequestID++
	return strconv.Itoa(h.GlobalRequestID)
}

// sendHTTPRequestWithSession sends HTTP request with session management
func (h *ProxyHandler) sendHTTPRequestWithSession(httpConn *discovery.MCPHTTPConnection, sessionID string, request map[string]interface{}) (map[string]interface{}, error) {
	// Marshal request
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", httpConn.BaseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set session headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Mcp-Session-Id", sessionID)
	
	resp, err := httpConn.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	var mcpResponse map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&mcpResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return mcpResponse, nil
}

// sendHTTPNotificationWithSession sends HTTP notification with session management
func (h *ProxyHandler) sendHTTPNotificationWithSession(httpConn *discovery.MCPHTTPConnection, sessionID string, notification map[string]interface{}) error {
	// Marshal notification
	payload, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", httpConn.BaseURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set session headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Mcp-Session-Id", sessionID)
	
	resp, err := httpConn.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// sendSSERequestAndWaitForResponse sends SSE request and waits for response
func (h *ProxyHandler) sendSSERequestAndWaitForResponse(sessionURL string, request map[string]interface{}) (map[string]interface{}, error) {
	// Marshal request
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	h.Logger.Info("Sending SSE request to %s: %s", sessionURL, string(payload))

	// Create HTTP request to session endpoint with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "POST", sessionURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create session request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	
	// Send request and read SSE stream
	resp, err := h.sseClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send session request: %w", err)
	}
	defer resp.Body.Close()
	
	h.Logger.Info("SSE request response status: %d", resp.StatusCode)
	
	// Handle non-200 responses
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("SSE request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	
	// Parse SSE stream for MCP JSON-RPC response
	return h.parseSSEResponse(resp.Body)
}

// sendSSENotification sends SSE notification without waiting for response
func (h *ProxyHandler) sendSSENotification(sessionURL string, notification map[string]interface{}) error {
	// Marshal notification
	payload, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Create HTTP request to session endpoint
	req, err := http.NewRequest("POST", sessionURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create session request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	
	// Send notification
	resp, err := h.sseClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send session notification: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("SSE notification failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// Direct tool execution support


// HandleDirectToolCall handles direct tool calls without MCP protocol overhead
func (h *ProxyHandler) HandleDirectToolCall(w http.ResponseWriter, r *http.Request, toolName string) {
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

	// Find which server has this tool
	h.toolCacheMu.RLock()
	serverName, found := h.toolCache[toolName]
	h.toolCacheMu.RUnlock()

	if !found {
		h.corsError(w, "Tool not found", http.StatusNotFound)
		return
	}

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
	case "sse":
		h.forwardDirectSSEToolCall(w, r, conn, payload)
	default:
		h.corsError(w, "Unsupported protocol for direct tool calls", http.StatusNotImplemented)
	}
}

// forwardDirectHTTPToolCall forwards a direct tool call via HTTP
func (h *ProxyHandler) forwardDirectHTTPToolCall(w http.ResponseWriter, r *http.Request, conn *discovery.MCPConnection, payload []byte) {
	// Implementation similar to forwardHTTPRequest but for tool calls
	h.writeErrorResponse(w, "Direct HTTP tool calls not yet implemented", http.StatusNotImplemented)
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
	json.NewEncoder(w).Encode(response)
}

// Compatibility methods for existing server files

// sendMCPError sends an MCP-formatted error response
func (h *ProxyHandler) sendMCPError(w http.ResponseWriter, id interface{}, code int, message string, optionalData ...map[string]interface{}) {
	h.setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // MCP errors use 200 with error in body
	
	errorBody := map[string]interface{}{
		"code":    code,
		"message": message,
	}
	
	// Add optional data if provided
	if len(optionalData) > 0 {
		for key, value := range optionalData[0] {
			errorBody[key] = value
		}
	}
	
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"error":   errorBody,
		"id":      id,
	}
	
	json.NewEncoder(w).Encode(response)
}

// getServerHTTPURL returns the HTTP URL for a server
func (h *ProxyHandler) getServerHTTPURL(serverName string, cfg config.ServerConfig) string {
	// For K8s-native mode, we get the URL from service discovery
	if conn, err := h.ConnectionManager.GetConnection(serverName); err == nil {
		return conn.Endpoint
	}
	// Fallback to empty string if not found
	return ""
}

// getOptimalSSEConnection gets the best SSE connection for a server
func (h *ProxyHandler) getOptimalSSEConnection(serverName string) (interface{}, error) {
	// For K8s-native mode, return a placeholder legacy connection
	if conn, err := h.ConnectionManager.GetConnection(serverName); err == nil && conn.SSEConnection != nil {
		// Convert to legacy format
		return &MCPSSEConnection{
			ServerName:  serverName,
			BaseURL:     conn.Endpoint,
			LastUsed:    time.Now(),
			Initialized: true,
			Healthy:     true,
		}, nil
	}
	return nil, fmt.Errorf("no SSE connection available for server %s", serverName)
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
	tools, err := h.makeSSEToolsListRequest(conn, request)
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

// isProxyStandardMethod checks if a method is a standard proxy method
func isProxyStandardMethod(method string) bool {
	standardMethods := []string{
		"initialize",
		"initialized", 
		"shutdown",
		"ping",
		"tools/list",
		"tools/call",
		"resources/list",
		"resources/read",
		"prompts/list",
		"prompts/get",
	}
	
	for _, stdMethod := range standardMethods {
		if method == stdMethod {
			return true
		}
	}
	return false
}

// recordConnectionEvent records a connection event (compatibility method)
func (h *ProxyHandler) recordConnectionEvent(serverName string, success, timeout bool) {
	eventType := "SUCCESS"
	if !success {
		if timeout {
			eventType = "TIMEOUT"
		} else {
			eventType = "ERROR"
		}
	}
	h.Logger.Info("Connection event [%s] for server %s (success: %v, timeout: %v)", eventType, serverName, success, timeout)
}

// maintainEnhancedSSEConnections maintains enhanced SSE connections (compatibility method)
func (h *ProxyHandler) maintainEnhancedSSEConnections() {
	// For K8s-native mode, connection maintenance is handled by the ConnectionManager
	h.Logger.Debug("Enhanced SSE connection maintenance called - handled by K8s connection manager")
}

// shouldProcessForOpenWebUI determines if a response should be processed for OpenWebUI compatibility
func (h *ProxyHandler) shouldProcessForOpenWebUI(r *http.Request, responseBody []byte) bool {
	// Check if the client expects JSON-RPC format
	// If User-Agent indicates it's a standard MCP client or if Accept header specifies JSON, 
	// don't process for OpenWebUI
	userAgent := r.Header.Get("User-Agent")
	accept := r.Header.Get("Accept")
	
	// Check if this is a standard MCP client that expects JSON-RPC
	if strings.Contains(accept, "application/json") || 
	   strings.Contains(userAgent, "MCP") || 
	   strings.Contains(userAgent, "claude") || 
	   strings.Contains(userAgent, "curl") {
		h.Logger.Info("Client expects JSON-RPC format - NOT processing for OpenWebUI")
		return false
	}
	
	// Check if response looks like MCP JSON-RPC with tools/call result
	var responseData map[string]interface{}
	if json.Unmarshal(responseBody, &responseData) == nil {
		if _, hasResult := responseData["result"]; hasResult {
			if _, hasJsonRPC := responseData["jsonrpc"]; hasJsonRPC {
				// Check if result contains content array (typical of tools/call responses)
				if result, ok := responseData["result"].(map[string]interface{}); ok {
					if _, hasContent := result["content"]; hasContent {
						h.Logger.Info("Detected MCP tools/call response - processing for OpenWebUI")
						return true
					}
				}
			}
		}
	}
	
	return false
}

// processResponseForOpenWebUI processes MCP response for OpenWebUI compatibility
func (h *ProxyHandler) processResponseForOpenWebUI(responseBody []byte) []byte {
	h.Logger.Info("Processing MCP response for OpenWebUI: %s", string(responseBody))
	
	var response map[string]interface{}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		h.Logger.Warning("Failed to parse MCP response: %v", err)
		return nil
	}
	
	// Extract and format the successful result for OpenWebUI - return clean text
	if result, exists := response["result"]; exists {
		h.Logger.Info("Found result in MCP response")
		if resultMap, ok := result.(map[string]interface{}); ok {
			h.Logger.Info("Result is a map")
			if content, exists := resultMap["content"]; exists {
				h.Logger.Info("Found content in result: %+v", content)
				// Process the content for OpenWebUI - extract text from MCP content array
				cleanResult := h.processMCPContentForOpenWebUI(content)
				h.Logger.Info("processMCPContentForOpenWebUI returned: %+v (type: %T)", cleanResult, cleanResult)
				
				// For OpenWebUI, we want just the text content, not JSON
				if cleanText, ok := cleanResult.(string); ok {
					h.Logger.Info("Successfully converted to string: %s", cleanText)
					return []byte(cleanText)
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
	
	return nil
}

// processMCPContentForOpenWebUI processes MCP content for OpenWebUI compatibility
func (h *ProxyHandler) processMCPContentForOpenWebUI(content interface{}) interface{} {
	h.Logger.Info("processMCPContentForOpenWebUI called with: %+v (type: %T)", content, content)
	
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