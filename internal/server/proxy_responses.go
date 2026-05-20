// internal/server/proxy_responses.go
package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/phildougherty/m8e/internal/discovery"
)

// This file holds the proxy's small response/auth helpers: CORS headers, API
// key checks, OAuth token extraction onto connections and JSON-RPC error
// shaping. They are deliberately kept together and away from routing logic.

// CorsError writes a CORS-enabled error response (public method for cmd/proxy.go)
func (h *ProxyHandler) CorsError(w http.ResponseWriter, message string, statusCode int) {
	h.corsError(w, message, statusCode)
}

// SetCORSHeaders sets CORS headers for cross-origin requests (public method for cmd/proxy.go)
func (h *ProxyHandler) SetCORSHeaders(w http.ResponseWriter) {
	h.setCORSHeaders(w)
}

// corsError writes a CORS-enabled error response
func (h *ProxyHandler) corsError(w http.ResponseWriter, message string, statusCode int) {
	h.setCORSHeaders(w)
	h.writeErrorResponse(w, message, statusCode)
}

// setCORSHeaders sets CORS headers for cross-origin requests
func (h *ProxyHandler) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-MCP-Server, Mcp-Session-Id")
}

// CheckAuth validates API key authentication (public method for cmd/proxy.go)
func (h *ProxyHandler) CheckAuth(r *http.Request) bool {
	return h.checkAuth(r)
}

// checkAuth validates API key authentication
func (h *ProxyHandler) checkAuth(r *http.Request) bool {
	if h.APIKey == "" {
		return true // No auth required
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	return token == h.APIKey
}

// setOAuthTokenOnConnection extracts OAuth token from request and sets it on connection
func (h *ProxyHandler) setOAuthTokenOnConnection(r *http.Request, conn *discovery.MCPConnection) {
	// Extract Bearer token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return // No auth header, skip
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return // Not a Bearer token, skip
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		return // Empty token, skip
	}

	// Set the token on all available connection types
	if conn.HTTPConnection != nil {
		conn.HTTPConnection.AuthToken = token
		h.Logger.Debug("Set OAuth token on HTTP connection for %s", conn.Name)
	}
	if conn.SSEConnection != nil {
		conn.SSEConnection.AuthToken = token
		h.Logger.Debug("Set OAuth token on SSE connection for %s", conn.Name)
	}
	if conn.StreamableHTTPConnection != nil {
		conn.StreamableHTTPConnection.AuthToken = token
		h.Logger.Debug("Set OAuth token on Streamable HTTP connection for %s", conn.Name)
	}
}

// writeErrorResponse writes a JSON-RPC error response
func (h *ProxyHandler) writeErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	h.setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"error": map[string]interface{}{
			"code":    statusCode,
			"message": message,
		},
		"id": h.getNextRequestID(),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.Logger.Warning("Failed to encode response: %v", err)
	}
}

// sendMCPError sends an MCP-formatted error response
func (h *ProxyHandler) sendMCPError(w http.ResponseWriter, id interface{}, code int, message string, optionalData ...map[string]interface{}) {
	h.setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // MCP errors use 200 with error in body

	errorBody := map[string]interface{}{
		"code":    code,
		"message": message,
	}

	// Add optional data if provided
	if len(optionalData) > 0 {
		for key, value := range optionalData[0] {
			errorBody[key] = value
		}
	}

	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"error":   errorBody,
		"id":      id,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.Logger.Warning("Failed to encode response: %v", err)
	}
}
