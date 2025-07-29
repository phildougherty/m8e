// internal/server/unified_proxy_test.go
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/discovery"
	"github.com/phildougherty/m8e/internal/logging"
)

// MockServiceDiscovery implements discovery interface for testing
type MockServiceDiscovery struct {
	services []discovery.ServiceEndpoint
	handlers []discovery.ServiceDiscoveryHandler
}

func NewMockServiceDiscovery() *MockServiceDiscovery {
	return &MockServiceDiscovery{
		services: make([]discovery.ServiceEndpoint, 0),
		handlers: make([]discovery.ServiceDiscoveryHandler, 0),
	}
}

func (m *MockServiceDiscovery) Start() error {
	return nil
}

func (m *MockServiceDiscovery) Stop() {
	// No-op for mock
}

func (m *MockServiceDiscovery) DiscoverMCPServers() ([]discovery.ServiceEndpoint, error) {
	return m.services, nil
}

func (m *MockServiceDiscovery) GetServices() []discovery.ServiceEndpoint {
	return m.services
}

func (m *MockServiceDiscovery) AddHandler(handler discovery.ServiceDiscoveryHandler) {
	m.handlers = append(m.handlers, handler)
}

func (m *MockServiceDiscovery) AddService(endpoint discovery.ServiceEndpoint) {
	m.services = append(m.services, endpoint)
	// Notify handlers
	for _, handler := range m.handlers {
		handler.OnServiceAdded(endpoint)
	}
}

func (m *MockServiceDiscovery) RemoveService(name, namespace string) {
	for i, service := range m.services {
		if service.Name == name {
			m.services = append(m.services[:i], m.services[i+1:]...)
			break
		}
	}
	// Notify handlers
	for _, handler := range m.handlers {
		handler.OnServiceDeleted(name, namespace)
	}
}

// MockMCPServer simulates an MCP server for testing
type MockMCPServer struct {
	server      *httptest.Server
	protocol    string
	initialized bool
	sessionID   string
	tools       []Tool
}

func NewMockMCPServer(protocol string) *MockMCPServer {
	mock := &MockMCPServer{
		protocol:    protocol,
		initialized: false,
		sessionID:   "test-session-123",
		tools: []Tool{
			{
				Name:        "test_tool",
				Description: "A test tool",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"input": map[string]interface{}{
							"type":        "string",
							"description": "Input parameter",
						},
					},
				},
			},
		},
	}

	switch protocol {
	case "http":
		mock.server = httptest.NewServer(http.HandlerFunc(mock.handleHTTP))
	case "http-stream":
		mock.server = httptest.NewServer(http.HandlerFunc(mock.handleStreamableHTTP))
	case "sse":
		mock.server = httptest.NewServer(http.HandlerFunc(mock.handleSSE))
	}

	return mock
}

func (m *MockMCPServer) Close() {
	if m.server != nil {
		m.server.Close()
	}
}

func (m *MockMCPServer) URL() string {
	if m.server != nil {
		return m.server.URL
	}
	return ""
}

func (m *MockMCPServer) handleHTTP(w http.ResponseWriter, r *http.Request) {
	var request map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	method, _ := request["method"].(string)
	id := request["id"]

	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
	}

	switch method {
	case "initialize":
		response["result"] = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": true,
				},
			},
			"serverInfo": map[string]interface{}{
				"name":    "mock-server",
				"version": "1.0.0",
			},
		}
		m.initialized = true
		w.Header().Set("Mcp-Session-Id", m.sessionID)

	case "tools/list":
		if !m.initialized {
			response["error"] = map[string]interface{}{
				"code":    -32002,
				"message": "Server not initialized",
			}
		} else {
			response["result"] = map[string]interface{}{
				"tools": m.tools,
			}
		}

	case "tools/call":
		if !m.initialized {
			response["error"] = map[string]interface{}{
				"code":    -32002,
				"message": "Server not initialized",
			}
		} else {
			params, _ := request["params"].(map[string]interface{})
			toolName, _ := params["name"].(string)
			arguments, _ := params["arguments"].(map[string]interface{})

			response["result"] = map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": "Tool " + toolName + " executed with arguments: " + jsonToString(arguments),
					},
				},
			}
		}

	case "ping":
		response["result"] = map[string]interface{}{
			"status": "pong",
		}

	default:
		response["error"] = map[string]interface{}{
			"code":    -32601,
			"message": "Method not found",
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (m *MockMCPServer) handleStreamableHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if streaming is requested
	streaming := r.Header.Get("X-Streaming") == "true"

	if streaming {
		w.Header().Set("Transfer-Encoding", "chunked")
		w.Header().Set("X-Streaming", "true")
		w.Header().Set("Cache-Control", "no-cache")
	}

	// Handle the request similar to HTTP but with streaming response if requested
	var request map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	method, _ := request["method"].(string)
	id := request["id"]

	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
	}

	switch method {
	case "initialize":
		response["result"] = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": true,
				},
			},
			"serverInfo": map[string]interface{}{
				"name":    "mock-streamable-server",
				"version": "1.0.0",
			},
		}
		m.initialized = true
		w.Header().Set("Mcp-Session-Id", m.sessionID)

	case "tools/call":
		if !m.initialized {
			response["error"] = map[string]interface{}{
				"code":    -32002,
				"message": "Server not initialized",
			}
		} else {
			params, _ := request["params"].(map[string]interface{})
			toolName, _ := params["name"].(string)

			if streaming {
				// Simulate streaming response for long-running tools
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)

				// Send chunks
				chunks := []map[string]interface{}{
					{
						"jsonrpc": "2.0",
						"id":      id,
						"result": map[string]interface{}{
							"content": []map[string]interface{}{
								{
									"type": "text",
									"text": "Starting tool " + toolName + "...",
								},
							},
						},
					},
					{
						"jsonrpc": "2.0",
						"id":      id,
						"result": map[string]interface{}{
							"content": []map[string]interface{}{
								{
									"type": "text",
									"text": "Tool " + toolName + " completed successfully",
								},
							},
						},
					},
				}

				for _, chunk := range chunks {
					chunkData, _ := json.Marshal(chunk)
					w.Write(chunkData)
					w.Write([]byte("\n"))
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
					time.Sleep(100 * time.Millisecond) // Simulate processing time
				}
				return
			} else {
				response["result"] = map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": "Tool " + toolName + " executed (non-streaming)",
						},
					},
				}
			}
		}

	default:
		// Handle other methods like regular HTTP
		m.handleHTTP(w, &http.Request{
			Body: io.NopCloser(strings.NewReader(jsonToString(request))),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (m *MockMCPServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/sse" && r.Method == "GET" {
		// SSE handshake - provide session endpoint
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: endpoint\n"))
		w.Write([]byte("data: /messages/test-session-123\n\n"))
		return
	}

	if strings.HasPrefix(r.URL.Path, "/messages/") {
		// Handle MCP messages
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		method, _ := request["method"].(string)
		id := request["id"]

		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
		}

		switch method {
		case "initialize":
			response["result"] = map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{
						"listChanged": true,
					},
				},
				"serverInfo": map[string]interface{}{
					"name":    "mock-sse-server",
					"version": "1.0.0",
				},
			}
			m.initialized = true

		case "tools/list":
			if !m.initialized {
				response["error"] = map[string]interface{}{
					"code":    -32002,
					"message": "Server not initialized",
				}
			} else {
				response["result"] = map[string]interface{}{
					"tools": m.tools,
				}
			}

		case "ping":
			response["result"] = map[string]interface{}{
				"status": "pong",
			}

		default:
			response["error"] = map[string]interface{}{
				"code":    -32601,
				"message": "Method not found",
			}
		}

		// Send as SSE event
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: response\n"))
		responseData, _ := json.Marshal(response)
		w.Write([]byte("data: " + string(responseData) + "\n\n"))
		return
	}

	http.NotFound(w, r)
}

func jsonToString(data interface{}) string {
	bytes, _ := json.Marshal(data)
	return string(bytes)
}

// Test suite for unified proxy
func TestUnifiedProxyHTTP(t *testing.T) {
	// Create mock MCP server
	mockServer := NewMockMCPServer("http")
	defer mockServer.Close()

	// Create mock service discovery
	mockDiscovery := NewMockServiceDiscovery()

	// Create unified proxy
	cfg := &config.ComposeConfig{
		Logging: config.LoggingConfig{Level: "debug"},
	}

	proxy, err := NewProxyHandler(cfg, "default", "test-api-key")
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Replace service discovery with mock
	proxy.ServiceDiscovery = mockDiscovery
	proxy.ConnectionManager = discovery.NewDynamicConnectionManager(mockDiscovery, proxy.Logger)

	// Start the proxy
	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	// Add mock service
	mockDiscovery.AddService(discovery.ServiceEndpoint{
		Name:         "test-server",
		URL:          mockServer.URL(),
		Protocol:     "http",
		Port:         8080,
		Capabilities: []string{"tools"},
	})

	// Wait for connection to be established
	time.Sleep(100 * time.Millisecond)

	// Test 1: Basic HTTP request routing
	t.Run("HTTP Request Routing", func(t *testing.T) {
		request := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "test-1",
			"method":  "initialize",
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{},
			},
		}

		reqBody, _ := json.Marshal(request)
		req := httptest.NewRequest("POST", "/test-server", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-api-key")

		w := httptest.NewRecorder()
		proxy.HandleMCPRequest(w, req, "test-server")

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if response["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
		}

		if response["id"] != "test-1" {
			t.Errorf("Expected id test-1, got %v", response["id"])
		}
	})

	// Test 2: Tool discovery
	t.Run("Tool Discovery", func(t *testing.T) {
		tools, err := proxy.DiscoverServerTools("test-server")
		if err != nil {
			t.Fatalf("Failed to discover tools: %v", err)
		}

		if len(tools) == 0 {
			t.Error("Expected at least one tool, got none")
		}

		if tools[0].Name != "test_tool" {
			t.Errorf("Expected tool name 'test_tool', got %s", tools[0].Name)
		}
	})

	// Test 3: Direct tool calls
	t.Run("Direct Tool Call", func(t *testing.T) {
		arguments := map[string]interface{}{
			"input": "test input",
		}

		reqBody, _ := json.Marshal(arguments)
		req := httptest.NewRequest("POST", "/tools/test_tool", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-api-key")

		w := httptest.NewRecorder()
		proxy.HandleDirectToolCall(w, req, "test_tool")

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})
}

func TestUnifiedProxyStreamableHTTP(t *testing.T) {
	// Create mock streamable MCP server
	mockServer := NewMockMCPServer("http-stream")
	defer mockServer.Close()

	// Create mock service discovery
	mockDiscovery := NewMockServiceDiscovery()

	// Create unified proxy
	cfg := &config.ComposeConfig{
		Logging: config.LoggingConfig{Level: "debug"},
	}

	proxy, err := NewProxyHandler(cfg, "default", "test-api-key")
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Replace service discovery with mock
	proxy.ServiceDiscovery = mockDiscovery
	proxy.ConnectionManager = discovery.NewDynamicConnectionManager(mockDiscovery, proxy.Logger)

	// Start the proxy
	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	// Add mock service
	mockDiscovery.AddService(discovery.ServiceEndpoint{
		Name:         "streamable-server",
		URL:          mockServer.URL(),
		Protocol:     "http-stream",
		Port:         8080,
		Capabilities: []string{"tools"},
	})

	// Wait for connection to be established
	time.Sleep(100 * time.Millisecond)

	// Test 1: Streamable HTTP request with streaming
	t.Run("Streamable HTTP Streaming Request", func(t *testing.T) {
		request := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "stream-test-1",
			"method":  "tools/call",
			"params": map[string]interface{}{
				"name":      "test_tool",
				"arguments": map[string]interface{}{"input": "test"},
			},
		}

		reqBody, _ := json.Marshal(request)
		req := httptest.NewRequest("POST", "/streamable-server", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-api-key")
		req.Header.Set("X-Streaming", "true")

		w := httptest.NewRecorder()
		proxy.HandleMCPRequest(w, req, "streamable-server")

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		// Check for streaming headers
		if w.Header().Get("X-Streaming") != "true" {
			t.Error("Expected X-Streaming header to be true")
		}
	})

	// Test 2: Regular request to streamable server
	t.Run("Streamable HTTP Regular Request", func(t *testing.T) {
		request := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "regular-test-1",
			"method":  "initialize",
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{},
			},
		}

		reqBody, _ := json.Marshal(request)
		req := httptest.NewRequest("POST", "/streamable-server", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-api-key")

		w := httptest.NewRecorder()
		proxy.HandleMCPRequest(w, req, "streamable-server")

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if response["jsonrpc"] != "2.0" {
			t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
		}
	})
}

func TestUnifiedProxySSE(t *testing.T) {
	// Create mock SSE MCP server
	mockServer := NewMockMCPServer("sse")
	defer mockServer.Close()

	// Create mock service discovery
	mockDiscovery := NewMockServiceDiscovery()

	// Create unified proxy
	cfg := &config.ComposeConfig{
		Logging: config.LoggingConfig{Level: "debug"},
	}

	proxy, err := NewProxyHandler(cfg, "default", "test-api-key")
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Replace service discovery with mock
	proxy.ServiceDiscovery = mockDiscovery
	proxy.ConnectionManager = discovery.NewDynamicConnectionManager(mockDiscovery, proxy.Logger)

	// Start the proxy
	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	// Add mock service
	mockDiscovery.AddService(discovery.ServiceEndpoint{
		Name:         "sse-server",
		URL:          mockServer.URL(),
		Protocol:     "sse",
		Port:         8080,
		Capabilities: []string{"tools"},
	})

	// Wait for connection to be established
	time.Sleep(100 * time.Millisecond)

	// Test 1: SSE request
	t.Run("SSE Request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/sse-server", nil)
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Authorization", "Bearer test-api-key")

		w := httptest.NewRecorder()
		proxy.HandleMCPRequest(w, req, "sse-server")

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		// Check for SSE headers
		if w.Header().Get("Content-Type") != "text/event-stream" {
			t.Error("Expected Content-Type to be text/event-stream")
		}
	})
}

func TestConnectionHealthChecks(t *testing.T) {
	// Create mock servers
	httpServer := NewMockMCPServer("http")
	defer httpServer.Close()

	streamableServer := NewMockMCPServer("http-stream")
	defer streamableServer.Close()

	sseServer := NewMockMCPServer("sse")
	defer sseServer.Close()

	// Create mock service discovery
	mockDiscovery := NewMockServiceDiscovery()

	// Create connection manager
	logger := logging.NewLogger("debug")
	connectionManager := discovery.NewDynamicConnectionManager(mockDiscovery, logger)

	// Start connection manager
	if err := connectionManager.Start(); err != nil {
		t.Fatalf("Failed to start connection manager: %v", err)
	}
	defer connectionManager.Stop()

	// Add mock services
	mockDiscovery.AddService(discovery.ServiceEndpoint{
		Name:         "http-server",
		URL:          httpServer.URL(),
		Protocol:     "http",
		Port:         8080,
		Capabilities: []string{"tools"},
	})

	mockDiscovery.AddService(discovery.ServiceEndpoint{
		Name:         "streamable-server",
		URL:          streamableServer.URL(),
		Protocol:     "http-stream",
		Port:         8081,
		Capabilities: []string{"tools"},
	})

	mockDiscovery.AddService(discovery.ServiceEndpoint{
		Name:         "sse-server",
		URL:          sseServer.URL(),
		Protocol:     "sse",
		Port:         8082,
		Capabilities: []string{"tools"},
	})

	// Wait for connections to be established
	time.Sleep(500 * time.Millisecond)

	// Test health check status
	t.Run("Health Check Status", func(t *testing.T) {
		statuses := connectionManager.GetConnectionStatus()

		for serverName, status := range statuses {
			t.Logf("Server %s: Connected=%v, ErrorCount=%d", serverName, status.Connected, status.ErrorCount)

			if !status.Connected {
				t.Errorf("Expected server %s to be connected", serverName)
			}

			if status.ErrorCount > 0 {
				t.Errorf("Expected server %s to have no errors, got %d", serverName, status.ErrorCount)
			}
		}
	})

	// Test individual connection retrieval
	t.Run("Individual Connection Retrieval", func(t *testing.T) {
		httpConn, err := connectionManager.GetConnection("http-server")
		if err != nil {
			t.Errorf("Failed to get HTTP connection: %v", err)
		} else if httpConn.Protocol != "http" {
			t.Errorf("Expected HTTP protocol, got %s", httpConn.Protocol)
		}

		streamableConn, err := connectionManager.GetConnection("streamable-server")
		if err != nil {
			t.Errorf("Failed to get streamable connection: %v", err)
		} else if streamableConn.Protocol != "http-stream" {
			t.Errorf("Expected http-stream protocol, got %s", streamableConn.Protocol)
		}

		sseConn, err := connectionManager.GetConnection("sse-server")
		if err != nil {
			t.Errorf("Failed to get SSE connection: %v", err)
		} else if sseConn.Protocol != "sse" {
			t.Errorf("Expected sse protocol, got %s", sseConn.Protocol)
		}
	})
}

func TestProxyInfo(t *testing.T) {
	// Create unified proxy
	cfg := &config.ComposeConfig{
		Logging: config.LoggingConfig{Level: "info"},
	}

	proxy, err := NewProxyHandler(cfg, "default", "test-api-key")
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Test proxy info
	t.Run("Proxy Info", func(t *testing.T) {
		info := proxy.GetProxyInfo()

		if info["type"] != "kubernetes-native" {
			t.Errorf("Expected type 'kubernetes-native', got %v", info["type"])
		}

		if info["api_enabled"] != true {
			t.Errorf("Expected api_enabled to be true, got %v", info["api_enabled"])
		}

		if info["discovered_servers"] == nil {
			t.Error("Expected discovered_servers field")
		}

		if info["active_connections"] == nil {
			t.Error("Expected active_connections field")
		}
	})
}

func TestErrorHandling(t *testing.T) {
	// Create unified proxy
	cfg := &config.ComposeConfig{
		Logging: config.LoggingConfig{Level: "debug"},
	}

	proxy, err := NewProxyHandler(cfg, "default", "test-api-key")
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Test 1: Request to non-existent server
	t.Run("Non-existent Server", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/non-existent-server", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-api-key")

		w := httptest.NewRecorder()
		proxy.HandleMCPRequest(w, req, "non-existent-server")

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected status 503, got %d", w.Code)
		}
	})

	// Test 2: Invalid JSON request
	t.Run("Invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/test-server", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-api-key")

		w := httptest.NewRecorder()
		proxy.HandleDirectToolCall(w, req, "test_tool")

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	// Test 3: Unauthorized request
	t.Run("Unauthorized Request", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/test-server", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		// No authorization header

		w := httptest.NewRecorder()
		proxy.HandleMCPRequest(w, req, "test-server")

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})
}

// Benchmark tests
func BenchmarkHTTPRequest(b *testing.B) {
	mockServer := NewMockMCPServer("http")
	defer mockServer.Close()

	mockDiscovery := NewMockServiceDiscovery()
	cfg := &config.ComposeConfig{
		Logging: config.LoggingConfig{Level: "error"}, // Reduce logging for benchmarks
	}

	proxy, _ := NewProxyHandler(cfg, "default", "test-api-key")
	proxy.ServiceDiscovery = mockDiscovery
	proxy.ConnectionManager = discovery.NewDynamicConnectionManager(mockDiscovery, proxy.Logger)
	proxy.Start()
	defer proxy.Stop()

	mockDiscovery.AddService(discovery.ServiceEndpoint{
		Name:         "bench-server",
		URL:          mockServer.URL(),
		Protocol:     "http",
		Port:         8080,
		Capabilities: []string{"tools"},
	})

	time.Sleep(100 * time.Millisecond)

	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "bench-1",
		"method":  "ping",
	}

	reqBody, _ := json.Marshal(request)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("POST", "/bench-server", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer test-api-key")

			w := httptest.NewRecorder()
			proxy.HandleMCPRequest(w, req, "bench-server")

			if w.Code != http.StatusOK {
				b.Errorf("Expected status 200, got %d", w.Code)
			}
		}
	})
}