// internal/discovery/service_discovery.go
package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/rest"

	"github.com/phildougherty/m8e/internal/logging"
)

// ServiceEndpoint represents a discovered MCP service endpoint
type ServiceEndpoint struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	URL             string            `json:"url"`
	Endpoint        string            `json:"endpoint"` // For compatibility with tests
	Protocol        string            `json:"protocol"`
	Port            int32             `json:"port"`
	Capabilities    []string          `json:"capabilities"`
	Labels          map[string]string `json:"labels"`
	LastDiscovered  time.Time         `json:"lastDiscovered"`
	HealthStatus    string            `json:"healthStatus"`
	ServiceType     string            `json:"serviceType"`
}

// ServiceDiscoveryEvent represents a service discovery event
type ServiceDiscoveryEvent struct {
	Type     string          `json:"type"` // "ADDED", "MODIFIED", "DELETED"
	Service  ServiceEndpoint `json:"service"`
}

// ServiceDiscoveryHandler handles service discovery events
type ServiceDiscoveryHandler interface {
	OnServiceAdded(endpoint ServiceEndpoint)
	OnServiceModified(endpoint ServiceEndpoint)
	OnServiceDeleted(serviceName, namespace string)
}

// K8sServiceDiscovery provides Kubernetes-native service discovery for MCP servers
type K8sServiceDiscovery struct {
	Client          kubernetes.Interface // Public for testing
	Namespace       string               // Public for testing
	Logger          *logging.Logger      // Public for testing
	client          kubernetes.Interface
	namespace       string
	logger          *logging.Logger
	handlers        []ServiceDiscoveryHandler
	serviceInformer cache.SharedIndexInformer
	endpointInformer cache.SharedIndexInformer
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.RWMutex
	services        map[string]ServiceEndpoint
	running         bool
}

// NewK8sServiceDiscovery creates a new Kubernetes service discovery instance
func NewK8sServiceDiscovery(namespace string, logger *logging.Logger) (*K8sServiceDiscovery, error) {
	if namespace == "" {
		namespace = "default"
	}
	
	if logger == nil {
		logger = logging.NewLogger("info")
	}

	// Create Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	sd := &K8sServiceDiscovery{
		client:    client,
		namespace: namespace,
		logger:    logger,
		handlers:  make([]ServiceDiscoveryHandler, 0),
		ctx:       ctx,
		cancel:    cancel,
		services:  make(map[string]ServiceEndpoint),
	}

	if err := sd.setupInformers(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to setup informers: %w", err)
	}

	return sd, nil
}

// setupInformers creates and configures Kubernetes informers for services and endpoints
func (sd *K8sServiceDiscovery) setupInformers() error {
	// Service informer with label selector for MCP services
	serviceListWatcher := cache.NewListWatchFromClient(
		sd.client.CoreV1().RESTClient(),
		"services",
		sd.namespace,
		fields.Everything(),
	)

	sd.serviceInformer = cache.NewSharedIndexInformer(
		serviceListWatcher,
		&corev1.Service{},
		time.Minute*10, // Resync period
		cache.Indexers{},
	)

	// Add event handlers for services
	sd.serviceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if service, ok := obj.(*corev1.Service); ok {
				sd.handleServiceAdded(service)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if service, ok := newObj.(*corev1.Service); ok {
				sd.handleServiceModified(service)
			}
		},
		DeleteFunc: func(obj interface{}) {
			if service, ok := obj.(*corev1.Service); ok {
				sd.handleServiceDeleted(service)
			}
		},
	})

	// Endpoint informer for getting actual pod IPs
	endpointListWatcher := cache.NewListWatchFromClient(
		sd.client.CoreV1().RESTClient(),
		"endpoints",
		sd.namespace,
		fields.Everything(),
	)

	sd.endpointInformer = cache.NewSharedIndexInformer(
		endpointListWatcher,
		&corev1.Endpoints{},
		time.Minute*5, // Resync period
		cache.Indexers{},
	)

	return nil
}

// AddHandler adds a service discovery event handler
func (sd *K8sServiceDiscovery) AddHandler(handler ServiceDiscoveryHandler) {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	sd.handlers = append(sd.handlers, handler)
}

// Start begins the service discovery process
func (sd *K8sServiceDiscovery) Start() error {
	sd.mu.Lock()
	if sd.running {
		sd.mu.Unlock()
		return fmt.Errorf("service discovery is already running")
	}
	sd.running = true
	sd.mu.Unlock()

	sd.logger.Info("Starting Kubernetes service discovery")

	// Start informers
	go sd.serviceInformer.Run(sd.ctx.Done())
	go sd.endpointInformer.Run(sd.ctx.Done())

	// Wait for informers to sync
	if !cache.WaitForCacheSync(sd.ctx.Done(), sd.serviceInformer.HasSynced, sd.endpointInformer.HasSynced) {
		return fmt.Errorf("failed to sync informers")
	}

	sd.logger.Info("Service discovery informers synced successfully")

	return nil
}

// Stop stops the service discovery process
func (sd *K8sServiceDiscovery) Stop() {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if !sd.running {
		return
	}

	sd.logger.Info("Stopping Kubernetes service discovery")
	sd.cancel()
	sd.running = false
}

// GetServices returns all currently discovered services
func (sd *K8sServiceDiscovery) GetServices() []ServiceEndpoint {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	services := make([]ServiceEndpoint, 0, len(sd.services))
	for _, service := range sd.services {
		services = append(services, service)
	}
	return services
}

// GetService returns a specific service by name
func (sd *K8sServiceDiscovery) GetService(name string) (ServiceEndpoint, bool) {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	service, exists := sd.services[name]
	return service, exists
}

// handleServiceAdded processes service addition events
func (sd *K8sServiceDiscovery) handleServiceAdded(service *corev1.Service) {
	endpoint := sd.buildServiceEndpoint(service)
	if endpoint == nil {
		return
	}

	sd.mu.Lock()
	sd.services[endpoint.Name] = *endpoint
	sd.mu.Unlock()

	sd.logger.Info("Discovered MCP service: %s (protocol: %s, url: %s)", 
		endpoint.Name, endpoint.Protocol, endpoint.URL)

	// Notify handlers
	for _, handler := range sd.handlers {
		handler.OnServiceAdded(*endpoint)
	}
}

// handleServiceModified processes service modification events
func (sd *K8sServiceDiscovery) handleServiceModified(service *corev1.Service) {
	endpoint := sd.buildServiceEndpoint(service)
	if endpoint == nil {
		return
	}

	sd.mu.Lock()
	sd.services[endpoint.Name] = *endpoint
	sd.mu.Unlock()

	sd.logger.Info("Updated MCP service: %s", endpoint.Name)

	// Notify handlers
	for _, handler := range sd.handlers {
		handler.OnServiceModified(*endpoint)
	}
}

// handleServiceDeleted processes service deletion events
func (sd *K8sServiceDiscovery) handleServiceDeleted(service *corev1.Service) {
	serviceName := sd.getServiceName(service)
	if serviceName == "" {
		return
	}

	sd.mu.Lock()
	delete(sd.services, serviceName)
	sd.mu.Unlock()

	sd.logger.Info("Removed MCP service: %s", serviceName)

	// Notify handlers
	for _, handler := range sd.handlers {
		handler.OnServiceDeleted(serviceName, service.Namespace)
	}
}

// buildServiceEndpoint creates a ServiceEndpoint from a Kubernetes service
func (sd *K8sServiceDiscovery) buildServiceEndpoint(service *corev1.Service) *ServiceEndpoint {
	// Check if this is an MCP service
	if service.Labels["mcp.matey.ai/role"] != "server" {
		return nil
	}

	protocol := service.Labels["mcp.matey.ai/protocol"]
	if protocol == "" {
		protocol = "http" // default protocol
	}

	serviceName := sd.getServiceName(service)
	if serviceName == "" {
		return nil
	}

	// Determine port
	var port int32
	if len(service.Spec.Ports) > 0 {
		port = service.Spec.Ports[0].Port
	}

	// Build cluster DNS URL
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", 
		service.Name, service.Namespace, port)

	// Build cluster DNS endpoint (without protocol prefix)
	endpoint := fmt.Sprintf("%s.%s.svc.cluster.local:%d", 
		service.Name, service.Namespace, port)

	// Extract capabilities from labels
	capabilities := []string{}
	if caps, exists := service.Labels["mcp.matey.ai/capabilities"]; exists {
		// Parse dot-separated capabilities (set by controller)
		capabilities = parseCapabilities(caps)
	}

	return &ServiceEndpoint{
		Name:           serviceName,
		Namespace:      service.Namespace,
		URL:            url,
		Endpoint:       endpoint,
		Protocol:       protocol,
		Port:           port,
		Capabilities:   capabilities,
		Labels:         service.Labels,
		LastDiscovered: time.Now(),
		HealthStatus:   "unknown",
		ServiceType:    string(service.Spec.Type),
	}
}

// getServiceName extracts the MCP server name from service labels or metadata
func (sd *K8sServiceDiscovery) getServiceName(service *corev1.Service) string {
	// First try the explicit server name label
	if serverName, exists := service.Labels["mcp.matey.ai/server-name"]; exists {
		return serverName
	}

	// Fall back to service name
	return service.Name
}

// parseCapabilities parses dot-separated capabilities string
func parseCapabilities(caps string) []string {
	if caps == "" {
		return []string{}
	}

	// Split by dots (as used by controller)
	parts := strings.Split(caps, ".")
	capabilities := []string{}
	for _, part := range parts {
		if part != "" {
			capabilities = append(capabilities, part)
		}
	}
	return capabilities
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && 
		(s == substr || 
		 s[:len(substr)] == substr && s[len(substr)] == ',' ||
		 s[len(s)-len(substr):] == substr && s[len(s)-len(substr)-1] == ',' ||
		 fmt.Sprintf(",%s,", substr) != "" && fmt.Sprintf(",%s,", s) != "")
}

// DiscoverMCPServers performs an immediate discovery of all MCP servers
func (sd *K8sServiceDiscovery) DiscoverMCPServers() ([]ServiceEndpoint, error) {
	services, err := sd.client.CoreV1().Services(sd.namespace).List(sd.ctx, metav1.ListOptions{
		LabelSelector: "mcp.matey.ai/role=server",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	endpoints := make([]ServiceEndpoint, 0, len(services.Items))
	for _, service := range services.Items {
		if endpoint := sd.buildServiceEndpoint(&service); endpoint != nil {
			endpoints = append(endpoints, *endpoint)
		}
	}

	sd.logger.Info("Discovered %d MCP services", len(endpoints))
	return endpoints, nil
}

// DiscoverMCPServices performs an immediate discovery of all MCP services
// This is an alias for DiscoverMCPServers for API compatibility
func (sd *K8sServiceDiscovery) DiscoverMCPServices(ctx context.Context) ([]ServiceEndpoint, error) {
	// Use public Client if available (for testing), otherwise use private client
	client := sd.Client
	if client == nil {
		client = sd.client
	}
	
	namespace := sd.Namespace
	if namespace == "" {
		namespace = sd.namespace
	}
	
	services, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "mcp.matey.ai/role=server",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	endpoints := make([]ServiceEndpoint, 0, len(services.Items))
	for _, service := range services.Items {
		if endpoint := sd.buildServiceEndpoint(&service); endpoint != nil {
			endpoints = append(endpoints, *endpoint)
		}
	}

	return endpoints, nil
}

// DiscoverMCPServicesByProtocol discovers MCP services filtered by protocol
func (sd *K8sServiceDiscovery) DiscoverMCPServicesByProtocol(ctx context.Context, protocol string) ([]ServiceEndpoint, error) {
	client := sd.Client
	if client == nil {
		client = sd.client
	}
	
	namespace := sd.Namespace
	if namespace == "" {
		namespace = sd.namespace
	}
	
	services, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("mcp.matey.ai/role=server,mcp.matey.ai/protocol=%s", protocol),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	endpoints := make([]ServiceEndpoint, 0, len(services.Items))
	for _, service := range services.Items {
		if endpoint := sd.buildServiceEndpoint(&service); endpoint != nil {
			endpoints = append(endpoints, *endpoint)
		}
	}

	return endpoints, nil
}

// DiscoverMCPServicesByCapability discovers MCP services filtered by capability
func (sd *K8sServiceDiscovery) DiscoverMCPServicesByCapability(ctx context.Context, capability string) ([]ServiceEndpoint, error) {
	client := sd.Client
	if client == nil {
		client = sd.client
	}
	
	namespace := sd.Namespace
	if namespace == "" {
		namespace = sd.namespace
	}
	
	// Get all MCP services first, then filter by capability
	services, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "mcp.matey.ai/role=server",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	endpoints := make([]ServiceEndpoint, 0)
	for _, service := range services.Items {
		if endpoint := sd.buildServiceEndpoint(&service); endpoint != nil {
			// Check if this service has the required capability
			for _, cap := range endpoint.Capabilities {
				if cap == capability {
					endpoints = append(endpoints, *endpoint)
					break
				}
			}
		}
	}

	return endpoints, nil
}