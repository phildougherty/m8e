package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/discovery"
	"github.com/phildougherty/m8e/internal/protocol"
)

func (h *ProxyHandler) handleAPIReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Method not allowed - use POST",
		})

		return
	}

	h.logger.Info("Received reload request from %s", r.RemoteAddr)

	// Set JSON content type early
	w.Header().Set("Content-Type", "application/json")

	// Clear connection cache and reload config
	h.ConnectionMutex.Lock()
	oldHTTPConnCount := len(h.ServerConnections)
	h.ServerConnections = make(map[string]*discovery.MCPHTTPConnection)
	h.ConnectionMutex.Unlock()

	// STDIO connections no longer supported

	// Refresh tool cache
	h.toolCacheMu.Lock()
	h.cacheExpiry = time.Now() // Force cache refresh
	h.toolCache = make(map[string]string)
	h.toolCacheMu.Unlock()

	h.logger.Info("Proxy reload completed: cleared %d HTTP connections", oldHTTPConnCount)

	response := map[string]interface{}{
		"status":  "success",
		"message": "Proxy connections and cache reloaded",
		"cleared": map[string]int{
			"httpConnections": oldHTTPConnCount,
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode reload response: %v", err)
	}
}

func (h *ProxyHandler) handleAPIServers(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	serverList := make(map[string]map[string]interface{})

	// Get discovered services from K8s
	services := h.ServiceDiscovery.GetServices()
	connectionStatuses := h.ConnectionManager.GetConnectionStatus()

	for _, service := range services {
		name := service.Name
		
		// Get connection status
		connStatus, hasStatus := connectionStatuses[name]
		var containerStatus string
		
		if hasStatus && connStatus.Connected {
			containerStatus = "running"
		} else {
			containerStatus = "stopped"
		}

		serverInfo := map[string]interface{}{
			"name":               name,
			"containerStatus":    containerStatus,
			"configCapabilities": service.Capabilities,
			"configProtocol":     service.Protocol,
			"configHttpPort":     service.Port,
			"isContainer":        true, // All K8s services are containers
			"proxyTransportMode": strings.ToUpper(service.Protocol),
		}

		// Get connection details from discovery system
		if conn, err := h.ConnectionManager.GetConnection(name); err == nil {
			var connectionDetails map[string]interface{}
			
			switch conn.Protocol {
			case "http":
				if conn.HTTPConnection != nil {
					connectionDetails = map[string]interface{}{
						"proxyConnectionStatus": "connected",
						"mcpSessionID":          conn.HTTPConnection.SessionID,
						"lastUsedByProxy":       conn.HTTPConnection.LastUsed.Format(time.RFC3339Nano),
						"targetBaseURL":         conn.HTTPConnection.BaseURL,
						"healthy":               conn.HTTPConnection.Healthy,
					}
				}
			case "http-stream":
				if conn.StreamableHTTPConnection != nil {
					connectionDetails = map[string]interface{}{
						"proxyConnectionStatus": "connected",
						"mcpSessionID":          conn.StreamableHTTPConnection.SessionID,
						"lastUsedByProxy":       conn.StreamableHTTPConnection.LastUsed.Format(time.RFC3339Nano),
						"targetBaseURL":         conn.StreamableHTTPConnection.BaseURL,
						"healthy":               conn.StreamableHTTPConnection.Healthy,
					}
				}
			case "sse":
				if conn.SSEConnection != nil {
					connectionDetails = map[string]interface{}{
						"proxyConnectionStatus": "connected",
						"mcpSessionID":          conn.SSEConnection.SessionID,
						"lastUsedByProxy":       conn.SSEConnection.LastUsed.Format(time.RFC3339Nano),
						"targetBaseURL":         conn.SSEConnection.BaseURL,
						"healthy":               conn.SSEConnection.Healthy,
					}
				}
			}
			
			if connectionDetails != nil {
				serverInfo["connection"] = connectionDetails
			} else {
				serverInfo["connection"] = "Connection available but not initialized"
			}
		} else {
			serverInfo["connection"] = "No active connection to this server"
		}

		serverList[name] = serverInfo
	}

	if err := json.NewEncoder(w).Encode(serverList); err != nil {
		h.logger.Error("Failed to encode /api/servers response: %v", err)
	}
}

func (h *ProxyHandler) handleAPIStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get connection statistics from K8s discovery system
	connectionStatus := h.ConnectionManager.GetConnectionStatus()
	allConnections := h.ConnectionManager.GetAllConnections()
	
	totalServersDiscovered := len(allConnections)
	activeConnections := 0
	healthyConnections := 0
	
	for _, status := range connectionStatus {
		if status.Connected {
			activeConnections++
		}
	}
	
	for _, conn := range allConnections {
		if conn.Status == "connected" {
			healthyConnections++
		}
	}

	apiStatus := map[string]interface{}{
		"proxyStartTime":                 h.ProxyStarted.Format(time.RFC3339),
		"proxyUptime":                    time.Since(h.ProxyStarted).String(),
		"totalServersDiscovered":         totalServersDiscovered,
		"activeConnections":              activeConnections,
		"healthyConnections":             healthyConnections,
		"proxyTransportMode":             "HTTP",
		"mcpComposeVersion":              "dev",
		"mcpSpecVersionUsedByProxy":      protocol.MCPVersion,
		"standardMethodsSupported":       true,
		"standardHandlerInitialized":     h.standardHandler.IsInitialized(),
		"supportedCapabilities":          h.standardHandler.GetCapabilities(),
	}

	if err := json.NewEncoder(w).Encode(apiStatus); err != nil {
		h.logger.Error("Failed to encode /api/status response: %v", err)
	}
}

func (h *ProxyHandler) handleDiscoveryEndpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	serversForDiscovery := make([]map[string]interface{}, 0)

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	proxyExternalBaseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

	// Use K8s-native discovery for server information  
	allConnections := h.ConnectionManager.GetAllConnections()
	for serverName, conn := range allConnections {
		clientReachableEndpoint := fmt.Sprintf("%s/%s", proxyExternalBaseURL, serverName)
		var currentCapabilities interface{} = conn.Capabilities

		description := fmt.Sprintf("MCP %s server (via K8s discovery)", serverName)

		serverEntry := map[string]interface{}{
			"name":         serverName,
			"httpEndpoint": clientReachableEndpoint,
			"capabilities": currentCapabilities,
			"description":  description,
			"protocol":     conn.Protocol,
			"endpoint":     conn.Endpoint,
		}

		// Tools information is discovered dynamically from MCP servers

		serversForDiscovery = append(serversForDiscovery, serverEntry)
	}

	discoveryResponse := map[string]interface{}{
		"servers": serversForDiscovery,
	}

	if err := json.NewEncoder(w).Encode(discoveryResponse); err != nil {
		h.logger.Error("Failed to encode /api/discovery response: %v", err)
	}
}

func (h *ProxyHandler) handleConnectionsAPI(w http.ResponseWriter, _ *http.Request) {
	// Get connection information from K8s-native discovery system
	allConnections := h.ConnectionManager.GetAllConnections()
	connectionStatus := h.ConnectionManager.GetConnectionStatus()

	connectionsSnapshot := make(map[string]interface{})
	for name, conn := range allConnections {
		status := connectionStatus[name]
		
		connectionsSnapshot[name] = map[string]interface{}{
			"serverName":                 conn.Name,
			"targetEndpoint":             conn.Endpoint,
			"protocol":                   conn.Protocol,
			"status":                     conn.Status,
			"connected":                  status.Connected,
			"lastConnected":              status.LastConnected.Format(time.RFC3339Nano),
			"errorCount":                 status.ErrorCount,
			"lastUsedByProxy":            conn.LastUsed.Format(time.RFC3339Nano),
			"capabilities":               conn.Capabilities,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"activeConnectionsManagedByProxy": connectionsSnapshot,
		"totalActiveManagedConnections":   len(connectionsSnapshot),
		"timestamp":                       time.Now().Format(time.RFC3339Nano),
		"proxyToBackendTransportMode":     "K8s-native discovery (HTTP/SSE/Streamable-HTTP)",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode /api/connections response: %v", err)
	}
}

func (h *ProxyHandler) handleSubscriptionsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		// List all subscriptions
		clientID := h.getClientID(r)
		subscriptions := h.subscriptionManager.GetSubscriptions(clientID)
		response := map[string]interface{}{
			"subscriptions": subscriptions,
			"clientId":      clientID,
			"timestamp":     time.Now().Format(time.RFC3339),
		}
		_ = json.NewEncoder(w).Encode(response)

	case http.MethodDelete:
		// Cleanup expired subscriptions
		h.subscriptionManager.CleanupExpiredSubscriptions(constants.CleanupIntervalDefault)
		h.changeNotificationManager.CleanupInactiveSubscribers(constants.CleanupIntervalDefault)
		response := map[string]interface{}{
			"status":    "cleaned",
			"timestamp": time.Now().Format(time.RFC3339),
		}
		_ = json.NewEncoder(w).Encode(response)

	default:
		h.corsError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (h *ProxyHandler) handleNotificationsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		h.corsError(w, "Method Not Allowed", http.StatusMethodNotAllowed)

		return
	}

	toolSubscribers := h.changeNotificationManager.GetToolSubscribers()
	promptSubscribers := h.changeNotificationManager.GetPromptSubscribers()

	response := map[string]interface{}{
		"toolSubscribers":   len(toolSubscribers),
		"promptSubscribers": len(promptSubscribers),
		"subscribers": map[string]interface{}{
			"tools":   toolSubscribers,
			"prompts": promptSubscribers,
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	_ = json.NewEncoder(w).Encode(response)
}

func (h *ProxyHandler) handleOAuthStatus(w http.ResponseWriter, _ *http.Request) {
	if !h.oauthEnabled || h.authServer == nil {
		http.Error(w, "OAuth not enabled", http.StatusNotFound)

		return
	}

	accessTokens, refreshTokens, authCodes := h.authServer.GetTokenCount()
	response := map[string]interface{}{
		"oauth_enabled": true,
		"active_tokens": map[string]int{
			"access_tokens":  accessTokens,
			"refresh_tokens": refreshTokens,
			"auth_codes":     authCodes,
		},
		"issuer":           h.authServer.GetMetadata().Issuer,
		"scopes_supported": h.authServer.GetMetadata().ScopesSupported,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (h *ProxyHandler) handleOAuthClientsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	if !h.oauthEnabled || h.authServer == nil {
		http.Error(w, "OAuth not enabled", http.StatusNotFound)

		return
	}

	clients := h.authServer.GetAllClients()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(clients)
}

func (h *ProxyHandler) handleOAuthScopesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	if !h.oauthEnabled || h.authServer == nil {
		http.Error(w, "OAuth not enabled", http.StatusNotFound)

		return
	}

	scopes := []map[string]string{
		{"name": "mcp:tools", "description": "Access to MCP tools"},
		{"name": "mcp:resources", "description": "Access to MCP resources"},
		{"name": "mcp:prompts", "description": "Access to MCP prompts"},
		{"name": "mcp:*", "description": "Full access to all MCP capabilities"},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(scopes)
}

func (h *ProxyHandler) handleOAuthClientDelete(w http.ResponseWriter, r *http.Request) {
	if !h.oauthEnabled || h.authServer == nil {
		http.Error(w, "OAuth not enabled", http.StatusNotFound)

		return
	}

	// Extract client ID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/oauth/clients/")
	if path == "" {
		http.Error(w, "Client ID required", http.StatusBadRequest)

		return
	}

	// For now, just return success - implement actual deletion logic
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func (h *ProxyHandler) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	// This is just for testing - show the authorization code received
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")
	errorDescription := r.URL.Query().Get("error_description")

	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>OAuth Callback - MCP Compose</title>
    <style>
        body { 
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; 
            max-width: 800px; margin: 50px auto; padding: 20px; 
            background: #f5f5f5;
        }
        .result-box { 
            border: 1px solid #ddd; padding: 30px; border-radius: 8px; 
            background: white; box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .success { 
            border-left: 4px solid #28a745; 
        }
        .error { 
            border-left: 4px solid #dc3545; 
        }
        code { 
            background: #f8f9fa; padding: 4px 8px; border-radius: 4px; 
            font-family: 'Monaco', 'Consolas', monospace; font-size: 14px;
            word-break: break-all;
        }
        .field { margin: 15px 0; }
        .field strong { color: #333; }
        .test-section {
            margin-top: 30px; padding: 20px; background: #f8f9fa; 
            border-radius: 6px; border: 1px solid #e9ecef;
        }
        .back-link { 
            margin-top: 20px; 
        }
        .back-link a { 
            color: #007bff; text-decoration: none; 
        }
        .back-link a:hover { 
            text-decoration: underline; 
        }
        .copy-btn {
            background: #007bff; color: white; border: none; 
            padding: 5px 10px; border-radius: 3px; margin-left: 10px;
            cursor: pointer; font-size: 12px;
        }
        .copy-btn:hover { background: #0056b3; }
    </style>
    <script>
        function copyToClipboard(text) {
            navigator.clipboard.writeText(text).then(function() {
                event.target.textContent = 'Copied!';
                setTimeout(() => {
                    event.target.textContent = 'Copy';
                }, 2000);
            });
        }
    </script>
</head>
<body>
    <h2>OAuth Authorization Result</h2>
    %s
    <div class="back-link">
        <a href="/oauth/authorize?response_type=code&client_id=%s&redirect_uri=http%%3A%%2F%%2Fdesk%%3A3111%%2Foauth%%2Fcallback&scope=mcp:tools">← Try Authorization Again</a><br>
        <a href="/">← Back to Dashboard</a>
    </div>
</body>
</html>`, func() string {
		if errorParam != "" {

			return fmt.Sprintf(`<div class="result-box error">
                <h3>❌ Authorization Failed</h3>
                <div class="field"><strong>Error:</strong> %s</div>
                <div class="field"><strong>Description:</strong> %s</div>
                <div class="field"><strong>State:</strong> %s</div>
            </div>`, errorParam, errorDescription, state)
		} else if code != "" {

			return fmt.Sprintf(`<div class="result-box success">
                <h3>✅ Authorization Successful!</h3>
                <div class="field">
                    <strong>Authorization Code:</strong><br>
                    <code>%s</code>
                    <button class="copy-btn" onclick="copyToClipboard('%s')">Copy</button>
                </div>
                <div class="field"><strong>State:</strong> %s</div>
                <div class="test-section">
                    <h4>Next Steps:</h4>
                    <p>You can now exchange this authorization code for an access token using the <code>/oauth/token</code> endpoint.</p>
                    <p><strong>Test with cURL:</strong></p>
                    <pre><code style="display:block; padding:10px; background:#e9ecef;">curl -X POST http://desk:9876/oauth/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=authorization_code&code=%s&client_id=your_client_id&redirect_uri=http://desk:3111/oauth/callback"</code></pre>
                </div>
            </div>`, code, code, state, code)
		} else {

			return `<div class="result-box error">
                <h3>❓ Unexpected Response</h3>
                <p>No authorization code or error received.</p>
            </div>`
		}
	}(), r.URL.Query().Get("client_id"))

	w.Header().Set("Content-Type", "text/html")
	if _, err := w.Write([]byte(html)); err != nil {
		h.logger.Error("Failed to write OAuth callback HTML: %v", err)
	}
}

func (h *ProxyHandler) handleServerOAuthConfig(w http.ResponseWriter, r *http.Request) {
	// Extract server name from the path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "api" || pathParts[1] != "servers" || pathParts[3] != "oauth" {
		http.Error(w, "Invalid path format", http.StatusBadRequest)

		return
	}
	serverName := pathParts[2]
	w.Header().Set("Content-Type", "application/json")

	// Use tagged switch instead of if-else chain
	switch r.Method {
	case http.MethodGet:
		// Return current OAuth config for server
		config := h.getServerOAuthConfig(serverName)
		_ = json.NewEncoder(w).Encode(config)
	case http.MethodPut:
		// Update OAuth config for server
		var config config.ServerOAuthConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)

			return
		}
		err := h.updateServerOAuthConfig(serverName, config)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *ProxyHandler) handleServerOAuthTest(w http.ResponseWriter, r *http.Request) {
	// Extract server name from the path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "api" || pathParts[1] != "servers" || pathParts[3] != "test-oauth" {
		http.Error(w, "Invalid path format", http.StatusBadRequest)

		return
	}
	serverName := pathParts[2]

	w.Header().Set("Content-Type", "application/json")

	// Allow both GET and POST for testing
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "Method not allowed - use GET or POST", http.StatusMethodNotAllowed)

		return
	}

	h.logger.Info("Testing OAuth access for server: %s", serverName)

	// Test OAuth access to this server
	result := h.testServerOAuth(serverName)

	h.logger.Info("OAuth test result for %s: success=%v", serverName, result["success"])

	if err := json.NewEncoder(w).Encode(result); err != nil {
		h.logger.Error("Failed to encode OAuth test response: %v", err)
	}
}

func (h *ProxyHandler) handleServerTokens(w http.ResponseWriter, r *http.Request) {
	// Extract server name from the path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "api" || pathParts[1] != "servers" || pathParts[3] != "tokens" {
		http.Error(w, "Invalid path format", http.StatusBadRequest)

		return
	}
	serverName := pathParts[2]

	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	// Get active tokens for this server
	tokens := h.getServerTokens(serverName)
	_ = json.NewEncoder(w).Encode(tokens)
}

// Supporting methods - add these to api_handlers.go

func (h *ProxyHandler) getServerOAuthConfig(serverName string) config.ServerOAuthConfig {
	// Default OAuth config for unified proxy
	return config.ServerOAuthConfig{
		Enabled:             h.oauthEnabled,
		AllowAPIKeyFallback: true,
		OptionalAuth:        false,
		AllowedClients:      []string{},
	}
}

func (h *ProxyHandler) updateServerOAuthConfig(serverName string, newConfig config.ServerOAuthConfig) error {
	// In the unified proxy, OAuth configuration is global
	// Server-specific OAuth configuration is not supported in K8s-native mode
	h.logger.Info("OAuth configuration is global in unified proxy mode - ignoring server-specific config for %s", serverName)
	return nil
}

func (h *ProxyHandler) testServerOAuth(serverName string) map[string]interface{} {
	result := map[string]interface{}{
		"server":        serverName,
		"oauth_enabled": false,
		"success":       false,
	}

	// Check if OAuth is enabled globally
	if !h.oauthEnabled || h.authServer == nil {
		result["error"] = "OAuth not enabled globally"

		return result
	}

	// Get server OAuth config
	serverOAuthConfig := h.getServerOAuthConfig(serverName)
	result["oauth_enabled"] = serverOAuthConfig.Enabled

	if !serverOAuthConfig.Enabled {
		result["success"] = true
		result["message"] = "Server does not require OAuth authentication"

		return result
	}

	// Test with a mock client credentials token
	testClientID := "HFakeCpMUQnRX_m5HJKamRjU_vufUnNbG4xWpmUyvzo" // Your default test client

	// Check if this test client is allowed
	if len(serverOAuthConfig.AllowedClients) > 0 {
		allowed := false
		for _, allowedClient := range serverOAuthConfig.AllowedClients {
			if allowedClient == testClientID {
				allowed = true

				break
			}
		}
		if !allowed {
			result["error"] = "Test client not in allowed clients list"
			result["available_clients"] = serverOAuthConfig.AllowedClients

			return result
		}
	}

	// Test if the test client exists
	if h.authServer != nil {
		testClient, exists := h.authServer.GetClient(testClientID)
		if !exists {
			result["error"] = "Test client not found in OAuth server"
			result["test_client_id"] = testClientID

			return result
		}

		// Check if client has required scope
		if serverOAuthConfig.RequiredScope != "" {
			clientHasScope := false
			if testClient.Scope == "mcp:*" {
				clientHasScope = true
			} else {
				clientScopes := strings.Fields(testClient.Scope)
				for _, scope := range clientScopes {
					if scope == serverOAuthConfig.RequiredScope || scope == "mcp:*" {
						clientHasScope = true

						break
					}
				}
			}

			if !clientHasScope {
				result["error"] = fmt.Sprintf("Test client does not have required scope: %s", serverOAuthConfig.RequiredScope)
				result["client_scopes"] = testClient.Scope

				return result
			}
		}
	}

	// Simulate successful OAuth test
	result["success"] = true
	result["message"] = "OAuth configuration appears valid"
	result["details"] = map[string]interface{}{
		"required_scope":         serverOAuthConfig.RequiredScope,
		"allow_api_key_fallback": serverOAuthConfig.AllowAPIKeyFallback,
		"optional_auth":          serverOAuthConfig.OptionalAuth,
		"allowed_clients_count":  len(serverOAuthConfig.AllowedClients),
		"test_client_id":         testClientID,
	}

	return result
}

func (h *ProxyHandler) getServerTokens(serverName string) map[string]interface{} {
	result := map[string]interface{}{
		"server": serverName,
		"tokens": []map[string]interface{}{},
	}

	if !h.oauthEnabled || h.authServer == nil {
		result["message"] = "OAuth not enabled"

		return result
	}

	// Get server OAuth config to check scope requirements
	serverOAuthConfig := h.getServerOAuthConfig(serverName)
	requiredScope := serverOAuthConfig.RequiredScope

	// Get all active tokens and filter by scope if needed
	if h.authServer != nil {
		allTokens := h.authServer.GetAllAccessTokens()
		var relevantTokens []map[string]interface{}

		for _, tokenInfo := range allTokens {
			// If server has a required scope, check if token has it
			if requiredScope != "" {
				tokenScopes := strings.Fields(tokenInfo.Scope)
				hasRequiredScope := false
				for _, scope := range tokenScopes {
					if scope == requiredScope || scope == "mcp:*" {
						hasRequiredScope = true

						break
					}
				}
				if !hasRequiredScope {

					continue // Skip this token
				}
			}

			// Add relevant token info (without the actual token value for security)
			relevantTokens = append(relevantTokens, map[string]interface{}{
				"client_id":  tokenInfo.ClientID,
				"user_id":    tokenInfo.UserID,
				"scope":      tokenInfo.Scope,
				"expires_at": tokenInfo.ExpiresAt.Format(time.RFC3339),
				"created_at": tokenInfo.CreatedAt.Format(time.RFC3339),
				"active":     !tokenInfo.Revoked && time.Now().Before(tokenInfo.ExpiresAt),
			})
		}

		result["tokens"] = relevantTokens
		result["total_relevant_tokens"] = len(relevantTokens)
	}

	result["server_config"] = map[string]interface{}{
		"oauth_enabled":   serverOAuthConfig.Enabled,
		"required_scope":  serverOAuthConfig.RequiredScope,
		"optional_auth":   serverOAuthConfig.OptionalAuth,
		"allowed_clients": serverOAuthConfig.AllowedClients,
	}

	return result
}

func (h *ProxyHandler) handleContainerAPI(w http.ResponseWriter, r *http.Request) {
	// Extract container name and action from path
	path := strings.TrimPrefix(r.URL.Path, "/api/containers/")
	parts := strings.Split(path, "/")
	if len(parts) < constants.ServerNameParts {
		http.Error(w, "Invalid path format", http.StatusBadRequest)

		return
	}

	containerName := parts[0]
	action := parts[1]

	h.logger.Info("MCP Proxy handling container %s for %s", action, containerName)

	switch action {
	case "logs":
		h.handleContainerLogs(w, r, containerName)
	case "stats":
		h.handleContainerStats(w, r, containerName)
	default:
		http.Error(w, "Unknown container action", http.StatusBadRequest)
	}
}

func (h *ProxyHandler) handleContainerLogs(w http.ResponseWriter, r *http.Request, containerName string) {
	// Get query parameters
	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "100"
	}

	follow := r.URL.Query().Get("follow") == "true"
	timestamps := r.URL.Query().Get("timestamps") == "true"
	since := r.URL.Query().Get("since")

	h.logger.Info("Getting logs for container: %s (tail: %s, follow: %t, timestamps: %t)",
		containerName, tail, follow, timestamps)

	// Build docker command
	args := []string{"logs"}
	if timestamps {
		args = append(args, "-t")
	}
	if tail != "" {
		args = append(args, "--tail", tail)
	}
	if since != "" {
		args = append(args, "--since", since)
	}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, containerName)

	h.logger.Debug("Executing: docker %v", args)

	if follow {
		h.streamContainerLogs(w, r, containerName, args)
	} else {
		h.getStaticContainerLogs(w, r, containerName, args)
	}
}

func (h *ProxyHandler) getStaticContainerLogs(w http.ResponseWriter, r *http.Request, containerName string, args []string) {
	ctx, cancel := context.WithTimeout(r.Context(), constants.HTTPRequestTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.Output()
	if err != nil {
		h.logger.Error("Docker logs command failed for %s: %v", containerName, err)
		http.Error(w, fmt.Sprintf("Failed to get logs: %v", err), http.StatusInternalServerError)

		return
	}

	// Parse output into lines
	lines := strings.Split(string(output), "\n")
	filteredLines := make([]string, 0) // Initialize as empty slice instead of nil
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			filteredLines = append(filteredLines, line)
		}
	}

	response := map[string]interface{}{
		"container": containerName,
		"logs":      filteredLines,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode logs response: %v", err)
	}
}

func (h *ProxyHandler) streamContainerLogs(w http.ResponseWriter, r *http.Request, containerName string, args []string) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)

		return
	}

	// Send initial connection event
	_, _ = fmt.Fprintf(w, "event: connected\n")
	_, _ = fmt.Fprintf(w, "data: {\"container\":\"%s\",\"message\":\"Log stream connected\"}\n\n", containerName)
	flusher.Flush()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_, _ = fmt.Fprintf(w, "event: error\n")
		_, _ = fmt.Fprintf(w, "data: {\"error\":\"Failed to create stdout pipe: %v\"}\n\n", err)
		flusher.Flush()

		return
	}

	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintf(w, "event: error\n")
		_, _ = fmt.Fprintf(w, "data: {\"error\":\"Failed to start command: %v\"}\n\n", err)
		flusher.Flush()

		return
	}

	// Stream stdout line by line
	scanner := bufio.NewScanner(stdout)
	lineCount := 0

	for scanner.Scan() {
		select {
		case <-ctx.Done():

			return
		default:
		}

		line := scanner.Text()
		lineCount++

		// Format log entry
		logEntry := map[string]interface{}{
			"line":      lineCount,
			"content":   line,
			"timestamp": time.Now().Format(time.RFC3339Nano),
		}

		// Detect log level
		content := strings.ToLower(line)
		if strings.Contains(content, "error") {
			logEntry["level"] = "error"
		} else if strings.Contains(content, "warn") {
			logEntry["level"] = "warning"
		} else if strings.Contains(content, "info") {
			logEntry["level"] = "info"
		} else if strings.Contains(content, "debug") {
			logEntry["level"] = "debug"
		} else {
			logEntry["level"] = "info"
		}

		jsonBytes, _ := json.Marshal(logEntry)

		// Send as SSE event
		_, _ = fmt.Fprintf(w, "event: log\n")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(jsonBytes))
		flusher.Flush()
	}

	if err := scanner.Err(); err != nil {
		_, _ = fmt.Fprintf(w, "event: error\n")
		_, _ = fmt.Fprintf(w, "data: {\"error\":\"Error reading logs: %v\"}\n\n", err)
		flusher.Flush()
	}

	if err := cmd.Wait(); err != nil {
		h.logger.Debug("Docker logs command finished with error: %v", err)
	}

	// Send completion event
	_, _ = fmt.Fprintf(w, "event: completed\n")
	_, _ = fmt.Fprintf(w, "data: {\"message\":\"Log stream completed\"}\n\n")
	flusher.Flush()
}

func (h *ProxyHandler) handleContainerStats(w http.ResponseWriter, r *http.Request, containerName string) {
	ctx, cancel := context.WithTimeout(r.Context(), constants.HTTPQuickTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format",
		"table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}",
		containerName)

	output, err := cmd.Output()
	if err != nil {
		h.logger.Error("Docker stats command failed for %s: %v", containerName, err)
		http.Error(w, fmt.Sprintf("Failed to get stats: %v", err), http.StatusInternalServerError)

		return
	}

	response := map[string]interface{}{
		"container": containerName,
		"stats":     string(output),
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// handleHealthCheck provides a public health check endpoint for Kubernetes probes
func (h *ProxyHandler) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	// Get discovery and connection information
	allConnections := h.ConnectionManager.GetAllConnections()
	connectionStatus := h.ConnectionManager.GetConnectionStatus()
	
	// Proxy is healthy if it's running - individual server connections are reported but don't affect health status
	healthy := true
	details := make(map[string]interface{})
	connectedCount := 0
	
	for serverName, status := range connectionStatus {
		details[serverName] = map[string]interface{}{
			"connected":   status.Connected,
			"error_count": status.ErrorCount,
		}
		if status.Connected {
			connectedCount++
		}
	}

	// Always return 200 if proxy is running
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"healthy":              healthy,
		"servers_discovered":   len(allConnections),
		"servers_connected":    connectedCount,
		"connection_details":   details,
		"timestamp":           time.Now().Format(time.RFC3339),
	})
}
