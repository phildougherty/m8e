// internal/cmd/proxy.go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
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

func NewProxyCommand() *cobra.Command {
	var port int
	var namespace string
	var apiKey string

	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Run a Kubernetes-native MCP proxy server",
		Long: `Run a Kubernetes-native proxy server that automatically discovers and routes to MCP services
using Kubernetes service discovery. The proxy uses cluster DNS and Kubernetes APIs to find and
connect to MCP servers without requiring static configuration.

Key features:
- Automatic service discovery via Kubernetes labels
- Dynamic connection management
- Real-time service updates
- Support for HTTP, SSE, and STDIO protocols (where supported)
- Built-in health checking and retry logic`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProxy(cmd, port, namespace, apiKey)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to run the proxy server on")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace to discover services in")
	cmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "API key for proxy authentication (optional)")

	return cmd
}

func runProxy(cmd *cobra.Command, port int, namespace, apiKey string) error {
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

	fmt.Printf("Starting Kubernetes-native MCP proxy server...\n")
	fmt.Printf("Namespace: %s\n", namespace)
	fmt.Printf("Port: %d\n", port)
	if apiKey != "" {
		fmt.Printf("Authentication: Enabled\n")
	} else {
		fmt.Printf("Authentication: Disabled\n")
	}

	// Create Kubernetes-native proxy handler
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
	
	// Dynamic server-specific OpenAPI endpoints 
	// (handled in the catch-all since server names are discovered at runtime)
	
	// Catch-all for dynamic routing (must be last)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleDynamicRequest(w, r, proxyHandler)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		fmt.Printf("Kubernetes-native MCP proxy server listening on :%d\n", port)
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

func handleProxyRequest(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	// Extract server name from path or headers
	serverName := extractServerName(r)
	if serverName == "" {
		writeErrorResponse(w, "Server name required in path or X-MCP-Server header", http.StatusBadRequest)
		return
	}

	// Handle the MCP request
	handler.HandleMCPRequest(w, r, serverName)
}

func handleServerRequest(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	// Extract server name from path: /servers/{server-name}/...
	path := r.URL.Path[9:] // Remove "/servers/"
	parts := splitPath(path)
	if len(parts) < 1 {
		writeErrorResponse(w, "Server name required in path", http.StatusBadRequest)
		return
	}

	serverName := parts[0]
	
	// Update request path to remove server prefix
	r.URL.Path = "/" + joinPath(parts[1:])
	
	handler.HandleMCPRequest(w, r, serverName)
}

func handleAPIRequest(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	path := r.URL.Path[5:] // Remove "/api/"
	
	switch {
	case path == "info":
		handleProxyInfo(w, r, handler)
	case path == "servers":
		handleServersList(w, r, handler)
	case path == "connections":
		handleConnectionsStatus(w, r, handler)
	case path == "refresh":
		handleRefreshConnections(w, r, handler)
	default:
		writeErrorResponse(w, "Unknown API endpoint", http.StatusNotFound)
	}
}

func handleProxyInfo(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	info := handler.GetProxyInfo()
	writeJSONResponse(w, info)
}

func handleServersList(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	servers := handler.GetDiscoveredServers()
	writeJSONResponse(w, map[string]interface{}{
		"servers": servers,
		"count":   len(servers),
	})
}

func handleConnectionsStatus(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	status := handler.GetConnectionStatus()
	writeJSONResponse(w, map[string]interface{}{
		"connections": status,
		"count":       len(status),
	})
}

func handleRefreshConnections(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	if r.Method != http.MethodPost {
		writeErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := handler.RefreshConnections(); err != nil {
		writeErrorResponse(w, fmt.Sprintf("Failed to refresh connections: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"status":  "success",
		"message": "Connections refreshed successfully",
	})
}

func handleHealthCheck(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	servers := handler.GetDiscoveredServers()
	connections := handler.GetConnectionStatus()
	
	healthy := true
	details := make(map[string]interface{})
	
	for _, server := range servers {
		if connStatus, exists := connections[server.Name]; exists {
			details[server.Name] = map[string]interface{}{
				"connected":   connStatus.Connected,
				"error_count": connStatus.ErrorCount,
			}
			if !connStatus.Connected {
				healthy = false
			}
		} else {
			details[server.Name] = map[string]interface{}{
				"connected":   false,
				"error_count": -1,
			}
			healthy = false
		}
	}

	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}

	w.WriteHeader(status)
	writeJSONResponse(w, map[string]interface{}{
		"healthy":     healthy,
		"servers":     len(servers),
		"connections": len(connections),
		"details":     details,
	})
}

func handleDiscoveryInfo(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	servers := handler.GetDiscoveredServers()
	connections := handler.GetConnectionStatus()
	
	writeJSONResponse(w, map[string]interface{}{
		"discovery_type":    "kubernetes-native",
		"namespace":         "default", // This should be configurable
		"discovered_servers": servers,
		"connection_status": connections,
		"last_updated":     time.Now(),
	})
}

func handleOpenAPISpec(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	// Check authentication
	if !handler.CheckAuth(r) {
		handler.CorsError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Collect all tools from all discovered services
	servers := handler.GetDiscoveredServers()
	var allTools []openapi.Tool
	
	for _, server := range servers {
		if server.Name == "" {
			continue
		}
		
		// Get tools for this server and convert to openapi.Tool format
		serverTools, err := handler.DiscoverServerTools(server.Name)
		if err != nil {
			handler.Logger.Warning("Failed to discover tools for %s: %v", server.Name, err)
			// Add a generic tool for this server
			allTools = append(allTools, openapi.Tool{
				Name:        fmt.Sprintf("%s_default", server.Name),
				Description: fmt.Sprintf("Default tool for %s server", server.Name),
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"action": map[string]interface{}{
							"type":        "string",
							"description": "Action to perform on the server",
						},
					},
				},
			})
			continue
		}
		
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
			Description: "Kubernetes-native MCP Proxy Server",
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(schema); err != nil {
		writeErrorResponse(w, "Failed to encode OpenAPI spec", http.StatusInternalServerError)
	}
}

// Helper functions

func formatToolTitle(toolName string) string {
	// Convert snake_case or kebab-case to Title Case
	words := strings.FieldsFunc(toolName, func(c rune) bool {
		return c == '_' || c == '-'
	})
	
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
		}
	}
	
	return strings.Join(words, " ")
}

func getServerNames(handler *server.ProxyHandler) []string {
	servers := handler.GetDiscoveredServers()
	names := make([]string, 0, len(servers))
	for _, srv := range servers {
		if srv.Name != "" {
			names = append(names, srv.Name)
		}
	}
	return names
}

func handleServerOpenAPISpec(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler, serverName string) {
	// Check authentication
	if !handler.CheckAuth(r) {
		handler.CorsError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get tools for this specific server
	serverTools, err := handler.DiscoverServerTools(serverName)
	var tools []openapi.Tool
	
	if err != nil {
		handler.Logger.Warning("Failed to discover tools for %s: %v", serverName, err)
		// Add a generic tool for this server
		tools = append(tools, openapi.Tool{
			Name:        fmt.Sprintf("%s_default", serverName),
			Description: fmt.Sprintf("Default tool for %s server", serverName),
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "Action to perform on the server",
					},
				},
			},
		})
	} else {
		// Convert server.Tool to openapi.Tool
		for _, tool := range serverTools {
			openAPITool := openapi.Tool{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.Parameters,
			}
			tools = append(tools, openAPITool)
		}
	}
	
	// Generate OpenAPI schema using the proper openapi module
	schema, err := openapi.GenerateOpenAPISchema(serverName, tools)
	if err != nil {
		handler.Logger.Error("Failed to generate OpenAPI schema for %s: %v", serverName, err)
		writeErrorResponse(w, "Failed to generate server OpenAPI spec", http.StatusInternalServerError)
		return
	}
	
	// Update the schema to match our proxy
	schema.Servers = []openapi.Server{
		{
			URL:         fmt.Sprintf("http://%s", r.Host),
			Description: fmt.Sprintf("%s MCP Server via Kubernetes proxy", serverName),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(schema); err != nil {
		writeErrorResponse(w, "Failed to encode server OpenAPI spec", http.StatusInternalServerError)
	}
}

func handleDynamicRequest(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	
	// Skip if it's a root request
	if path == "" {
		writeErrorResponse(w, "Proxy ready - use /openapi.json for API documentation", http.StatusOK)
		return
	}
	
	// Handle server-specific OpenAPI endpoints: /{server}/openapi.json
	if strings.HasSuffix(path, "/openapi.json") {
		serverName := strings.TrimSuffix(path, "/openapi.json")
		if serverName != "" {
			// Check if this server exists
			servers := handler.GetDiscoveredServers()
			for _, server := range servers {
				if server.Name == serverName {
					handleServerOpenAPISpec(w, r, handler, serverName)
					return
				}
			}
		}
		writeErrorResponse(w, fmt.Sprintf("Server not found: %s", serverName), http.StatusNotFound)
		return
	}
	
	// Skip if it contains other slashes
	if strings.Contains(path, "/") {
		writeErrorResponse(w, "Path not found", http.StatusNotFound)
		return
	}
	
	// Skip known endpoints
	knownEndpoints := map[string]bool{
		"api":          true,
		"servers":      true,
		"openapi.json": true,
		"health":       true,
		"discovery":    true,
	}
	
	if knownEndpoints[path] {
		writeErrorResponse(w, "Endpoint already handled", http.StatusNotFound)
		return
	}
	
	// Check if it's a server name
	servers := handler.GetDiscoveredServers()
	for _, server := range servers {
		if server.Name == path {
			// Route to server handler with proper path setup
			// Create a new request with proper server routing
			newPath := "/servers/" + path + "/"
			r.URL.Path = newPath
			handleServerRequest(w, r, handler)
			return
		}
	}
	
	// Check if it's a tool name (for direct tool calls)
	if r.Method == "POST" {
		handleDirectToolCall(w, r, handler, path)
		return
	}
	
	// Nothing matches
	writeErrorResponse(w, fmt.Sprintf("Unknown endpoint: %s", path), http.StatusNotFound)
}

func handleDirectToolCall(w http.ResponseWriter, r *http.Request, handler *server.ProxyHandler, toolName string) {
	// Check authentication
	if !handler.CheckAuth(r) {
		handler.CorsError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Set CORS headers
	handler.SetCORSHeaders(w)

	// Handle OPTIONS requests
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Only allow POST for tool calls
	if r.Method != "POST" {
		writeErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Delegate to the proxy handler's direct tool call method
	handler.HandleDirectToolCall(w, r, toolName)
}

func extractServerName(r *http.Request) string {
	// First try X-MCP-Server header
	if serverName := r.Header.Get("X-MCP-Server"); serverName != "" {
		return serverName
	}
	
	// Then try query parameter
	if serverName := r.URL.Query().Get("server"); serverName != "" {
		return serverName
	}
	
	// Finally try to extract from path
	path := r.URL.Path
	if len(path) > 1 {
		parts := splitPath(path[1:]) // Remove leading /
		if len(parts) > 0 {
			return parts[0]
		}
	}
	
	return ""
}

func splitPath(path string) []string {
	if path == "" {
		return []string{}
	}
	// Simple split for now
	parts := strings.Split(path, "/")
	result := []string{}
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func joinPath(parts []string) string {
	return strings.Join(parts, "/")
}

func writeJSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
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