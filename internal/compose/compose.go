// internal/compose/k8s_composer.go
package compose

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/controllers"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
	"github.com/phildougherty/m8e/internal/memory"
	"github.com/phildougherty/m8e/internal/task_scheduler"
)

// K8sComposer orchestrates MCP services using Kubernetes resources
type K8sComposer struct {
	config     *config.ComposeConfig
	k8sClient  client.Client
	namespace  string
	logger     *logging.Logger
	mu         sync.RWMutex
	
	// Service managers
	memoryManager       *memory.K8sManager
	taskSchedulerManager *task_scheduler.K8sManager
	
	// Controller manager
	controllerManager   *controllers.ControllerManager
}

// NewK8sComposer creates a new composer instance
func NewK8sComposer(configPath string, namespace string) (*K8sComposer, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if namespace == "" {
		namespace = "default"
	}

	logger := logging.NewLogger("info")
	if cfg.Logging.Level != "" {
		logger = logging.NewLogger(cfg.Logging.Level)
	}

	// Create Kubernetes client
	k8sClient, err := createK8sClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create service managers
	memoryManager := memory.NewK8sManager(cfg, k8sClient, namespace)
	taskSchedulerManager := task_scheduler.NewK8sManager(cfg, k8sClient, namespace)

	return &K8sComposer{
		config:               cfg,
		k8sClient:            k8sClient,
		namespace:            namespace,
		logger:               logger,
		memoryManager:        memoryManager,
		taskSchedulerManager: taskSchedulerManager,
	}, nil
}

// Up starts all enabled services
func (c *K8sComposer) Up(serviceNames []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("Starting MCP services")
	
	// FIRST: Ensure controller manager is running
	if err := c.ensureControllerManagerRunning(); err != nil {
		return fmt.Errorf("failed to start controller manager: %w", err)
	}

	// If no specific services requested, start all enabled services
	if len(serviceNames) == 0 {
		return c.startAllEnabledServices()
	}

	// Start specific services
	return c.startSpecificServices(serviceNames)
}

// Down stops all services
func (c *K8sComposer) Down(serviceNames []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("Stopping MCP services")
	

	// If no specific services requested, stop all services
	if len(serviceNames) == 0 {
		return c.stopAllServices()
	}

	// Stop specific services
	return c.stopSpecificServices(serviceNames)
}

// Start starts specific services
func (c *K8sComposer) Start(serviceNames []string) error {
	if len(serviceNames) == 0 {
		return fmt.Errorf("no services specified")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.startSpecificServices(serviceNames)
}

// Stop stops specific services
func (c *K8sComposer) Stop(serviceNames []string) error {
	if len(serviceNames) == 0 {
		return fmt.Errorf("no services specified")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.stopSpecificServices(serviceNames)
}

// Restart restarts specific services or all services if none specified
func (c *K8sComposer) Restart(serviceNames []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If no services specified, restart all enabled services
	if len(serviceNames) == 0 {
		return c.restartAllServices()
	}

	// Restart specific services with progress indicators
	c.logger.Info("Restarting %d service(s): %s", len(serviceNames), strings.Join(serviceNames, ", "))

	for _, serviceName := range serviceNames {
		c.logger.Info("Restarting service: %s", serviceName)

		// Stop the service
		if err := c.stopSpecificServices([]string{serviceName}); err != nil {
			c.logger.Warning("Warning during stop of %s: %v", serviceName, err)
		}

		// Wait for cleanup
		time.Sleep(2 * time.Second)

		// Start the service
		if err := c.startSpecificServices([]string{serviceName}); err != nil {
			return fmt.Errorf("failed to restart %s: %w", serviceName, err)
		}

		c.logger.Info("Service restarted successfully: %s", serviceName)
	}

	c.logger.Info("All specified services restarted successfully")
	return nil
}

// restartAllServices restarts all enabled services
func (c *K8sComposer) restartAllServices() error {
	c.logger.Info("Restarting all Matey services")

	// Stop all services
	c.logger.Info("Stopping all services for restart")
	if err := c.stopAllServices(); err != nil {
		c.logger.Warning("Warning during full stop: %v", err)
	}

	// Wait for cleanup
	c.logger.Info("Waiting for cleanup...")
	time.Sleep(5 * time.Second)

	// Start all enabled services
	c.logger.Info("Starting all enabled services")
	if err := c.startAllEnabledServices(); err != nil {
		return fmt.Errorf("failed to restart all services: %w", err)
	}

	c.logger.Info("All services restarted successfully")
	return nil
}

// Status returns the status of all services
func (c *K8sComposer) Status() (*ComposeStatus, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := &ComposeStatus{
		Services: make(map[string]*ServiceStatus),
	}

	// Check memory service status
	if c.config.Memory.Enabled {
		memStatus, err := c.memoryManager.Status()
		if err != nil {
			memStatus = "error"
		}
		status.Services["memory"] = &ServiceStatus{
			Name:   "memory",
			Status: memStatus,
			Type:   "memory",
		}
	}

	// Check task scheduler status
	if c.config.TaskScheduler.Enabled {
		tsStatus, err := c.taskSchedulerManager.GetStatus()
		if err != nil {
			tsStatus = "error"
		}
		status.Services["task-scheduler"] = &ServiceStatus{
			Name:   "task-scheduler",
			Status: tsStatus,
			Type:   "task-scheduler",
		}
	}

	// Check MCP server statuses via Kubernetes API
	for serverName := range c.config.Servers {
		serverStatus := c.getMCPServerStatus(serverName)
		status.Services[serverName] = &ServiceStatus{
			Name:   serverName,
			Status: serverStatus,
			Type:   "mcp-server",
		}
	}

	return status, nil
}

// getMCPServerStatus gets the status of an MCP server via MCPServer CRD with deployment fallback
func (c *K8sComposer) getMCPServerStatus(serverName string) string {
	ctx := context.Background()
	
	// Use controller manager's client if available, otherwise fall back to k8sClient
	var clientToUse client.Client
	if c.controllerManager != nil && c.controllerManager.GetClient() != nil {
		clientToUse = c.controllerManager.GetClient()
	} else {
		clientToUse = c.k8sClient
	}
	
	// Try to get MCPServer CRD status first
	mcpServer := &crd.MCPServer{}
	err := clientToUse.Get(ctx, client.ObjectKey{
		Name:      serverName,
		Namespace: c.namespace,
	}, mcpServer)
	
	if err == nil {
		// MCPServer CRD exists, use its status
		switch mcpServer.Status.Phase {
		case crd.MCPServerPhaseRunning:
			return "running"
		case crd.MCPServerPhasePending:
			return "pending"
		case crd.MCPServerPhaseFailed:
			return "failed"
		default:
			return "unknown"
		}
	}
	
	// MCPServer CRD not found or error, fall back to deployment status
	if client.IgnoreNotFound(err) != nil {
		// Real error, not just not found
		return "error"
	}
	
	// Get the deployment for this server
	deployment := &appsv1.Deployment{}
	err = clientToUse.Get(ctx, client.ObjectKey{
		Name:      serverName,
		Namespace: c.namespace,
	}, deployment)
	
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return "not-found"
		}
		return "error"
	}

	// Check deployment status
	if deployment.Status.ReadyReplicas > 0 {
		return "running"
	}

	// Check if there are any pods and their status
	podList := &corev1.PodList{}
	err = clientToUse.List(ctx, podList, client.InNamespace(c.namespace), client.MatchingLabels{"app": serverName})
	if err != nil || len(podList.Items) == 0 {
		return "pending"
	}

	// Check first pod status
	pod := podList.Items[0]
	switch pod.Status.Phase {
	case corev1.PodRunning:
		// If pod is running but deployment says 0 ready replicas, it's still starting
		return "starting"
	case corev1.PodPending:
		return "pending"
	case corev1.PodFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// startAllEnabledServices starts all services that are enabled in config
func (c *K8sComposer) startAllEnabledServices() error {
	var errors []string

	// Start memory service if enabled
	if c.config.Memory.Enabled {
		c.logger.Info("Starting memory service")
		if err := c.memoryManager.Start(); err != nil {
			errors = append(errors, fmt.Sprintf("memory: %v", err))
		}
	}

	// Start task scheduler if enabled
	if c.config.TaskScheduler.Enabled {
		c.logger.Info("Starting task scheduler service")
		if err := c.taskSchedulerManager.Start(); err != nil {
			errors = append(errors, fmt.Sprintf("task-scheduler: %v", err))
		}
	}

	// Start all configured servers
	for serverName, serverConfig := range c.config.Servers {
		c.logger.Info("Starting server: %s", serverName)
		if err := c.startMCPServer(serverName, serverConfig); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", serverName, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to start services: %s", strings.Join(errors, ", "))
	}

	return nil
}

// startSpecificServices starts the specified services
func (c *K8sComposer) startSpecificServices(serviceNames []string) error {
	var errors []string

	for _, serviceName := range serviceNames {
		switch serviceName {
		case "memory":
			c.logger.Info("Starting memory service")
			if err := c.memoryManager.Start(); err != nil {
				errors = append(errors, fmt.Sprintf("memory: %v", err))
			}
		case "task-scheduler":
			c.logger.Info("Starting task scheduler service")
			if err := c.taskSchedulerManager.Start(); err != nil {
				errors = append(errors, fmt.Sprintf("task-scheduler: %v", err))
			}
		case "controller-manager":
			c.logger.Info("Starting controller manager")
			if err := c.ensureControllerManagerRunning(); err != nil {
				errors = append(errors, fmt.Sprintf("controller-manager: %v", err))
			}
		default:
			// Check if it's a configured server
			if serverConfig, exists := c.config.Servers[serviceName]; exists {
				c.logger.Info("Starting server: %s", serviceName)
				if err := c.startMCPServer(serviceName, serverConfig); err != nil {
					errors = append(errors, fmt.Sprintf("%s: %v", serviceName, err))
				}
			} else {
				errors = append(errors, fmt.Sprintf("unknown service: %s", serviceName))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to start services: %s", strings.Join(errors, ", "))
	}

	return nil
}

// stopAllServices stops all services
func (c *K8sComposer) stopAllServices() error {
	var errors []string

	// Stop all MCP servers first
	for serverName := range c.config.Servers {
		c.logger.Info("Stopping MCP server: %s", serverName)
		if err := c.stopMCPServer(serverName); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", serverName, err))
		}
	}

	// Stop memory service
	if c.config.Memory.Enabled {
		c.logger.Info("Stopping memory service")
		if err := c.memoryManager.Stop(); err != nil {
			errors = append(errors, fmt.Sprintf("memory: %v", err))
		}
	}

	// Stop task scheduler
	if c.config.TaskScheduler.Enabled {
		c.logger.Info("Stopping task scheduler service")
		if err := c.taskSchedulerManager.Stop(); err != nil {
			errors = append(errors, fmt.Sprintf("task-scheduler: %v", err))
		}
	}

	// Stop controller manager last
	if c.controllerManager != nil {
		c.logger.Info("Stopping controller manager")
		if err := c.controllerManager.Stop(); err != nil {
			errors = append(errors, fmt.Sprintf("controller-manager: %v", err))
		}
		c.controllerManager = nil
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to stop services: %s", strings.Join(errors, ", "))
	}

	return nil
}

// stopSpecificServices stops the specified services
func (c *K8sComposer) stopSpecificServices(serviceNames []string) error {
	var errors []string

	for _, serviceName := range serviceNames {
		switch serviceName {
		case "memory":
			c.logger.Info("Stopping memory service")
			if err := c.memoryManager.Stop(); err != nil {
				errors = append(errors, fmt.Sprintf("memory: %v", err))
			}
		case "task-scheduler":
			c.logger.Info("Stopping task scheduler service")
			if err := c.taskSchedulerManager.Stop(); err != nil {
				errors = append(errors, fmt.Sprintf("task-scheduler: %v", err))
			}
		case "controller-manager":
			c.logger.Info("Stopping controller manager")
			if c.controllerManager != nil {
				if err := c.controllerManager.Stop(); err != nil {
					errors = append(errors, fmt.Sprintf("controller-manager: %v", err))
				}
				c.controllerManager = nil
			}
		default:
			// Check if it's an MCP server defined in config
			if _, exists := c.config.Servers[serviceName]; exists {
				c.logger.Info("Stopping MCP server: %s", serviceName)
				if err := c.stopMCPServer(serviceName); err != nil {
					errors = append(errors, fmt.Sprintf("%s: %v", serviceName, err))
				}
			} else {
				errors = append(errors, fmt.Sprintf("unknown service: %s", serviceName))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to stop services: %s", strings.Join(errors, ", "))
	}

	return nil
}

// ensureControllerManagerRunning ensures the controller manager is running
func (c *K8sComposer) ensureControllerManagerRunning() error {
	// Check if we already have a controller manager running
	if c.controllerManager != nil && c.controllerManager.IsReady() {
		c.logger.Info("Controller manager already running")
		return nil
	}

	c.logger.Info("Starting Matey controller manager...")
	
	// Start the controller manager in the background
	cm, err := controllers.StartControllerManagerInBackground(c.namespace, c.config)
	if err != nil {
		return fmt.Errorf("failed to start controller manager: %w", err)
	}

	c.controllerManager = cm
	c.logger.Info("Controller manager started successfully")
	
	return nil
}

// createK8sClient creates a Kubernetes client
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

	// Create the scheme
	scheme := runtime.NewScheme()
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

// startMCPServer creates and deploys an MCPServer resource
func (c *K8sComposer) startMCPServer(name string, serverConfig config.ServerConfig) error {
	ctx := context.Background()
	
	// Convert server config to MCPServer CRD
	mcpServer := c.convertServerConfigToMCPServer(name, serverConfig)
	if mcpServer == nil {
		// Conversion failed - skip this server
		return nil
	}
	
	// Check if MCPServer already exists
	existingServer := &crd.MCPServer{}
	err := c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: c.namespace,
	}, existingServer)
	
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Server doesn't exist, create it
			c.logger.Info("Creating MCPServer resource: %s", name)
			if err := c.k8sClient.Create(ctx, mcpServer); err != nil {
				return fmt.Errorf("failed to create MCPServer %s: %w", name, err)
			}
		} else {
			return fmt.Errorf("failed to check if MCPServer %s exists: %w", name, err)
		}
	} else {
		// Server exists, update it
		c.logger.Info("Updating MCPServer resource: %s", name)
		mcpServer.ResourceVersion = existingServer.ResourceVersion
		if err := c.k8sClient.Update(ctx, mcpServer); err != nil {
			return fmt.Errorf("failed to update MCPServer %s: %w", name, err)
		}
	}
	
	return nil
}

// stopMCPServer stops an MCP server by deleting its Kubernetes resources
func (c *K8sComposer) stopMCPServer(name string) error {
	ctx := context.Background()
	
	// Delete MCPServer CRD - this will trigger the controller to clean up associated resources
	mcpServer := &crd.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.namespace,
		},
	}
	
	err := c.k8sClient.Delete(ctx, mcpServer)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			c.logger.Info("MCPServer %s not found, may already be deleted", name)
		} else {
			return fmt.Errorf("failed to delete MCPServer %s: %w", name, err)
		}
	} else {
		c.logger.Info("MCPServer %s deleted successfully", name)
	}
	
	// The controller will handle cleanup of associated Deployments, Services, ConfigMaps, etc.
	// But we can also try to clean up manually as fallback
	
	// Delete associated Deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.namespace,
		},
	}
	
	err = c.k8sClient.Delete(ctx, deployment)
	if err != nil && client.IgnoreNotFound(err) != nil {
		c.logger.Warning("Failed to delete Deployment %s: %v", name, err)
	}
	
	// Delete associated Service
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.namespace,
		},
	}
	
	err = c.k8sClient.Delete(ctx, service)
	if err != nil && client.IgnoreNotFound(err) != nil {
		c.logger.Warning("Failed to delete Service %s: %v", name, err)
	}
	
	c.logger.Info("MCP server %s stopped successfully", name)
	return nil
}

// convertServerConfigToMCPServer converts a ServerConfig to an MCPServer CRD
func (c *K8sComposer) convertServerConfigToMCPServer(name string, serverConfig config.ServerConfig) *crd.MCPServer {
	mcpServer := &crd.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "mcp-server",
				"app.kubernetes.io/instance":   name,
				"app.kubernetes.io/component":  "mcp-server",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "server",
			},
		},
		Spec: crd.MCPServerSpec{
			Capabilities: serverConfig.Capabilities,
			Protocol:     serverConfig.Protocol,
		},
	}
	
	// Handle image - either from image field or build-based image from registry
	if serverConfig.Image != "" {
		// For standard Docker Hub images (like postgres:15-alpine), don't add registry prefix
		if strings.Contains(serverConfig.Image, ":") && !strings.Contains(serverConfig.Image, "/") {
			// Standard image like "postgres:15-alpine" - use as-is
			mcpServer.Spec.Image = serverConfig.Image
		} else {
			// Custom image - use registry
			mcpServer.Spec.Image = c.config.GetRegistryImage(serverConfig.Image)
		}
	} else if serverConfig.Build.Context != "" {
		// For build configs, use the image we just built and pushed
		mcpServer.Spec.Image = fmt.Sprintf("%s/%s:latest", c.config.Registry.URL, name)
	} else {
		c.logger.Warning("No image or build config found for server %s, skipping", name)
		return nil
	}
	
	// Handle command - only set if not empty
	if serverConfig.Command != "" {
		mcpServer.Spec.Command = []string{serverConfig.Command}
	}
	
	// Handle args
	if len(serverConfig.Args) > 0 {
		mcpServer.Spec.Args = serverConfig.Args
	}
	
	// Handle environment variables
	if len(serverConfig.Env) > 0 {
		mcpServer.Spec.Env = serverConfig.Env
	}
	
	// Handle ports - HttpPort is required for services that need networking
	if serverConfig.HttpPort > 0 {
		mcpServer.Spec.HttpPort = int32(serverConfig.HttpPort)
	} else if serverConfig.StdioHosterPort > 0 {
		// Services using stdio_hoster_port are actually HTTP services
		mcpServer.Spec.HttpPort = int32(serverConfig.StdioHosterPort)
		mcpServer.Spec.Protocol = "http"  // Override protocol to http
	} else if serverConfig.Protocol == "http" || serverConfig.Protocol == "sse" {
		// Default port for HTTP/SSE services
		mcpServer.Spec.HttpPort = 8080
	} else if len(serverConfig.Ports) > 0 {
		// Try to extract port from ports array (e.g., "8007:8007")
		portStr := serverConfig.Ports[0]
		if strings.Contains(portStr, ":") {
			// Format like "8007:8007"
			parts := strings.Split(portStr, ":")
			if len(parts) >= 2 {
				if port, err := strconv.Atoi(parts[1]); err == nil {
					mcpServer.Spec.HttpPort = int32(port)
				}
			}
		}
	}
	
	// Set protocol default if not specified
	if mcpServer.Spec.Protocol == "" {
		if mcpServer.Spec.HttpPort > 0 {
			mcpServer.Spec.Protocol = "http"
		} else {
			mcpServer.Spec.Protocol = "stdio"
		}
	}
	
	// Set authentication if configured
	if serverConfig.Authentication != nil {
		mcpServer.Spec.Authentication = &crd.AuthenticationConfig{
			Enabled:       serverConfig.Authentication.Enabled,
			RequiredScope: serverConfig.Authentication.RequiredScope,
			OptionalAuth:  serverConfig.Authentication.OptionalAuth,
			AllowAPIKey:   serverConfig.Authentication.AllowAPIKey,
		}
	}
	
	// Set security configuration
	mcpServer.Spec.Security = &crd.SecurityConfig{
		AllowPrivilegedOps: serverConfig.Privileged,
		ReadOnlyRootFS:     serverConfig.ReadOnly,
		CapAdd:             serverConfig.CapAdd,
		CapDrop:            serverConfig.CapDrop,
		// Set from Security field if available
		AllowHostMounts:    serverConfig.Security.AllowHostMounts,
		TrustedImage:       serverConfig.Security.TrustedImage,
		NoNewPrivileges:    serverConfig.Security.NoNewPrivileges,
	}
	
	// Override privileged setting if explicitly set in Security field
	if serverConfig.Security.AllowPrivilegedOps {
		mcpServer.Spec.Security.AllowPrivilegedOps = true
	}
	
	// Parse user configuration (e.g., "1000:1000" or "root")
	if serverConfig.User != "" {
		if serverConfig.User == "root" {
			// Root user
			mcpServer.Spec.Security.RunAsUser = &[]int64{0}[0]
			mcpServer.Spec.Security.RunAsGroup = &[]int64{0}[0]
		} else if strings.Contains(serverConfig.User, ":") {
			// Format like "1000:1000"
			parts := strings.Split(serverConfig.User, ":")
			if len(parts) >= 2 {
				if uid, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
					mcpServer.Spec.Security.RunAsUser = &uid
				}
				if gid, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					mcpServer.Spec.Security.RunAsGroup = &gid
				}
			}
		} else {
			// Just user ID
			if uid, err := strconv.ParseInt(serverConfig.User, 10, 64); err == nil {
				mcpServer.Spec.Security.RunAsUser = &uid
			}
		}
	}
	
	// Set resource limits if configured
	if serverConfig.Deploy.Resources.Limits.CPUs != "" || serverConfig.Deploy.Resources.Limits.Memory != "" {
		limits := make(crd.ResourceList)
		if serverConfig.Deploy.Resources.Limits.CPUs != "" {
			limits["cpu"] = serverConfig.Deploy.Resources.Limits.CPUs
		}
		if serverConfig.Deploy.Resources.Limits.Memory != "" {
			limits["memory"] = serverConfig.Deploy.Resources.Limits.Memory
		}
		mcpServer.Spec.Resources.Limits = limits
	}
	
	// Set volumes if configured
	for _, volume := range serverConfig.Volumes {
		// Parse volume mount (e.g., "/host/path:/container/path:rw")
		parts := strings.Split(volume, ":")
		if len(parts) >= 2 {
			mcpServer.Spec.Volumes = append(mcpServer.Spec.Volumes, crd.VolumeSpec{
				Name:      fmt.Sprintf("volume-%d", len(mcpServer.Spec.Volumes)),
				MountPath: parts[1],
				HostPath:  parts[0],
			})
		}
	}
	
	return mcpServer
}

// ComposeStatus represents the status of all services
type ComposeStatus struct {
	Services map[string]*ServiceStatus `json:"services"`
}

// ServiceStatus represents the status of a single service
type ServiceStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Type   string `json:"type"`
}

// Public API functions for backwards compatibility

// Up starts services using system approach
func Up(configFile string, serviceNames []string) error {
	composer, err := NewK8sComposer(configFile, "default")
	if err != nil {
		return err
	}
	return composer.Up(serviceNames)
}

// Down stops services using system approach
func Down(configFile string, serviceNames []string) error {
	composer, err := NewK8sComposer(configFile, "default")
	if err != nil {
		return err
	}
	return composer.Down(serviceNames)
}

// Start starts specific services using system approach
func Start(configFile string, serviceNames []string) error {
	composer, err := NewK8sComposer(configFile, "default")
	if err != nil {
		return err
	}
	return composer.Start(serviceNames)
}

// Stop stops specific services using system approach
func Stop(configFile string, serviceNames []string) error {
	composer, err := NewK8sComposer(configFile, "default")
	if err != nil {
		return err
	}
	return composer.Stop(serviceNames)
}

// Restart restarts specific services using system approach
func Restart(configFile string, serviceNames []string) error {
	composer, err := NewK8sComposer(configFile, "default")
	if err != nil {
		return err
	}
	return composer.Restart(serviceNames)
}

// Status returns the status of all services using system approach
func Status(configFile string) (*ComposeStatus, error) {
	composer, err := NewK8sComposer(configFile, "default")
	if err != nil {
		return nil, err
	}
	return composer.Status()
}

// List lists all services and their status (alias for Status but outputs to console)
func List(configFile string) error {
	status, err := Status(configFile)
	if err != nil {
		return err
	}

	fmt.Printf("%-20s %-15s %-10s\n", "SERVICE", "STATUS", "TYPE")
	fmt.Println(strings.Repeat("-", 50))

	for name, svc := range status.Services {
		fmt.Printf("%-20s %-15s %-10s\n", name, svc.Status, svc.Type)
	}

	return nil
}

// Logs returns logs from services (placeholder for future implementation)
func Logs(configFile string, serviceNames []string, follow bool) error {
	fmt.Println("Use kubectl logs to view pod logs")
	fmt.Printf("Example: kubectl logs -n default -l app.kubernetes.io/name=memory\n")
	return nil
}

// Validate validates the configuration
func Validate(configFile string) error {
	_, err := config.LoadConfig(configFile)
	if err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}
	fmt.Println("Configuration is valid")
	return nil
}