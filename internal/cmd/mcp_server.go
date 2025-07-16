package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"

	"github.com/phildougherty/m8e/internal/mcp"
)

// NewMCPServerCommand creates the mcp-server command
func NewMCPServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp-server",
		Short: "Start the Matey MCP server",
		Long: `Start the Matey MCP server that provides MCP tools for interacting with the cluster.

This server provides tools for:
- Checking server status (matey ps)
- Starting/stopping services (matey up/down)
- Viewing logs (matey logs)
- Inspecting resources (matey inspect)
- Applying configurations
- Managing workflows

The server runs as an HTTP service that can be accessed by MCP clients.`,
		RunE: runMCPServer,
	}
	
	cmd.Flags().StringP("port", "p", "8081", "Port to run the MCP server on")
	cmd.Flags().String("matey-binary", "/usr/local/bin/matey", "Path to the matey binary")
	// Note: config and namespace flags are inherited from root command
	
	return cmd
}

func runMCPServer(cmd *cobra.Command, args []string) error {
	port, _ := cmd.Flags().GetString("port")
	mateyBinary, _ := cmd.Flags().GetString("matey-binary")
	configFile, _ := cmd.Flags().GetString("file") // Use inherited flag from root
	namespace, _ := cmd.Flags().GetString("namespace") // Use inherited flag from root

	// Create the MCP server
	mcpServer := mcp.NewMateyMCPServer(mateyBinary, configFile, namespace)
	
	// Create HTTP server
	router := mux.NewRouter()
	
	// Add routes
	router.HandleFunc("/tools", handleGetTools(mcpServer)).Methods("GET")
	router.HandleFunc("/tools/{tool}", handleCallTool(mcpServer)).Methods("POST")
	router.HandleFunc("/health", handleHealth).Methods("GET")
	router.HandleFunc("/info", handleInfo(mcpServer)).Methods("GET")
	
	// Add CORS middleware
	router.Use(corsMiddleware)
	
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: router,
	}
	
	// Start server in goroutine
	go func() {
		log.Printf("Starting Matey MCP server on port %s", port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()
	
	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	
	// Shutdown server
	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	
	log.Println("Server stopped")
	return nil
}

func handleGetTools(mcpServer *mcp.MateyMCPServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tools := mcpServer.GetTools()
		
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"tools": tools,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func handleCallTool(mcpServer *mcp.MateyMCPServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		toolName := vars["tool"]
		
		var req struct {
			Arguments map[string]interface{} `json:"arguments"`
		}
		
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		
		result, err := mcpServer.ExecuteTool(r.Context(), toolName, req.Arguments)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"timestamp": time.Now().Unix(),
	})
}

func handleInfo(mcpServer *mcp.MateyMCPServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":        "matey",
			"version":     "1.0.0",
			"description": "Matey MCP server for cluster management",
			"tools":       mcpServer.GetTools(),
		})
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		
		next.ServeHTTP(w, r)
	})
}