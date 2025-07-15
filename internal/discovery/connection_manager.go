// internal/discovery/connection_manager.go
package discovery

import (
	"context"
	"fmt"
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
	Endpoint       ServiceEndpoint
	HTTPConnection *MCPHTTPConnection
	SSEConnection  *MCPSSEConnection
	StdioConnection *MCPSTDIOConnection
	LastUsed       time.Time
	Status         string
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
		dcm.logger.Error(fmt.Sprintf("Failed to discover initial services: %v", err))
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
	dcm.logger.Info("Adding connection to service: %s (%s", endpoint.Name, endpoint.URL))

	dcm.mu.Lock()
	defer dcm.mu.Unlock()

	// Check if connection already exists
	if _, exists := dcm.connections[endpoint.Name]; exists {
		dcm.logger.Info("Connection to %s already exists", endpoint.Name)
		return
	}

	// Create new connection
	conn := &MCPConnection{
		Endpoint:   endpoint,
		LastUsed:   time.Now(),
		Status:     "connecting",
		ErrorCount: 0,
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
		conn.Endpoint = endpoint
		
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
	dcm.logger.Info("Initializing connection to %s (%s", conn.Endpoint.Name, conn.Endpoint.Protocol))

	var err error
	
	switch conn.Endpoint.Protocol {
	case "http":
		err = dcm.initializeHTTPConnection(conn)
	case "sse":
		err = dcm.initializeSSEConnection(conn)
	case "stdio":
		err = dcm.initializeStdioConnection(conn)
	default:
		err = fmt.Errorf("unsupported protocol: %s", conn.Endpoint.Protocol)
	}

	conn.mu.Lock()
	if err != nil {
		conn.Status = "failed"
		conn.ErrorCount++
		dcm.logger.Error(fmt.Sprintf("Failed to connect to %s: %v", conn.Endpoint.Name, err))
		
		// Schedule retry if not exceeded max retries
		if conn.ErrorCount <= dcm.maxRetries {
			conn.mu.Unlock()
			go dcm.scheduleRetry(conn)
			return
		}
	} else {
		conn.Status = "connected"
		conn.ErrorCount = 0
		dcm.logger.Info("Successfully connected to %s", conn.Endpoint.Name)
	}
	conn.mu.Unlock()
}

// initializeHTTPConnection creates an HTTP connection
func (dcm *DynamicConnectionManager) initializeHTTPConnection(conn *MCPConnection) error {
	httpConn := &MCPHTTPConnection{
		BaseURL:    conn.Endpoint.URL,
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
	sseURL := conn.Endpoint.URL + "/sse"
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
	switch conn.Endpoint.Protocol {
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
		dcm.logger.Info("Health check failed for %s", conn.Endpoint.Name)
	} else {
		conn.ErrorCount = 0
	}
	conn.mu.Unlock()
}

// checkHTTPHealth performs an HTTP health check
func (dcm *DynamicConnectionManager) checkHTTPHealth(conn *MCPConnection) bool {
	if conn.HTTPConnection == nil {
		return false
	}

	// Implement actual health check logic here
	// This might involve making a request to a health endpoint
	return true
}

// checkSSEHealth performs an SSE health check
func (dcm *DynamicConnectionManager) checkSSEHealth(conn *MCPConnection) bool {
	if conn.SSEConnection == nil {
		return false
	}

	// Implement actual SSE health check logic here
	return true
}