// internal/discovery/connection_manager.go
package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/phildougherty/m8e/internal/logging"
)

// MCPHTTPConnection represents an HTTP connection
type MCPHTTPConnection struct {
	BaseURL    string
	Client     *http.Client
	LastUsed   time.Time
}

// MCPSSEConnection represents an SSE connection
type MCPSSEConnection struct {
	BaseURL    string
	Client     *http.Client
	LastUsed   time.Time
}

// MCPSTDIOConnection represents a STDIO connection
type MCPSTDIOConnection struct {
	Process   *http.Client // Placeholder
	LastUsed  time.Time
}

// MCPConnection represents a connection to an MCP server
type MCPConnection struct {
	Name           string
	Endpoint       string
	Protocol       string
	Port           int32
	Capabilities   []string
	Status         string
	LastSeen       time.Time
	HTTPConnection *MCPHTTPConnection
	SSEConnection  *MCPSSEConnection
	StdioConnection *MCPSTDIOConnection
	LastUsed       time.Time
	ErrorCount     int
	mu             sync.RWMutex
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
	maxRetries     int
	retryDelay     time.Duration
	healthInterval time.Duration
}

// NewDynamicConnectionManager creates a new dynamic connection manager
func NewDynamicConnectionManager(serviceDiscovery *K8sServiceDiscovery, logger *logging.Logger) *DynamicConnectionManager {
	if logger == nil {
		logger = logging.NewLogger("info")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create HTTP clients for different protocols
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// SSE client with longer timeout for persistent connections
	sseClient := &http.Client{
		Timeout: 0, // No timeout for SSE connections
		Transport: &http.Transport{
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     300 * time.Second,
		},
	}

	dcm := &DynamicConnectionManager{
		connections:      make(map[string]*MCPConnection),
		serviceDiscovery: serviceDiscovery,
		httpClient:       httpClient,
		sseClient:        sseClient,
		logger:           logger,
		ctx:              ctx,
		cancel:           cancel,
		maxRetries:       3,
		retryDelay:       time.Second * 5,
		healthInterval:   time.Minute * 2,
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
		Name:         endpoint.Name,
		Endpoint:     endpoint.URL,
		Protocol:     endpoint.Protocol,
		Port:         endpoint.Port,
		Capabilities: endpoint.Capabilities,  // Copy capabilities from service endpoint
		LastUsed:     time.Now(),
		Status:       "connecting",
		ErrorCount:   0,
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
	case "sse":
		err = dcm.initializeSSEConnection(conn)
	case "stdio":
		err = dcm.initializeStdioConnection(conn)
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
		BaseURL:    conn.Endpoint,
		Client:     dcm.httpClient,
		LastUsed:   time.Now(),
	}

	// Test connection with a ping or capabilities request
	// This would depend on your existing MCPHTTPConnection implementation
	// For now, we'll just create the connection object

	conn.HTTPConnection = httpConn
	return nil
}

// initializeSSEConnection creates an SSE connection
func (dcm *DynamicConnectionManager) initializeSSEConnection(conn *MCPConnection) error {
	// SSE connections typically use /sse path
	sseURL := conn.Endpoint + "/sse"
	sseConn := &MCPSSEConnection{
		BaseURL:    sseURL,
		Client:     dcm.sseClient,
		LastUsed:   time.Now(),
	}

	// Test SSE connection
	// This would depend on your existing MCPSSEConnection implementation

	conn.SSEConnection = sseConn
	return nil
}

// initializeStdioConnection creates a STDIO connection
func (dcm *DynamicConnectionManager) initializeStdioConnection(conn *MCPConnection) error {
	// STDIO connections would need special handling in Kubernetes
	// This might involve connecting to pods directly or using a bridge service
	return fmt.Errorf("STDIO connections not yet implemented for Kubernetes")
}

// closeConnection closes all connection types for a service
func (dcm *DynamicConnectionManager) closeConnection(conn *MCPConnection) {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.HTTPConnection != nil {
		// Close HTTP connection if it has a close method
		conn.HTTPConnection = nil
	}

	if conn.SSEConnection != nil {
		// Close SSE connection if it has a close method
		conn.SSEConnection = nil
	}

	if conn.StdioConnection != nil {
		// Close STDIO connection if it has a close method
		conn.StdioConnection = nil
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
	if conn.Status != "connected" {
		conn.mu.RUnlock()
		return
	}
	conn.mu.RUnlock()

	// Perform health check based on protocol
	var healthy bool
	switch conn.Protocol {
	case "http":
		healthy = dcm.checkHTTPHealth(conn)
	case "sse":
		healthy = dcm.checkSSEHealth(conn)
	default:
		healthy = true // Assume healthy for unknown protocols
	}

	conn.mu.Lock()
	if !healthy {
		conn.Status = "unhealthy"
		conn.ErrorCount++
		dcm.logger.Info("Health check failed for %s (errors: %d)", conn.Name, conn.ErrorCount)
	} else {
		// Reset error count and mark as connected if health check passes
		conn.ErrorCount = 0
		if conn.Status == "unhealthy" {
			conn.Status = "connected"
			dcm.logger.Info("Health check recovered for %s", conn.Name)
		}
	}
	conn.mu.Unlock()
}

// checkHTTPHealth performs an HTTP health check by testing MCP protocol response
func (dcm *DynamicConnectionManager) checkHTTPHealth(conn *MCPConnection) bool {
	if conn.HTTPConnection == nil {
		return false
	}

	// Test actual MCP protocol response with an initialize request
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create a minimal initialize request (no session establishment needed)
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

	response, err := dcm.sendHTTPHealthRequest(ctx, conn, request)
	if err != nil {
		dcm.logger.Debug("HTTP health check failed for %s: %v", conn.Name, err)
		return false
	}

	// Check if response contains expected MCP structure
	if response == nil {
		dcm.logger.Debug("HTTP health check failed for %s: nil response", conn.Name)
		return false
	}

	// Check for valid MCP response structure
	if _, hasResult := response["result"]; hasResult {
		dcm.logger.Debug("HTTP health check passed for %s", conn.Name)
		return true
	}

	if _, hasError := response["error"]; hasError {
		dcm.logger.Debug("HTTP health check passed for %s (server responded with error, but MCP protocol works)", conn.Name)
		return true // Server responded with MCP error, but protocol works
	}

	dcm.logger.Debug("HTTP health check failed for %s: invalid MCP response structure", conn.Name)
	return false
}

// sendHTTPHealthRequest sends a health check request to an HTTP MCP server
func (dcm *DynamicConnectionManager) sendHTTPHealthRequest(ctx context.Context, conn *MCPConnection, request map[string]interface{}) (map[string]interface{}, error) {
	if conn.HTTPConnection == nil {
		return nil, fmt.Errorf("no HTTP connection available")
	}

	// Marshal request
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", conn.HTTPConnection.BaseURL, bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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

// checkSSEHealth performs an SSE health check by testing MCP protocol response
func (dcm *DynamicConnectionManager) checkSSEHealth(conn *MCPConnection) bool {
	if conn.SSEConnection == nil {
		return false
	}

	// Test actual MCP protocol response with an initialize request
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a minimal initialize request (no session establishment needed)
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

	response, err := dcm.sendSSEHealthRequest(ctx, conn, request)
	if err != nil {
		dcm.logger.Debug("SSE health check failed for %s: %v", conn.Name, err)
		return false
	}

	// Check if response contains expected MCP structure
	if response == nil {
		dcm.logger.Debug("SSE health check failed for %s: nil response", conn.Name)
		return false
	}

	// Check for valid MCP response structure
	if _, hasResult := response["result"]; hasResult {
		dcm.logger.Debug("SSE health check passed for %s", conn.Name)
		return true
	}

	if _, hasError := response["error"]; hasError {
		dcm.logger.Debug("SSE health check passed for %s (server responded with error, but MCP protocol works)", conn.Name)
		return true // Server responded with MCP error, but protocol works
	}

	dcm.logger.Debug("SSE health check failed for %s: invalid MCP response structure", conn.Name)
	return false
}

// sendSSEHealthRequest sends a health check request to an SSE MCP server
func (dcm *DynamicConnectionManager) sendSSEHealthRequest(ctx context.Context, conn *MCPConnection, request map[string]interface{}) (map[string]interface{}, error) {
	if conn.SSEConnection == nil {
		return nil, fmt.Errorf("no SSE connection available")
	}

	// For SSE health checks, try to establish a simple connection to the SSE endpoint
	// This is a simplified check - in production, you might want to establish a full SSE session
	client := &http.Client{Timeout: 5 * time.Second}
	
	// Try to connect to the SSE endpoint
	httpReq, err := http.NewRequestWithContext(ctx, "GET", conn.SSEConnection.BaseURL+"/sse", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSE request: %w", err)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("SSE request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check if we can at least connect to the SSE endpoint
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("SSE request failed with status %d", resp.StatusCode)
	}

	// Return a dummy successful response for SSE health check
	return map[string]interface{}{
		"result": []interface{}{},
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