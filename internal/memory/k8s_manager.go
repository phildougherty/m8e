// internal/memory/k8s_manager.go
package memory

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
)

// K8sManager is a system manager for the memory service
type K8sManager struct {
	client     client.Client
	config     *config.ComposeConfig
	configFile string
	logger     *logging.Logger
	namespace  string
}

// NewK8sManager creates a new system memory manager
func NewK8sManager(cfg *config.ComposeConfig, k8sClient client.Client, namespace string) *K8sManager {
	logger := logging.NewLogger("info")
	if cfg != nil && cfg.Logging.Level != "" {
		logger = logging.NewLogger(cfg.Logging.Level)
	}

	if namespace == "" {
		namespace = "default"
	}

	return &K8sManager{
		client:    k8sClient,
		config:    cfg,
		logger:    logger,
		namespace: namespace,
	}
}

// SetConfigFile sets the configuration file path
func (m *K8sManager) SetConfigFile(configFile string) {
	m.configFile = configFile
}

// Start starts the memory service using Kubernetes resources
func (m *K8sManager) Start() error {
	m.logger.Info("Starting memory service")


	// Check if MCPMemory resource already exists
	ctx := context.Background()
	memory := &crd.MCPMemory{}
	err := m.client.Get(ctx, types.NamespacedName{
		Name:      "memory",
		Namespace: m.namespace,
	}, memory)

	if err == nil {
		// Resource exists, check its status
		switch memory.Status.Phase {
		case crd.MCPMemoryPhaseRunning:
			return nil
		case crd.MCPMemoryPhaseFailed:
			m.logger.Info("Memory service is in failed state, will restart")
		default:
			m.logger.Info("Memory service exists in phase: %s", memory.Status.Phase)
		}
	} else {
		// Create new MCPMemory resource
		memory = m.createMemoryResource()
		if err := m.client.Create(ctx, memory); err != nil {
			return fmt.Errorf("failed to create MCPMemory resource: %w", err)
		}
		m.logger.Info("Created MCPMemory resource")
	}

	// Don't wait for ready - let controller handle deployment in background
	return nil
}

// Stop stops the memory service
func (m *K8sManager) Stop() error {
	m.logger.Info("Stopping memory service")


	ctx := context.Background()
	memory := &crd.MCPMemory{}
	err := m.client.Get(ctx, types.NamespacedName{
		Name:      "memory",
		Namespace: m.namespace,
	}, memory)

	if err != nil {
		return nil // Already stopped or doesn't exist
	}

	// Delete the MCPMemory resource
	if err := m.client.Delete(ctx, memory); err != nil {
		return fmt.Errorf("failed to delete MCPMemory resource: %w", err)
	}


	return nil
}

// Restart restarts the memory service
func (m *K8sManager) Restart() error {
	m.logger.Info("Restarting memory service")

	// Stop first
	if err := m.Stop(); err != nil {
		return err
	}

	// Wait a moment for cleanup
	time.Sleep(2 * time.Second)

	// Start again
	return m.Start()
}

// Status returns the status of the memory service
func (m *K8sManager) Status() (string, error) {
	ctx := context.Background()
	memory := &crd.MCPMemory{}
	err := m.client.Get(ctx, types.NamespacedName{
		Name:      "memory",
		Namespace: m.namespace,
	}, memory)

	if err != nil {
		return "not-found", err
	}

	switch memory.Status.Phase {
	case crd.MCPMemoryPhaseRunning:
		if memory.Status.ReadyReplicas > 0 {
			return "running", nil
		}
		return "starting", nil
	case crd.MCPMemoryPhaseFailed:
		return "failed", nil
	case crd.MCPMemoryPhaseTerminating:
		return "stopping", nil
	default:
		return string(memory.Status.Phase), nil
	}
}

// GetMemoryInfo returns detailed information about the memory service
func (m *K8sManager) GetMemoryInfo() (*MemoryInfo, error) {
	ctx := context.Background()
	memory := &crd.MCPMemory{}
	err := m.client.Get(ctx, types.NamespacedName{
		Name:      "memory",
		Namespace: m.namespace,
	}, memory)

	if err != nil {
		return nil, err
	}

	info := &MemoryInfo{
		Status:         string(memory.Status.Phase),
		ReadyReplicas:  memory.Status.ReadyReplicas,
		TotalReplicas:  memory.Status.Replicas,
		PostgresStatus: memory.Status.PostgresStatus,
		Conditions:     make([]string, 0),
	}

	// Add condition information
	for _, condition := range memory.Status.Conditions {
		if condition.Status == metav1.ConditionTrue {
			info.Conditions = append(info.Conditions, string(condition.Type))
		}
	}

	// Add endpoint information
	if memory.Status.ReadyReplicas > 0 {
		info.Endpoint = fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
			memory.Name, memory.Namespace, memory.Spec.Port)
	}

	return info, nil
}

// GetLogs returns logs from the memory service (placeholder for future implementation)
func (m *K8sManager) GetLogs() (string, error) {
	// This would need to be implemented to get logs from the deployment pods
	// For now, return a placeholder
	return "Memory service - use kubectl logs to view pod logs", nil
}

// createMemoryResource creates a new MCPMemory resource
func (m *K8sManager) createMemoryResource() *crd.MCPMemory {
	// Set defaults
	port := int32(3001)
	host := "0.0.0.0"
	postgresPort := int32(5432)
	postgresDB := "memory_graph"
	postgresUser := "postgres"
	postgresPassword := "password"
	replicas := int32(1)

	// Override with config values if available
	if m.config != nil {
		mem := m.config.Memory
		if mem.Port > 0 {
			port = int32(mem.Port)
		}
		if mem.Host != "" {
			host = mem.Host
		}
		if mem.PostgresPort > 0 {
			postgresPort = int32(mem.PostgresPort)
		}
		if mem.PostgresDB != "" {
			postgresDB = mem.PostgresDB
		}
		if mem.PostgresUser != "" {
			postgresUser = mem.PostgresUser
		}
		if mem.PostgresPassword != "" {
			postgresPassword = mem.PostgresPassword
		}
	}

	memory := &crd.MCPMemory{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "memory",
			Namespace: m.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "memory",
				"app.kubernetes.io/component":  "memory",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "memory",
			},
		},
		Spec: crd.MCPMemorySpec{
			Port:             port,
			Host:             host,
			PostgresEnabled:  true,
			PostgresPort:     postgresPort,
			PostgresDB:       postgresDB,
			PostgresUser:     postgresUser,
			PostgresPassword: postgresPassword,
			Replicas:         &replicas,
			CPUs:             "1.0",
			Memory:           "1Gi",
			PostgresCPUs:     "2.0",
			PostgresMemory:   "2Gi",
		},
	}

	// Add configuration from config file if available
	if m.config != nil {
		mem := m.config.Memory

		// Add database URL if specified
		if mem.DatabaseURL != "" {
			memory.Spec.DatabaseURL = mem.DatabaseURL
		} else {
			// Build database URL from components
			memory.Spec.DatabaseURL = fmt.Sprintf(
				"postgresql://%s:%s@%s-postgres:%d/%s?sslmode=disable",
				postgresUser, postgresPassword, memory.Name, postgresPort, postgresDB)
		}

		// Add resource limits
		if mem.CPUs != "" {
			memory.Spec.CPUs = mem.CPUs
		}
		if mem.Memory != "" {
			memory.Spec.Memory = mem.Memory
		}
		if mem.PostgresCPUs != "" {
			memory.Spec.PostgresCPUs = mem.PostgresCPUs
		}
		if mem.PostgresMemory != "" {
			memory.Spec.PostgresMemory = mem.PostgresMemory
		}

		// Add volumes configuration
		if len(mem.Volumes) > 0 {
			memory.Spec.Volumes = mem.Volumes
		}

		// Add authentication configuration
		if mem.Authentication != nil {
			memory.Spec.Authentication = &crd.AuthenticationConfig{
				Enabled:       mem.Authentication.Enabled,
				RequiredScope: mem.Authentication.RequiredScope,
				OptionalAuth:  mem.Authentication.OptionalAuth,
			}
		}
	}

	return memory
}

// waitForReady waits for the memory service to become ready
func (m *K8sManager) waitForReady(ctx context.Context, name string, timeout time.Duration) error {
	m.logger.Info("Waiting for memory service %s to become ready...", name)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for memory service to become ready")
		case <-ticker.C:
			memory := &crd.MCPMemory{}
			err := m.client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: m.namespace,
			}, memory)

			if err != nil {
				m.logger.Warning("Failed to get memory service status: %v", err)
				continue
			}

			if memory.Status.Phase == crd.MCPMemoryPhaseRunning && memory.Status.ReadyReplicas > 0 {
				m.logger.Info("Memory service is now ready")
				return nil
			}

			if memory.Status.Phase == crd.MCPMemoryPhaseFailed {
				return fmt.Errorf("memory service failed to start")
			}

			m.logger.Info("Memory service status: %s (ready: %d/%d)",
				memory.Status.Phase,
				memory.Status.ReadyReplicas,
				memory.Status.Replicas)
		}
	}
}

// MemoryInfo represents detailed information about the memory service
type MemoryInfo struct {
	Status         string   `json:"status"`
	ReadyReplicas  int32    `json:"readyReplicas"`
	TotalReplicas  int32    `json:"totalReplicas"`
	PostgresStatus string   `json:"postgresStatus"`
	Conditions     []string `json:"conditions"`
	Endpoint       string   `json:"endpoint,omitempty"`
}

// Helper function to convert the old Manager interface to the new K8sManager
func NewManagerFromConfig(cfg *config.ComposeConfig, k8sClient client.Client, namespace string) *K8sManager {
	return NewK8sManager(cfg, k8sClient, namespace)
}