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
	
	// Main MCP proxy route
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleProxyRequest(w, r, proxyHandler)
	})
	
	// Server-specific routes
	mux.HandleFunc("/servers/", func(w http.ResponseWriter, r *http.Request) {
		handleServerRequest(w, r, proxyHandler)
	})
	
	// API routes
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		handleAPIRequest(w, r, proxyHandler)
	})
	
	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		handleHealthCheck(w, r, proxyHandler)
	})
	
	// Service discovery info
	mux.HandleFunc("/discovery", func(w http.ResponseWriter, r *http.Request) {
		handleDiscoveryInfo(w, r, proxyHandler)
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

// Helper functions

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