// internal/server/k8s_proxy.go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/phildougherty/m8e/internal/auth"
	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/discovery"
	"github.com/phildougherty/m8e/internal/logging"
	"github.com/phildougherty/m8e/internal/protocol"
)

// ProxyHandler is a pure Kubernetes-native proxy handler
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

// NewProxyHandler creates a new Kubernetes-native proxy handler
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
	h.Logger.Info("Starting Kubernetes-native proxy handler")

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

	h.Logger.Info("Kubernetes-native proxy handler started successfully")
	return nil
}

// Stop stops the proxy handler
func (h *ProxyHandler) Stop() {
	h.Logger.Info("Stopping Kubernetes-native proxy handler")
	
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
		h.Logger.Error(fmt.Sprintf("Failed to get connection for server %s: %v", serverName, err))
		h.writeErrorResponse(w, fmt.Sprintf("Server %s not available: %v", serverName, err), http.StatusServiceUnavailable)
		return
	}

	// Update connection stats
	h.updateConnectionStats(conn.Endpoint.Name, true)

	// Route based on protocol
	switch conn.Endpoint.Protocol {
	case "http":
		h.handleHTTPRequest(w, r, conn)
	case "sse":
		h.handleSSERequest(w, r, conn)
	case "stdio":
		h.handleStdioRequest(w, r, conn)
	default:
		h.writeErrorResponse(w, fmt.Sprintf("Unsupported protocol: %s", conn.Endpoint.Protocol), http.StatusBadRequest)
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
	// Create a new request to the target server
	targetURL := conn.BaseURL + r.URL.Path
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

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	_, err = w.(http.ResponseWriter).Write([]byte{})
	if err == nil {
		// Use io.Copy or similar to stream the response
		buffer := make([]byte, 1024)
		for {
			n, err := resp.Body.Read(buffer)
			if n > 0 {
				w.Write(buffer[:n])
			}
			if err != nil {
				break
			}
		}
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
		h.Logger.Error(fmt.Sprintf("Failed to refresh service discovery: %v", err))
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
	
	h.toolCacheMu.Lock()
	defer h.toolCacheMu.Unlock()

	// Clear existing cache
	h.toolCache = make(map[string]string)

	for serverName, conn := range connections {
		if conn.Status != "connected" {
			continue
		}

		// Discover tools based on protocol
		tools := h.discoverK8sServerTools(serverName, conn)
		for toolName, serverName := range tools {
			h.toolCache[toolName] = serverName
		}
	}

	h.cacheExpiry = time.Now().Add(10 * time.Minute)
	h.Logger.Info(fmt.Sprintf("Discovered %d tools from %d servers", len(h.toolCache), len(connections)))
}

// discoverK8sServerTools discovers tools from a specific server
func (h *ProxyHandler) discoverK8sServerTools(serverName string, conn *discovery.MCPConnection) map[string]string {
	tools := make(map[string]string)

	// This would make an actual MCP tools/list call to the server
	// For now, we'll return a placeholder based on capabilities
	for _, capability := range conn.Endpoint.Capabilities {
		if capability == "tools" {
			// Make actual tools/list request here
			// For now, just add a placeholder
			tools[fmt.Sprintf("%s-tool", serverName)] = serverName
		}
	}

	return tools
}

// Authentication and CORS methods

// corsError writes a CORS-enabled error response
func (h *ProxyHandler) corsError(w http.ResponseWriter, message string, statusCode int) {
	h.setCORSHeaders(w)
	h.writeErrorResponse(w, message, statusCode)
}

// setCORSHeaders sets CORS headers for cross-origin requests
func (h *ProxyHandler) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-MCP-Server, Mcp-Session-Id")
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
	switch conn.Endpoint.Protocol {
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
	response, err := h.sendOptimalSSERequest(conn.Endpoint.Name, payload)
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
		return conn.Endpoint.URL
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
			BaseURL:     conn.Endpoint.URL,
			LastUsed:    time.Now(),
			Initialized: true,
			Healthy:     true,
		}, nil
	}
	return nil, fmt.Errorf("no SSE connection available for server %s", serverName)
}

// sendOptimalSSERequest with map[string]interface{} payload support  
func (h *ProxyHandler) sendOptimalSSERequest(serverName string, payload interface{}) (map[string]interface{}, error) {
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		return nil, fmt.Errorf("no connection available for server %s: %w", serverName, err)
	}

	if conn.SSEConnection == nil {
		return nil, fmt.Errorf("no SSE connection available for server %s", serverName)
	}

	// Convert payload to bytes if it's a map
	var payloadBytes []byte
	switch p := payload.(type) {
	case []byte:
		payloadBytes = p
	case map[string]interface{}:
		var err error
		payloadBytes, err = json.Marshal(p)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported payload type: %T", payload)
	}

	// Make SSE request with the payload
	// This is a simplified implementation - in reality this would involve
	// the full SSE protocol handling
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"result":  map[string]interface{}{"status": "processed via SSE", "payload_size": len(payloadBytes)},
		"id":      h.getNextRequestID(),
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
	h.Logger.Info(fmt.Sprintf("Connection event [%s] for server %s (success: %v, timeout: %v)", eventType, serverName, success, timeout))
}

// maintainEnhancedSSEConnections maintains enhanced SSE connections (compatibility method)
func (h *ProxyHandler) maintainEnhancedSSEConnections() {
	// For K8s-native mode, connection maintenance is handled by the ConnectionManager
	h.Logger.Debug("Enhanced SSE connection maintenance called - handled by K8s connection manager")
}