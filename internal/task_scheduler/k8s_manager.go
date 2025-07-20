// internal/task_scheduler/k8s_manager.go
package task_scheduler

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
)

// K8sManager is a system manager for the task scheduler service
type K8sManager struct {
	client     client.Client
	config     *config.ComposeConfig
	configFile string
	logger     *logging.Logger
	namespace  string
	jobManager *K8sJobManager
}

// NewK8sManager creates a new system task scheduler manager
func NewK8sManager(cfg *config.ComposeConfig, k8sClient client.Client, namespace string) *K8sManager {
	logger := logging.NewLogger("info")
	if cfg != nil && cfg.Logging.Level != "" {
		logger = logging.NewLogger(cfg.Logging.Level)
	}

	if namespace == "" {
		namespace = "default"
	}

	// Create job manager
	jobManager, err := NewK8sJobManager(namespace, cfg, logger)
	if err != nil {
		logger.Error("Failed to create job manager: %v", err)
		// Continue without job manager for now
	}

	return &K8sManager{
		client:     k8sClient,
		config:     cfg,
		logger:     logger,
		namespace:  namespace,
		jobManager: jobManager,
	}
}

// SetConfigFile sets the configuration file path
func (m *K8sManager) SetConfigFile(configFile string) {
	m.configFile = configFile
}

// Start starts the task scheduler service using Kubernetes resources
func (m *K8sManager) Start() error {
	m.logger.Info("Starting task scheduler service")


	// Check if MCPTaskScheduler resource already exists
	ctx := context.Background()
	taskScheduler := &crd.MCPTaskScheduler{}
	err := m.client.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: m.namespace,
	}, taskScheduler)

	if err == nil {
		// Resource exists, check its status
		switch taskScheduler.Status.Phase {
		case crd.MCPTaskSchedulerPhaseRunning:
			return nil
		case crd.MCPTaskSchedulerPhaseFailed:
			m.logger.Info("Task scheduler is in failed state, will restart")
		default:
			m.logger.Info("Task scheduler exists in phase: %s", taskScheduler.Status.Phase)
		}
	} else {
		// Create new MCPTaskScheduler resource
		taskScheduler = m.createTaskSchedulerResource()
		if err := m.client.Create(ctx, taskScheduler); err != nil {
			return fmt.Errorf("failed to create MCPTaskScheduler resource: %w", err)
		}
		m.logger.Info("Created MCPTaskScheduler resource")
	}

	// Don't wait for ready - let controller handle deployment in background
	return nil
}

// Stop stops the task scheduler service
func (m *K8sManager) Stop() error {
	m.logger.Info("Stopping task scheduler service")


	ctx := context.Background()
	taskScheduler := &crd.MCPTaskScheduler{}
	err := m.client.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: m.namespace,
	}, taskScheduler)

	if err != nil {
		return nil // Already stopped or doesn't exist
	}

	// Delete the MCPTaskScheduler resource
	if err := m.client.Delete(ctx, taskScheduler); err != nil {
		return fmt.Errorf("failed to delete MCPTaskScheduler resource: %w", err)
	}


	return nil
}

// Restart restarts the task scheduler service
func (m *K8sManager) Restart() error {
	m.logger.Info("Restarting task scheduler service")

	// Stop first
	if err := m.Stop(); err != nil {
		return err
	}

	// Wait a moment for cleanup
	time.Sleep(2 * time.Second)

	// Start again
	return m.Start()
}

// GetStatus returns the status of the task scheduler service
func (m *K8sManager) GetStatus() (string, error) {
	ctx := context.Background()
	
	// Check actual pod status first for real-time accuracy
	podList := &corev1.PodList{}
	err := m.client.List(ctx, podList, client.InNamespace(m.namespace), client.MatchingLabels{"app.kubernetes.io/name": "task-scheduler"})
	if err == nil && len(podList.Items) > 0 {
		pod := podList.Items[0]
		
		// Check for CrashLoopBackOff or other failure states
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.State.Waiting != nil && 
				containerStatus.State.Waiting.Reason == "CrashLoopBackOff" {
				return "failed", nil
			}
		}
		
		// Check pod phase
		switch pod.Status.Phase {
		case corev1.PodRunning:
			// Check if container is ready
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					return "running", nil
				}
			}
			return "starting", nil
		case corev1.PodFailed:
			return "failed", nil
		case corev1.PodPending:
			return "pending", nil
		}
	}
	
	// Fallback to CRD status
	taskScheduler := &crd.MCPTaskScheduler{}
	err = m.client.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: m.namespace,
	}, taskScheduler)

	if err != nil {
		return "not-found", err
	}

	switch taskScheduler.Status.Phase {
	case crd.MCPTaskSchedulerPhaseRunning:
		if taskScheduler.Status.ReadyReplicas > 0 {
			return "running", nil
		}
		return "starting", nil
	case crd.MCPTaskSchedulerPhaseFailed:
		return "failed", nil
	case crd.MCPTaskSchedulerPhaseTerminating:
		return "stopping", nil
	default:
		return string(taskScheduler.Status.Phase), nil
	}
}

// GetLogs returns logs from the task scheduler service
func (m *K8sManager) GetLogs() (string, error) {
	if m.jobManager == nil {
		return "", fmt.Errorf("job manager not available")
	}

	// This would need to be implemented to get logs from the deployment pods
	// For now, return a placeholder
	return "Task scheduler - use kubectl logs to view pod logs", nil
}

// ExecuteTask executes a task using Kubernetes Jobs
func (m *K8sManager) ExecuteTask(task *TaskRequest) (*TaskStatus, error) {
	if m.jobManager == nil {
		return nil, fmt.Errorf("job manager not available")
	}

	ctx := context.Background()
	return m.jobManager.SubmitTask(ctx, task)
}

// GetTaskStatus gets the status of a specific task
func (m *K8sManager) GetTaskStatus(taskID string) (*TaskStatus, error) {
	if m.jobManager == nil {
		return nil, fmt.Errorf("job manager not available")
	}

	ctx := context.Background()
	return m.jobManager.GetTaskStatus(ctx, taskID)
}

// ListTasks lists all tasks
func (m *K8sManager) ListTasks() ([]*TaskStatus, error) {
	if m.jobManager == nil {
		return nil, fmt.Errorf("job manager not available")
	}

	ctx := context.Background()
	return m.jobManager.ListTasks(ctx, "task-scheduler")
}

// CancelTask cancels a running task
func (m *K8sManager) CancelTask(taskID string) error {
	if m.jobManager == nil {
		return fmt.Errorf("job manager not available")
	}

	ctx := context.Background()
	return m.jobManager.CancelTask(ctx, taskID)
}

// GetTaskLogs gets logs from a specific task
func (m *K8sManager) GetTaskLogs(taskID string) (string, error) {
	if m.jobManager == nil {
		return "", fmt.Errorf("job manager not available")
	}

	ctx := context.Background()
	return m.jobManager.GetTaskLogs(ctx, taskID)
}

// GetTaskStatistics gets task execution statistics
func (m *K8sManager) GetTaskStatistics() (*TaskStatistics, error) {
	if m.jobManager == nil {
		return nil, fmt.Errorf("job manager not available")
	}

	ctx := context.Background()
	return m.jobManager.GetTaskStatistics(ctx, "task-scheduler")
}

// CleanupOldTasks cleans up old completed tasks
func (m *K8sManager) CleanupOldTasks(olderThan time.Duration) error {
	if m.jobManager == nil {
		return fmt.Errorf("job manager not available")
	}

	ctx := context.Background()
	return m.jobManager.CleanupCompletedTasks(ctx, "task-scheduler", olderThan)
}

// createTaskSchedulerResource creates a new MCPTaskScheduler resource
func (m *K8sManager) createTaskSchedulerResource() *crd.MCPTaskScheduler {
	// Set defaults from config
	port := int32(8084)
	host := "0.0.0.0"
	logLevel := "info"
	databasePath := "/app/data/scheduler.db"
	replicas := int32(1)

	if m.config != nil && m.config.TaskScheduler != nil {
		if m.config.TaskScheduler.Port > 0 {
			port = int32(m.config.TaskScheduler.Port)
		}
		if m.config.TaskScheduler.Host != "" {
			host = m.config.TaskScheduler.Host
		}
		if m.config.TaskScheduler.LogLevel != "" {
			logLevel = m.config.TaskScheduler.LogLevel
		}
		if m.config.TaskScheduler.DatabasePath != "" {
			databasePath = m.config.TaskScheduler.DatabasePath
		}
	}

	taskScheduler := &crd.MCPTaskScheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-scheduler",
			Namespace: m.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "task-scheduler",
				"app.kubernetes.io/component":  "task-scheduler",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "task-scheduler",
			},
		},
		Spec: crd.MCPTaskSchedulerSpec{
			// Leave Image empty to use controller default (matey:latest with scheduler-server)
			// Leave Command/Args empty to use controller default (./matey scheduler-server)
			Port:         port,
			Host:         host,
			LogLevel:     logLevel,
			DatabasePath: databasePath,
			Replicas:     &replicas,
			Resources: crd.ResourceRequirements{
				Requests: crd.ResourceList{
					"cpu":    "100m",
					"memory": "128Mi",
				},
				Limits: crd.ResourceList{
					"cpu":    "500m",
					"memory": "512Mi",
				},
			},
			Security: &crd.SecurityConfig{
				RunAsUser:       &[]int64{1000}[0],
				RunAsGroup:      &[]int64{1000}[0],
				CapDrop:         []string{"ALL"},
				ReadOnlyRootFS:  true,
				NoNewPrivileges: true,
			},
			SchedulerConfig: crd.TaskSchedulerConfig{
				DefaultTimeout:     "5m",
				MaxConcurrentTasks: 10,
				RetryPolicy: crd.TaskRetryPolicy{
					MaxRetries:      3,
					RetryDelay:      "30s",
					BackoffStrategy: "exponential",
				},
				TaskStorageEnabled: true,
				TaskHistoryLimit:   100,
				TaskCleanupPolicy:  "auto",
			},
		},
	}

	// Add configuration from config file if available
	if m.config != nil && m.config.TaskScheduler != nil {
		ts := m.config.TaskScheduler

		// Add environment variables
		if len(ts.Env) > 0 {
			taskScheduler.Spec.Env = ts.Env
		}

		// Add LLM configuration
		if ts.OpenRouterAPIKey != "" {
			taskScheduler.Spec.OpenRouterAPIKey = ts.OpenRouterAPIKey
		}
		if ts.OpenRouterModel != "" {
			taskScheduler.Spec.OpenRouterModel = ts.OpenRouterModel
		}
		if ts.OllamaURL != "" {
			taskScheduler.Spec.OllamaURL = ts.OllamaURL
		}
		if ts.OllamaModel != "" {
			taskScheduler.Spec.OllamaModel = ts.OllamaModel
		}

		// Add workspace configuration
		if ts.Workspace != "" {
			taskScheduler.Spec.Workspace = ts.Workspace
		}

		// Add volumes
		if len(ts.Volumes) > 0 {
			taskScheduler.Spec.Volumes = ts.Volumes
		}
	}

	return taskScheduler
}

// waitForReady waits for the task scheduler to become ready
func (m *K8sManager) waitForReady(ctx context.Context, name string, timeout time.Duration) error {
	m.logger.Info("Waiting for task scheduler %s to become ready...", name)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for task scheduler to become ready")
		case <-ticker.C:
			taskScheduler := &crd.MCPTaskScheduler{}
			err := m.client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: m.namespace,
			}, taskScheduler)

			if err != nil {
				m.logger.Warning("Failed to get task scheduler status: %v", err)
				continue
			}

			if taskScheduler.Status.Phase == crd.MCPTaskSchedulerPhaseRunning && taskScheduler.Status.ReadyReplicas > 0 {
				m.logger.Info("Task scheduler is now ready")
				return nil
			}

			if taskScheduler.Status.Phase == crd.MCPTaskSchedulerPhaseFailed {
				return fmt.Errorf("task scheduler failed to start")
			}

			m.logger.Info("Task scheduler status: %s (ready: %d/%d)",
				taskScheduler.Status.Phase,
				taskScheduler.Status.ReadyReplicas,
				taskScheduler.Status.Replicas)
		}
	}
}

// Helper function to convert the old Manager interface to the new K8sManager
func NewManagerFromConfig(cfg *config.ComposeConfig, k8sClient client.Client, namespace string) *K8sManager {
	return NewK8sManager(cfg, k8sClient, namespace)
}