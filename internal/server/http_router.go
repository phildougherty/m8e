package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/protocol"
)

// MCPRequest, MCPResponse, MCPError structs (standard JSON-RPC definitions)
type MCPRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	h.logger.Info("Request: %s %s from %s (User-Agent: %s)", r.Method, r.URL.Path, r.RemoteAddr, r.Header.Get("User-Agent"))

	// CORS Headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, Mcp-Session-Id, X-Client-ID, X-MCP-Capabilities, X-Supports-Notifications")
	w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id, Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)

		return
	}

	path := strings.TrimSuffix(r.URL.Path, "/")
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", constants.URLPathParts)

	// Handle OAuth endpoints FIRST - these should NOT require API key authentication
	if h.oauthEnabled && h.authServer != nil {
		if h.handleOAuthEndpoints(w, r, path) {

			return
		}
	}

	// Handle global OpenAPI spec BEFORE authentication - this should be public
	if path == "/openapi.json" {
		h.handleOpenAPISpec(w, r)
		h.logger.Debug("Processed global OpenAPI spec %s %s in %v", r.Method, r.URL.Path, time.Since(start))
		return
	}

	// Handle health check BEFORE authentication - this should be public for K8s probes
	if path == "/health" {
		h.handleHealthCheck(w, r)
		h.logger.Debug("Processed health check %s %s in %v", r.Method, r.URL.Path, time.Since(start))
		return
	}

	// Handle discovery endpoint BEFORE authentication - this should be public for Claude Code
	if path == "/api/discovery" {
		h.handleDiscoveryEndpoint(w, r)
		h.logger.Debug("Processed discovery endpoint %s %s in %v", r.Method, r.URL.Path, time.Since(start))
		return
	}

	// Handle server-specific OpenAPI specs BEFORE authentication - these should be public
	if len(parts) >= 2 && parts[1] == "openapi.json" {
		serverName := parts[0]
		// Check if server exists in K8s-native discovery
		if _, err := h.ConnectionManager.GetConnection(serverName); err == nil {
			h.handleServerOpenAPISpec(w, r, serverName)
			h.logger.Debug("Processed server OpenAPI spec %s %s in %v", r.Method, r.URL.Path, time.Since(start))
			return
		}
	}

	// Handle server-specific docs BEFORE authentication - these should be public
	if len(parts) >= 2 && parts[1] == "docs" {
		serverName := parts[0]
		// Check if server exists in K8s-native discovery
		if _, err := h.ConnectionManager.GetConnection(serverName); err == nil {
			h.handleServerDocs(w, r, serverName)
			h.logger.Debug("Processed server docs %s %s in %v", r.Method, r.URL.Path, time.Since(start))
			return
		}
	}

	// Handle OAuth well-known endpoints BEFORE authentication - these should be public
	if path == "/.well-known/oauth-authorization-server" {
		if h.authServer != nil {
			h.authServer.HandleDiscovery(w, r)
		} else {
			http.Error(w, "OAuth not configured", http.StatusNotFound)
		}
		h.logger.Debug("Processed OAuth discovery %s %s in %v", r.Method, r.URL.Path, time.Since(start))
		return
	}

	if path == "/.well-known/oauth-protected-resource" {
		if h.resourceMeta != nil {
			h.resourceMeta.HandleProtectedResourceMetadata(w, r)
		} else {
			http.Error(w, "OAuth not configured", http.StatusNotFound)
		}
		h.logger.Debug("Processed OAuth protected resource metadata %s %s in %v", r.Method, r.URL.Path, time.Since(start))
		return
	}

	// Handle server-specific OAuth discovery endpoints
	if strings.HasPrefix(path, "/.well-known/oauth-authorization-server/") {
		if h.authServer != nil {
			h.authServer.HandleDiscovery(w, r)
		} else {
			http.Error(w, "OAuth not configured", http.StatusNotFound)
		}
		h.logger.Debug("Processed server-specific OAuth discovery %s %s in %v", r.Method, r.URL.Path, time.Since(start))
		return
	}

	// NOW do authentication check for other endpoints
	if !h.authenticateRequest(w, r, "api") {
		return
	}

	var handled bool
	if h.EnableAPI {
		handled = h.handleAPIEndpoints(w, r, path)
	}

	if handled {
		h.logger.Debug("Processed API request %s %s in %v", r.Method, r.URL.Path, time.Since(start))

		return
	}

	// CRITICAL FIX: Handle direct tool calls BEFORE server routing
	if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodPost {
		toolName := parts[0]
		// First check if it's a known tool using K8s-native approach
		if _, found := h.FindServerForTool(toolName); found {
			h.logger.Info("Handling direct tool call for: %s", toolName)
			h.handleDirectToolCall(w, r, toolName)
			h.logger.Debug("Processed direct tool call %s %s in %v", r.Method, r.URL.Path, time.Since(start))

			return
		}

		// If not a known tool, check if it's a server name using K8s-native approach
		if _, err := h.ConnectionManager.GetConnection(toolName); err == nil {
			h.logger.Info("Routing to server: %s", toolName)
			// This is a server, handle as server request
			goto handleServer
		}

		// Neither a tool nor a server
		h.logger.Warning("Unknown tool or server: %s", toolName)
		h.corsError(w, "Tool or server not found", http.StatusNotFound)

		return
	}

	if path == "/" {
		h.handleIndex(w, r)

		return
	}

handleServer:
	// Handle server routing using K8s-native connections
	if len(parts) > 0 && parts[0] != "api" {
		serverName := parts[0]
		if conn, err := h.ConnectionManager.GetConnection(serverName); err == nil {
			switch r.Method {
			case http.MethodPost:
				// Use the proper server forwarding implementation
				h.handleServerForward(w, r, serverName)
			case http.MethodDelete:
				// Use the proper server forwarding implementation for session termination
				h.handleServerForward(w, r, serverName)
			case http.MethodGet:
				// For GET requests, show connection info
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if err := json.NewEncoder(w).Encode(map[string]interface{}{
					"server":   serverName,
					"status":   conn.Status,
					"protocol": conn.Protocol,
					"url":      conn.Endpoint,
				}); err != nil {
					http.Error(w, "Failed to encode response", http.StatusInternalServerError)
				}
			default:
				h.logger.Warning("Method %s not allowed for /%s", r.Method, serverName)
				h.corsError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			}
		} else {
			h.logger.Warning("Requested server '%s' not found in config.", serverName)
			h.corsError(w, "Server Not Found", http.StatusNotFound)
		}
	} else {
		h.logger.Warning("Path not found: %s", r.URL.Path)
		h.corsError(w, "Not Found", http.StatusNotFound)
	}

	h.logger.Info("Processed request %s %s (%s) in %v", r.Method, r.URL.Path, path, time.Since(start))
}

func (h *ProxyHandler) handleOAuthEndpoints(w http.ResponseWriter, r *http.Request, path string) bool {
	switch path {
	case "/oauth/authorize":
		h.authServer.HandleAuthorize(w, r)

		return true
	case "/oauth/token":
		h.authServer.HandleToken(w, r)

		return true
	case "/oauth/userinfo": // Add this
		h.authServer.HandleUserInfo(w, r)

		return true
	case "/oauth/revoke": // Add this
		h.authServer.HandleRevoke(w, r)

		return true
	case "/oauth/register":
		h.authServer.HandleRegister(w, r)

		return true
	case "/oauth/callback":
		h.handleOAuthCallback(w, r)

		return true
	case "/api/oauth/status":
		h.handleOAuthStatus(w, r)

		return true
	case "/api/oauth/clients":
		h.handleOAuthClientsList(w, r)

		return true
	case "/api/oauth/scopes":
		h.handleOAuthScopesList(w, r)

		return true
	}

	// Handle OAuth client deletion (path starts with /api/oauth/clients/)
	if strings.HasPrefix(path, "/api/oauth/clients/") && r.Method == http.MethodDelete {
		h.handleOAuthClientDelete(w, r)

		return true
	}

	// Handle server-specific OAuth discovery endpoints (e.g., /.well-known/oauth-authorization-server/dexcom)
	if strings.HasPrefix(path, "/.well-known/oauth-authorization-server/") {
		// Claude Code expects server-specific discovery endpoints to return the same metadata
		h.authServer.HandleDiscovery(w, r)

		return true
	}

	return false
}

func (h *ProxyHandler) handleAPIEndpoints(w http.ResponseWriter, r *http.Request, path string) bool {
	switch path {
	case "/api/reload":
		h.handleAPIReload(w, r)

		return true
	case "/api/servers":
		h.handleAPIServers(w, r)

		return true
	case "/api/status":
		h.handleAPIStatus(w, r)

		return true
	case "/api/connections":
		h.handleConnectionsAPI(w, r)

		return true
	case "/api/subscriptions":
		h.handleSubscriptionsAPI(w, r)

		return true
	case "/api/notifications":
		h.handleNotificationsAPI(w, r)

		return true
	}

	// ADD CONTAINER ENDPOINTS HERE
	if strings.HasPrefix(path, "/api/containers/") {
		h.handleContainerAPI(w, r)

		return true
	}

	// Handle server-specific OAuth endpoints
	if strings.HasPrefix(path, "/api/servers/") {
		pathParts := strings.Split(strings.Trim(path, "/"), "/")
		if len(pathParts) >= constants.URLPathPartsExtended {
			switch pathParts[3] {
			case "oauth":
				h.handleServerOAuthConfig(w, r)

				return true
			case "test-oauth":
				h.handleServerOAuthTest(w, r)

				return true
			case "tokens":
				h.handleServerTokens(w, r)

				return true
			}
		}
	}

	return false
}

func (h *ProxyHandler) authenticateAPIRequest(w http.ResponseWriter, r *http.Request) bool {
	var apiKeyToCheck string
	if h.APIKey != "" {
		apiKeyToCheck = h.APIKey
	}

	if apiKeyToCheck != "" {
		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != apiKeyToCheck {
			h.logger.Warning("Unauthorized access attempt to %s from %s (API key mismatch)", r.URL.Path, r.RemoteAddr)
			h.corsError(w, "Unauthorized", http.StatusUnauthorized)

			return false
		}
	}

	return true
}



func (h *ProxyHandler) handleProxyStandardMethod(w http.ResponseWriter, _ *http.Request, requestPayload map[string]interface{}, reqIDVal interface{}, reqMethodVal string) {
	h.logger.Info("Handling proxy standard MCP method '%s'", reqMethodVal)
	var params json.RawMessage
	if requestPayload["params"] != nil {
		paramsBytes, marshalErr := json.Marshal(requestPayload["params"])
		if marshalErr != nil {
			h.sendMCPError(w, reqIDVal, protocol.InvalidParams, "Failed to marshal parameters")

			return
		}
		params = paramsBytes
	}

	// Handle standard method
	if strings.HasPrefix(reqMethodVal, "notifications/") {
		// Handle notification
		err := h.standardHandler.HandleStandardNotification(reqMethodVal, params)
		if err != nil {
			if mcpErr, ok := err.(*protocol.MCPError); ok {
				h.sendMCPError(w, reqIDVal, mcpErr.Code, mcpErr.Message, mcpErr.Data)
			} else {
				h.sendMCPError(w, reqIDVal, protocol.InternalError, err.Error())
			}

			return
		}
		// Notifications don't have responses
		w.WriteHeader(http.StatusOK)

		return
	} else {
		// Handle request method
		response, err := h.standardHandler.HandleStandardMethod(reqMethodVal, params, reqIDVal)
		if err != nil {
			if mcpErr, ok := err.(*protocol.MCPError); ok {
				h.sendMCPError(w, reqIDVal, mcpErr.Code, mcpErr.Message, mcpErr.Data)
			} else {
				h.sendMCPError(w, reqIDVal, protocol.InternalError, err.Error())
			}

			return
		}
		// Send successful response
		if err := json.NewEncoder(w).Encode(response); err != nil {
			h.logger.Error("Failed to encode standard method response: %v", err)
		}

		return
	}
}

func (h *ProxyHandler) handleHTTPServerRequestWithBody(w http.ResponseWriter, r *http.Request, serverName string, body []byte, reqIDVal interface{}, reqMethodVal string) {
	// Get connection from K8s-native discovery
	mcpConn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		h.logger.Error("Failed to get HTTP connection for %s: %v", serverName, err)
		h.sendMCPError(w, reqIDVal, -32002, fmt.Sprintf("Proxy cannot connect to server '%s'", serverName))
		return
	}
	
	// CRITICAL FIX: Extract and set OAuth token from request
	h.setOAuthTokenOnConnection(r, mcpConn)
	
	if mcpConn.HTTPConnection == nil {
		h.logger.Error("No HTTP connection available for server %s", serverName)
		h.sendMCPError(w, reqIDVal, -32002, fmt.Sprintf("No HTTP connection available for server '%s'", serverName))
		return
	}
	
	conn := mcpConn.HTTPConnection

	// Forward client's Mcp-Session-Id to the backend if present
	clientSessionID := r.Header.Get("Mcp-Session-Id")
	if clientSessionID != "" && conn.SessionID == "" {
		h.logger.Info("Using client-provided Mcp-Session-Id '%s' for backend request to %s", clientSessionID, serverName)
		conn.SessionID = clientSessionID
	} else if clientSessionID != "" && clientSessionID != conn.SessionID {
		h.logger.Warning("Client Mcp-Session-Id '%s' differs from proxy's stored session '%s' for %s. Using proxy's.", clientSessionID, conn.SessionID, serverName)
	}

	// Parse the pre-read body bytes as JSON
	var requestPayload map[string]interface{}
	if err := json.Unmarshal(body, &requestPayload); err != nil {
		h.sendMCPError(w, reqIDVal, -32700, "Invalid JSON in request body")
		return
	}

	// Use K8s-native HTTP request handling
	responsePayload, err := h.sendHTTPRequestWithSession(conn, conn.SessionID, requestPayload)
	if err != nil {
		errData := map[string]interface{}{"details": err.Error()}
		if conn != nil {
			errData["targetUrl"] = conn.BaseURL
		}
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") {
			h.sendMCPError(w, reqIDVal, -32001, fmt.Sprintf("Server '%s' is unreachable or did not respond in time", serverName), errData)
		} else {
			h.sendMCPError(w, reqIDVal, -32003, fmt.Sprintf("Error during MCP call to '%s'", serverName), errData)
		}

		return
	}

	// Relay Mcp-Session-Id from backend server's response
	if conn.SessionID != "" {
		w.Header().Set("Mcp-Session-Id", conn.SessionID)
	}

	if err := json.NewEncoder(w).Encode(responsePayload); err != nil {
		h.logger.Error("Failed to encode/send response for %s: %v", serverName, err)
	}
}

func (h *ProxyHandler) handleSSEServerRequest(w http.ResponseWriter, r *http.Request, serverName string, requestPayload map[string]interface{}, reqIDVal interface{}, reqMethodVal string) {
	// Get connection directly from K8s connection manager
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		h.logger.Error("Failed to get SSE connection for %s: %v", serverName, err)
		h.sendMCPError(w, reqIDVal, -32002, fmt.Sprintf("Proxy cannot connect to server '%s' via SSE", serverName))
		return
	}

	// CRITICAL FIX: Extract and set OAuth token from request
	h.setOAuthTokenOnConnection(r, conn)

	if conn.SSEConnection == nil {
		h.logger.Error("No SSE connection available for server %s", serverName)
		h.sendMCPError(w, reqIDVal, -32002, fmt.Sprintf("No SSE connection available for server '%s'", serverName))
		return
	}

	// Forward client's Mcp-Session-Id to the backend if present
	clientSessionID := r.Header.Get("Mcp-Session-Id")
	
	// Use the K8s-native SSE functionality
	responsePayload, err := h.sendOptimalSSERequest(serverName, requestPayload)
	if err != nil {
		errData := map[string]interface{}{
			"details": err.Error(),
			"targetEndpoint": conn.Endpoint,
		}
		h.sendMCPError(w, reqIDVal, -32003, fmt.Sprintf("Error during SSE call to '%s'", serverName), errData)
		return
	}

	// Set session ID header if available
	if clientSessionID != "" {
		w.Header().Set("Mcp-Session-Id", clientSessionID)
	}

	// Return the response as JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(responsePayload); err != nil {
		h.logger.Error("Failed to encode/send response for %s: %v", serverName, err)
	}
}

