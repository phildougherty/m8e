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
	
	log.Printf("Registering routes...")
	
	// Add MCP JSON-RPC routes  
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id": nil,
				"error": map[string]interface{}{
					"code": -32700,
					"message": "Parse error",
				},
			}); err != nil {
				fmt.Printf("Warning: Failed to encode error response: %v\n", err)
			}
			return
		}
		
		method, _ := req["method"].(string)
		id := req["id"]
		
		if method == "initialize" {
			// MCP protocol initialization
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id": id,
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities": map[string]interface{}{
						"tools": map[string]interface{}{
							"listChanged": false,
						},
					},
					"serverInfo": map[string]interface{}{
						"name":    "matey",
						"version": "0.0.4",
					},
				},
			}); err != nil {
				fmt.Printf("Warning: Failed to encode initialize response: %v\n", err)
			}
			return
		}
		
		if method == "notifications/initialized" {
			// Notification that client has been initialized - no response needed
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id": id,
				"result": nil,
			}); err != nil {
				fmt.Printf("Warning: Failed to encode initialized response: %v\n", err)
			}
			return
		}
		
		if method == "tools/list" {
			tools := mcpServer.GetTools()
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id": id,
				"result": map[string]interface{}{
					"tools": tools,
				},
			}); err != nil {
				log.Printf("Warning: Failed to encode tools list response: %v", err)
			}
			return
		}
		
		if method == "tools/call" {
			params, ok := req["params"].(map[string]interface{})
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				if err := json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"id": id,
					"error": map[string]interface{}{
						"code": -32602,
						"message": "Invalid params",
					},
				}); err != nil {
					log.Printf("Warning: Failed to encode invalid params response: %v", err)
				}
				return
			}
			
			toolName, ok := params["name"].(string)
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				if err := json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"id": id,
					"error": map[string]interface{}{
						"code": -32602,
						"message": "Missing tool name",
					},
				}); err != nil {
					log.Printf("Warning: Failed to encode missing tool name response: %v", err)
				}
				return
			}
			
			arguments, _ := params["arguments"].(map[string]interface{})
			if arguments == nil {
				arguments = make(map[string]interface{})
			}
			
			// Execute the tool
			result, err := mcpServer.ExecuteTool(r.Context(), toolName, arguments)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				if encErr := json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"id": id,
					"error": map[string]interface{}{
						"code": -32603,
						"message": err.Error(),
					},
				}); encErr != nil {
					log.Printf("Warning: Failed to encode tool execution error response: %v", encErr)
				}
				return
			}
			
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id": id,
				"result": result,
			}); err != nil {
				log.Printf("Warning: Failed to encode tool call success response: %v", err)
			}
			return
		}
		
		// Method not found
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id": id,
			"error": map[string]interface{}{
				"code": -32601,
				"message": "Method not found",
			},
		}); err != nil {
			log.Printf("Warning: Failed to encode method not found response: %v", err)
		}
	})
	log.Printf("Registered POST /")
	router.HandleFunc("/health", handleHealth).Methods("GET")
	log.Printf("Registered GET /health")
	router.HandleFunc("/info", handleInfo(mcpServer)).Methods("GET")
	log.Printf("Registered GET /info")
	
	// Test route
	router.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Test route hit: %s %s", r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "text/plain")
		if _, err := fmt.Fprintf(w, "Test route works!"); err != nil {
			log.Printf("Warning: Failed to write test response: %v", err)
		}
	}).Methods("GET")
	log.Printf("Registered GET /test")
	
	// Legacy REST routes for backward compatibility
	router.HandleFunc("/tools", handleGetTools(mcpServer)).Methods("GET")
	log.Printf("Registered GET /tools")
	router.HandleFunc("/tools/{tool}", handleCallTool(mcpServer)).Methods("POST")
	log.Printf("Registered POST /tools/{tool}")
	
	// Add CORS middleware
	router.Use(corsMiddleware)
	
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%s", port),
		Handler:      router,
		ReadTimeout:  25 * time.Minute, // Extended for execute_agent
		WriteTimeout: 25 * time.Minute, // Extended for execute_agent
		IdleTimeout:  25 * time.Minute, // Extended for execute_agent
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

// MCPRequest represents an MCP JSON-RPC request
type MCPRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// MCPResponse represents an MCP JSON-RPC response
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents an MCP JSON-RPC error
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
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
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"timestamp": time.Now().Unix(),
	}); err != nil {
		log.Printf("Warning: Failed to encode health response: %v", err)
	}
}

func handleInfo(mcpServer *mcp.MateyMCPServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"name":        "matey",
			"version":     "1.0.0",
			"description": "Matey MCP server for cluster management",
			"tools":       mcpServer.GetTools(),
		}); err != nil {
			log.Printf("Warning: Failed to encode info response: %v", err)
		}
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