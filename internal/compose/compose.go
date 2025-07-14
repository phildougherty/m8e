// internal/compose/k8s_composer.go
package compose

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

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

// NewK8sComposer creates a new Kubernetes-native composer instance
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

	c.logger.Info("Starting Kubernetes-native MCP services")
	
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

	c.logger.Info("Stopping Kubernetes-native MCP services")
	

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

// Restart restarts specific services
func (c *K8sComposer) Restart(serviceNames []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Stop first
	if err := c.stopSpecificServices(serviceNames); err != nil {
		c.logger.Warning(fmt.Sprintf("Warning during stop: %v", err))
	}

	// Wait a moment for cleanup
	time.Sleep(2 * time.Second)

	// Start again
	return c.startSpecificServices(serviceNames)
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

	return status, nil
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

	// Stop memory service
	c.logger.Info("Stopping memory service")
	if err := c.memoryManager.Stop(); err != nil {
		errors = append(errors, fmt.Sprintf("memory: %v", err))
	}

	// Stop task scheduler
	c.logger.Info("Stopping task scheduler service")
	if err := c.taskSchedulerManager.Stop(); err != nil {
		errors = append(errors, fmt.Sprintf("task-scheduler: %v", err))
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
		default:
			errors = append(errors, fmt.Sprintf("unknown service: %s", serviceName))
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
		AllowDockerSocket:  serverConfig.Security.AllowDockerSocket,
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

// Up starts services using Kubernetes-native approach
func Up(configFile string, serviceNames []string) error {
	composer, err := NewK8sComposer(configFile, "default")
	if err != nil {
		return err
	}
	return composer.Up(serviceNames)
}

// Down stops services using Kubernetes-native approach
func Down(configFile string, serviceNames []string) error {
	composer, err := NewK8sComposer(configFile, "default")
	if err != nil {
		return err
	}
	return composer.Down(serviceNames)
}

// Start starts specific services using Kubernetes-native approach
func Start(configFile string, serviceNames []string) error {
	composer, err := NewK8sComposer(configFile, "default")
	if err != nil {
		return err
	}
	return composer.Start(serviceNames)
}

// Stop stops specific services using Kubernetes-native approach
func Stop(configFile string, serviceNames []string) error {
	composer, err := NewK8sComposer(configFile, "default")
	if err != nil {
		return err
	}
	return composer.Stop(serviceNames)
}

// Restart restarts specific services using Kubernetes-native approach
func Restart(configFile string, serviceNames []string) error {
	composer, err := NewK8sComposer(configFile, "default")
	if err != nil {
		return err
	}
	return composer.Restart(serviceNames)
}

// Status returns the status of all services using Kubernetes-native approach
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
	fmt.Println("Kubernetes-native logs - use kubectl logs to view pod logs")
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