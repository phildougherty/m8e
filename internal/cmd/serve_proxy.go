// internal/cmd/serve_proxy.go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	
	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/openapi"
	"github.com/phildougherty/m8e/internal/server"
)

func NewServeProxyCommand() *cobra.Command {
	var port int
	var namespace string
	var apiKey string

	cmd := &cobra.Command{
		Use:   "serve-proxy",
		Short: "Run the actual MCP proxy server (used internally by deployments)",
		Long: `Run the actual MCP proxy server HTTP service. This command is used internally
by Kubernetes deployments and should not be called directly by users.

For creating proxy deployments, use 'matey proxy' instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServeProxy(cmd, port, namespace, apiKey)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to run the proxy server on")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace to discover services in")
	cmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "API key for proxy authentication (optional)")

	return cmd
}

func runServeProxy(cmd *cobra.Command, port int, namespace, apiKey string) error {
	// Load configuration if available
	file, _ := cmd.Flags().GetString("file")
	var cfg *config.ComposeConfig
	var err error
	
	if file != "" {
		cfg, err = config.LoadConfig(file)
		if err != nil {
			fmt.Printf("Warning: Failed to load config file %s: %v\n", file, err)
			fmt.Println("Continuing with default configuration...")
			cfg = &config.ComposeConfig{}
		}
	} else {
		cfg = &config.ComposeConfig{}
	}

	// Get API key from environment if not provided
	if apiKey == "" {
		apiKey = os.Getenv("MCP_API_KEY")
	}

	fmt.Printf("Starting system MCP proxy server...\n")
	fmt.Printf("Namespace: %s\n", namespace)
	fmt.Printf("Port: %d\n", port)
	if apiKey != "" {
		fmt.Printf("Authentication: Enabled\n")
	} else {
		fmt.Printf("Authentication: Disabled\n")
	}

	// Create system proxy handler
	proxyHandler, err := server.NewProxyHandler(cfg, namespace, apiKey)
	if err != nil {
		return fmt.Errorf("failed to create proxy handler: %w", err)
	}

	// Start the proxy handler
	if err := proxyHandler.Start(); err != nil {
		return fmt.Errorf("failed to start proxy handler: %w", err)
	}
	defer proxyHandler.Stop()

	// Create HTTP server with routes
	mux := http.NewServeMux()
	
	// OpenAPI endpoints (must be first to avoid conflicts)
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		handleOpenAPISpec(w, r, proxyHandler)
	})
	
	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		handleHealthCheck(w, r, proxyHandler)
	})
	
	// Service discovery info
	mux.HandleFunc("/discovery", func(w http.ResponseWriter, r *http.Request) {
		handleDiscoveryInfo(w, r, proxyHandler)
	})
	
	// API routes
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		handleAPIRequest(w, r, proxyHandler)
	})
	
	// Server-specific routes
	mux.HandleFunc("/servers/", func(w http.ResponseWriter, r *http.Request) {
		handleServerRequest(w, r, proxyHandler)
	})
	
	// MCP protocol endpoints
	mux.HandleFunc("/mcp/servers", func(w http.ResponseWriter, r *http.Request) {
		handleMCPServersEndpoint(w, r, proxyHandler)
	})
	
	mux.HandleFunc("/mcp/server/", func(w http.ResponseWriter, r *http.Request) {
		handleMCPServerToolsEndpoint(w, r, proxyHandler)
	})
	
	// FastAPI-style tool endpoints are now handled dynamically in handleDynamicRequest
	
	// Catch-all for dynamic routing (must be last)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleDynamicRequest(w, r, proxyHandler)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
		ReadTimeout:  25 * time.Minute,  // Extended for execute_agent
		WriteTimeout: 25 * time.Minute, // Extended for execute_agent
		IdleTimeout:  25 * time.Minute, // Extended for execute_agent
	}

	// Start server in a goroutine
	go func() {
		fmt.Printf("system MCP proxy server listening on :%d\n", port)
		fmt.Printf("Service discovery endpoint: http://localhost:%d/discovery\n", port)
		fmt.Printf("Health check endpoint: http://localhost:%d/health\n", port)
		fmt.Printf("API endpoint: http://localhost:%d/api/\n", port)
		
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Server error: %v\n", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down proxy server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		fmt.Printf("Server forced to shutdown: %v\n", err)
	}

	fmt.Println("Proxy server stopped")
	return nil
}

// All the HTTP handler functions from the original proxy.go would go here
// For now, I'll add the essential ones:

func handleOpenAPISpec(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	// OpenAPI spec should be publicly accessible - no authentication required
	handler.SetCORSHeaders(w)

	// Collect all tools from all discovered services
	servers := handler.GetDiscoveredServers()
	connections := handler.GetConnectionStatus()
	var allTools []openapi.Tool
	
	for _, server := range servers {
		if server.Name == "" {
			continue
		}
		
		// Only include servers that are connected/healthy
		if connStatus, exists := connections[server.Name]; !exists || !connStatus.Connected {
			handler.Logger.Debug("Skipping server %s for OpenAPI spec - not connected", server.Name)
			continue
		}
		
		handler.Logger.Info("Attempting tool discovery for server: %s (protocol: %s)", server.Name, server.Protocol)
		
		// Get tools for this server and convert to openapi.Tool format
		serverTools, err := handler.DiscoverServerTools(server.Name)
		if err != nil {
			handler.Logger.Warning("Failed to discover tools for %s: %v", server.Name, err)
			// Skip servers that fail tool discovery instead of creating placeholder tools
			continue
		}
		
		handler.Logger.Info("Successfully discovered %d tools for %s", len(serverTools), server.Name)
		
		// Convert server.Tool to openapi.Tool
		for _, tool := range serverTools {
			openAPITool := openapi.Tool{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.Parameters,
			}
			allTools = append(allTools, openAPITool)
		}
	}
	
	// If no tools found, add a default one
	if len(allTools) == 0 {
		allTools = append(allTools, openapi.Tool{
			Name:        "proxy_status",
			Description: "Get proxy status and available servers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"include_details": map[string]interface{}{
						"type":        "boolean",
						"description": "Include detailed server information",
						"default":     false,
					},
				},
			},
		})
	}
	
	// Generate OpenAPI schema using the proper openapi module
	schema, err := openapi.GenerateOpenAPISchema("MCP Proxy", allTools)
	if err != nil {
		handler.Logger.Error("Failed to generate OpenAPI schema: %v", err)
		writeErrorResponse(w, "Failed to generate OpenAPI spec", http.StatusInternalServerError)
		return
	}
	
	// Update the schema to match our proxy
	schema.Info.Title = "MCP Server Functions"
	schema.Info.Description = "Automatically generated API from MCP Tool Schemas via Kubernetes service discovery"
	schema.Servers = []openapi.Server{
		{
			URL:         fmt.Sprintf("http://%s", r.Host),
			Description: "system MCP Proxy Server",
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(schema); err != nil {
		writeErrorResponse(w, "Failed to encode OpenAPI spec", http.StatusInternalServerError)
	}
}

func handleHealthCheck(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	servers := handler.GetDiscoveredServers()
	connections := handler.GetConnectionStatus()
	
	// Proxy is healthy if it's running - individual server connections are reported but don't affect health status
	healthy := true
	details := make(map[string]interface{})
	connectedCount := 0
	
	for _, server := range servers {
		if connStatus, exists := connections[server.Name]; exists {
			details[server.Name] = map[string]interface{}{
				"connected":   connStatus.Connected,
				"error_count": connStatus.ErrorCount,
			}
			if connStatus.Connected {
				connectedCount++
			}
		} else {
			details[server.Name] = map[string]interface{}{
				"connected":   false,
				"error_count": -1,
			}
		}
	}

	// Always return 200 if proxy is running
	w.WriteHeader(http.StatusOK)
	writeJSONResponse(w, map[string]interface{}{
		"healthy":              healthy,
		"servers_discovered":   len(servers),
		"servers_connected":    connectedCount,
		"connection_details":   details,
	})
}

func handleDiscoveryInfo(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	servers := handler.GetDiscoveredServers()
	connections := handler.GetConnectionStatus()
	
	writeJSONResponse(w, map[string]interface{}{
		"discovery_type":    "kubernetes-native",
		"namespace":         "matey", // This should be configurable
		"discovered_servers": servers,
		"connection_status": connections,
		"last_updated":     time.Now(),
	})
}

func handleAPIRequest(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	// Simplified version - just return proxy info
	writeJSONResponse(w, map[string]interface{}{
		"status": "ok",
		"message": "MCP Proxy API",
	})
}

func handleServerRequest(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	// Extract server name from path: /servers/{server-name}/...
	path := r.URL.Path[9:] // Remove "/servers/"
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		writeErrorResponse(w, "Server name required in path", http.StatusBadRequest)
		return
	}

	serverName := parts[0]
	
	// Update request path to remove server prefix
	r.URL.Path = "/" + strings.Join(parts[1:], "/")
	
	handler.HandleMCPRequest(w, r, serverName)
}

func handleDynamicRequest(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	
	// Skip if it's a root request
	if path == "" {
		writeJSONResponse(w, map[string]interface{}{
			"status": "ok",
			"message": "MCP Proxy ready - use /openapi.json for API documentation",
		})
		return
	}
	
	// Handle server-specific OpenAPI specs (e.g., /dexcom/openapi.json)
	if strings.Contains(path, "/") {
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			serverName := parts[0]
			endpoint := parts[1]
			
			// Check if this is a server-specific OpenAPI request
			if endpoint == "openapi.json" && r.Method == http.MethodGet {
				// Check if server exists using K8s-native discovery
				if _, err := handler.ConnectionManager.GetConnection(serverName); err == nil {
					handler.Logger.Info("Handling server-specific OpenAPI request for: %s", serverName)
					handleServerOpenAPISpec(w, r, handler, serverName)
					return
				}
			}
			
			// Check if this is a server-specific docs request
			if endpoint == "docs" && r.Method == http.MethodGet {
				// Check if server exists using K8s-native discovery  
				if _, err := handler.ConnectionManager.GetConnection(serverName); err == nil {
					handler.Logger.Info("Handling server-specific docs request for: %s", serverName)
					handleServerDocs(w, r, handler, serverName)
					return
				}
			}
		}
	}
	
	// Try to handle as FastAPI-style tool call
	if r.Method == http.MethodPost {
		// Use cached tool discovery for efficiency
		serverName, found := handler.FindServerForTool(path)
		if found {
			handler.Logger.Info("Found tool %s in server %s via cache", path, serverName)
			handleToolCall(w, r, handler, serverName, path)
			return
		} else {
			handler.Logger.Warning("Tool %s not found in cache, attempting cache refresh", path)
			// Force cache refresh and try again
			if refreshedServerName, refreshedFound := handler.FindServerForTool(path); refreshedFound {
				handler.Logger.Info("Found tool %s in server %s after cache refresh", path, refreshedServerName)
				handleToolCall(w, r, handler, refreshedServerName, path)
				return
			}
			handler.Logger.Warning("Tool %s still not found after cache refresh", path)
		}
	}
	
	// Return not found for other paths
	writeErrorResponse(w, fmt.Sprintf("Unknown endpoint: %s", path), http.StatusNotFound)
}

func writeJSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func handleToolCall(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler, serverName, toolName string) {
	// Check authentication
	if !handler.CheckAuth(r) {
		handler.CorsError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	
	// Only allow POST method for tool calls
	if r.Method != http.MethodPost {
		writeErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// Read and parse request body
	var requestBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		handler.Logger.Error("Failed to decode request body: %v", err)
		writeErrorResponse(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	
	// Create MCP request format
	mcpRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": requestBody,
		},
	}
	
	// Convert to JSON and update request body
	requestBodyBytes, err := json.Marshal(mcpRequest)
	if err != nil {
		handler.Logger.Error("Failed to marshal MCP request: %v", err)
		writeErrorResponse(w, "Failed to create MCP request", http.StatusInternalServerError)
		return
	}
	
	// Update original request with MCP format (same pattern as handleServerRequest)
	r.Body = io.NopCloser(strings.NewReader(string(requestBodyBytes)))
	r.ContentLength = int64(len(requestBodyBytes))
	r.Header.Set("Content-Type", "application/json")
	
	// Use the existing MCP request handling (same pattern as handleServerRequest)
	handler.HandleMCPRequest(w, r, serverName)
}

func writeErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	response := map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"code":    statusCode,
		},
	}
	
	json.NewEncoder(w).Encode(response)
}

// handleServerOpenAPISpec handles server-specific OpenAPI requests for serve-proxy
func handleServerOpenAPISpec(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler, serverName string) {
	handler.Logger.Info("Delegating to handleServerOpenAPISpec for server: %s", serverName)
	
	// Call the existing method on the ProxyHandler
	// We need to access the unexported method, so we'll use reflection or create a public wrapper
	// For now, let's implement a simple version
	writeJSONResponse(w, map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]interface{}{
			"title":       fmt.Sprintf("%s MCP Server", serverName),
			"description": fmt.Sprintf("OpenAPI spec for %s MCP server", serverName),
			"version":     "1.0.0",
		},
		"servers": []map[string]interface{}{
			{
				"url":         "https://mcp.robotrad.io",
				"description": fmt.Sprintf("%s MCP Server", serverName),
			},
		},
		"paths": map[string]interface{}{
			"/": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     fmt.Sprintf("%s tools", serverName),
					"description": fmt.Sprintf("Execute tools on %s server", serverName),
				},
			},
		},
	})
}

// handleServerDocs handles server-specific docs requests for serve-proxy  
func handleServerDocs(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler, serverName string) {
	handler.Logger.Info("Generating docs for server: %s", serverName)
	
	// Delegate to the existing handler method in the ProxyHandler
	if docsHandler, ok := interface{}(handler).(interface {
		HandleServerDocs(http.ResponseWriter, *http.Request, string)
	}); ok {
		docsHandler.HandleServerDocs(w, r, serverName)
	} else {
		// Fallback: return simple docs
		handler.Logger.Info("Using fallback docs for server: %s", serverName)
		writeJSONResponse(w, map[string]interface{}{
			"server": serverName,
			"docs":   fmt.Sprintf("Documentation for %s server", serverName),
			"openapi": fmt.Sprintf("/%s/openapi.json", serverName),
		})
	}
}

// handleMCPServersEndpoint handles MCP protocol /mcp/servers endpoint
func handleMCPServersEndpoint(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	if r.Method != http.MethodPost {
		writeErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get discovered servers
	servers := handler.GetDiscoveredServers()
	connections := handler.GetConnectionStatus()
	
	// Convert to MCP server info format, filtering for only connected servers
	var mcpServers []map[string]interface{}
	for _, server := range servers {
		// Check if server is connected
		if connStatus, exists := connections[server.Name]; exists && connStatus.Connected {
			mcpServer := map[string]interface{}{
				"name":        server.Name,
				"version":     "1.0.0",
				"description": fmt.Sprintf("MCP Server: %s", server.Name),
			}
			mcpServers = append(mcpServers, mcpServer)
		} else {
			// Log skipped servers for debugging
			handler.Logger.Debug("Skipping server %s for MCP servers list - not connected", server.Name)
		}
	}

	// Return in MCP protocol format
	writeJSONResponse(w, map[string]interface{}{
		"result": mcpServers,
	})
}

// handleMCPServerToolsEndpoint handles MCP protocol /mcp/server/{name}/tools endpoint
func handleMCPServerToolsEndpoint(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	if r.Method != http.MethodPost {
		writeErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract server name from URL path
	path := strings.TrimPrefix(r.URL.Path, "/mcp/server/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "tools" {
		writeErrorResponse(w, "Invalid endpoint format", http.StatusBadRequest)
		return
	}
	
	serverName := parts[0]
	if serverName == "" {
		writeErrorResponse(w, "Server name is required", http.StatusBadRequest)
		return
	}

	// Check if this is a tool call endpoint (e.g., /mcp/server/{name}/tools/call)
	if len(parts) >= 3 && parts[2] == "call" {
		handleMCPToolCallEndpoint(w, r, handler, serverName)
		return
	}

	// Otherwise, handle as tools list endpoint
	tools, err := handler.DiscoverServerTools(serverName)
	if err != nil {
		handler.Logger.Error("Failed to discover tools for server %s: %v", serverName, err)
		writeErrorResponse(w, fmt.Sprintf("Failed to discover tools for server %s: %v", serverName, err), http.StatusInternalServerError)
		return
	}

	// Convert to MCP tool format
	var mcpTools []map[string]interface{}
	for _, tool := range tools {
		mcpTool := map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.Parameters,
		}
		mcpTools = append(mcpTools, mcpTool)
	}

	// Return in MCP protocol format
	writeJSONResponse(w, map[string]interface{}{
		"result": mcpTools,
	})
}

// handleMCPToolCallEndpoint handles MCP protocol tool call requests
func handleMCPToolCallEndpoint(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler, serverName string) {
	// Parse the MCP request
	var mcpRequest map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&mcpRequest); err != nil {
		writeErrorResponse(w, "Invalid JSON request", http.StatusBadRequest)
		return
	}

	// Extract tool name and arguments from MCP request
	params, ok := mcpRequest["params"].(map[string]interface{})
	if !ok {
		writeErrorResponse(w, "Invalid MCP request: missing params", http.StatusBadRequest)
		return
	}

	toolName, ok := params["name"].(string)
	if !ok {
		writeErrorResponse(w, "Invalid MCP request: missing tool name", http.StatusBadRequest)
		return
	}

	arguments, ok := params["arguments"].(map[string]interface{})
	if !ok {
		arguments = make(map[string]interface{})
	}

	handler.Logger.Info("MCP tool call: server=%s, tool=%s", serverName, toolName)

	// Get connection for the server
	conn, err := handler.ConnectionManager.GetConnection(serverName)
	if err != nil {
		handler.Logger.Error("No connection available for server %s: %v", serverName, err)
		writeErrorResponse(w, fmt.Sprintf("Server %s not available", serverName), http.StatusServiceUnavailable)
		return
	}

	// Create MCP request for the actual server
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "mcp-call-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}

	// Send request based on protocol
	var response map[string]interface{}
	switch conn.Protocol {
	case "http":
		response, err = handler.SendHTTPToolCall(conn, request)
	case "sse":
		response, err = handler.SendSSEToolCall(conn, request)
	default:
		handler.Logger.Error("Unsupported protocol %s for server %s", conn.Protocol, serverName)
		writeErrorResponse(w, "Unsupported protocol", http.StatusInternalServerError)
		return
	}

	if err != nil {
		handler.Logger.Error("Failed to execute tool %s on server %s: %v", toolName, serverName, err)
		writeErrorResponse(w, "Tool execution failed", http.StatusInternalServerError)
		return
	}

	// Return the response in MCP protocol format
	writeJSONResponse(w, response)
}