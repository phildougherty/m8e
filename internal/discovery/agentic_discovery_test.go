package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolDiscoveryForAgenticBehavior tests that critical agentic tools are discovered
func TestToolDiscoveryForAgenticBehavior(t *testing.T) {
	// Critical tools required for agentic workflow creation
	criticalTools := []struct {
		toolName    string
		serverName  string
		description string
		required    bool
	}{
		{
			toolName:    "create_workflow",
			serverName:  "matey",
			description: "Create new workflows - critical for agentic behavior",
			required:    true,
		},
		{
			toolName:    "matey_up",
			serverName:  "matey", 
			description: "Deploy services - required for completion",
			required:    true,
		},
		{
			toolName:    "matey_ps",
			serverName:  "matey",
			description: "Verify deployment - required for validation",
			required:    true,
		},
		{
			toolName:    "get_current_glucose",
			serverName:  "dexcom",
			description: "Data gathering tool - example domain tool",
			required:    false,
		},
	}

	// Setup mock servers
	mateyServer := setupMockMateyServer(t)
	defer mateyServer.Close()

	dexcomServer := setupMockDexcomServer(t)
	defer dexcomServer.Close()

	// Create connection manager
	manager := &ConnectionManager{
		connections: make(map[string]*Connection),
		logger:      nil, // Use nil logger for testing
	}

	// Add connections
	mateyConn := &Connection{
		ServerName: "matey",
		URL:        mateyServer.URL,
		Protocol:   "http",
		Status:     "healthy",
		client:     &http.Client{Timeout: 5 * time.Second},
	}
	
	dexcomConn := &Connection{
		ServerName: "dexcom", 
		URL:        dexcomServer.URL,
		Protocol:   "http",
		Status:     "healthy",
		client:     &http.Client{Timeout: 5 * time.Second},
	}

	manager.connections["matey"] = mateyConn
	manager.connections["dexcom"] = dexcomConn

	// Test tool discovery
	ctx := context.Background()
	
	for _, tool := range criticalTools {
		t.Run(tool.toolName, func(t *testing.T) {
			// Get tools from the expected server
			conn, exists := manager.connections[tool.serverName]
			require.True(t, exists, "Server %s should exist", tool.serverName)

			tools, err := getServerTools(ctx, conn)
			require.NoError(t, err, "Should discover tools from %s", tool.serverName)

			// Check if the tool exists
			var foundTool *protocol.MCPTool
			for _, discoveredTool := range tools {
				if discoveredTool.Name == tool.toolName {
					foundTool = &discoveredTool
					break
				}
			}

			if tool.required {
				assert.NotNil(t, foundTool, "Critical tool %s must be available on %s server", tool.toolName, tool.serverName)
				if foundTool != nil {
					assert.NotEmpty(t, foundTool.Description, "Tool should have description")
					assert.NotNil(t, foundTool.InputSchema, "Tool should have input schema")
				}
			}

			if foundTool != nil {
				t.Logf("✓ Found tool: %s on %s - %s", tool.toolName, tool.serverName, foundTool.Description)
			}
		})
	}
}

// TestToolRoutingForAgenticFlow tests that tools are routed to correct servers
func TestToolRoutingForAgenticFlow(t *testing.T) {
	mateyServer := setupMockMateyServer(t)
	defer mateyServer.Close()

	dexcomServer := setupMockDexcomServer(t)
	defer dexcomServer.Close()

	manager := &ConnectionManager{
		connections: make(map[string]*Connection),
	}

	// Add connections
	manager.connections["matey"] = &Connection{
		ServerName: "matey",
		URL:        mateyServer.URL,
		Protocol:   "http",
		Status:     "healthy",
		client:     &http.Client{Timeout: 5 * time.Second},
	}
	
	manager.connections["dexcom"] = &Connection{
		ServerName: "dexcom",
		URL:        dexcomServer.URL,
		Protocol:   "http", 
		Status:     "healthy",
		client:     &http.Client{Timeout: 5 * time.Second},
	}

	// Test typical agentic workflow tool sequence
	agenticSequence := []struct {
		step     string
		toolName string
		server   string
	}{
		{"1. Data Gathering", "get_current_glucose", "dexcom"},
		{"2. Workflow Creation", "create_workflow", "matey"},
		{"3. Service Deployment", "matey_up", "matey"},
		{"4. Status Verification", "matey_ps", "matey"},
	}

	ctx := context.Background()

	for _, step := range agenticSequence {
		t.Run(step.step, func(t *testing.T) {
			// Find which server has the tool
			var foundServer string
			var foundTool *protocol.MCPTool

			for serverName, conn := range manager.connections {
				tools, err := getServerTools(ctx, conn)
				if err != nil {
					continue
				}

				for _, tool := range tools {
					if tool.Name == step.toolName {
						foundServer = serverName
						foundTool = &tool
						break
					}
				}
				if foundServer != "" {
					break
				}
			}

			// Verify routing
			assert.Equal(t, step.server, foundServer, "Tool %s should be found on %s server", step.toolName, step.server)
			assert.NotNil(t, foundTool, "Tool %s should be discoverable", step.toolName)

			if foundTool != nil {
				// Test tool execution
				conn := manager.connections[foundServer]
				result, err := executeToolCall(ctx, conn, step.toolName, map[string]interface{}{})
				assert.NoError(t, err, "Tool %s should execute successfully", step.toolName)
				assert.NotNil(t, result, "Tool execution should return result")
			}
		})
	}
}

// TestConnectionHealthForAgenticBehavior tests that connection health checks work properly
func TestConnectionHealthForAgenticBehavior(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectedStatus string
	}{
		{
			name: "healthy server with proper MCP responses",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				
				if r.URL.Path == "/health" {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"status": "healthy",
						"version": "0.0.4",
					})
					return
				}

				// Handle MCP requests
				var req map[string]interface{}
				json.NewDecoder(r.Body).Decode(&req)
				
				method, _ := req["method"].(string)
				switch method {
				case "initialize":
					json.NewEncoder(w).Encode(map[string]interface{}{
						"result": map[string]interface{}{
							"protocolVersion": "2024-11-05",
							"capabilities": map[string]interface{}{
								"tools": map[string]interface{}{},
							},
							"serverInfo": map[string]interface{}{
								"name": "test-server",
								"version": "1.0.0",
							},
						},
					})
				case "tools/list":
					json.NewEncoder(w).Encode(map[string]interface{}{
						"result": map[string]interface{}{
							"tools": []map[string]interface{}{
								{
									"name": "test_tool",
									"description": "Test tool",
								},
							},
						},
					})
				default:
					http.Error(w, "Method not found", http.StatusNotFound)
				}
			},
			expectedStatus: "healthy",
		},
		{
			name: "server with connection issues",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			},
			expectedStatus: "unhealthy",
		},
		{
			name: "server with invalid MCP responses",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				// Return invalid JSON
				w.Write([]byte("{invalid json"))
			},
			expectedStatus: "unhealthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			conn := &Connection{
				ServerName: "test-server",
				URL:        server.URL,
				Protocol:   "http",
				Status:     "unknown",
				client:     &http.Client{Timeout: 5 * time.Second},
			}

			ctx := context.Background()
			
			// Test health check
			healthy := checkConnectionHealth(ctx, conn)
			
			if tt.expectedStatus == "healthy" {
				assert.True(t, healthy, "Server should be healthy")
				assert.Equal(t, "healthy", conn.Status)
			} else {
				assert.False(t, healthy, "Server should be unhealthy")
				assert.NotEqual(t, "healthy", conn.Status)
			}
		})
	}
}

// TestAgenticToolDiscoveryPerformance tests that tool discovery is fast enough for real-time use
func TestAgenticToolDiscoveryPerformance(t *testing.T) {
	// Setup multiple mock servers
	servers := make([]*httptest.Server, 5)
	for i := range servers {
		servers[i] = setupMockMateyServer(t)
		defer servers[i].Close()
	}

	manager := &ConnectionManager{
		connections: make(map[string]*Connection),
	}

	// Add multiple connections
	for i, server := range servers {
		name := fmt.Sprintf("server-%d", i)
		manager.connections[name] = &Connection{
			ServerName: name,
			URL:        server.URL,
			Protocol:   "http",
			Status:     "healthy",
			client:     &http.Client{Timeout: 5 * time.Second},
		}
	}

	ctx := context.Background()
	start := time.Now()

	// Discover tools from all servers
	allTools := make(map[string][]protocol.MCPTool)
	for serverName, conn := range manager.connections {
		tools, err := getServerTools(ctx, conn)
		assert.NoError(t, err, "Should discover tools from %s", serverName)
		allTools[serverName] = tools
	}

	duration := time.Since(start)

	// Performance assertions
	assert.Less(t, duration, 2*time.Second, "Tool discovery should complete within 2 seconds")
	assert.Len(t, allTools, 5, "Should discover tools from all servers")

	// Verify each server has the expected tools
	for serverName, tools := range allTools {
		assert.NotEmpty(t, tools, "Server %s should have tools", serverName)
		
		// Check for critical tools
		hasCreateWorkflow := false
		for _, tool := range tools {
			if tool.Name == "create_workflow" {
				hasCreateWorkflow = true
				break
			}
		}
		assert.True(t, hasCreateWorkflow, "Server %s should have create_workflow tool", serverName)
	}
}

// Helper functions

func setupMockMateyServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/health" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy",
				"server": "matey",
			})
			return
		}

		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		
		method, _ := req["method"].(string)
		switch method {
		case "initialize":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities": map[string]interface{}{
						"tools": map[string]interface{}{},
					},
					"serverInfo": map[string]interface{}{
						"name": "matey",
						"version": "0.0.4",
					},
				},
			})
		case "notifications/initialized":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"result": nil,
			})
		case "tools/list":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"tools": []map[string]interface{}{
						{
							"name":        "create_workflow",
							"description": "Create a new workflow configuration",
							"inputSchema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"name": map[string]interface{}{
										"type": "string",
										"description": "Workflow name",
									},
									"description": map[string]interface{}{
										"type": "string", 
										"description": "Workflow description",
									},
								},
								"required": []string{"name"},
							},
						},
						{
							"name":        "matey_up",
							"description": "Start MCP services and deploy configurations",
							"inputSchema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{},
							},
						},
						{
							"name":        "matey_ps",
							"description": "List running MCP server processes and their status",
							"inputSchema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{},
							},
						},
						{
							"name":        "matey_down",
							"description": "Stop MCP services",
							"inputSchema": map[string]interface{}{
								"type": "object",
							},
						},
						{
							"name":        "apply_config",
							"description": "Apply configuration changes to the cluster",
							"inputSchema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"config": map[string]interface{}{
										"type": "object",
										"description": "Configuration to apply",
									},
								},
							},
						},
						{
							"name":        "get_cluster_state",
							"description": "Get current cluster state and resource status",
							"inputSchema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{},
							},
						},
					},
				},
			})
		case "tools/call":
			params, _ := req["params"].(map[string]interface{})
			toolName, _ := params["name"].(string)
			
			json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": fmt.Sprintf("Tool %s executed successfully", toolName),
						},
					},
				},
			})
		default:
			http.Error(w, "Method not found", http.StatusNotFound)
		}
	}))
}

func setupMockDexcomServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/health" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy",
				"server": "dexcom",
			})
			return
		}

		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		
		method, _ := req["method"].(string)
		switch method {
		case "initialize":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities": map[string]interface{}{
						"tools": map[string]interface{}{},
					},
					"serverInfo": map[string]interface{}{
						"name": "dexcom",
						"version": "1.0.0",
					},
				},
			})
		case "tools/list":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"tools": []map[string]interface{}{
						{
							"name":        "get_current_glucose",
							"description": "Get current glucose reading from Dexcom G7",
							"inputSchema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{},
							},
						},
						{
							"name":        "get_glucose_history",
							"description": "Get historical glucose readings",
							"inputSchema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"hours": map[string]interface{}{
										"type": "number",
										"description": "Number of hours of history to retrieve",
										"default": 6,
									},
								},
							},
						},
					},
				},
			})
		case "tools/call":
			params, _ := req["params"].(map[string]interface{})
			toolName, _ := params["name"].(string)
			
			var result map[string]interface{}
			switch toolName {
			case "get_current_glucose":
				result = map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": "Current Glucose: 120 mg/dL (6.67 mmol/L) - Trend: steady",
						},
					},
				}
			case "get_glucose_history":
				result = map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": "Last 6h glucose readings: [120, 118, 122, 125, 119, 117] mg/dL",
						},
					},
				}
			default:
				result = map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": fmt.Sprintf("Tool %s executed", toolName),
						},
					},
				}
			}
			
			json.NewEncoder(w).Encode(map[string]interface{}{
				"result": result,
			})
		default:
			http.Error(w, "Method not found", http.StatusNotFound)
		}
	}))
}

func getServerTools(ctx context.Context, conn *Connection) ([]protocol.MCPTool, error) {
	// Send tools/list request
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	reqBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, "POST", conn.URL, bytes.NewReader(reqBytes))
	req.Header.Set("Content-Type", "application/json")

	resp, err := conn.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	result, ok := response["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid response format")
	}

	toolsList, ok := result["tools"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("tools list not found")
	}

	var tools []protocol.MCPTool
	for _, toolData := range toolsList {
		tool := toolData.(map[string]interface{})
		tools = append(tools, protocol.MCPTool{
			Name:        tool["name"].(string),
			Description: tool["description"].(string),
			InputSchema: tool["inputSchema"],
		})
	}

	return tools, nil
}

func executeToolCall(ctx context.Context, conn *Connection, toolName string, arguments map[string]interface{}) (map[string]interface{}, error) {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}

	reqBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, "POST", conn.URL, bytes.NewReader(reqBytes))
	req.Header.Set("Content-Type", "application/json")

	resp, err := conn.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	result, ok := response["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid response format")
	}

	return result, nil
}

func checkConnectionHealth(ctx context.Context, conn *Connection) bool {
	// Try health check endpoint first
	req, _ := http.NewRequestWithContext(ctx, "GET", conn.URL+"/health", nil)
	resp, err := conn.client.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		conn.Status = "healthy"
		return true
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Try MCP initialize
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}

	reqBytes, _ := json.Marshal(reqBody)
	req, _ = http.NewRequestWithContext(ctx, "POST", conn.URL, bytes.NewReader(reqBytes))
	req.Header.Set("Content-Type", "application/json")

	resp, err = conn.client.Do(req)
	if err != nil {
		conn.Status = "unhealthy"
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		conn.Status = "unhealthy"
		return false
	}

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		conn.Status = "unhealthy"
		return false
	}

	// Check for valid MCP response
	if result, ok := response["result"]; ok && result != nil {
		conn.Status = "healthy"
		return true
	}

	conn.Status = "unhealthy"
	return false
}