// internal/compose/k8s_composer.go
package compose

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	
	// Clean up any stale resources first
	if err := c.cleanupStaleResources(); err != nil {
		c.logger.Warning("Failed to clean up stale resources: %v", err)
	}

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

		// Use efficient rollout restart for system services
		switch serviceName {
		case "matey-proxy", "proxy":
			if err := c.restartSystemService("matey-proxy"); err != nil {
				return fmt.Errorf("failed to restart proxy: %w", err)
			}
		case "matey-mcp-server", "mcp-server":
			if err := c.restartSystemService("matey-mcp-server"); err != nil {
				return fmt.Errorf("failed to restart mcp-server: %w", err)
			}
		case "matey-controller-manager", "controller-manager":
			if err := c.restartSystemService("matey-controller-manager"); err != nil {
				return fmt.Errorf("failed to restart controller-manager: %w", err)
			}
		default:
			// Use stop-start approach for other services (memory, task-scheduler, user servers)
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
		serviceInfo := c.getEnhancedServiceInfo("memory", "memory")
		status.Services["memory"] = &ServiceStatus{
			Name:            "memory",
			Status:          memStatus,
			Type:            "memory",
			StartTime:       serviceInfo.StartTime,
			ProxyConnected:  serviceInfo.ProxyConnected,
			HealthStatus:    serviceInfo.HealthStatus,
			RestartCount:    serviceInfo.RestartCount,
		}
	}


	// Check MCP server statuses via Kubernetes API
	for serverName := range c.config.Servers {
		// Skip memory if it has dedicated configuration
		if serverName == "memory" && c.config.Memory.Enabled {
			continue
		}
		
		serverStatus := c.getMCPServerStatus(serverName)
		serviceType := c.getServiceType(serverName)
		serviceInfo := c.getEnhancedServiceInfo(serverName, "mcp-server")
		status.Services[serverName] = &ServiceStatus{
			Name:            serverName,
			Status:          serverStatus,
			Type:            serviceType,
			StartTime:       serviceInfo.StartTime,
			ProxyConnected:  serviceInfo.ProxyConnected,
			HealthStatus:    serviceInfo.HealthStatus,
			RestartCount:    serviceInfo.RestartCount,
		}
	}

	// Add core Matey system services - always check these regardless of config
	systemServices := []struct {
		name           string
		deploymentName string
		serviceType    string
	}{
		{"matey-mcp-server", "matey-mcp-server", "mcp-server"},
		{"matey-proxy", "matey-proxy", "matey-core"},
		{"matey-controller-manager", "matey-controller-manager", "matey-core"},
	}

	for _, svc := range systemServices {
		systemStatus := c.getSystemServiceStatus(svc.deploymentName)
		serviceInfo := c.getEnhancedServiceInfo(svc.deploymentName, svc.serviceType)
		status.Services[svc.name] = &ServiceStatus{
			Name:            svc.name,
			Status:          systemStatus,
			Type:            svc.serviceType,
			StartTime:       serviceInfo.StartTime,
			ProxyConnected:  serviceInfo.ProxyConnected,
			HealthStatus:    serviceInfo.HealthStatus,
			RestartCount:    serviceInfo.RestartCount,
		}
	}

	return status, nil
}

// getSystemServiceStatus gets the status of a core system service deployment
func (c *K8sComposer) getSystemServiceStatus(deploymentName string) string {
	ctx := context.Background()
	
	deployment := &appsv1.Deployment{}
	err := c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      deploymentName,
		Namespace: c.namespace,
	}, deployment)
	
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return "error"
		}
		return "not-found"
	}
	
	// Check deployment status
	if deployment.Status.ReadyReplicas == 0 && deployment.Status.Replicas > 0 {
		if deployment.Status.UnavailableReplicas > 0 {
			return "starting"
		}
		return "pending"
	}
	
	if deployment.Status.ReadyReplicas > 0 && deployment.Status.ReadyReplicas == deployment.Status.Replicas {
		return "running"
	}
	
	if deployment.Status.Replicas == 0 {
		return "stopped"
	}
	
	return "unknown"
}

// getEnhancedServiceInfo gets enhanced information about a service
func (c *K8sComposer) getEnhancedServiceInfo(serviceName, serviceType string) *EnhancedServiceInfo {
	ctx := context.Background()
	info := &EnhancedServiceInfo{
		StartTime:      "N/A",
		ProxyConnected: false,
		HealthStatus:   "unknown",
		RestartCount:   0,
	}

	// First try to get deployment info for more reliable start time
	deployment := &appsv1.Deployment{}
	err := c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      serviceName,
		Namespace: c.namespace,
	}, deployment)
	
	if err == nil && deployment.Status.ReadyReplicas > 0 {
		// Use deployment creation time as fallback start time
		info.StartTime = deployment.CreationTimestamp.Format("2006-01-02 15:04:05")
	}

	// Try multiple pod label strategies to find pods
	var podList *corev1.PodList
	var foundPods bool

	// Strategy 1: Try app.kubernetes.io/instance label (most common for CRD-managed resources)
	podList = &corev1.PodList{}
	err = c.k8sClient.List(ctx, podList, 
		client.InNamespace(c.namespace),
		client.MatchingLabels(map[string]string{
			"app.kubernetes.io/instance": serviceName,
		}))
	
	if err == nil && len(podList.Items) > 0 {
		foundPods = true
	}

	// Strategy 2: Try legacy app label
	if !foundPods {
		podList = &corev1.PodList{}
		err = c.k8sClient.List(ctx, podList,
			client.InNamespace(c.namespace), 
			client.MatchingLabels(map[string]string{
				"app": serviceName,
			}))
		
		if err == nil && len(podList.Items) > 0 {
			foundPods = true
		}
	}

	// Strategy 3: For matey-core services, try component-based matching
	if !foundPods && serviceType == "matey-core" {
		component := ""
		if strings.Contains(serviceName, "mcp-server") {
			component = "mcp-server"
		} else if strings.Contains(serviceName, "proxy") {
			component = "proxy"
		} else if strings.Contains(serviceName, "controller") {
			component = "controller-manager"
		}
		
		if component != "" {
			podList = &corev1.PodList{}
			err = c.k8sClient.List(ctx, podList,
				client.InNamespace(c.namespace),
				client.MatchingLabels(map[string]string{
					"app.kubernetes.io/component": component,
				}))
			
			if err == nil && len(podList.Items) > 0 {
				foundPods = true
			}
		}
	}

	// Strategy 4: For matey-mcp-server specifically, try app=matey + component=mcp-server
	if !foundPods && serviceName == "matey-mcp-server" {
		podList = &corev1.PodList{}
		err = c.k8sClient.List(ctx, podList,
			client.InNamespace(c.namespace),
			client.MatchingLabels(map[string]string{
				"app": "matey",
				"component": "mcp-server",
			}))
		
		if err == nil && len(podList.Items) > 0 {
			foundPods = true
		}
	}

	if !foundPods {
		c.logger.Debug("No pods found for service %s using any label strategy", serviceName)
		// Still return deployment info if we have it
		if deployment.CreationTimestamp.IsZero() {
			info.StartTime = "N/A"
		}
		return info
	}

	// Find the most recent pod
	var newestPod *corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		if newestPod == nil || pod.CreationTimestamp.After(newestPod.CreationTimestamp.Time) {
			newestPod = pod
		}
	}

	if newestPod != nil {
		// Get start time - prefer pod start time over deployment creation time
		if newestPod.Status.StartTime != nil {
			info.StartTime = newestPod.Status.StartTime.Format("2006-01-02 15:04:05")
		} else if !newestPod.CreationTimestamp.IsZero() {
			info.StartTime = newestPod.CreationTimestamp.Format("2006-01-02 15:04:05")
		}
		// If both pod times are zero, keep deployment time from earlier

		// Get restart count
		for _, containerStatus := range newestPod.Status.ContainerStatuses {
			info.RestartCount += int(containerStatus.RestartCount)
		}

		// Determine health status
		switch newestPod.Status.Phase {
		case corev1.PodRunning:
			allReady := true
			for _, containerStatus := range newestPod.Status.ContainerStatuses {
				if !containerStatus.Ready {
					allReady = false
					break
				}
			}
			if allReady {
				info.HealthStatus = "healthy"
			} else {
				info.HealthStatus = "unhealthy"
			}
		case corev1.PodPending:
			info.HealthStatus = "starting"
		case corev1.PodSucceeded:
			info.HealthStatus = "completed"
		case corev1.PodFailed:
			info.HealthStatus = "failed"
		default:
			info.HealthStatus = "unknown"
		}
	}

	// Check proxy connection status and health via proxy API
	if serviceType == "matey-core" {
		// Core services don't connect to proxy, they ARE the proxy infrastructure
		info.ProxyConnected = true
		// For core services, use pod readiness as health status
		if info.HealthStatus == "unknown" && newestPod != nil {
			info.HealthStatus = c.getPodHealthStatus(newestPod)
		}
		// Special case for proxy: also check if it's responding to HTTP requests
		if strings.Contains(serviceName, "proxy") {
			// Try multiple proxy URLs to find one that works
			urlsToTry := []string{
				c.config.GetProxyURL(),
				"http://localhost:9876",
				"http://matey-proxy:9876",
				"http://matey-proxy.matey.svc.cluster.local:9876",
			}
			
			proxyHealthy := false
			for _, url := range urlsToTry {
				if url != "" && c.checkProxyHealthSingle(url) {
					proxyHealthy = true
					break
				}
			}
			
			if proxyHealthy {
				info.HealthStatus = "healthy"
			} else if info.HealthStatus == "healthy" {
				// Pod is ready but HTTP not responding
				info.HealthStatus = "degraded"
			}
		}
	} else {
		// For MCP services, check via proxy API
		info.ProxyConnected, info.HealthStatus = c.checkServiceViaProxy(serviceName)
	}

	return info
}

// checkProxyConnection checks if a service is connected to the proxy
func (c *K8sComposer) checkProxyConnection(serviceName string) bool {
	ctx := context.Background()
	
	// Check if matey-proxy service is running
	proxyDeployment := &appsv1.Deployment{}
	err := c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      "matey-proxy",
		Namespace: c.namespace,
	}, proxyDeployment)
	
	if err != nil || proxyDeployment.Status.ReadyReplicas == 0 {
		// If proxy is not running, nothing can be connected
		return false
	}
	
	// Check if the service has a corresponding MCPServer resource
	// which would indicate it's managed by the proxy
	mcpServer := &crd.MCPServer{}
	err = c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      serviceName,
		Namespace: c.namespace,
	}, mcpServer)
	
	// If MCPServer exists and has status indicating it's running, 
	// assume it's connected to proxy
	return err == nil && mcpServer.Status.Phase == "Running"
}

// checkServiceViaProxy checks service health and connectivity via proxy API
func (c *K8sComposer) checkServiceViaProxy(serviceName string) (connected bool, health string) {
	// Try multiple potential proxy URLs, prioritizing localhost and cluster URLs
	urlsToTry := []string{
		c.config.GetProxyURL(),
		"http://localhost:9876", // Default localhost
		"http://matey-proxy.matey.svc.cluster.local:9876", // Internal cluster URL
		"http://matey-proxy:9876",
	}
	
	// Remove empty URLs
	var validURLs []string
	for _, url := range urlsToTry {
		if url != "" {
			validURLs = append(validURLs, url)
		}
	}
	
	// First check if proxy is running by hitting health endpoint
	proxyHealthy := false
	var workingProxyURL string
	
	for _, proxyURL := range validURLs {
		if c.checkProxyHealthSingle(proxyURL) {
			proxyHealthy = true
			workingProxyURL = proxyURL
			break
		}
	}
	
	if !proxyHealthy {
		return false, "proxy-unavailable"
	}
	
	// Try to get service's OpenAPI spec through proxy
	client := &http.Client{Timeout: 5 * time.Second}
	openAPIURL := fmt.Sprintf("%s/%s/openapi.json", workingProxyURL, serviceName)
	
	// Create request with authentication if available
	req, err := http.NewRequest("GET", openAPIURL, nil)
	if err != nil {
		return false, "error"
	}
	
	// Add API key if configured
	if c.config.ProxyAuth.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.ProxyAuth.APIKey)
	}
	
	resp, err := client.Do(req)
	if err != nil {
		// Service not accessible through proxy
		return false, "unreachable"
	}
	defer resp.Body.Close()
	
	switch resp.StatusCode {
	case http.StatusOK:
		// Service is connected and responding
		return true, "healthy"
	case http.StatusNotFound:
		// Service not found in proxy
		return false, "not-registered"
	case http.StatusServiceUnavailable, http.StatusBadGateway:
		// Service registered but not responding
		return true, "unhealthy"
	case http.StatusUnauthorized, http.StatusForbidden:
		// Service exists but auth issue (still consider it connected)
		return true, "auth-error"
	default:
		// Other errors
		return true, "error"
	}
}

// checkProxyHealthSingle checks a single proxy URL for health
func (c *K8sComposer) checkProxyHealthSingle(proxyURL string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	healthURL := fmt.Sprintf("%s/health", proxyURL)
	
	resp, err := client.Get(healthURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	return resp.StatusCode == http.StatusOK
}

// checkProxyHealth checks if the proxy itself is healthy
func (c *K8sComposer) checkProxyHealth(proxyURL string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	
	// Try multiple potential proxy URLs
	urlsToTry := []string{
		proxyURL,
		"http://localhost:9876",
		"http://matey-proxy:9876",
		"http://matey-proxy.matey.svc.cluster.local:9876",
	}
	
	for _, url := range urlsToTry {
		healthURL := fmt.Sprintf("%s/health", url)
		resp, err := client.Get(healthURL)
		if err != nil {
			c.logger.Debug("Failed to reach proxy health at %s: %v", healthURL, err)
			continue
		}
		resp.Body.Close()
		
		if resp.StatusCode == http.StatusOK {
			c.logger.Debug("Successfully reached proxy health at %s", healthURL)
			return true
		}
		c.logger.Debug("Proxy health at %s returned status %d", healthURL, resp.StatusCode)
	}
	
	return false
}

// getPodHealthStatus determines health status from pod state
func (c *K8sComposer) getPodHealthStatus(pod *corev1.Pod) string {
	switch pod.Status.Phase {
	case corev1.PodRunning:
		allReady := true
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				allReady = false
				break
			}
		}
		if allReady {
			return "healthy"
		}
		return "unhealthy"
	case corev1.PodPending:
		return "starting"
	case corev1.PodSucceeded:
		return "completed"
	case corev1.PodFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// getMCPServerStatus gets the status of an MCP server via CRDs with deployment fallback
func (c *K8sComposer) getMCPServerStatus(serverName string) string {
	ctx := context.Background()
	
	// Always use our k8sClient to ensure consistent scheme
	clientToUse := c.k8sClient
	
	// Check for MCPPostgres CRD first - if it exists, use its status
	mcpPostgres := &crd.MCPPostgres{}
	err := clientToUse.Get(ctx, client.ObjectKey{
		Name:      serverName,
		Namespace: c.namespace,
	}, mcpPostgres)
	
	if err == nil {
		// MCPPostgres CRD exists - use CRD status (most accurate for database services)
		switch mcpPostgres.Status.Phase {
		case crd.PostgresPhaseRunning:
			return "running"
		case crd.PostgresPhasePending:
			return "pending"
		case crd.PostgresPhaseCreating:
			return "creating"
		case crd.PostgresPhaseUpdating:
			return "updating"
		case crd.PostgresPhaseDegraded:
			return "degraded"
		case crd.PostgresPhaseTerminating:
			return "terminating"
		case crd.PostgresPhaseFailed:
			return "failed"
		case "":
			return "stopped"
		default:
			return "unknown"
		}
	}
	
	// Get the deployment for this server (most accurate real-time status for regular services)
	deployment := &appsv1.Deployment{}
	err = clientToUse.Get(ctx, client.ObjectKey{
		Name:      serverName,
		Namespace: c.namespace,
	}, deployment)
	
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			c.logger.Warning("Error getting Deployment %s: %v", serverName, err)
			return "error"
		}
		// No deployment found, check if MCPServer CRD exists
		mcpServer := &crd.MCPServer{}
		err = clientToUse.Get(ctx, client.ObjectKey{
			Name:      serverName,
			Namespace: c.namespace,
		}, mcpServer)
		
		if err == nil {
			// MCPServer CRD exists but no deployment - use CRD status
			switch mcpServer.Status.Phase {
			case crd.MCPServerPhaseRunning:
				return "running"
			case crd.MCPServerPhasePending:
				return "pending"
			case crd.MCPServerPhaseCreating:
				return "creating"
			case crd.MCPServerPhaseStarting:
				return "starting"
			case crd.MCPServerPhaseTerminating:
				return "terminating"
			case crd.MCPServerPhaseFailed:
				return "failed"
			case "":
				return "stopped"
			default:
				return "unknown"
			}
		}
		
		// Neither deployment nor CRD found
		return "stopped"
	}

	// Deployment exists - check pods for more accurate status
	podList := &corev1.PodList{}
	err = clientToUse.List(ctx, podList, client.InNamespace(c.namespace), client.MatchingLabels{"app": serverName})
	if err != nil {
		c.logger.Warning("Error getting pods for %s: %v", serverName, err)
		// Fall back to deployment status
		if deployment.Status.ReadyReplicas > 0 {
			return "running"
		}
		return "starting"
	}
	
	if len(podList.Items) == 0 {
		return "pending"
	}

	// Check pod status for most accurate real-time status
	for _, pod := range podList.Items {
		// First check container statuses for CrashLoopBackOff regardless of phase
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.State.Waiting != nil && 
				containerStatus.State.Waiting.Reason == "CrashLoopBackOff" {
				return "failed"
			}
		}
		
		switch pod.Status.Phase {
		case corev1.PodRunning:
			// Check if all containers are ready
			allReady := true
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status != corev1.ConditionTrue {
					allReady = false
					break
				}
			}
			if allReady && deployment.Status.ReadyReplicas > 0 {
				return "running"
			}
			return "starting"
		case corev1.PodPending:
			// Check if it's stuck in pending due to image pull or other issues
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionTrue {
					// Pod is scheduled but still pending, likely container issues
					return "starting"
				}
			}
			return "pending"
		case corev1.PodFailed:
			return "failed"
		case corev1.PodSucceeded:
			// This shouldn't happen for long-running services
			return "stopped"
		}
	}

	// Fallback to deployment status
	if deployment.Status.ReadyReplicas > 0 {
		return "running"
	}
	
	return "starting"
}

// getServiceType determines the service type based on the resource type
func (c *K8sComposer) getServiceType(serverName string) string {
	ctx := context.Background()
	
	// Check if it's an MCPPostgres resource
	mcpPostgres := &crd.MCPPostgres{}
	err := c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      serverName,
		Namespace: c.namespace,
	}, mcpPostgres)
	
	if err == nil {
		return "database"
	}
	
	// Default to mcp-server for other services
	return "mcp-server"
}

// startProxyService creates and starts the MCPProxy resource
func (c *K8sComposer) startProxyService() error {
	ctx := context.Background()
	
	// Determine proxy port
	port := 9876
	if c.config.Proxy.Port != 0 {
		port = c.config.Proxy.Port
	}
	
	// Get API key from config
	apiKey := ""
	if c.config.ProxyAuth.Enabled {
		apiKey = c.config.ProxyAuth.APIKey
	}
	
	// Create MCPProxy resource
	replicas := int32(1)
	mcpProxy := &crd.MCPProxy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-proxy",
			Namespace: c.namespace,
			Labels: map[string]string{
				"app":                        "matey",
				"app.kubernetes.io/name":     "matey",
				"app.kubernetes.io/instance": "matey-proxy",
			},
		},
		Spec: crd.MCPProxySpec{
			Port:        int32(port),
			Replicas:    &replicas,
			ServiceType: "NodePort",
			Auth: &crd.ProxyAuthConfig{
				Enabled: apiKey != "",
				APIKey:  apiKey,
			},
			OAuth:          c.buildOAuthConfig(),
			ServiceAccount: "matey-controller",
			Ingress: &crd.IngressConfig{
				Enabled: true,
				Host:    c.config.Proxy.Host,
				Annotations: map[string]string{
					"nginx.ingress.kubernetes.io/rewrite-target":       "/",
					"nginx.ingress.kubernetes.io/cors-allow-origin":    "*",
					"nginx.ingress.kubernetes.io/cors-allow-methods":   "GET, POST, OPTIONS",
					"nginx.ingress.kubernetes.io/cors-allow-headers":   "Authorization, Content-Type",
				},
			},
		},
	}
	
	// Create or update the resource
	err := c.k8sClient.Create(ctx, mcpProxy)
	if err != nil {
		// Check if it already exists
		if errors.IsAlreadyExists(err) {
			existing := &crd.MCPProxy{}
			if err := c.k8sClient.Get(ctx, client.ObjectKeyFromObject(mcpProxy), existing); err != nil {
				return fmt.Errorf("failed to get existing MCPProxy: %w", err)
			}
			existing.Spec = mcpProxy.Spec
			if err := c.k8sClient.Update(ctx, existing); err != nil {
				return fmt.Errorf("failed to update MCPProxy: %w", err)
			}
			c.logger.Info("Updated existing MCPProxy resource")
			return nil
		}
		return fmt.Errorf("failed to create MCPProxy: %w", err)
	}
	
	c.logger.Info("MCPProxy resource created successfully")
	return nil
}

// buildOAuthConfig builds OAuth configuration from config
func (c *K8sComposer) buildOAuthConfig() *crd.OAuthConfig {
	if c.config.OAuth == nil || !c.config.OAuth.Enabled {
		return nil
	}
	
	oauth := &crd.OAuthConfig{
		Enabled: true,
		Issuer:  c.config.OAuth.Issuer,
	}
	
	// Convert endpoints
	if c.config.OAuth.Endpoints != (config.OAuthEndpoints{}) {
		oauth.Endpoints = crd.OAuthEndpoints{
			Authorization: c.config.OAuth.Endpoints.Authorization,
			Token:         c.config.OAuth.Endpoints.Token,
			UserInfo:      c.config.OAuth.Endpoints.UserInfo,
			Revoke:        c.config.OAuth.Endpoints.Revoke,
			Discovery:     c.config.OAuth.Endpoints.Discovery,
		}
	}
	
	// Convert tokens
	if c.config.OAuth.Tokens != (config.TokenConfig{}) {
		oauth.Tokens = crd.TokenConfig{
			AccessTokenTTL:  c.config.OAuth.Tokens.AccessTokenTTL,
			RefreshTokenTTL: c.config.OAuth.Tokens.RefreshTokenTTL,
			CodeTTL:         c.config.OAuth.Tokens.CodeTTL,
			Algorithm:       c.config.OAuth.Tokens.Algorithm,
		}
	}
	
	// Convert security
	oauth.Security = crd.OAuthSecurityConfig{
		RequirePKCE: c.config.OAuth.Security.RequirePKCE,
	}
	
	// Convert arrays
	oauth.GrantTypes = c.config.OAuth.GrantTypes
	oauth.ResponseTypes = c.config.OAuth.ResponseTypes
	oauth.ScopesSupported = c.config.OAuth.ScopesSupported
	
	return oauth
}

// startAllEnabledServices starts all services that are enabled in config
func (c *K8sComposer) startAllEnabledServices() error {
	var errors []string

	// Start proxy service - always start if install created the MCPProxy resource
	c.logger.Info("Starting proxy service")
	if err := c.startProxyService(); err != nil {
		errors = append(errors, fmt.Sprintf("proxy: %v", err))
	}

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
		case "controller-manager":
			c.logger.Info("Starting controller manager")
			if err := c.ensureControllerManagerRunning(); err != nil {
				errors = append(errors, fmt.Sprintf("controller-manager: %v", err))
			}
		case "matey-proxy", "proxy":
			c.logger.Info("Starting proxy service")
			if err := c.startSystemService("matey-proxy"); err != nil {
				errors = append(errors, fmt.Sprintf("matey-proxy: %v", err))
			}
		case "matey-mcp-server", "mcp-server":
			c.logger.Info("Starting MCP server service")
			if err := c.startSystemService("matey-mcp-server"); err != nil {
				errors = append(errors, fmt.Sprintf("matey-mcp-server: %v", err))
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
		case "controller-manager":
			c.logger.Info("Stopping controller manager")
			if c.controllerManager != nil {
				if err := c.controllerManager.Stop(); err != nil {
					errors = append(errors, fmt.Sprintf("controller-manager: %v", err))
				}
				c.controllerManager = nil
			}
		case "matey-proxy", "proxy":
			c.logger.Info("Stopping proxy service")
			if err := c.stopSystemService("matey-proxy"); err != nil {
				errors = append(errors, fmt.Sprintf("matey-proxy: %v", err))
			}
		case "matey-mcp-server", "mcp-server":
			c.logger.Info("Stopping MCP server service")
			if err := c.stopSystemService("matey-mcp-server"); err != nil {
				errors = append(errors, fmt.Sprintf("matey-mcp-server: %v", err))
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
	// Check if controller manager deployment already exists and is running
	if c.isControllerManagerDeploymentRunning() {
		c.logger.Info("Controller manager deployment already running")
		return nil
	}

	c.logger.Info("Deploying Matey controller manager...")
	
	// Deploy the controller manager as a pod
	if err := c.deployControllerManager(); err != nil {
		return fmt.Errorf("failed to deploy controller manager: %w", err)
	}

	c.logger.Info("Controller manager deployment created successfully")
	
	return nil
}

// isControllerManagerDeploymentRunning checks if the controller manager deployment is running
func (c *K8sComposer) isControllerManagerDeploymentRunning() bool {
	ctx := context.Background()
	deployment := &appsv1.Deployment{}
	err := c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      "matey-controller-manager",
		Namespace: c.namespace,
	}, deployment)
	
	if err != nil {
		return false
	}
	
	// Check if deployment has ready replicas
	return deployment.Status.ReadyReplicas > 0
}

// deployControllerManager deploys the controller manager as a Kubernetes deployment
func (c *K8sComposer) deployControllerManager() error {
	ctx := context.Background()
	
	// Create ConfigMap with matey.yaml
	if err := c.createControllerManagerConfigMap(); err != nil {
		return fmt.Errorf("failed to create controller manager config map: %w", err)
	}
	
	// Create the deployment
	deployment := c.createControllerManagerDeployment()
	
	// Apply the deployment
	existingDeployment := &appsv1.Deployment{}
	err := c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      deployment.Name,
		Namespace: deployment.Namespace,
	}, existingDeployment)
	
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Create new deployment
			if err := c.k8sClient.Create(ctx, deployment); err != nil {
				return fmt.Errorf("failed to create controller manager deployment: %w", err)
			}
			c.logger.Info("Created controller manager deployment")
		} else {
			return fmt.Errorf("failed to get existing controller manager deployment: %w", err)
		}
	} else {
		// Update existing deployment
		existingDeployment.Spec = deployment.Spec
		if err := c.k8sClient.Update(ctx, existingDeployment); err != nil {
			return fmt.Errorf("failed to update controller manager deployment: %w", err)
		}
		c.logger.Info("Updated controller manager deployment")
	}
	
	// Create service for metrics
	service := c.createControllerManagerService()
	existingService := &corev1.Service{}
	err = c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      service.Name,
		Namespace: service.Namespace,
	}, existingService)
	
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Create new service
			if err := c.k8sClient.Create(ctx, service); err != nil {
				return fmt.Errorf("failed to create controller manager service: %w", err)
			}
			c.logger.Info("Created controller manager service")
		} else {
			return fmt.Errorf("failed to get existing controller manager service: %w", err)
		}
	}
	
	return nil
}

// createControllerManagerConfigMap creates a ConfigMap with the matey configuration
func (c *K8sComposer) createControllerManagerConfigMap() error {
	ctx := context.Background()
	
	// Convert config to YAML
	configYAML, err := config.ToYAML(c.config)
	if err != nil {
		return fmt.Errorf("failed to convert config to YAML: %w", err)
	}
	
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-config",
			Namespace: c.namespace,
			Labels: map[string]string{
				"app":                          "matey-controller-manager",
				"app.kubernetes.io/name":       "matey-controller-manager",
				"app.kubernetes.io/component":  "controller-manager",
				"app.kubernetes.io/managed-by": "matey",
			},
		},
		Data: map[string]string{
			"matey.yaml": configYAML,
		},
	}
	
	// Check if ConfigMap exists
	existingConfigMap := &corev1.ConfigMap{}
	err = c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      configMap.Name,
		Namespace: configMap.Namespace,
	}, existingConfigMap)
	
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Create new ConfigMap
			if err := c.k8sClient.Create(ctx, configMap); err != nil {
				return fmt.Errorf("failed to create controller manager config map: %w", err)
			}
			c.logger.Info("Created controller manager config map")
		} else {
			return fmt.Errorf("failed to get existing controller manager config map: %w", err)
		}
	} else {
		// Update existing ConfigMap
		existingConfigMap.Data = configMap.Data
		if err := c.k8sClient.Update(ctx, existingConfigMap); err != nil {
			return fmt.Errorf("failed to update controller manager config map: %w", err)
		}
		c.logger.Info("Updated controller manager config map")
	}
	
	return nil
}

// createControllerManagerDeployment creates a Deployment for the controller manager
func (c *K8sComposer) createControllerManagerDeployment() *appsv1.Deployment {
	replicas := int32(1)
	
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-controller-manager",
			Namespace: c.namespace,
			Labels: map[string]string{
				"app":                          "matey-controller-manager",
				"app.kubernetes.io/name":       "matey-controller-manager",
				"app.kubernetes.io/component":  "controller-manager",
				"app.kubernetes.io/managed-by": "matey",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "matey-controller-manager",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":                          "matey-controller-manager",
						"app.kubernetes.io/name":       "matey-controller-manager",
						"app.kubernetes.io/component":  "controller-manager",
						"app.kubernetes.io/managed-by": "matey",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "matey-controller",
					Containers: []corev1.Container{
						{
							Name:  "controller-manager",
							Image: "ghcr.io/phildougherty/matey:latest",
							Command: []string{"/app/matey"},
							Args: []string{
								"controller-manager",
								"--config=/config/matey.yaml",
								"--namespace=" + c.namespace,
								"--log-level=info",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "metrics",
									ContainerPort: 8083,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "health",
									ContainerPort: 8082,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "webhook",
									ContainerPort: 9443,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(8082),
									},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       20,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz",
										Port: intstr.FromInt(8082),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: "/config",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "matey-config",
									},
								},
							},
						},
					},
					TerminationGracePeriodSeconds: &[]int64{10}[0],
				},
			},
		},
	}
}

// createControllerManagerService creates a Service for the controller manager metrics
func (c *K8sComposer) createControllerManagerService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-controller-manager-metrics",
			Namespace: c.namespace,
			Labels: map[string]string{
				"app":                          "matey-controller-manager",
				"app.kubernetes.io/name":       "matey-controller-manager",
				"app.kubernetes.io/component":  "controller-manager",
				"app.kubernetes.io/managed-by": "matey",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "matey-controller-manager",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "metrics",
					Port:       8083,
					TargetPort: intstr.FromInt(8083),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// createK8sClient creates a Kubernetes client with a comprehensive scheme
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

	// Create the scheme with all required types
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

// cleanupStaleResources removes stale MCPServer resources that may cause conflicts
func (c *K8sComposer) cleanupStaleResources() error {
	ctx := context.Background()
	
	// List all MCPServer resources
	mcpServerList := &crd.MCPServerList{}
	err := c.k8sClient.List(ctx, mcpServerList, client.InNamespace(c.namespace))
	if err != nil {
		c.logger.Warning("Failed to list MCPServer resources: %v", err)
		return nil // Don't fail the entire operation
	}
	
	// Check for servers that are in terminating state for too long
	for _, server := range mcpServerList.Items {
		if server.Status.Phase == crd.MCPServerPhaseTerminating {
			// If terminating for more than 2 minutes, force cleanup
			if server.DeletionTimestamp != nil && 
				time.Since(server.DeletionTimestamp.Time) > 2*time.Minute {
				c.logger.Info("Force cleaning up stale MCPServer: %s", server.Name)
				// Remove finalizers to allow deletion
				server.Finalizers = nil
				if err := c.k8sClient.Update(ctx, &server); err != nil {
					c.logger.Warning("Failed to remove finalizers from %s: %v", server.Name, err)
				}
			}
		}
	}
	
	return nil
}

// startMCPServer creates and deploys an MCPServer resource with conflict resolution
func (c *K8sComposer) startMCPServer(name string, serverConfig config.ServerConfig) error {
	ctx := context.Background()
	
	// Convert server config to MCPServer CRD
	mcpServer := c.convertServerConfigToMCPServer(name, serverConfig)
	if mcpServer == nil {
		// Conversion failed - skip this server
		return nil
	}
	
	// Retry logic for resource conflicts
	var lastErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			c.logger.Info("Retrying MCPServer operation for %s (attempt %d/3)", name, i+1)
			time.Sleep(time.Duration(i) * time.Second)
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
					lastErr = fmt.Errorf("failed to create MCPServer %s: %w", name, err)
					if strings.Contains(err.Error(), "already exists") {
						continue // Retry
					}
					return lastErr
				}
				return nil // Success
			} else {
				lastErr = fmt.Errorf("failed to check if MCPServer %s exists: %w", name, err)
				continue // Retry
			}
		} else {
			// Server exists, update it with proper resource version
			c.logger.Info("Updating MCPServer resource: %s", name)
			mcpServer.ResourceVersion = existingServer.ResourceVersion
			mcpServer.UID = existingServer.UID
			
			if err := c.k8sClient.Update(ctx, mcpServer); err != nil {
				lastErr = fmt.Errorf("failed to update MCPServer %s: %w", name, err)
				if strings.Contains(err.Error(), "object has been modified") {
					continue // Retry
				}
				return lastErr
			}
			return nil // Success
		}
	}
	
	return lastErr
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
			c.logger.Warning("Failed to delete MCPServer %s: %v", name, err)
			// Continue with cleanup even if MCPServer deletion fails
		}
	} else {
		c.logger.Info("MCPServer %s deleted successfully", name)
	}
	
	// Give the controller time to clean up, but also do manual cleanup
	time.Sleep(1 * time.Second)
	
	// Delete associated Deployment with better error handling
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.namespace,
		},
	}
	
	err = c.k8sClient.Delete(ctx, deployment)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			c.logger.Warning("Failed to delete Deployment %s: %v", name, err)
			// Check if it's a scheme error and try to work around it
			if strings.Contains(err.Error(), "no kind is registered") {
				c.logger.Info("Deployment %s cleanup will be handled by controller", name)
			}
		}
	} else {
		c.logger.Info("Deployment %s deleted successfully", name)
	}
	
	// Delete associated Service with better error handling
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.namespace,
		},
	}
	
	err = c.k8sClient.Delete(ctx, service)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			c.logger.Warning("Failed to delete Service %s: %v", name, err)
			// Check if it's a scheme error and try to work around it
			if strings.Contains(err.Error(), "no kind is registered") {
				c.logger.Info("Service %s cleanup will be handled by controller", name)
			}
		}
	} else {
		c.logger.Info("Service %s deleted successfully", name)
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
		// Parse volume mount (e.g., "/host/path:/container/path:rw" or "volume-name:/container/path")
		parts := strings.Split(volume, ":")
		if len(parts) >= 2 {
			hostPath := parts[0]
			
			// If hostPath doesn't start with /, it's a named volume - resolve to data directory
			if !strings.HasPrefix(hostPath, "/") {
				// Use configurable data directory (defaults to ./data)
				dataDir := os.Getenv("MATEY_DATA_DIR")
				if dataDir == "" {
					dataDir = "./data"
				}
				hostPath = fmt.Sprintf("%s/%s", dataDir, hostPath)
				
				// Convert relative path to absolute path for Kubernetes
				if !strings.HasPrefix(hostPath, "/") {
					pwd, err := os.Getwd()
					if err == nil {
						// Clean up relative path components before joining
						cleanPath := strings.TrimPrefix(hostPath, "./")
						hostPath = fmt.Sprintf("%s/%s", pwd, cleanPath)
					}
				}
			}
			
			mcpServer.Spec.Volumes = append(mcpServer.Spec.Volumes, crd.VolumeSpec{
				Name:      fmt.Sprintf("volume-%d", len(mcpServer.Spec.Volumes)),
				MountPath: parts[1],
				HostPath:  hostPath,
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
	Name            string `json:"name"`
	Status          string `json:"status"`
	Type            string `json:"type"`
	StartTime       string `json:"start_time,omitempty"`
	ProxyConnected  bool   `json:"proxy_connected"`
	HealthStatus    string `json:"health_status"`
	RestartCount    int    `json:"restart_count"`
}

// EnhancedServiceInfo contains additional service information
type EnhancedServiceInfo struct {
	StartTime      string
	ProxyConnected bool
	HealthStatus   string
	RestartCount   int
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
	composer, err := NewK8sComposer(configFile, "matey")
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

	serviceCount := len(status.Services)
	runningCount := 0
	for _, svc := range status.Services {
		if strings.ToLower(svc.Status) == "running" || strings.ToLower(svc.Status) == "up" {
			runningCount++
		}
	}
	fmt.Printf("Services (%d total, %d running)\n", serviceCount, runningCount)
	fmt.Println(strings.Repeat("=", 130))
	
	// Print header
	fmt.Printf("%-25s %-10s %-15s %-19s %-12s %-12s %-8s\n", 
		"NAME", "STATUS", "TYPE", "STARTED", "HEALTH", "PROXY", "RESTARTS")
	fmt.Println(strings.Repeat("-", 130))

	// Sort service names alphabetically
	var serviceNames []string
	for name := range status.Services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	for _, name := range serviceNames {
		svc := status.Services[name]
		proxyStatus := "N/A"
		if svc.Type != "matey-core" {
			if svc.ProxyConnected {
				proxyStatus = "connected"
			} else {
				proxyStatus = "disconnected"
			}
		}
		
		startTime := svc.StartTime
		if len(startTime) > 19 {
			startTime = startTime[:19] // Truncate to fit column
		}
		
		fmt.Printf("%-25s %-10s %-15s %-19s %-12s %-12s %-8d\n",
			name, 
			svc.Status, 
			svc.Type, 
			startTime,
			svc.HealthStatus,
			proxyStatus,
			svc.RestartCount)
	}

	return nil
}

// Logs returns logs from services using kubectl logs on pods
func Logs(configFile string, serviceNames []string, follow bool) error {
	namespace := os.Getenv("MATEY_NAMESPACE")
	if namespace == "" {
		namespace = "matey" // Default namespace
	}
	
	// If no service names specified, get all services from status
	if len(serviceNames) == 0 {
		status, err := Status(configFile)
		if err != nil {
			return fmt.Errorf("failed to get service status: %w", err)
		}
		for name := range status.Services {
			serviceNames = append(serviceNames, name)
		}
	}

	// Show logs from each service
	for _, serviceName := range serviceNames {
		if len(serviceNames) > 1 {
			fmt.Printf("=== Logs for %s ===\n", serviceName)
		}
		
		err := showServiceLogs(serviceName, namespace, follow)
		if err != nil {
			fmt.Printf("Error getting logs for %s: %v\n", serviceName, err)
		}
		
		if len(serviceNames) > 1 {
			fmt.Println()
		}
	}

	return nil
}

// showServiceLogs shows logs for a service by finding its pods and calling kubectl logs
func showServiceLogs(serviceName, namespace string, follow bool) error {
	// Use kubectl to get logs from pods
	followFlag := ""
	if follow {
		followFlag = "-f "
	}
	
	// Try by instance label first (most common pattern for CRD-managed services)
	cmd := fmt.Sprintf("kubectl logs %s-l app.kubernetes.io/instance=%s -n %s --tail=100", followFlag, serviceName, namespace)
	if err := executeCommand(cmd); err != nil {
		// Try by legacy app label (for older deployments)
		cmd = fmt.Sprintf("kubectl logs %s-l app=%s -n %s --tail=100", followFlag, serviceName, namespace)
		if err := executeCommand(cmd); err != nil {
			// Handle special service name mappings for system services
			deploymentName := getDeploymentName(serviceName)
			if deploymentName != serviceName {
				cmd = fmt.Sprintf("kubectl logs %s-l app.kubernetes.io/instance=%s -n %s --tail=100", followFlag, deploymentName, namespace)
				if err := executeCommand(cmd); err != nil {
					cmd = fmt.Sprintf("kubectl logs %s-l app=%s -n %s --tail=100", followFlag, deploymentName, namespace)
					return executeCommand(cmd)
				}
			} else {
				return fmt.Errorf("no pods found for service %s", serviceName)
			}
		}
	}
	
	return nil
}

// getDeploymentName maps service names to deployment names
func getDeploymentName(serviceName string) string {
	switch serviceName {
	case "proxy":
		return "matey-proxy"
	case "memory":
		return "matey-memory"
	case "controller-manager":
		return "matey-controller-manager"
	default:
		return serviceName
	}
}

// executeCommand executes a shell command and prints output
func executeCommand(cmd string) error {
	// For simplicity, we'll use os/exec to run kubectl directly
	// In production, you might want to use the Kubernetes Go client
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}
	
	cmdObj := exec.Command(parts[0], parts[1:]...)
	cmdObj.Stdout = os.Stdout
	cmdObj.Stderr = os.Stderr
	
	return cmdObj.Run()
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

// startSystemService starts a system service by scaling its deployment to 1 replica
func (c *K8sComposer) startSystemService(serviceName string) error {
	ctx := context.Background()
	
	deployment := &appsv1.Deployment{}
	err := c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      serviceName,
		Namespace: c.namespace,
	}, deployment)
	
	if err != nil {
		return fmt.Errorf("failed to get deployment %s: %w", serviceName, err)
	}
	
	// Scale deployment to 1 replica
	replicas := int32(1)
	deployment.Spec.Replicas = &replicas
	
	if err := c.k8sClient.Update(ctx, deployment); err != nil {
		return fmt.Errorf("failed to start system service %s: %w", serviceName, err)
	}
	
	c.logger.Info("System service %s started successfully", serviceName)
	return nil
}

// stopSystemService stops a system service by scaling its deployment to 0 replicas
func (c *K8sComposer) stopSystemService(serviceName string) error {
	ctx := context.Background()
	
	deployment := &appsv1.Deployment{}
	err := c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      serviceName,
		Namespace: c.namespace,
	}, deployment)
	
	if err != nil {
		return fmt.Errorf("failed to get deployment %s: %w", serviceName, err)
	}
	
	// Scale deployment to 0 replicas
	replicas := int32(0)
	deployment.Spec.Replicas = &replicas
	
	if err := c.k8sClient.Update(ctx, deployment); err != nil {
		return fmt.Errorf("failed to stop system service %s: %w", serviceName, err)
	}
	
	c.logger.Info("System service %s stopped successfully", serviceName)
	return nil
}

// restartSystemService restarts a system service using Kubernetes deployment rollout
func (c *K8sComposer) restartSystemService(serviceName string) error {
	ctx := context.Background()
	
	deployment := &appsv1.Deployment{}
	err := c.k8sClient.Get(ctx, client.ObjectKey{
		Name:      serviceName,
		Namespace: c.namespace,
	}, deployment)
	
	if err != nil {
		return fmt.Errorf("failed to get deployment %s: %w", serviceName, err)
	}
	
	// Add/update restart annotation to trigger rollout
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = fmt.Sprintf("%d", time.Now().Unix())
	
	if err := c.k8sClient.Update(ctx, deployment); err != nil {
		return fmt.Errorf("failed to restart system service %s: %w", serviceName, err)
	}
	
	c.logger.Info("System service %s restart triggered successfully", serviceName)
	return nil
}