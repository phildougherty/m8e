package discovery

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/logging"
)

func TestMCPConnection_Basic(t *testing.T) {
	endpoint := ServiceEndpoint{
		Name:      "test-service",
		Namespace: "matey",
		Protocol:  "http",
		URL:       "http://test-service.matey.svc.cluster.local:8080/mcp",
		Port:      8080,
	}

	connection := &MCPConnection{
		Name:      endpoint.Name,
		Endpoint:  endpoint.URL,
		Protocol:  endpoint.Protocol,
		Port:      endpoint.Port,
		LastUsed:  time.Now(),
		Status:    "connected",
		ErrorCount: 0,
	}

	// Test basic properties
	if connection.Name != "test-service" {
		t.Errorf("Expected service name 'test-service', got %s", connection.Name)
	}

	if connection.Protocol != "http" {
		t.Errorf("Expected protocol 'http', got %s", connection.Protocol)
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
		Name:       "test-service",
		Endpoint:   "http://test-service.matey.svc.cluster.local:8080/mcp",
		Protocol:   "http",
		Port:       8080,
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
	logger := logging.NewLogger("info")
	serviceDiscovery, err := NewK8sServiceDiscovery("default", logger)
	if err != nil {
		t.Skip("Skipping test - K8s service discovery unavailable")
	}

	manager := NewDynamicConnectionManager(serviceDiscovery, logger)
	
	if manager == nil {
		t.Fatal("Expected non-nil connection manager")
	}

	// Test initial state
	connections := manager.GetAllConnections()
	if len(connections) != 0 {
		t.Errorf("Expected 0 connections initially, got %d", len(connections))
	}

	// Test getting status
	status := manager.GetConnectionStatus()
	if len(status) != 0 {
		t.Errorf("Expected 0 connection statuses initially, got %d", len(status))
	}
}

func TestDynamicConnectionManager_GetConnection(t *testing.T) {
	logger := logging.NewLogger("info")
	serviceDiscovery, err := NewK8sServiceDiscovery("default", logger)
	if err != nil {
		t.Skip("Skipping test - K8s service discovery unavailable")
	}

	manager := NewDynamicConnectionManager(serviceDiscovery, logger)

	// Test getting non-existent connection
	_, err = manager.GetConnection("non-existent")
	if err == nil {
		t.Error("Expected error getting non-existent connection")
	}
}

func TestDynamicConnectionManager_ServiceDiscoveryCallbacks(t *testing.T) {
	logger := logging.NewLogger("info")
	serviceDiscovery, err := NewK8sServiceDiscovery("default", logger)
	if err != nil {
		t.Skip("Skipping test - K8s service discovery unavailable")
	}

	manager := NewDynamicConnectionManager(serviceDiscovery, logger)

	// Test service discovery callbacks
	endpoint := ServiceEndpoint{
		Name:      "test-service",
		Namespace: "matey",
		Protocol:  "http",
		URL:       "http://test-service.matey.svc.cluster.local:8080/mcp",
		Port:      8080,
	}

	// Test OnServiceAdded callback
	manager.OnServiceAdded(endpoint)
	time.Sleep(50 * time.Millisecond) // Allow goroutines to start

	// Test OnServiceModified callback
	manager.OnServiceModified(endpoint)
	time.Sleep(50 * time.Millisecond) // Allow operations to complete

	// Test OnServiceDeleted callback
	manager.OnServiceDeleted("test-service", "default")
	time.Sleep(50 * time.Millisecond) // Allow cleanup to complete
}

func TestDynamicConnectionManager_StartStop(t *testing.T) {
	logger := logging.NewLogger("info")
	serviceDiscovery, err := NewK8sServiceDiscovery("default", logger)
	if err != nil {
		t.Skip("Skipping test - K8s service discovery unavailable")
	}

	manager := NewDynamicConnectionManager(serviceDiscovery, logger)

	// Test starting the manager
	err = manager.Start()
	if err != nil {
		t.Errorf("Expected no error starting manager, got %v", err)
	}

	// Give a small delay to allow initialization goroutines to start
	time.Sleep(50 * time.Millisecond)

	// Test stopping the manager
	manager.Stop()
}