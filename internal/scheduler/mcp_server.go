// internal/scheduler/mcp_server.go
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/gorilla/websocket"
)

// MCPServer provides HTTP and WebSocket endpoints for MCP protocol
type MCPServer struct {
	toolServer *MCPToolServer
	logger     logr.Logger
	upgrader   websocket.Upgrader
}

type MCPRequest struct {
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

type MCPResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type MCPToolCallRequest struct {
	Tool       string                 `json:"tool"`
	Parameters map[string]interface{} `json:"parameters"`
}

func NewMCPServer(toolServer *MCPToolServer, logger logr.Logger) *MCPServer {
	return &MCPServer{
		toolServer: toolServer,
		logger:     logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
	}
}

// SetupRoutes configures HTTP routes for the MCP server
func (s *MCPServer) SetupRoutes(mux *http.ServeMux) {
	// MCP protocol endpoints
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/tools", s.handleListTools)
	mux.HandleFunc("/tools/", s.handleToolCall)
	mux.HandleFunc("/sse", s.handleSSE)
	mux.HandleFunc("/ws", s.handleWebSocket)

	// Legacy compatibility endpoints
	mux.HandleFunc("/api/v1/tools", s.handleListTools)
	mux.HandleFunc("/api/v1/call", s.handleGenericCall)
}

func (s *MCPServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := map[string]interface{}{
		"name":        "matey-task-scheduler",
		"version":     "0.0.4",
		"description": "system MCP Task Scheduler",
		"protocol":    "mcp",
		"capabilities": []string{"tools", "sse", "websocket"},
		"endpoints": map[string]string{
			"health":    "/health",
			"tools":     "/tools",
			"sse":       "/sse",
			"websocket": "/ws",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func (s *MCPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	health, err := s.toolServer.ExecuteMCPTool(ctx, "health_check", map[string]interface{}{})
	if err != nil {
		s.sendError(w, 500, fmt.Sprintf("Health check failed: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func (s *MCPServer) handleListTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tools := s.toolServer.GetMCPTools()
	
	response := map[string]interface{}{
		"tools": tools,
		"count": len(tools),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *MCPServer) handleToolCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract tool name from URL path
	toolName := r.URL.Path[len("/tools/"):]
	if toolName == "" {
		s.sendError(w, 400, "Tool name is required")
		return
	}

	// Handle special case for tool call with /call suffix
	if len(toolName) > 5 && toolName[len(toolName)-5:] == "/call" {
		toolName = toolName[:len(toolName)-5]
	}

	var request MCPToolCallRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		// If JSON decode fails, try to use the tool name from URL
		request.Tool = toolName
		request.Parameters = make(map[string]interface{})
	}

	// Use tool name from URL if not provided in request
	if request.Tool == "" {
		request.Tool = toolName
	}

	ctx := r.Context()
	result, err := s.toolServer.ExecuteMCPTool(ctx, request.Tool, request.Parameters)
	if err != nil {
		s.sendError(w, 400, err.Error())
		return
	}

	response := MCPResponse{Result: result}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *MCPServer) handleGenericCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request MCPToolCallRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.sendError(w, 400, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	if request.Tool == "" {
		s.sendError(w, 400, "Tool name is required")
		return
	}

	ctx := r.Context()
	result, err := s.toolServer.ExecuteMCPTool(ctx, request.Tool, request.Parameters)
	if err != nil {
		s.sendError(w, 400, err.Error())
		return
	}

	response := MCPResponse{Result: result}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *MCPServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\n")
	fmt.Fprintf(w, "data: {\"message\": \"Connected to MCP task scheduler\"}\n\n")
	flusher.Flush()

	// Keep connection alive and send periodic heartbeats
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, "event: heartbeat\n")
			fmt.Fprintf(w, "data: {\"timestamp\": \"%s\"}\n\n", time.Now().Format(time.RFC3339))
			flusher.Flush()
		}
	}
}

func (s *MCPServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error(err, "Failed to upgrade to WebSocket")
		return
	}
	defer conn.Close()

	s.logger.Info("WebSocket connection established")

	// Send welcome message
	welcomeMsg := map[string]interface{}{
		"type":    "welcome",
		"message": "Connected to MCP task scheduler",
		"tools":   s.toolServer.GetMCPTools(),
	}
	if err := conn.WriteJSON(welcomeMsg); err != nil {
		s.logger.Error(err, "Failed to send welcome message")
		return
	}

	// Handle messages
	for {
		var request MCPRequest
		if err := conn.ReadJSON(&request); err != nil {
			s.logger.Error(err, "Failed to read WebSocket message")
			break
		}

		ctx := context.Background()
		
		var response MCPResponse
		if request.Method == "call_tool" {
			toolName, ok := request.Params["tool"].(string)
			if !ok {
				response.Error = &MCPError{Code: 400, Message: "Tool name is required"}
			} else {
				parameters, _ := request.Params["parameters"].(map[string]interface{})
				if parameters == nil {
					parameters = make(map[string]interface{})
				}
				
				result, err := s.toolServer.ExecuteMCPTool(ctx, toolName, parameters)
				if err != nil {
					response.Error = &MCPError{Code: 400, Message: err.Error()}
				} else {
					response.Result = result
				}
			}
		} else {
			response.Error = &MCPError{Code: 400, Message: fmt.Sprintf("Unknown method: %s", request.Method)}
		}

		if err := conn.WriteJSON(response); err != nil {
			s.logger.Error(err, "Failed to send WebSocket response")
			break
		}
	}
}

func (s *MCPServer) sendError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	
	response := MCPResponse{
		Error: &MCPError{
			Code:    code,
			Message: message,
		},
	}
	json.NewEncoder(w).Encode(response)
}

// StartServer starts the MCP HTTP server
func (s *MCPServer) StartServer(ctx context.Context, host string, port int) error {
	mux := http.NewServeMux()
	s.SetupRoutes(mux)

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", host, port),
		Handler: mux,
	}

	s.logger.Info("Starting MCP server", "host", host, "port", port)

	// Start server in background
	errChan := make(chan error, 1)
	go func() {
		errChan <- server.ListenAndServe()
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		s.logger.Info("Shutting down MCP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errChan:
		if err != http.ErrServerClosed {
			return fmt.Errorf("MCP server error: %w", err)
		}
		return nil
	}
}