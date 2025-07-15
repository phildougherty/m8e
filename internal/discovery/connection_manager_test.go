package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMCPConnection_Basic(t *testing.T) {
	endpoint := ServiceEndpoint{
		Name:      "test-service",
		Namespace: "default",
		Protocol:  "http",
		URL:       "http://test-service.default.svc.cluster.local:8080/mcp",
		Port:      8080,
	}

	connection := &MCPConnection{
		Endpoint:  endpoint,
		LastUsed:  time.Now(),
		Status:    "connected",
		ErrorCount: 0,
	}

	// Test basic properties
	if connection.Endpoint.Name != "test-service" {
		t.Errorf("Expected service name 'test-service', got %s", connection.Endpoint.Name)
	}

	if connection.Endpoint.Protocol != "http" {
		t.Errorf("Expected protocol 'http', got %s", connection.Endpoint.Protocol)
	}

	if connection.Status != "connected" {
		t.Errorf("Expected status 'connected', got %s", connection.Status)
	}

	if connection.ErrorCount != 0 {
		t.Errorf("Expected error count 0, got %d", connection.ErrorCount)
	}
}

func TestMCPHTTPConnection(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc": "2.0", "result": {"protocolVersion": "1.0.0"}}`))
	}))
	defer server.Close()

	// Create HTTP connection
	httpConn := &MCPHTTPConnection{
		BaseURL:  server.URL,
		Client:   &http.Client{Timeout: 5 * time.Second},
		LastUsed: time.Now(),
	}

	// Test connection properties
	if httpConn.BaseURL != server.URL {
		t.Errorf("Expected base URL %s, got %s", server.URL, httpConn.BaseURL)
	}

	if httpConn.Client == nil {
		t.Error("Expected HTTP client to be set")
	}

	if httpConn.Client.Timeout != 5*time.Second {
		t.Errorf("Expected timeout 5s, got %v", httpConn.Client.Timeout)
	}

	// Test making a request
	resp, err := httpConn.Client.Get(httpConn.BaseURL + "/test")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestMCPSSEConnection(t *testing.T) {
	// Create a test server for SSE
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"type\": \"test\", \"data\": \"hello\"}\n\n"))
	}))
	defer server.Close()

	// Create SSE connection
	sseConn := &MCPSSEConnection{
		BaseURL:  server.URL,
		Client:   &http.Client{Timeout: 10 * time.Second},
		LastUsed: time.Now(),
	}

	// Test connection properties
	if sseConn.BaseURL != server.URL {
		t.Errorf("Expected base URL %s, got %s", server.URL, sseConn.BaseURL)
	}

	if sseConn.Client == nil {
		t.Error("Expected HTTP client to be set")
	}

	// Test making an SSE request
	resp, err := sseConn.Client.Get(sseConn.BaseURL + "/events")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Check SSE headers
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got %s", resp.Header.Get("Content-Type"))
	}

	if resp.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("Expected Cache-Control 'no-cache', got %s", resp.Header.Get("Cache-Control"))
	}
}

func TestMCPConnection_StatusManagement(t *testing.T) {
	connection := &MCPConnection{
		Endpoint: ServiceEndpoint{
			Name:      "test-service",
			Namespace: "default",
			Protocol:  "http",
			Host:      "test-service.default.svc.cluster.local",
			Port:      8080,
		},
		LastUsed:   time.Now(),
		Status:     "connected",
		ErrorCount: 0,
	}

	// Test status changes
	connection.Status = "disconnected"
	if connection.Status != "disconnected" {
		t.Errorf("Expected status 'disconnected', got %s", connection.Status)
	}

	// Test error count increment
	connection.ErrorCount++
	if connection.ErrorCount != 1 {
		t.Errorf("Expected error count 1, got %d", connection.ErrorCount)
	}

	// Test thread safety with mutex
	connection.mu.Lock()
	connection.LastUsed = time.Now()
	connection.mu.Unlock()

	connection.mu.RLock()
	lastUsed := connection.LastUsed
	connection.mu.RUnlock()

	if lastUsed.IsZero() {
		t.Error("Expected last used time to be set")
	}
}

func TestConnectionStatus(t *testing.T) {
	now := time.Now()
	status := ConnectionStatus{
		Connected:     true,
		LastConnected: now,
		ErrorCount:    0,
		LastError:     "",
	}

	// Test initial status
	if !status.Connected {
		t.Error("Expected connected to be true")
	}

	if status.ErrorCount != 0 {
		t.Errorf("Expected error count 0, got %d", status.ErrorCount)
	}

	if status.LastError != "" {
		t.Errorf("Expected no last error, got %s", status.LastError)
	}

	// Test status update
	status.Connected = false
	status.ErrorCount = 3
	status.LastError = "connection timeout"

	if status.Connected {
		t.Error("Expected connected to be false")
	}

	if status.ErrorCount != 3 {
		t.Errorf("Expected error count 3, got %d", status.ErrorCount)
	}

	if status.LastError != "connection timeout" {
		t.Errorf("Expected last error 'connection timeout', got %s", status.LastError)
	}
}

func TestDynamicConnectionManager_Basic(t *testing.T) {
	manager := NewDynamicConnectionManager()
	
	if manager == nil {
		t.Fatal("Expected non-nil connection manager")
	}

	// Test initial state
	connections := manager.GetConnections()
	if len(connections) != 0 {
		t.Errorf("Expected 0 connections initially, got %d", len(connections))
	}

	// Test adding a connection
	endpoint := ServiceEndpoint{
		Name:      "test-service",
		Namespace: "default",
		Protocol:  "http",
		Host:      "test-service.default.svc.cluster.local",
		Port:      8080,
		Path:      "/mcp",
	}

	err := manager.AddConnection(endpoint)
	if err != nil {
		t.Errorf("Expected no error adding connection, got %v", err)
	}

	// Test getting connections
	connections = manager.GetConnections()
	if len(connections) != 1 {
		t.Errorf("Expected 1 connection, got %d", len(connections))
	}

	// Test getting specific connection
	conn, err := manager.GetConnection("test-service")
	if err != nil {
		t.Errorf("Expected no error getting connection, got %v", err)
	}

	if conn.Endpoint.Name != "test-service" {
		t.Errorf("Expected connection name 'test-service', got %s", conn.Endpoint.Name)
	}

	// Test removing connection
	err = manager.RemoveConnection("test-service")
	if err != nil {
		t.Errorf("Expected no error removing connection, got %v", err)
	}

	connections = manager.GetConnections()
	if len(connections) != 0 {
		t.Errorf("Expected 0 connections after removal, got %d", len(connections))
	}
}

func TestDynamicConnectionManager_MultipleConnections(t *testing.T) {
	manager := NewDynamicConnectionManager()

	// Add multiple connections
	endpoints := []ServiceEndpoint{
		{
			Name:      "http-service",
			Namespace: "default",
			Protocol:  "http",
			Host:      "http-service.default.svc.cluster.local",
			Port:      8080,
			Path:      "/mcp",
		},
		{
			Name:      "sse-service",
			Namespace: "default",
			Protocol:  "sse",
			Host:      "sse-service.default.svc.cluster.local",
			Port:      8080,
			Path:      "/events",
		},
		{
			Name:      "stdio-service",
			Namespace: "default",
			Protocol:  "stdio",
			Host:      "stdio-service.default.svc.cluster.local",
			Port:      8080,
		},
	}

	for _, endpoint := range endpoints {
		err := manager.AddConnection(endpoint)
		if err != nil {
			t.Errorf("Expected no error adding connection %s, got %v", endpoint.Name, err)
		}
	}

	// Test getting all connections
	connections := manager.GetConnections()
	if len(connections) != 3 {
		t.Errorf("Expected 3 connections, got %d", len(connections))
	}

	// Test getting specific connections
	for _, endpoint := range endpoints {
		conn, err := manager.GetConnection(endpoint.Name)
		if err != nil {
			t.Errorf("Expected no error getting connection %s, got %v", endpoint.Name, err)
		}

		if conn.Endpoint.Protocol != endpoint.Protocol {
			t.Errorf("Expected protocol %s for %s, got %s", endpoint.Protocol, endpoint.Name, conn.Endpoint.Protocol)
		}

		if conn.Endpoint.Host != endpoint.Host {
			t.Errorf("Expected host %s for %s, got %s", endpoint.Host, endpoint.Name, conn.Endpoint.Host)
		}

		if conn.Endpoint.Port != endpoint.Port {
			t.Errorf("Expected port %d for %s, got %d", endpoint.Port, endpoint.Name, conn.Endpoint.Port)
		}
	}

	// Test connection filtering by protocol
	httpConnections := manager.GetConnectionsByProtocol("http")
	if len(httpConnections) != 1 {
		t.Errorf("Expected 1 HTTP connection, got %d", len(httpConnections))
	}

	sseConnections := manager.GetConnectionsByProtocol("sse")
	if len(sseConnections) != 1 {
		t.Errorf("Expected 1 SSE connection, got %d", len(sseConnections))
	}

	stdioConnections := manager.GetConnectionsByProtocol("stdio")
	if len(stdioConnections) != 1 {
		t.Errorf("Expected 1 STDIO connection, got %d", len(stdioConnections))
	}
}

func TestDynamicConnectionManager_ErrorHandling(t *testing.T) {
	manager := NewDynamicConnectionManager()

	// Test getting non-existent connection
	_, err := manager.GetConnection("non-existent")
	if err == nil {
		t.Error("Expected error getting non-existent connection")
	}

	// Test removing non-existent connection
	err = manager.RemoveConnection("non-existent")
	if err == nil {
		t.Error("Expected error removing non-existent connection")
	}

	// Test adding duplicate connection
	endpoint := ServiceEndpoint{
		Name:      "test-service",
		Namespace: "default",
		Protocol:  "http",
		Host:      "test-service.default.svc.cluster.local",
		Port:      8080,
	}

	err = manager.AddConnection(endpoint)
	if err != nil {
		t.Errorf("Expected no error adding connection, got %v", err)
	}

	err = manager.AddConnection(endpoint)
	if err == nil {
		t.Error("Expected error adding duplicate connection")
	}
}

func TestDynamicConnectionManager_ConcurrentAccess(t *testing.T) {
	manager := NewDynamicConnectionManager()

	// Test concurrent access
	done := make(chan bool, 10)

	// Concurrent connection additions
	for i := 0; i < 5; i++ {
		go func(i int) {
			endpoint := ServiceEndpoint{
				Name:      fmt.Sprintf("service-%d", i),
				Namespace: "default",
				Protocol:  "http",
				Host:      fmt.Sprintf("service-%d.default.svc.cluster.local", i),
				Port:      8080,
			}
			err := manager.AddConnection(endpoint)
			if err != nil {
				t.Errorf("Expected no error adding connection %d, got %v", i, err)
			}
			done <- true
		}(i)
	}

	// Concurrent connection retrievals
	for i := 0; i < 5; i++ {
		go func(i int) {
			// Wait a bit to ensure connections are added
			time.Sleep(10 * time.Millisecond)
			connections := manager.GetConnections()
			if len(connections) == 0 {
				t.Error("Expected connections to be available")
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify final state
	connections := manager.GetConnections()
	if len(connections) != 5 {
		t.Errorf("Expected 5 connections, got %d", len(connections))
	}
}

func TestDynamicConnectionManager_UpdateConnection(t *testing.T) {
	manager := NewDynamicConnectionManager()

	// Add initial connection
	endpoint := ServiceEndpoint{
		Name:      "test-service",
		Namespace: "default",
		Protocol:  "http",
		Host:      "test-service.default.svc.cluster.local",
		Port:      8080,
		Path:      "/mcp",
	}

	err := manager.AddConnection(endpoint)
	if err != nil {
		t.Errorf("Expected no error adding connection, got %v", err)
	}

	// Update connection
	updatedEndpoint := ServiceEndpoint{
		Name:      "test-service",
		Namespace: "default",
		Protocol:  "http",
		Host:      "test-service.default.svc.cluster.local",
		Port:      9090, // Changed port
		Path:      "/api/mcp", // Changed path
	}

	err = manager.UpdateConnection(updatedEndpoint)
	if err != nil {
		t.Errorf("Expected no error updating connection, got %v", err)
	}

	// Verify update
	conn, err := manager.GetConnection("test-service")
	if err != nil {
		t.Errorf("Expected no error getting updated connection, got %v", err)
	}

	if conn.Endpoint.Port != 9090 {
		t.Errorf("Expected updated port 9090, got %d", conn.Endpoint.Port)
	}

	if conn.Endpoint.Path != "/api/mcp" {
		t.Errorf("Expected updated path '/api/mcp', got %s", conn.Endpoint.Path)
	}
}

func TestDynamicConnectionManager_HealthCheck(t *testing.T) {
	manager := NewDynamicConnectionManager()

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "healthy"}`))
	}))
	defer server.Close()

	// Add connection with real server
	endpoint := ServiceEndpoint{
		Name:      "test-service",
		Namespace: "default",
		Protocol:  "http",
		Host:      server.URL[7:], // Remove "http://" prefix
		Port:      80,
		Path:      "/health",
	}

	err := manager.AddConnection(endpoint)
	if err != nil {
		t.Errorf("Expected no error adding connection, got %v", err)
	}

	// Test health check
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	healthy, err := manager.CheckHealth(ctx, "test-service")
	if err != nil {
		t.Errorf("Expected no error checking health, got %v", err)
	}

	if !healthy {
		t.Error("Expected service to be healthy")
	}

	// Test health check for non-existent service
	healthy, err = manager.CheckHealth(ctx, "non-existent")
	if err == nil {
		t.Error("Expected error checking health for non-existent service")
	}

	if healthy {
		t.Error("Expected non-existent service to be unhealthy")
	}
}

// Helper function to create a test service endpoint
func createTestEndpoint(name, protocol string, port int32) ServiceEndpoint {
	return ServiceEndpoint{
		Name:      name,
		Namespace: "default",
		Protocol:  protocol,
		Host:      fmt.Sprintf("%s.default.svc.cluster.local", name),
		Port:      port,
		Path:      "/mcp",
	}
}

// Helper function to create a test HTTP server
func createTestServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

// Helper function to create a test connection manager with sample data
func createTestConnectionManager() *DynamicConnectionManager {
	manager := NewDynamicConnectionManager()
	
	endpoints := []ServiceEndpoint{
		createTestEndpoint("http-service", "http", 8080),
		createTestEndpoint("sse-service", "sse", 8080),
		createTestEndpoint("stdio-service", "stdio", 8080),
	}

	for _, endpoint := range endpoints {
		manager.AddConnection(endpoint)
	}

	return manager
}