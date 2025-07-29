// internal/server/k8s_manager.go
package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
)

// K8sServerInstance represents a system server instance
type K8sServerInstance struct {
	Name      string
	Config    config.ServerConfig
	Status    string
	StartTime time.Time
	ctx       context.Context
	cancel    context.CancelFunc
}

// K8sManager handles system server lifecycle operations
type K8sManager struct {
	config     *config.ComposeConfig
	servers    map[string]*K8sServerInstance
	logger     *logging.Logger
	k8sClient  client.Client
	namespace  string
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewK8sManager creates a new system server manager
func NewK8sManager(cfg *config.ComposeConfig, namespace string) (*K8sManager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	logLevel := "info"
	if cfg.Logging.Level != "" {
		logLevel = cfg.Logging.Level
	}

	logger := logging.NewLogger(logLevel)

	// Create Kubernetes client
	k8sClient, err := createK8sClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	if namespace == "" {
		namespace = "default"
	}

	ctx, cancel := context.WithCancel(context.Background())

	manager := &K8sManager{
		config:    cfg,
		servers:   make(map[string]*K8sServerInstance),
		logger:    logger,
		k8sClient: k8sClient,
		namespace: namespace,
		ctx:       ctx,
		cancel:    cancel,
	}

	// Initialize server instances
	for name, serverCfg := range cfg.Servers {
		instanceCtx, instanceCancel := context.WithCancel(ctx)

		manager.servers[name] = &K8sServerInstance{
			Name:   name,
			Config: serverCfg,
			Status: "stopped",
			ctx:    instanceCtx,
			cancel: instanceCancel,
		}

		logger.Info("Initialized K8s server instance '%s'", name)
	}

	logger.Info("K8s Manager initialized with %d servers", len(manager.servers))

	return manager, nil
}

// StartServer starts a server using Kubernetes MCPServer CRD
func (m *K8sManager) StartServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("K8S_MANAGER: StartServer called for '%s'", name)

	instance, ok := m.servers[name]
	if !ok {
		m.logger.Error("K8S_MANAGER: Server '%s' not found in configuration", name)
		return fmt.Errorf("server '%s' not found in configuration", name)
	}

	// Check if MCPServer already exists
	mcpServer := &crd.MCPServer{}
	err := m.k8sClient.Get(m.ctx, client.ObjectKey{
		Name:      name,
		Namespace: m.namespace,
	}, mcpServer)

	if err == nil {
		// Server exists, check status
		if mcpServer.Status.Phase == "Running" {
			m.logger.Info("K8S_MANAGER: Server '%s' already running", name)
			instance.Status = "running"
			return nil
		}
	}

	// Create or update MCPServer resource
	mcpServer = &crd.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: m.namespace,
		},
		Spec: crd.MCPServerSpec{
			Image:     instance.Config.Image,
			HttpPort:  int32(instance.Config.HttpPort),
			Protocol:  instance.Config.Protocol,
			Replicas:  &[]int32{1}[0],
		},
	}

	// Add resource limits if specified
	if instance.Config.Deploy.Resources.Limits.CPUs != "" || instance.Config.Deploy.Resources.Limits.Memory != "" {
		mcpServer.Spec.Resources = crd.ResourceRequirements{
			Limits: map[string]string{},
		}
		if instance.Config.Deploy.Resources.Limits.CPUs != "" {
			mcpServer.Spec.Resources.Limits["cpu"] = instance.Config.Deploy.Resources.Limits.CPUs
		}
		if instance.Config.Deploy.Resources.Limits.Memory != "" {
			mcpServer.Spec.Resources.Limits["memory"] = instance.Config.Deploy.Resources.Limits.Memory
		}
	}

	err = m.k8sClient.Create(m.ctx, mcpServer)
	if err != nil {
		return fmt.Errorf("failed to create MCPServer resource: %w", err)
	}

	instance.Status = "starting"
	instance.StartTime = time.Now()

	m.logger.Info("K8S_MANAGER: Created MCPServer resource for '%s'", name)
	return nil
}

// StopServer stops a server by deleting the MCPServer CRD
func (m *K8sManager) StopServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("K8S_MANAGER: StopServer called for '%s'", name)

	instance, ok := m.servers[name]
	if !ok {
		return fmt.Errorf("server '%s' not found", name)
	}

	// Delete MCPServer resource
	mcpServer := &crd.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: m.namespace,
		},
	}

	err := m.k8sClient.Delete(m.ctx, mcpServer)
	if err != nil {
		return fmt.Errorf("failed to delete MCPServer resource: %w", err)
	}

	instance.Status = "stopped"
	m.logger.Info("K8S_MANAGER: Deleted MCPServer resource for '%s'", name)
	return nil
}

// GetServerStatus returns the status of a server
func (m *K8sManager) GetServerStatus(name string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	instance, ok := m.servers[name]
	if !ok {
		return "not_found", fmt.Errorf("server '%s' not found", name)
	}

	// Check MCPServer status in Kubernetes
	mcpServer := &crd.MCPServer{}
	err := m.k8sClient.Get(m.ctx, client.ObjectKey{
		Name:      name,
		Namespace: m.namespace,
	}, mcpServer)

	if err != nil {
		instance.Status = "stopped"
		return "stopped", nil
	}

	// Update local status based on Kubernetes status
	switch mcpServer.Status.Phase {
	case "Running":
		instance.Status = "running"
	case "Pending", "Creating":
		instance.Status = "starting"
	case "Failed":
		instance.Status = "failed"
	default:
		instance.Status = "unknown"
	}

	return instance.Status, nil
}

// ListServers returns a list of all configured servers
func (m *K8sManager) ListServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	servers := make([]string, 0, len(m.servers))
	for name := range m.servers {
		servers = append(servers, name)
	}
	return servers
}

// Shutdown gracefully shuts down the manager
func (m *K8sManager) Shutdown() error {
	m.logger.Info("K8S_MANAGER: Shutting down...")

	m.cancel()
	m.wg.Wait()

	// Cancel all server contexts
	m.mu.Lock()
	for _, instance := range m.servers {
		instance.cancel()
	}
	m.mu.Unlock()

	m.logger.Info("K8S_MANAGER: Shutdown complete")
	return nil
}

// createK8sClient creates a Kubernetes client with CRD support
func createK8sClient() (client.Client, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	// Create the scheme with CRDs
	scheme := runtime.NewScheme()
	
	// Add core Kubernetes types
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add core v1 scheme: %w", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add apps v1 scheme: %w", err)
	}
	
	// Add our CRDs
	if err := crd.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add CRD scheme: %w", err)
	}

	// Create the client
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return k8sClient, nil
}