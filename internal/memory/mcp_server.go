// internal/memory/mcp_server.go
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// MCPMemoryServer provides HTTP/SSE/WebSocket endpoints for memory MCP tools
type MCPMemoryServer struct {
	store     *MemoryStore
	tools     *MCPMemoryTools
	logger    logr.Logger
	server    *http.Server
	clients   map[string]*websocket.Conn
	clientsMu sync.RWMutex
	upgrader  websocket.Upgrader
}

// MCPRequest represents an MCP protocol request
type MCPRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// MCPResponse represents an MCP protocol response
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents an MCP protocol error
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCPToolsListResponse represents the response to tools/list
type MCPToolsListResponse struct {
	Tools []MCPToolDefinition `json:"tools"`
}

// MCPToolCallRequest represents a tool call request
type MCPToolCallRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// MCPToolCallResponse represents a tool call response
type MCPToolCallResponse struct {
	Content []MCPContent `json:"content"`
}

// MCPContent represents MCP content
type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func NewMCPMemoryServer(store *MemoryStore, logger logr.Logger) *MCPMemoryServer {
	tools := NewMCPMemoryTools(store, logger)
	
	return &MCPMemoryServer{
		store:   store,
		tools:   tools,
		logger:  logger,
		clients: make(map[string]*websocket.Conn),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
	}
}

func (s *MCPMemoryServer) Start(host string, port int) error {
	s.logger.Info("Starting MCP Memory Server", "host", host, "port", port)

	router := mux.NewRouter()
	s.setupRoutes(router)

	s.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", host, port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	s.logger.Info("MCP Memory Server listening", "address", s.server.Addr)
	return s.server.ListenAndServe()
}

func (s *MCPMemoryServer) Stop() error {
	s.logger.Info("Stopping MCP Memory Server")
	
	// Close all WebSocket connections
	s.clientsMu.Lock()
	for clientID, conn := range s.clients {
		conn.Close()
		delete(s.clients, clientID)
	}
	s.clientsMu.Unlock()

	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *MCPMemoryServer) setupRoutes(router *mux.Router) {
	// Health check endpoint
	router.HandleFunc("/health", s.healthHandler).Methods("GET")
	
	// MCP HTTP endpoints
	router.HandleFunc("/mcp", s.mcpHandler).Methods("POST")
	router.HandleFunc("/mcp/tools", s.toolsHandler).Methods("GET")
	router.HandleFunc("/mcp/tools/call", s.toolCallHandler).Methods("POST")
	
	// Server-Sent Events endpoint
	router.HandleFunc("/mcp/sse", s.sseHandler).Methods("GET")
	
	// WebSocket endpoint
	router.HandleFunc("/mcp/ws", s.websocketHandler).Methods("GET")
}

func (s *MCPMemoryServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	// Check store health
	if err := s.store.HealthCheck(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"service": "mcp-memory-server",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

func (s *MCPMemoryServer) mcpHandler(w http.ResponseWriter, r *http.Request) {
	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, nil, -32700, "Parse error", err.Error())
		return
	}

	s.logger.Info("MCP request received", "method", req.Method, "id", req.ID)

	ctx := r.Context()
	switch req.Method {
	case "tools/list":
		s.handleToolsList(w, req)
	case "tools/call":
		s.handleToolCall(w, req, ctx)
	default:
		s.sendError(w, req.ID, -32601, "Method not found", fmt.Sprintf("Unknown method: %s", req.Method))
	}
}

func (s *MCPMemoryServer) toolsHandler(w http.ResponseWriter, r *http.Request) {
	tools := s.tools.GetMCPTools()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MCPToolsListResponse{Tools: tools})
}

func (s *MCPMemoryServer) toolCallHandler(w http.ResponseWriter, r *http.Request) {
	var req MCPToolCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	result, err := s.tools.ExecuteMCPTool(ctx, req.Name, req.Arguments)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert result to text content
	resultText, _ := json.Marshal(result)
	response := MCPToolCallResponse{
		Content: []MCPContent{
			{
				Type: "text",
				Text: string(resultText),
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *MCPMemoryServer) sseHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.Info("SSE connection established")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Send initial connection message
	fmt.Fprintf(w, "data: %s\n\n", `{"type":"connection","status":"connected"}`)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Keep connection alive
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			s.logger.Info("SSE connection closed")
			return
		case <-ticker.C:
			fmt.Fprintf(w, "data: %s\n\n", `{"type":"ping","timestamp":"`+time.Now().Format(time.RFC3339)+`"}`)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

func (s *MCPMemoryServer) websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error(err, "Failed to upgrade to WebSocket")
		return
	}

	clientID := fmt.Sprintf("client_%d", time.Now().UnixNano())
	s.clientsMu.Lock()
	s.clients[clientID] = conn
	s.clientsMu.Unlock()

	s.logger.Info("WebSocket connection established", "clientID", clientID)

	// Send welcome message
	welcomeMsg := map[string]interface{}{
		"type":      "connection",
		"status":    "connected",
		"clientID":  clientID,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	conn.WriteJSON(welcomeMsg)

	// Handle messages
	go s.handleWebSocketMessages(clientID, conn)
}

func (s *MCPMemoryServer) handleWebSocketMessages(clientID string, conn *websocket.Conn) {
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, clientID)
		s.clientsMu.Unlock()
		conn.Close()
		s.logger.Info("WebSocket connection closed", "clientID", clientID)
	}()

	for {
		var req MCPRequest
		if err := conn.ReadJSON(&req); err != nil {
			s.logger.Error(err, "Failed to read WebSocket message", "clientID", clientID)
			break
		}

		s.logger.Info("WebSocket MCP request", "clientID", clientID, "method", req.Method, "id", req.ID)

		ctx := context.Background()
		switch req.Method {
		case "tools/list":
			tools := s.tools.GetMCPTools()
			response := MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  MCPToolsListResponse{Tools: tools},
			}
			conn.WriteJSON(response)

		case "tools/call":
			if toolCall, ok := req.Params["tool"].(map[string]interface{}); ok {
				toolName, _ := toolCall["name"].(string)
				toolArgs, _ := toolCall["arguments"].(map[string]interface{})
				
				result, err := s.tools.ExecuteMCPTool(ctx, toolName, toolArgs)
				if err != nil {
					response := MCPResponse{
						JSONRPC: "2.0",
						ID:      req.ID,
						Error: &MCPError{
							Code:    -32000,
							Message: "Tool execution failed",
							Data:    err.Error(),
						},
					}
					conn.WriteJSON(response)
				} else {
					resultText, _ := json.Marshal(result)
					response := MCPResponse{
						JSONRPC: "2.0",
						ID:      req.ID,
						Result: MCPToolCallResponse{
							Content: []MCPContent{
								{
									Type: "text",
									Text: string(resultText),
								},
							},
						},
					}
					conn.WriteJSON(response)
				}
			}

		default:
			response := MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &MCPError{
					Code:    -32601,
					Message: "Method not found",
					Data:    fmt.Sprintf("Unknown method: %s", req.Method),
				},
			}
			conn.WriteJSON(response)
		}
	}
}

func (s *MCPMemoryServer) handleToolsList(w http.ResponseWriter, req MCPRequest) {
	tools := s.tools.GetMCPTools()
	response := MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  MCPToolsListResponse{Tools: tools},
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *MCPMemoryServer) handleToolCall(w http.ResponseWriter, req MCPRequest, ctx context.Context) {
	toolCall, ok := req.Params["tool"].(map[string]interface{})
	if !ok {
		s.sendError(w, req.ID, -32602, "Invalid params", "Missing tool parameter")
		return
	}

	toolName, ok := toolCall["name"].(string)
	if !ok {
		s.sendError(w, req.ID, -32602, "Invalid params", "Missing tool name")
		return
	}

	toolArgs, _ := toolCall["arguments"].(map[string]interface{})
	
	result, err := s.tools.ExecuteMCPTool(ctx, toolName, toolArgs)
	if err != nil {
		s.sendError(w, req.ID, -32000, "Tool execution failed", err.Error())
		return
	}

	// Convert result to text content
	resultText, _ := json.Marshal(result)
	response := MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: MCPToolCallResponse{
			Content: []MCPContent{
				{
					Type: "text",
					Text: string(resultText),
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *MCPMemoryServer) sendError(w http.ResponseWriter, id interface{}, code int, message string, data interface{}) {
	response := MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(response)
}

// BroadcastToClients sends a message to all connected WebSocket clients
func (s *MCPMemoryServer) BroadcastToClients(message interface{}) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for clientID, conn := range s.clients {
		if err := conn.WriteJSON(message); err != nil {
			s.logger.Error(err, "Failed to send message to client", "clientID", clientID)
			// Remove failed connection
			delete(s.clients, clientID)
			conn.Close()
		}
	}
}

// GetServerInfo returns information about the MCP server
func (s *MCPMemoryServer) GetServerInfo() map[string]interface{} {
	s.clientsMu.RLock()
	clientCount := len(s.clients)
	s.clientsMu.RUnlock()

	return map[string]interface{}{
		"service":       "mcp-memory-server",
		"version":       "1.0.0",
		"protocol":      "mcp",
		"transports":    []string{"http", "sse", "websocket"},
		"tools":         len(s.tools.GetMCPTools()),
		"clients":       clientCount,
		"status":        "running",
		"timestamp":     time.Now().Format(time.RFC3339),
	}
}

// GetClientIDs returns a list of connected client IDs
func (s *MCPMemoryServer) GetClientIDs() []string {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	clients := make([]string, 0, len(s.clients))
	for clientID := range s.clients {
		clients = append(clients, clientID)
	}
	return clients
}