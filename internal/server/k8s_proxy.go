// internal/server/k8s_proxy.go
package server

import (
	"fmt"
	"sync"
	"time"

	"github.com/phildougherty/m8e/internal/auth"
	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/discovery"
	"github.com/phildougherty/m8e/internal/logging"
	"github.com/phildougherty/m8e/internal/observability"
	"github.com/phildougherty/m8e/internal/protocol"
)

// ProxyHandler is the K8s-native MCP proxy. It is a thin composition: HTTP/SSE
// routing lives in proxy_router.go, the MCP JSON-RPC machinery in the embedded
// *ProtocolBridge, tool discovery caching in *ToolCache, connection metrics in
// *connStatsTracker, request IDs in *idGenerator and ctx/cancel lifecycle in
// *proxyLifecycle. Auth helpers live in utils.go.
type ProxyHandler struct {
	// Embedded MCP protocol bridge - promotes the transport/session methods
	// (sendHTTPRequestWithSession, establishSSESession, ...) used by the
	// routing and tool-discovery files.
	*ProtocolBridge

	// Core K8s components. ServiceDiscovery is the ServiceRegistry interface
	// (not the concrete *K8sServiceDiscovery) so the proxy can be tested
	// against a mock registry.
	ServiceDiscovery  discovery.ServiceRegistry
	ConnectionManager *discovery.DynamicConnectionManager
	Config            *config.ComposeConfig
	Logger            *logging.Logger

	// Existing functionality maintained
	APIKey       string
	EnableAPI    bool
	ProxyStarted time.Time

	// Legacy connection management (for compatibility with existing API handlers)
	ServerConnections map[string]*discovery.MCPHTTPConnection
	ConnectionMutex   sync.RWMutex

	// Protocol managers - same as before
	subscriptionManager       *protocol.SubscriptionManager
	changeNotificationManager *protocol.ChangeNotificationManager
	standardHandler           *protocol.StandardMethodHandler

	// Authentication - same as before
	authServer     *auth.AuthorizationServer
	authMiddleware *auth.AuthenticationMiddleware
	resourceMeta   *auth.ResourceMetadataHandler
	oauthEnabled   bool

	// Extracted, focused collaborators
	ids       *idGenerator
	connStats *connStatsTracker
	toolCache *ToolCache
	lifecycle *proxyLifecycle
	openWebUI *openWebUIAdapter

	// metrics records proxy request/latency/connection observability. It is
	// always non-nil after construction (observability.Nop() when the caller
	// does not wire real metrics); every method is nil-safe regardless.
	metrics *observability.Metrics

	// activeChannels indexes in-flight clientChannels (one per active
	// streamable/SSE request) so that the proxy can route a client's reply to
	// a server-initiated request — which arrives as a *separate* HTTP POST —
	// back to the originating channel's pending wait.
	activeChannels *clientChannelRegistry
}

// NewProxyHandler creates a new proxy handler. metrics may be observability.Nop()
// for callers (and tests) that do not wire real observability; all *Metrics
// methods are nil-safe.
func NewProxyHandler(cfg *config.ComposeConfig, namespace, apiKey string, metrics *observability.Metrics) (*ProxyHandler, error) {
	lifecycle := newProxyLifecycle()

	logLevel := "info"
	if cfg != nil && cfg.Logging.Level != "" {
		logLevel = cfg.Logging.Level
	}
	logger := logging.NewLogger(logLevel)

	// Create service discovery
	serviceDiscovery, err := discovery.NewK8sServiceDiscovery(namespace, logger)
	if err != nil {
		lifecycle.shutdown()
		return nil, fmt.Errorf("failed to create service discovery: %w", err)
	}

	// Create connection manager
	connectionManager := discovery.NewDynamicConnectionManager(serviceDiscovery, logger)

	ids := &idGenerator{}
	connStats := newConnStatsTracker()

	handler := &ProxyHandler{
		ProtocolBridge:            newProtocolBridge(ids, connStats, logger),
		ServiceDiscovery:          serviceDiscovery,
		ConnectionManager:         connectionManager,
		Config:                    cfg,
		Logger:                    logger,
		APIKey:                    apiKey,
		EnableAPI:                 true,
		ProxyStarted:              time.Now(),
		ServerConnections:         make(map[string]*discovery.MCPHTTPConnection),
		subscriptionManager:       protocol.NewSubscriptionManager(),
		changeNotificationManager: protocol.NewChangeNotificationManager(),
		standardHandler:           protocol.NewStandardMethodHandler(protocol.ServerInfo{}, protocol.CapabilitiesOpts{}, logger),
		ids:                       ids,
		connStats:                 connStats,
		toolCache:                 newToolCache(),
		lifecycle:                 lifecycle,
		openWebUI:                 newOpenWebUIAdapter(logger),
		metrics:                   metrics,
		activeChannels:            newClientChannelRegistry(),
	}

	// Setup authentication if enabled
	if err := handler.setupAuthentication(); err != nil {
		lifecycle.shutdown()
		return nil, fmt.Errorf("failed to setup authentication: %w", err)
	}

	return handler, nil
}

// Start begins the proxy handler operation
func (h *ProxyHandler) Start() error {
	h.Logger.Info("Starting proxy handler")

	// Start service discovery
	if err := h.ServiceDiscovery.Start(); err != nil {
		return fmt.Errorf("failed to start service discovery: %w", err)
	}

	// Start connection manager
	if err := h.ConnectionManager.Start(); err != nil {
		return fmt.Errorf("failed to start connection manager: %w", err)
	}

	// Start tool discovery refresh
	h.lifecycle.goroutine(h.toolDiscoveryLoop)

	h.Logger.Info("Proxy handler started successfully")
	return nil
}

// Stop stops the proxy handler
func (h *ProxyHandler) Stop() {
	h.Logger.Info("Stopping proxy handler")

	h.lifecycle.shutdown()

	if h.ConnectionManager != nil {
		h.ConnectionManager.Stop()
	}

	if h.ServiceDiscovery != nil {
		h.ServiceDiscovery.Stop()
	}
}

// setupAuthentication configures authentication if enabled
func (h *ProxyHandler) setupAuthentication() error {
	if h.Config == nil {
		return nil
	}

	// Setup OAuth if enabled
	if h.Config.OAuth != nil && h.Config.OAuth.Enabled {
		h.oauthEnabled = true

		// Initialize OAuth components
		authServer, authMiddleware, resourceMeta := initializeOAuth(h.Config.OAuth, h.Logger)
		h.authServer = authServer
		h.authMiddleware = authMiddleware
		h.resourceMeta = resourceMeta

		// Register default OAuth clients
		h.registerDefaultOAuthClients()

		h.Logger.Info("OAuth initialized successfully")
	}

	// Setup resource metadata handler
	if h.Config.RBAC != nil && h.Config.RBAC.Enabled {
		// For K8s-native mode, we'd use RBAC from Kubernetes itself
		h.Logger.Info("RBAC configuration detected but using K8s-native RBAC")
	}

	return nil
}

// API Methods - matching the old proxy interface

// GetDiscoveredServers returns information about discovered servers
func (h *ProxyHandler) GetDiscoveredServers() []discovery.ServiceEndpoint {
	return h.ServiceDiscovery.GetServices()
}

// GetConnectionStatus returns the status of all connections
func (h *ProxyHandler) GetConnectionStatus() map[string]discovery.ConnectionStatus {
	return h.ConnectionManager.GetConnectionStatus()
}

// RefreshConnections triggers a refresh of service discovery and connections
func (h *ProxyHandler) RefreshConnections() error {
	h.Logger.Info("Refreshing service discovery and connections")

	// Service discovery is automatic in Kubernetes, but we can trigger
	// a manual discovery to get immediate results
	_, err := h.ServiceDiscovery.DiscoverMCPServers()
	if err != nil {
		h.Logger.Error("Failed to refresh service discovery: %v", err)
		return err
	}

	return nil
}

// GetProxyInfo returns information about the proxy
func (h *ProxyHandler) GetProxyInfo() map[string]interface{} {
	services := h.GetDiscoveredServers()
	connections := h.GetConnectionStatus()

	return map[string]interface{}{
		"type":               "kubernetes-native",
		"started":            h.ProxyStarted,
		"uptime":             time.Since(h.ProxyStarted).String(),
		"discovered_servers": len(services),
		"active_connections": len(connections),
		"oauth_enabled":      h.oauthEnabled,
		"api_enabled":        h.EnableAPI,
		"services":           services,
		"connections":        connections,
	}
}

// Request ID and connection-stats wrappers - kept so the rest of the server
// package keeps a stable call surface while the state lives in focused types.

// getNextRequestID returns the next request ID as an int.
func (h *ProxyHandler) getNextRequestID() int {
	return h.ids.nextInt()
}

// generateStringID returns the next request ID as a string.
func (h *ProxyHandler) generateStringID() string {
	return h.ids.nextString()
}

// updateConnectionStats records the outcome of a request against a server.
func (h *ProxyHandler) updateConnectionStats(serverName string, success bool) {
	h.connStats.record(serverName, success)
}

// Tool is the tool shape used for OpenAPI generation.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}
