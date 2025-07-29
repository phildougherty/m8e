package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/phildougherty/m8e/internal/auth"
	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/logging"
)

func initializeOAuth(oauthConfig *config.OAuthConfig, logger *logging.Logger) (*auth.AuthorizationServer, *auth.AuthenticationMiddleware, *auth.ResourceMetadataHandler) {
	// Use the issuer from config, with a sensible default for container environments
	defaultIssuer := "http://matey-http-proxy:9876"
	if oauthConfig.Issuer != "" {
		defaultIssuer = oauthConfig.Issuer
	}

	// Create authorization server config
	serverConfig := &auth.AuthorizationServerConfig{
		Issuer:                                 defaultIssuer, // Use config value or container-aware default
		AuthorizationEndpoint:                  defaultIssuer + "/oauth/authorize",
		TokenEndpoint:                          defaultIssuer + "/oauth/token",
		UserinfoEndpoint:                       defaultIssuer + "/oauth/userinfo",
		RevocationEndpoint:                     defaultIssuer + "/oauth/revoke",
		RegistrationEndpoint:                   defaultIssuer + "/oauth/register",
		ScopesSupported:                        []string{"mcp:*", "mcp:tools", "mcp:resources", "mcp:prompts"},
		ResponseTypesSupported:                 []string{"code"},
		GrantTypesSupported:                    []string{"authorization_code", "client_credentials", "refresh_token"},
		TokenEndpointAuthMethodsSupported:      []string{"client_secret_post", "client_secret_basic", "none"},
		RevocationEndpointAuthMethodsSupported: []string{"client_secret_post", "client_secret_basic", "none"},
		CodeChallengeMethodsSupported:          []string{"plain", "S256"},
	}

	// Apply any additional config overrides
	if len(oauthConfig.ScopesSupported) > 0 {
		serverConfig.ScopesSupported = oauthConfig.ScopesSupported
	}

	logger.Info("OAuth server initialized with issuer: %s", serverConfig.Issuer)

	authServer := auth.NewAuthorizationServer(serverConfig, logger)
	authMiddleware := auth.NewAuthenticationMiddleware(authServer)

	// Create resource metadata handler
	authServers := []string{serverConfig.Issuer}
	resourceMeta := auth.NewResourceMetadataHandler(authServers, serverConfig.ScopesSupported)

	return authServer, authMiddleware, resourceMeta
}

// authenticateRequest handles authentication for server requests
func (h *ProxyHandler) authenticateRequest(w http.ResponseWriter, r *http.Request, serverName string) bool {
	// Skip authentication for OPTIONS requests
	if r.Method == "OPTIONS" {

		return true
	}

	var authenticatedViaOAuth bool
	var authenticatedViaAPIKey bool
	var requiresAuth bool

	// Determine if authentication is required
	apiKeyToCheck := h.getAPIKeyToCheck()
	oauthRequired := h.oauthEnabled 
	apiKeyRequired := apiKeyToCheck != ""

	// Check if any authentication is required
	requiresAuth = oauthRequired || apiKeyRequired

	// If no authentication is configured, allow access
	if !requiresAuth {

		return true
	}

	// Extract token from Authorization header
	token := h.extractBearerToken(r)
	if token == "" {
		if requiresAuth {
			h.sendAuthenticationError(w, "missing_token", "Access token required")

			return false
		}

		return true // Allow if no auth required or optional auth
	}

	// Try OAuth authentication first (if enabled and configured)
	if h.oauthEnabled && h.authServer != nil {
		accessToken, err := h.validateOAuthToken(token)
		if err == nil && accessToken != nil {
			// OAuth token is valid
			authenticatedViaOAuth = true
			// Add OAuth context to request
			client, _ := h.authServer.GetClient(accessToken.ClientID)
			ctx := context.WithValue(r.Context(), auth.ClientContextKey, client)
			ctx = context.WithValue(ctx, auth.TokenContextKey, accessToken)
			ctx = context.WithValue(ctx, auth.UserContextKey, accessToken.UserID)
			ctx = context.WithValue(ctx, auth.ScopeContextKey, accessToken.Scope)
			ctx = context.WithValue(ctx, auth.AuthTypeContextKey, "oauth")
			*r = *r.WithContext(ctx)
			h.logger.Debug("Request authenticated via OAuth for server %s (scope: %s)", serverName, accessToken.Scope)

			return true
		}
		// OAuth validation failed, but don't return error yet - try API key fallback
	}

	// Try API key authentication if not authenticated via OAuth
	if !authenticatedViaOAuth && apiKeyToCheck != "" {
		if token == apiKeyToCheck {
			authenticatedViaAPIKey = true
			// Add API key context
			ctx := context.WithValue(r.Context(), auth.AuthTypeContextKey, "api_key")
			*r = *r.WithContext(ctx)
			h.logger.Debug("Request authenticated via API key for server %s", serverName)

			return true
		}
	}

	// Check if API key fallback is allowed for OAuth-configured servers
	if oauthRequired && !authenticatedViaOAuth {
		// Allow API key fallback by default for unified proxy
		allowAPIKey := true

		if !allowAPIKey {
			h.sendOAuthError(w, "invalid_token", "OAuth authentication required (API key not allowed)")

			return false
		}
	}

	// Authentication failed
	if requiresAuth && !authenticatedViaOAuth && !authenticatedViaAPIKey {
		if h.oauthEnabled {
			h.sendOAuthError(w, "invalid_token", "Invalid access token or API key")
		} else {
			h.sendAuthenticationError(w, "invalid_token", "Invalid API key")
		}

		return false
	}

	// Check if server requires authentication but none was provided
	if oauthRequired && !authenticatedViaOAuth && !authenticatedViaAPIKey && requiresAuth {
		h.sendOAuthError(w, "access_denied", "Authentication required for this server")

		return false
	}

	return true
}

// Helper methods for authentication
func (h *ProxyHandler) extractBearerToken(r *http.Request) string {
	if h.authMiddleware != nil {
		return h.authMiddleware.ExtractToken(r)
	}
	// Fallback if OAuth is not configured
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return ""
	}
	return parts[1]
}

func (h *ProxyHandler) validateOAuthToken(token string) (*auth.AccessToken, error) {
	if h.authServer == nil {

		return nil, fmt.Errorf("OAuth not enabled")
	}

	return h.authServer.ValidateAccessToken(token)
}

func (h *ProxyHandler) hasRequiredScope(tokenScope, requiredScope string) bool {
	if h.authServer == nil {

		return false
	}

	return h.authServer.HasScope(tokenScope, requiredScope)
}

func (h *ProxyHandler) getAPIKeyToCheck() string {
	var apiKeyToCheck string
	if h.APIKey != "" {
		apiKeyToCheck = h.APIKey
	}

	return apiKeyToCheck
}

func (h *ProxyHandler) sendOAuthError(w http.ResponseWriter, errorCode, description string) {
	if h.authMiddleware != nil {
		h.authMiddleware.SendUnauthorized(w, errorCode, description)
		return
	}
	// Fallback if OAuth is not configured
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="mcp-compose"`)
	w.WriteHeader(http.StatusUnauthorized)
	response := map[string]string{
		"error":             errorCode,
		"error_description": description,
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (h *ProxyHandler) sendAuthenticationError(w http.ResponseWriter, errorCode, description string) {
	if h.authMiddleware != nil {
		// Use the standard OAuth error response from middleware
		h.authMiddleware.SendUnauthorized(w, errorCode, description)
		return
	}
	// Fallback if OAuth is not configured
	w.Header().Set("Content-Type", "application/json")
	if errorCode == "missing_token" {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	w.WriteHeader(http.StatusUnauthorized)
	response := map[string]string{
		"error":             errorCode,
		"error_description": description,
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (h *ProxyHandler) registerDefaultOAuthClients() {
	if !h.oauthEnabled || h.authServer == nil {

		return
	}

	// Register default test client with both proxy and dashboard callbacks
	testClientConfig := &auth.OAuthConfig{
		ClientID:     "HFakeCpMUQnRX_m5HJKamRjU_vufUnNbG4xWpmUyvzo",
		ClientSecret: "test-secret-123",
		RedirectURIs: []string{
			"http://desk:3111/oauth/callback",           // Dashboard callback
			h.Config.GetProxyURL() + "/oauth/callback",  // External proxy callback
			"http://desk:9876/oauth/callback",           // Proxy callback (fallback)
		},
		GrantTypes:        []string{"authorization_code", "client_credentials", "refresh_token"},
		ResponseTypes:     []string{"code"},
		Scope:             "mcp:* mcp:tools mcp:resources mcp:prompts",
		ClientName:        "Testing",
		TokenEndpointAuth: "client_secret_post",
	}

	_, err := h.authServer.RegisterClient(testClientConfig)
	if err != nil {
		h.logger.Warning("Failed to register default test client: %v", err)
	} else {
		h.logger.Info("Registered default OAuth test client with dashboard callback support")
	}

	// Register any clients from config - using unified proxy, no Manager config needed
	if h.Config != nil && h.Config.OAuthClients != nil {
		for clientID, clientConfig := range h.Config.OAuthClients {
			// Handle nullable client secret
			var clientSecret string
			if clientConfig.ClientSecret != nil {
				clientSecret = *clientConfig.ClientSecret
			}

			oauthConfig := &auth.OAuthConfig{
				ClientID:          clientConfig.ClientID,
				ClientSecret:      clientSecret,
				RedirectURIs:      clientConfig.RedirectURIs,
				GrantTypes:        clientConfig.GrantTypes,
				ResponseTypes:     []string{"code"}, // Default response type for OAuth 2.1
				Scope:             strings.Join(clientConfig.Scopes, " "),
				ClientName:        clientConfig.Name,
				TokenEndpointAuth: "none", // Public client default
			}

			// Set appropriate auth method based on client type
			if !clientConfig.PublicClient && clientConfig.ClientSecret != nil {
				oauthConfig.TokenEndpointAuth = "client_secret_post"
			}

			_, err := h.authServer.RegisterClient(oauthConfig)
			if err != nil {
				h.logger.Warning("Failed to register OAuth client %s: %v", clientID, err)
			} else {
				h.logger.Info("Registered OAuth client: %s (public: %v)", clientID, clientConfig.PublicClient)
			}
		}
	}
}
