// internal/discovery/connection_manager.go
package discovery

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

	"github.com/phildougherty/m8e/internal/logging"
)

// MCPHTTPConnection represents an HTTP connection
type MCPHTTPConnection struct {
	BaseURL    string
	Client     *http.Client
	LastUsed   time.Time
	SessionID  string
	AuthToken  string // OAuth Bearer token for authentication
	Healthy    bool
	mu         sync.RWMutex
}

// MCPSSEConnection represents an SSE connection
type MCPSSEConnection struct {
	BaseURL    string
	Client     *http.Client
	LastUsed   time.Time
	SessionID  string
	AuthToken  string // OAuth Bearer token for authentication
	Healthy    bool
	mu         sync.RWMutex
}

// MCPStreamableHTTPConnection represents a streamable HTTP connection
type MCPStreamableHTTPConnection struct {
	BaseURL    string
	Client     *http.Client
	LastUsed   time.Time
	SessionID  string
	AuthToken  string // OAuth Bearer token for authentication
	Healthy    bool
	mu         sync.RWMutex
}


// MCPConnection represents a connection to an MCP server
type MCPConnection struct {
	Name                     string
	Endpoint                 string
	Protocol                 string
	Port                     int32
	Capabilities             []string
	Status                   string
	LastSeen                 time.Time
	HTTPConnection           *MCPHTTPConnection
	SSEConnection            *MCPSSEConnection
	StreamableHTTPConnection *MCPStreamableHTTPConnection
	LastUsed                 time.Time
	ErrorCount               int
	ConsecutiveFailures      int
	LastKeepAlive            time.Time
	mu                       sync.RWMutex
}

// ConnectionStatus represents the status of a connection
type ConnectionStatus struct {
	Connected     bool      `json:"connected"`
	LastConnected time.Time `json:"lastConnected"`
	ErrorCount    int       `json:"errorCount"`
	LastError     string    `json:"lastError"`
}

// DynamicConnectionManager manages connections to MCP servers discovered via Kubernetes
type DynamicConnectionManager struct {
	connections    map[string]*MCPConnection
	serviceDiscovery *K8sServiceDiscovery
	httpClient     *http.Client
	sseClient      *http.Client
	logger         *logging.Logger
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.RWMutex
	
	// Connection settings
	maxRetries      int
	retryDelay      time.Duration
	healthInterval  time.Duration
	keepAliveInterval time.Duration
	healthTimeout   time.Duration
	maxConsecutiveFailures int
}

// NewDynamicConnectionManager creates a new dynamic connection manager
func NewDynamicConnectionManager(serviceDiscovery *K8sServiceDiscovery, logger *logging.Logger) *DynamicConnectionManager {
	if logger == nil {
		logger = logging.NewLogger("info")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create HTTP clients for different protocols with enhanced keep-alive settings
	httpClient := &http.Client{
		Timeout: 30 * time.Minute, // Extended for execute_agent
		Transport: &http.Transport{
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
			IdleConnTimeout:       600 * time.Second, // 10 minutes
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableKeepAlives:     false,
		},
	}

	// SSE client with enhanced persistent connection settings
	sseClient := &http.Client{
		Timeout: 0, // No timeout for SSE connections
		Transport: &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       900 * time.Second, // 15 minutes
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableKeepAlives:     false,
		},
	}

	dcm := &DynamicConnectionManager{
		connections:            make(map[string]*MCPConnection),
		serviceDiscovery:       serviceDiscovery,
		httpClient:             httpClient,
		sseClient:              sseClient,
		logger:                 logger,
		ctx:                    ctx,
		cancel:                 cancel,
		maxRetries:             5, // Increased from 3
		retryDelay:             time.Second * 10, // Increased from 5
		healthInterval:         time.Minute * 3, // Increased from 2
		keepAliveInterval:      time.Second * 30, // New keep-alive interval
		healthTimeout:          time.Second * 15, // Increased from 5
		maxConsecutiveFailures: 3, // New threshold for marking unhealthy
	}

	// Register as service discovery handler
	serviceDiscovery.AddHandler(dcm)

	return dcm
}

// Start begins the connection manager
func (dcm *DynamicConnectionManager) Start() error {
	dcm.logger.Info("Starting dynamic connection manager")

	// Start health checking goroutine
	go dcm.healthCheckLoop()
	
	// Start keep-alive goroutine
	go dcm.keepAliveLoop()
	
	// Start connection recovery goroutine
	go dcm.connectionRecoveryLoop()

	// Discover existing services and establish connections
	services, err := dcm.serviceDiscovery.DiscoverMCPServers()
	if err != nil {
		dcm.logger.Error("Failed to discover initial services: %v", err)
		return err
	}

	// Establish connections to existing services
	for _, service := range services {
		dcm.OnServiceAdded(service)
	}

	dcm.logger.Info("Connection manager started with %d initial services", len(services))
	return nil
}

// Stop stops the connection manager
func (dcm *DynamicConnectionManager) Stop() {
	dcm.logger.Info("Stopping dynamic connection manager")
	
	dcm.cancel()

	dcm.mu.Lock()
	defer dcm.mu.Unlock()

	// Close all connections
	for name, conn := range dcm.connections {
		dcm.closeConnection(conn)
		delete(dcm.connections, name)
	}
}

// OnServiceAdded handles service discovery events when a new service is added
func (dcm *DynamicConnectionManager) OnServiceAdded(endpoint ServiceEndpoint) {
	dcm.logger.Debug("Adding connection to service: %s (%s)", endpoint.Name, endpoint.URL)

	dcm.mu.Lock()
	defer dcm.mu.Unlock()

	// Check if connection already exists
	if _, exists := dcm.connections[endpoint.Name]; exists {
		dcm.logger.Debug("Connection to %s already exists", endpoint.Name)
		return
	}

	// Create new connection
	conn := &MCPConnection{
		Name:                endpoint.Name,
		Endpoint:            endpoint.URL,
		Protocol:            endpoint.Protocol,
		Port:                endpoint.Port,
		Capabilities:        endpoint.Capabilities,  // Copy capabilities from service endpoint
		LastUsed:            time.Now(),
		Status:              "connecting",
		ErrorCount:          0,
		ConsecutiveFailures: 0,
		LastKeepAlive:       time.Now(),
	}

	dcm.connections[endpoint.Name] = conn

	// Initialize connection asynchronously
	go dcm.initializeConnection(conn)
}

// OnServiceModified handles service discovery events when a service is modified
func (dcm *DynamicConnectionManager) OnServiceModified(endpoint ServiceEndpoint) {
	dcm.logger.Info("Updating connection to service: %s", endpoint.Name)

	dcm.mu.Lock()
	defer dcm.mu.Unlock()

	if conn, exists := dcm.connections[endpoint.Name]; exists {
		// Update endpoint information
		conn.Name = endpoint.Name
		conn.Endpoint = endpoint.URL
		conn.Protocol = endpoint.Protocol
		conn.Port = endpoint.Port
		conn.Capabilities = endpoint.Capabilities  // Update capabilities
		
		// If URL changed, reinitialize connection
		if conn.HTTPConnection != nil && conn.HTTPConnection.BaseURL != endpoint.URL {
			dcm.closeConnection(conn)
			go dcm.initializeConnection(conn)
		}
	} else {
		// Service didn't exist before, add it
		dcm.OnServiceAdded(endpoint)
	}
}

// OnServiceDeleted handles service discovery events when a service is deleted
func (dcm *DynamicConnectionManager) OnServiceDeleted(serviceName, namespace string) {
	dcm.logger.Info("Removing connection to service: %s", serviceName)

	dcm.mu.Lock()
	defer dcm.mu.Unlock()

	if conn, exists := dcm.connections[serviceName]; exists {
		dcm.closeConnection(conn)
		delete(dcm.connections, serviceName)
	}
}

// GetConnection returns a connection for the specified service
func (dcm *DynamicConnectionManager) GetConnection(serviceName string) (*MCPConnection, error) {
	dcm.mu.RLock()
	defer dcm.mu.RUnlock()

	conn, exists := dcm.connections[serviceName]
	if !exists {
		return nil, fmt.Errorf("no connection found for service: %s", serviceName)
	}

	if conn.Status != "connected" {
		return nil, fmt.Errorf("service %s is not connected (status: %s)", serviceName, conn.Status)
	}

	// Update last used time
	conn.mu.Lock()
	conn.LastUsed = time.Now()
	conn.mu.Unlock()

	return conn, nil
}

// GetAllConnections returns all active connections
func (dcm *DynamicConnectionManager) GetAllConnections() map[string]*MCPConnection {
	dcm.mu.RLock()
	defer dcm.mu.RUnlock()

	connections := make(map[string]*MCPConnection)
	for name, conn := range dcm.connections {
		connections[name] = conn
	}
	return connections
}

// GetConnectionStatus returns the status of all connections
func (dcm *DynamicConnectionManager) GetConnectionStatus() map[string]ConnectionStatus {
	dcm.mu.RLock()
	defer dcm.mu.RUnlock()

	status := make(map[string]ConnectionStatus)
	for name, conn := range dcm.connections {
		conn.mu.RLock()
		status[name] = ConnectionStatus{
			Connected:     conn.Status == "connected",
			LastConnected: conn.LastUsed,
			ErrorCount:    conn.ErrorCount,
		}
		conn.mu.RUnlock()
	}
	return status
}

// initializeConnection establishes a connection to an MCP server
func (dcm *DynamicConnectionManager) initializeConnection(conn *MCPConnection) {
	dcm.logger.Debug("Initializing connection to %s (%s)", conn.Name, conn.Protocol)

	var err error
	
	switch conn.Protocol {
	case "http":
		err = dcm.initializeHTTPConnection(conn)
	case "http-stream":
		err = dcm.initializeStreamableHTTPConnection(conn)
	case "sse":
		err = dcm.initializeSSEConnection(conn)
	default:
		err = fmt.Errorf("unsupported protocol: %s", conn.Protocol)
	}

	conn.mu.Lock()
	if err != nil {
		conn.Status = "failed"
		conn.ErrorCount++
		dcm.logger.Error("Failed to connect to %s: %v", conn.Name, err)
		
		// Schedule retry if not exceeded max retries
		if conn.ErrorCount <= dcm.maxRetries {
			conn.mu.Unlock()
			go dcm.scheduleRetry(conn)
			return
		}
	} else {
		conn.Status = "connected"
		conn.ErrorCount = 0
		conn.ConsecutiveFailures = 0
		conn.LastKeepAlive = time.Now()
		dcm.logger.Debug("Successfully connected to %s", conn.Name)
		
		// Perform initial health check to verify MCP protocol response
		conn.mu.Unlock()
		go dcm.checkConnectionHealth(conn)
		return
	}
	conn.mu.Unlock()
}

// initializeHTTPConnection creates an HTTP connection
func (dcm *DynamicConnectionManager) initializeHTTPConnection(conn *MCPConnection) error {
	httpConn := &MCPHTTPConnection{
		BaseURL:   conn.Endpoint,
		Client:    dcm.httpClient,
		LastUsed:  time.Now(),
		Healthy:   true,
		SessionID: "",
	}

	conn.HTTPConnection = httpConn
	return nil
}

// initializeStreamableHTTPConnection creates a streamable HTTP connection
func (dcm *DynamicConnectionManager) initializeStreamableHTTPConnection(conn *MCPConnection) error {
	streamableConn := &MCPStreamableHTTPConnection{
		BaseURL:   conn.Endpoint,
		Client:    dcm.httpClient,
		LastUsed:  time.Now(),
		Healthy:   true,
		SessionID: "",
	}

	conn.StreamableHTTPConnection = streamableConn
	return nil
}

// initializeSSEConnection creates an SSE connection
func (dcm *DynamicConnectionManager) initializeSSEConnection(conn *MCPConnection) error {
	sseConn := &MCPSSEConnection{
		BaseURL:   conn.Endpoint,
		Client:    dcm.sseClient,
		LastUsed:  time.Now(),
		Healthy:   true,
		SessionID: "",
	}

	conn.SSEConnection = sseConn
	return nil
}

// closeConnection closes all connection types for a service
func (dcm *DynamicConnectionManager) closeConnection(conn *MCPConnection) {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.HTTPConnection != nil {
		conn.HTTPConnection.mu.Lock()
		conn.HTTPConnection.Healthy = false
		conn.HTTPConnection.mu.Unlock()
		conn.HTTPConnection = nil
	}

	if conn.StreamableHTTPConnection != nil {
		conn.StreamableHTTPConnection.mu.Lock()
		conn.StreamableHTTPConnection.Healthy = false
		conn.StreamableHTTPConnection.mu.Unlock()
		conn.StreamableHTTPConnection = nil
	}

	if conn.SSEConnection != nil {
		conn.SSEConnection.mu.Lock()
		conn.SSEConnection.Healthy = false
		conn.SSEConnection.mu.Unlock()
		conn.SSEConnection = nil
	}

	conn.Status = "disconnected"
}

// scheduleRetry schedules a connection retry after a delay
func (dcm *DynamicConnectionManager) scheduleRetry(conn *MCPConnection) {
	select {
	case <-dcm.ctx.Done():
		return
	case <-time.After(dcm.retryDelay):
		dcm.initializeConnection(conn)
	}
}

// healthCheckLoop performs periodic health checks on connections
func (dcm *DynamicConnectionManager) healthCheckLoop() {
	ticker := time.NewTicker(dcm.healthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-dcm.ctx.Done():
			return
		case <-ticker.C:
			dcm.performHealthChecks()
		}
	}
}

// keepAliveLoop performs periodic keep-alive requests on connections
func (dcm *DynamicConnectionManager) keepAliveLoop() {
	ticker := time.NewTicker(dcm.keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-dcm.ctx.Done():
			return
		case <-ticker.C:
			dcm.performKeepAlive()
		}
	}
}

// performKeepAlive sends keep-alive requests to all connected services
func (dcm *DynamicConnectionManager) performKeepAlive() {
	dcm.mu.RLock()
	connections := make([]*MCPConnection, 0, len(dcm.connections))
	for _, conn := range dcm.connections {
		connections = append(connections, conn)
	}
	dcm.mu.RUnlock()

	for _, conn := range connections {
		go dcm.sendKeepAlive(conn)
	}
}

// sendKeepAlive sends a lightweight keep-alive request to maintain connection
func (dcm *DynamicConnectionManager) sendKeepAlive(conn *MCPConnection) {
	conn.mu.RLock()
	if conn.Status != "connected" {
		conn.mu.RUnlock()
		return
	}
	connName := conn.Name
	protocol := conn.Protocol
	conn.mu.RUnlock()

	dcm.logger.Debug("Sending keep-alive to %s (%s)", connName, protocol)

	// Send lightweight ping based on protocol
	var success bool
	switch protocol {
	case "http":
		success = dcm.sendHTTPKeepAlive(conn)
	case "http-stream":
		success = dcm.sendStreamableHTTPKeepAlive(conn)
	case "sse":
		success = dcm.sendSSEKeepAlive(conn)
	default:
		success = true // Assume success for unknown protocols
	}

	conn.mu.Lock()
	if success {
		conn.LastKeepAlive = time.Now()
		// Reset consecutive failures on successful keep-alive
		if conn.ConsecutiveFailures > 0 {
			dcm.logger.Debug("Keep-alive successful for %s, resetting consecutive failures", connName)
			conn.ConsecutiveFailures = 0
		}
	} else {
		conn.ConsecutiveFailures++
		dcm.logger.Debug("Keep-alive failed for %s (consecutive failures: %d)", connName, conn.ConsecutiveFailures)
	}
	conn.mu.Unlock()
}

// sendHTTPKeepAlive sends a lightweight HTTP keep-alive request
func (dcm *DynamicConnectionManager) sendHTTPKeepAlive(conn *MCPConnection) bool {
	conn.mu.RLock()
	if conn.HTTPConnection == nil {
		conn.mu.RUnlock()
		return false
	}
	baseURL := conn.HTTPConnection.BaseURL
	authToken := conn.HTTPConnection.AuthToken
	conn.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Send a simple ping request
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "keep-alive",
		"method":  "ping",
	}

	reqBytes, err := json.Marshal(request)
	if err != nil {
		return false
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL, bytes.NewBuffer(reqBytes))
	if err != nil {
		return false
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if authToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+authToken)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Any response (including errors) indicates the connection is alive
	return resp.StatusCode < 500
}

// sendStreamableHTTPKeepAlive sends a lightweight streamable HTTP keep-alive request
func (dcm *DynamicConnectionManager) sendStreamableHTTPKeepAlive(conn *MCPConnection) bool {
	conn.mu.RLock()
	if conn.StreamableHTTPConnection == nil {
		conn.mu.RUnlock()
		return false
	}
	baseURL := conn.StreamableHTTPConnection.BaseURL
	authToken := conn.StreamableHTTPConnection.AuthToken
	conn.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "keep-alive",
		"method":  "ping",
	}

	reqBytes, err := json.Marshal(request)
	if err != nil {
		return false
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL, bytes.NewBuffer(reqBytes))
	if err != nil {
		return false
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if authToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+authToken)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode < 500
}

// sendSSEKeepAlive sends a lightweight SSE keep-alive request
func (dcm *DynamicConnectionManager) sendSSEKeepAlive(conn *MCPConnection) bool {
	conn.mu.RLock()
	if conn.SSEConnection == nil {
		conn.mu.RUnlock()
		return false
	}
	baseURL := conn.SSEConnection.BaseURL
	conn.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Simple connectivity test for SSE
	sseURL := baseURL + "/sse"
	req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		return false
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	client := &http.Client{Timeout: dcm.healthTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}

// connectionRecoveryLoop periodically attempts to recover failed connections
func (dcm *DynamicConnectionManager) connectionRecoveryLoop() {
	ticker := time.NewTicker(time.Minute * 10) // Recovery attempts every 10 minutes
	defer ticker.Stop()

	for {
		select {
		case <-dcm.ctx.Done():
			return
		case <-ticker.C:
			dcm.attemptConnectionRecovery()
		}
	}
}

// attemptConnectionRecovery tries to recover connections that have failed or are unhealthy
func (dcm *DynamicConnectionManager) attemptConnectionRecovery() {
	dcm.mu.RLock()
	connections := make([]*MCPConnection, 0, len(dcm.connections))
	for _, conn := range dcm.connections {
		connections = append(connections, conn)
	}
	dcm.mu.RUnlock()

	for _, conn := range connections {
		conn.mu.RLock()
		shouldRecover := (conn.Status == "failed" || conn.Status == "unhealthy") && 
			conn.ErrorCount > dcm.maxRetries &&
			time.Since(conn.LastUsed) > time.Minute*5 // Wait 5 minutes before recovery attempt
		connName := conn.Name
		errorCount := conn.ErrorCount
		conn.mu.RUnlock()

		if shouldRecover {
			dcm.logger.Info("Attempting connection recovery for %s (errors: %d)", connName, errorCount)
			
			// Reset error counts to allow retry
			conn.mu.Lock()
			conn.ErrorCount = 0
			conn.ConsecutiveFailures = 0
			conn.Status = "connecting"
			conn.mu.Unlock()
			
			// Attempt to reinitialize connection
			go dcm.initializeConnection(conn)
		}
	}
}

// performHealthChecks checks the health of all connections
func (dcm *DynamicConnectionManager) performHealthChecks() {
	dcm.mu.RLock()
	connections := make([]*MCPConnection, 0, len(dcm.connections))
	for _, conn := range dcm.connections {
		connections = append(connections, conn)
	}
	dcm.mu.RUnlock()

	for _, conn := range connections {
		go dcm.checkConnectionHealth(conn)
	}
}

// checkConnectionHealth checks the health of a single connection
func (dcm *DynamicConnectionManager) checkConnectionHealth(conn *MCPConnection) {
	conn.mu.RLock()
	connName := conn.Name
	connStatus := conn.Status
	conn.mu.RUnlock()
	
	// Health check all connections, including unhealthy ones, to allow recovery
	dcm.logger.Debug("Performing health check for %s (current status: %s)", connName, connStatus)

	// Perform health check based on protocol
	var healthy bool
	switch conn.Protocol {
	case "http":
		healthy = dcm.checkHTTPHealth(conn)
	case "http-stream":
		healthy = dcm.checkStreamableHTTPHealth(conn)
	case "sse":
		healthy = dcm.checkSSEHealth(conn)
	default:
		healthy = true // Assume healthy for unknown protocols
	}

	conn.mu.Lock()
	if !healthy {
		conn.ConsecutiveFailures++
		conn.ErrorCount++
		
		// Only mark unhealthy if consecutive failures exceed threshold
		if conn.ConsecutiveFailures >= dcm.maxConsecutiveFailures {
			conn.Status = "unhealthy"
			dcm.logger.Info("Health check failed for %s (consecutive failures: %d, total errors: %d) - marking unhealthy", 
				conn.Name, conn.ConsecutiveFailures, conn.ErrorCount)
		} else {
			dcm.logger.Debug("Health check failed for %s (consecutive failures: %d/%d, total errors: %d) - staying connected", 
				conn.Name, conn.ConsecutiveFailures, dcm.maxConsecutiveFailures, conn.ErrorCount)
		}
	} else {
		// Reset consecutive failures and error count on successful health check
		if conn.ConsecutiveFailures > 0 || conn.ErrorCount > 0 {
			dcm.logger.Debug("Health check recovered for %s (resetting %d consecutive failures, %d total errors)", 
				conn.Name, conn.ConsecutiveFailures, conn.ErrorCount)
		}
		conn.ConsecutiveFailures = 0
		conn.ErrorCount = 0
		if conn.Status == "unhealthy" {
			conn.Status = "connected"
			dcm.logger.Info("Health check fully recovered for %s", conn.Name)
		}
	}
	conn.mu.Unlock()
}

// checkHTTPHealth performs an HTTP health check by testing MCP protocol response
func (dcm *DynamicConnectionManager) checkHTTPHealth(conn *MCPConnection) bool {
	// Safely read connection details under lock
	conn.mu.RLock()
	connName := conn.Name
	var baseURL string
	hasHTTPConnection := conn.HTTPConnection != nil
	if hasHTTPConnection {
		baseURL = conn.HTTPConnection.BaseURL
	}
	conn.mu.RUnlock()
	
	dcm.logger.Debug("Starting HTTP health check for %s", connName)
	
	if !hasHTTPConnection {
		dcm.logger.Debug("No HTTP connection available for %s", connName)
		return false
	}

	dcm.logger.Debug("HTTP connection available for %s - BaseURL: %s", connName, baseURL)

	// Test actual MCP protocol response with an initialize request
	ctx, cancel := context.WithTimeout(context.Background(), dcm.healthTimeout)
	defer cancel()

	// Create a minimal initialize request
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "health-check",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]interface{}{
				"name":    "matey-health-check",
				"version": "1.0.0",
			},
			"capabilities": map[string]interface{}{},
		},
	}

	dcm.logger.Debug("Sending HTTP health request for %s", connName)
	response, err := dcm.sendHTTPHealthRequest(ctx, baseURL, request)
	if err != nil {
		dcm.logger.Debug("HTTP health request failed for %s: %v", connName, err)
		return false
	}

	// Check if response contains expected MCP structure
	dcm.logger.Debug("Checking HTTP response for %s", connName)
	
	if response == nil {
		dcm.logger.Debug("HTTP health check failed for %s: nil response", connName)
		return false
	}

	// Check for valid MCP response structure
	if _, hasResult := response["result"]; hasResult {
		dcm.logger.Debug("HTTP health check passed for %s - found result field", connName)
		return true
	}

	if _, hasError := response["error"]; hasError {
		dcm.logger.Debug("HTTP health check passed for %s - found error field (server responded with error, but MCP protocol works)", connName)
		return true // Server responded with MCP error, but protocol works
	}

	dcm.logger.Debug("HTTP health check failed for %s: invalid MCP response structure", connName)
	return false
}

// sendHTTPHealthRequest sends a health check request to an HTTP MCP server
func (dcm *DynamicConnectionManager) sendHTTPHealthRequest(ctx context.Context, baseURL string, request map[string]interface{}) (map[string]interface{}, error) {
	// Marshal request
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL, bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	// Send request with shorter timeout for health checks
	client := &http.Client{Timeout: dcm.healthTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			dcm.logger.Debug("Failed to close response body: %s", err.Error())
		}
	}()

	// Check status code - accept any response including errors
	if resp.StatusCode < 200 || resp.StatusCode >= 500 {
		return nil, fmt.Errorf("HTTP request failed with status %d", resp.StatusCode)
	}

	// Read response
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse JSON response
	var response map[string]interface{}
	if err := json.Unmarshal(respBytes, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return response, nil
}

// checkStreamableHTTPHealth performs a streamable HTTP health check
func (dcm *DynamicConnectionManager) checkStreamableHTTPHealth(conn *MCPConnection) bool {
	conn.mu.RLock()
	connName := conn.Name
	var baseURL string
	hasStreamableConnection := conn.StreamableHTTPConnection != nil
	if hasStreamableConnection {
		baseURL = conn.StreamableHTTPConnection.BaseURL
	}
	conn.mu.RUnlock()
	
	dcm.logger.Debug("Starting streamable HTTP health check for %s", connName)
	
	if !hasStreamableConnection {
		dcm.logger.Debug("No streamable HTTP connection available for %s", connName)
		return false
	}

	// Test actual MCP protocol response with an initialize request
	ctx, cancel := context.WithTimeout(context.Background(), dcm.healthTimeout)
	defer cancel()

	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "health-check",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]interface{}{
				"name":    "matey-health-check",
				"version": "1.0.0",
			},
			"capabilities": map[string]interface{}{},
		},
	}

	response, err := dcm.sendHTTPHealthRequest(ctx, baseURL, request)
	if err != nil {
		dcm.logger.Debug("Streamable HTTP health request failed for %s: %v", connName, err)
		return false
	}

	// Check if response contains expected MCP structure
	if response == nil {
		dcm.logger.Debug("Streamable HTTP health check failed for %s: nil response", connName)
		return false
	}

	// Check for valid MCP response structure
	if _, hasResult := response["result"]; hasResult {
		dcm.logger.Debug("Streamable HTTP health check passed for %s - found result field", connName)
		return true
	}

	if _, hasError := response["error"]; hasError {
		dcm.logger.Debug("Streamable HTTP health check passed for %s - found error field (server responded with error, but MCP protocol works)", connName)
		return true
	}

	dcm.logger.Debug("Streamable HTTP health check failed for %s: invalid MCP response structure", connName)
	return false
}

// sendStreamableHTTPHealthRequest sends a health check request to a streamable HTTP MCP server
func (dcm *DynamicConnectionManager) sendStreamableHTTPHealthRequest(ctx context.Context, conn *MCPConnection, request map[string]interface{}) (map[string]interface{}, error) {
	if conn == nil || conn.StreamableHTTPConnection == nil {
		return nil, fmt.Errorf("no streamable HTTP connection available")
	}
	conn.mu.RLock()
	baseURL := conn.StreamableHTTPConnection.BaseURL
	conn.mu.RUnlock()

	// Marshal request
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request with streaming headers
	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL, bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("X-Streaming", "true")
	httpReq.Header.Set("Cache-Control", "no-cache")

	// Send request with extended timeout for streamable operations
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			dcm.logger.Info("Failed to close response body: %s", err.Error())
		}
	}()

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP request failed with status %d", resp.StatusCode)
	}

	// Handle streaming response
	var response map[string]interface{}
	
	// Check if response is chunked or streaming
	if resp.Header.Get("Transfer-Encoding") == "chunked" || resp.Header.Get("X-Streaming") == "true" {
		response, err = dcm.readChunkedJSONResponse(resp)
	} else {
		err = json.NewDecoder(resp.Body).Decode(&response)
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return response, nil
}

// readChunkedJSONResponse reads a chunked JSON response
func (dcm *DynamicConnectionManager) readChunkedJSONResponse(resp *http.Response) (map[string]interface{}, error) {
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

// checkSSEHealth performs an SSE health check by testing basic SSE connectivity
func (dcm *DynamicConnectionManager) checkSSEHealth(conn *MCPConnection) bool {
	conn.mu.RLock()
	connName := conn.Name
	var baseURL string
	hasSSEConnection := conn.SSEConnection != nil
	if hasSSEConnection {
		baseURL = conn.SSEConnection.BaseURL
	}
	conn.mu.RUnlock()
	
	dcm.logger.Debug("Starting SSE health check for %s", connName)
	
	if !hasSSEConnection {
		dcm.logger.Debug("No SSE connection available for %s", connName)
		return false
	}

	// Simple SSE connectivity test - just check if SSE endpoint responds
	ctx, cancel := context.WithTimeout(context.Background(), dcm.healthTimeout)
	defer cancel()

	// Try to connect to SSE endpoint
	sseURL := baseURL + "/sse"
	req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		dcm.logger.Debug("Failed to create SSE health check request for %s: %v", connName, err)
		return false
	}

	// Set SSE headers
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	dcm.logger.Debug("Sending SSE health request for %s to %s", connName, sseURL)
	resp, err := dcm.sseClient.Do(req)
	if err != nil {
		dcm.logger.Debug("SSE health request failed for %s: %v", connName, err)
		return false
	}
	defer resp.Body.Close()

	// Any response from SSE endpoint indicates the service is running
	dcm.logger.Debug("SSE health check for %s returned status: %d", connName, resp.StatusCode)
	
	// Consider service healthy if SSE endpoint responds
	if resp.StatusCode == 200 {
		dcm.logger.Debug("SSE health check passed for %s - endpoint is responding", connName)
		return true
	}

	dcm.logger.Debug("SSE health check failed for %s - endpoint returned status %d", connName, resp.StatusCode)
	return false
}

// sendSSEHealthRequest sends a health check request to an SSE MCP server
func (dcm *DynamicConnectionManager) sendSSEHealthRequest(ctx context.Context, conn *MCPConnection, request map[string]interface{}) (map[string]interface{}, error) {
	if conn.SSEConnection == nil {
		return nil, fmt.Errorf("no SSE connection available")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	
	// Step 1: Connect to SSE endpoint to get session endpoint
	sseURL := conn.SSEConnection.BaseURL + "/sse"
	dcm.logger.Debug("Attempting SSE health check for %s at URL: %s", conn.Name, sseURL)
	
	httpReq, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		dcm.logger.Debug("Failed to create SSE request for %s: %v", conn.Name, err)
		return nil, fmt.Errorf("failed to create SSE request: %w", err)
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Cache-Control", "no-cache")

	dcm.logger.Debug("Sending SSE request for %s...", conn.Name)
	resp, err := client.Do(httpReq)
	if err != nil {
		dcm.logger.Debug("SSE request failed for %s: %v", conn.Name, err)
		return nil, fmt.Errorf("SSE request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			dcm.logger.Info("Failed to close response body: %s", err.Error())
		}
	}()

	dcm.logger.Debug("SSE response for %s - Status: %d", conn.Name, resp.StatusCode)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		dcm.logger.Debug("SSE request for %s failed with status %d", conn.Name, resp.StatusCode)
		return nil, fmt.Errorf("SSE request failed with status %d", resp.StatusCode)
	}

	// Step 2: Read the endpoint discovery from SSE stream
	dcm.logger.Debug("Reading SSE stream for %s...", conn.Name)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Split(bufio.ScanLines)
	
	sessionEndpoint := ""
	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		// Removed excessive line-by-line logging
		
		if strings.HasPrefix(line, "event: endpoint") {
			dcm.logger.Debug("Found event: endpoint for %s", conn.Name)
			// Next line should have the endpoint data
			if scanner.Scan() {
				dataLine := scanner.Text()
				lineCount++
				// Removed excessive line logging
				if strings.HasPrefix(dataLine, "data: ") {
					sessionPath := strings.TrimPrefix(dataLine, "data: ")
					// BaseURL is now the server base URL, so use it directly
					serverBaseURL := strings.TrimSuffix(conn.SSEConnection.BaseURL, "/")
					sessionEndpoint = serverBaseURL + sessionPath
					dcm.logger.Debug("SSE health check found session endpoint for %s: %s", conn.Name, sessionEndpoint)
					break
				} else {
					dcm.logger.Debug("Data line for %s does not start with 'data: '", conn.Name)
				}
			} else {
				dcm.logger.Debug("Failed to read data line after event: endpoint for %s", conn.Name)
			}
		}
	}

	if scanner.Err() != nil {
		dcm.logger.Debug("Scanner error for %s: %v", conn.Name, scanner.Err())
		return nil, fmt.Errorf("error reading SSE stream: %w", scanner.Err())
	}

	dcm.logger.Debug("Finished reading SSE stream for %s, session endpoint: '%s'", conn.Name, sessionEndpoint)

	if sessionEndpoint == "" {
		dcm.logger.Debug("No session endpoint found for %s after reading %d lines", conn.Name, lineCount)
		return nil, fmt.Errorf("no session endpoint found in SSE stream")
	}

	// Step 3: Try to send a simple ping to the base messages endpoint (no session required)
	requestData, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "health-ping",
		"method":  "ping",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ping request: %w", err)
	}

	// Use base messages endpoint without session ID for ping
	messagesEndpoint := strings.TrimSuffix(conn.SSEConnection.BaseURL, "/") + "/messages"
	dcm.logger.Info("TRACE: Sending ping to messages endpoint: %s", messagesEndpoint)
	pingReq, err := http.NewRequestWithContext(ctx, "POST", messagesEndpoint, bytes.NewBuffer(requestData))
	if err != nil {
		return nil, fmt.Errorf("failed to create ping request: %w", err)
	}
	pingReq.Header.Set("Content-Type", "application/json")

	pingResp, err := client.Do(pingReq)
	if err != nil {
		return nil, fmt.Errorf("ping request failed: %w", err)
	}
	defer func() {
		if err := pingResp.Body.Close(); err != nil {
			// Log error but don't affect ping result
			fmt.Printf("Warning: Failed to close ping response body: %v\n", err)
		}
	}()

	if pingResp.StatusCode < 200 || pingResp.StatusCode >= 300 {
		return nil, fmt.Errorf("ping request failed with status %d", pingResp.StatusCode)
	}

	// Return successful health check
	return map[string]interface{}{
		"result": "healthy",
	}, nil
}

// AddConnection adds a new connection to the manager
func (dcm *DynamicConnectionManager) AddConnection(conn *MCPConnection) error {
	if conn == nil {
		return fmt.Errorf("connection cannot be nil")
	}

	dcm.mu.Lock()
	defer dcm.mu.Unlock()

	if dcm.connections == nil {
		dcm.connections = make(map[string]*MCPConnection)
	}

	dcm.connections[conn.Name] = conn
	return nil
}

// RemoveConnection removes a connection from the manager
func (dcm *DynamicConnectionManager) RemoveConnection(name string) error {
	dcm.mu.Lock()
	defer dcm.mu.Unlock()

	if conn, exists := dcm.connections[name]; exists {
		dcm.closeConnection(conn)
		delete(dcm.connections, name)
		return nil
	}

	return fmt.Errorf("connection %s not found", name)
}


// GetConnections returns all connections as a slice
func (dcm *DynamicConnectionManager) GetConnections() []*MCPConnection {
	dcm.mu.RLock()
	defer dcm.mu.RUnlock()

	connections := make([]*MCPConnection, 0, len(dcm.connections))
	for _, conn := range dcm.connections {
		connections = append(connections, conn)
	}
	return connections
}

// GetHealthyConnections returns connections that are healthy within the given time window
func (dcm *DynamicConnectionManager) GetHealthyConnections(maxAge time.Duration) []*MCPConnection {
	dcm.mu.RLock()
	defer dcm.mu.RUnlock()

	healthy := make([]*MCPConnection, 0)
	cutoff := time.Now().Add(-maxAge)

	for _, conn := range dcm.connections {
		if conn.Status == "connected" && conn.LastSeen.After(cutoff) {
			healthy = append(healthy, conn)
		}
	}

	return healthy
}

// UpdateConnectionStatus updates the status of a connection
func (dcm *DynamicConnectionManager) UpdateConnectionStatus(name, status string) error {
	dcm.mu.Lock()
	defer dcm.mu.Unlock()

	if conn, exists := dcm.connections[name]; exists {
		conn.Status = status
		conn.LastSeen = time.Now()
		return nil
	}

	return fmt.Errorf("connection %s not found", name)
}

// CleanupStaleConnections removes connections that haven't been seen recently
func (dcm *DynamicConnectionManager) CleanupStaleConnections(maxAge time.Duration) error {
	dcm.mu.Lock()
	defer dcm.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	toRemove := make([]string, 0)

	for name, conn := range dcm.connections {
		if conn.LastSeen.Before(cutoff) {
			toRemove = append(toRemove, name)
		}
	}

	for _, name := range toRemove {
		if conn, exists := dcm.connections[name]; exists {
			dcm.closeConnection(conn)
			delete(dcm.connections, name)
		}
	}

	return nil
}
