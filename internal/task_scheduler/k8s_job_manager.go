// internal/task_scheduler/k8s_job_manager.go
package task_scheduler

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/logging"
)

// K8sJobManager manages Kubernetes Jobs for task execution
type K8sJobManager struct {
	client    kubernetes.Interface
	namespace string
	logger    *logging.Logger
	config    *config.ComposeConfig
}

// TaskRequest represents a task to be executed
type TaskRequest struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Command     []string               `json:"command"`
	Args        []string               `json:"args"`
	Image       string                 `json:"image"`
	Env         map[string]string      `json:"env"`
	Timeout     time.Duration          `json:"timeout"`
	Retry       TaskRetryConfig        `json:"retry"`
	Resources   TaskResourceConfig     `json:"resources"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// TaskRetryConfig defines retry behavior for tasks
type TaskRetryConfig struct {
	MaxRetries      int           `json:"maxRetries"`
	RetryDelay      time.Duration `json:"retryDelay"`
	BackoffStrategy string        `json:"backoffStrategy"`
}

// TaskResourceConfig defines resource requirements for tasks
type TaskResourceConfig struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// TaskStatus represents the status of a task
type TaskStatus struct {
	ID            string                 `json:"id"`
	Phase         string                 `json:"phase"`
	StartTime     *time.Time             `json:"startTime,omitempty"`
	CompletionTime *time.Time            `json:"completionTime,omitempty"`
	Message       string                 `json:"message"`
	Reason        string                 `json:"reason"`
	ExitCode      *int32                 `json:"exitCode,omitempty"`
	JobName       string                 `json:"jobName"`
	PodName       string                 `json:"podName"`
	Logs          string                 `json:"logs,omitempty"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// NewK8sJobManager creates a new Kubernetes Job manager
func NewK8sJobManager(namespace string, config *config.ComposeConfig, logger *logging.Logger) (*K8sJobManager, error) {
	if namespace == "" {
		namespace = "default"
	}
	
	if logger == nil {
		logger = logging.NewLogger("info")
	}

	// Create Kubernetes client
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		k8sConfig, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &K8sJobManager{
		client:    client,
		namespace: namespace,
		logger:    logger,
		config:    config,
	}, nil
}

// SubmitTask submits a task for execution as a Kubernetes Job
func (jm *K8sJobManager) SubmitTask(ctx context.Context, task *TaskRequest) (*TaskStatus, error) {
	jm.logger.Info("Submitting task %s (%s) to Kubernetes", task.ID, task.Name)

	// Create Job specification
	job, err := jm.createJobSpec(task)
	if err != nil {
		return nil, fmt.Errorf("failed to create job spec: %w", err)
	}

	// Create the Job
	createdJob, err := jm.client.BatchV1().Jobs(jm.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	jm.logger.Info("Created Kubernetes Job %s for task %s", createdJob.Name, task.ID)

	// Return initial status
	return &TaskStatus{
		ID:       task.ID,
		Phase:    "Pending",
		JobName:  createdJob.Name,
		Message:  "Task submitted to Kubernetes",
		Metadata: task.Metadata,
	}, nil
}

// GetTaskStatus retrieves the current status of a task
func (jm *K8sJobManager) GetTaskStatus(ctx context.Context, taskID string) (*TaskStatus, error) {
	// Find the job by label
	jobs, err := jm.client.BatchV1().Jobs(jm.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("mcp.matey.ai/task-id=%s", taskID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	if len(jobs.Items) == 0 {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	job := jobs.Items[0]
	status := &TaskStatus{
		ID:      taskID,
		JobName: job.Name,
	}

	// Determine phase based on job status
	if job.Status.Active > 0 {
		status.Phase = "Running"
		status.Message = "Task is running"
	} else if job.Status.Succeeded > 0 {
		status.Phase = "Succeeded"
		status.Message = "Task completed successfully"
		if job.Status.CompletionTime != nil {
			completionTime := job.Status.CompletionTime.Time
			status.CompletionTime = &completionTime
		}
	} else if job.Status.Failed > 0 {
		status.Phase = "Failed"
		status.Message = "Task failed"
		if job.Status.CompletionTime != nil {
			completionTime := job.Status.CompletionTime.Time
			status.CompletionTime = &completionTime
		}
	} else {
		status.Phase = "Pending"
		status.Message = "Task is pending"
	}

	// Set start time
	if job.Status.StartTime != nil {
		startTime := job.Status.StartTime.Time
		status.StartTime = &startTime
	}

	// Get pod information and logs if available
	if err := jm.enrichStatusWithPodInfo(ctx, status, &job); err != nil {
		jm.logger.Warning(fmt.Sprintf("Failed to get pod info for task %s: %v", taskID, err))
	}

	return status, nil
}

// ListTasks lists all tasks (jobs) created by the task scheduler
func (jm *K8sJobManager) ListTasks(ctx context.Context, schedulerName string) ([]*TaskStatus, error) {
	jobs, err := jm.client.BatchV1().Jobs(jm.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("mcp.matey.ai/scheduler=%s", schedulerName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	var tasks []*TaskStatus
	for _, job := range jobs.Items {
		if taskID, exists := job.Labels["mcp.matey.ai/task-id"]; exists {
			status, err := jm.GetTaskStatus(ctx, taskID)
			if err != nil {
				jm.logger.Error(fmt.Sprintf("Failed to get status for task %s: %v", taskID, err))
				continue
			}
			tasks = append(tasks, status)
		}
	}

	return tasks, nil
}

// CancelTask cancels a running task by deleting its Job
func (jm *K8sJobManager) CancelTask(ctx context.Context, taskID string) error {
	// Find the job by label
	jobs, err := jm.client.BatchV1().Jobs(jm.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("mcp.matey.ai/task-id=%s", taskID),
	})
	if err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	if len(jobs.Items) == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}

	job := jobs.Items[0]
	
	// Delete the job with propagation policy to clean up pods
	deletePolicy := metav1.DeletePropagationForeground
	err = jm.client.BatchV1().Jobs(jm.namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	jm.logger.Info("Cancelled task %s (deleted job %s)", taskID, job.Name)
	return nil
}

// GetTaskLogs retrieves logs from a task's pod
func (jm *K8sJobManager) GetTaskLogs(ctx context.Context, taskID string) (string, error) {
	// Find the job by label
	jobs, err := jm.client.BatchV1().Jobs(jm.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("mcp.matey.ai/task-id=%s", taskID),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list jobs: %w", err)
	}

	if len(jobs.Items) == 0 {
		return "", fmt.Errorf("task %s not found", taskID)
	}

	job := jobs.Items[0]

	// Find pods for this job
	pods, err := jm.client.CoreV1().Pods(jm.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", job.Name),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for task %s", taskID)
	}

	// Get logs from the first pod
	pod := pods.Items[0]
	req := jm.client.CoreV1().Pods(jm.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{})
	
	logs, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	defer logs.Close()

	// Read all logs
	buf := make([]byte, 1024)
	var logContent string
	for {
		n, err := logs.Read(buf)
		if n > 0 {
			logContent += string(buf[:n])
		}
		if err != nil {
			break
		}
	}

	return logContent, nil
}

// CleanupCompletedTasks removes completed tasks older than the specified duration
func (jm *K8sJobManager) CleanupCompletedTasks(ctx context.Context, schedulerName string, olderThan time.Duration) error {
	jobs, err := jm.client.BatchV1().Jobs(jm.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("mcp.matey.ai/scheduler=%s", schedulerName),
	})
	if err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	cutoffTime := time.Now().Add(-olderThan)
	var deletedCount int

	for _, job := range jobs.Items {
		// Only clean up completed jobs
		if job.Status.Succeeded > 0 || job.Status.Failed > 0 {
			var completionTime time.Time
			if job.Status.CompletionTime != nil {
				completionTime = job.Status.CompletionTime.Time
			} else if job.Status.StartTime != nil {
				// Fallback to start time if completion time is not set
				completionTime = job.Status.StartTime.Time
			} else {
				continue
			}

			if completionTime.Before(cutoffTime) {
				err := jm.client.BatchV1().Jobs(jm.namespace).Delete(ctx, job.Name, metav1.DeleteOptions{})
				if err != nil {
					jm.logger.Error(fmt.Sprintf("Failed to delete job %s: %v", job.Name, err))
					continue
				}
				deletedCount++
			}
		}
	}

	if deletedCount > 0 {
		jm.logger.Info("Cleaned up %d completed tasks older than %v", deletedCount, olderThan)
	}

	return nil
}

// createJobSpec creates a Kubernetes Job specification from a task request
func (jm *K8sJobManager) createJobSpec(task *TaskRequest) (*batchv1.Job, error) {
	// Generate unique job name
	jobName := fmt.Sprintf("task-%s", task.ID)
	
	// Default image if not specified
	image := task.Image
	if image == "" {
		image = "busybox:latest"
	}

	// Build environment variables
	env := []corev1.EnvVar{
		{Name: "TASK_ID", Value: task.ID},
		{Name: "TASK_NAME", Value: task.Name},
	}

	// Add custom environment variables
	for key, value := range task.Env {
		env = append(env, corev1.EnvVar{Name: key, Value: value})
	}

	// Build resource requirements
	resources := corev1.ResourceRequirements{}
	if task.Resources.CPU != "" || task.Resources.Memory != "" {
		resources.Requests = make(corev1.ResourceList)
		resources.Limits = make(corev1.ResourceList)
		
		if task.Resources.CPU != "" {
			resources.Requests[corev1.ResourceCPU] = parseResourceQuantity(task.Resources.CPU)
			resources.Limits[corev1.ResourceCPU] = parseResourceQuantity(task.Resources.CPU)
		}
		if task.Resources.Memory != "" {
			resources.Requests[corev1.ResourceMemory] = parseResourceQuantity(task.Resources.Memory)
			resources.Limits[corev1.ResourceMemory] = parseResourceQuantity(task.Resources.Memory)
		}
	}

	// Calculate active deadline seconds from timeout
	var activeDeadlineSeconds *int64
	if task.Timeout > 0 {
		seconds := int64(task.Timeout.Seconds())
		activeDeadlineSeconds = &seconds
	}

	// Calculate backoff limit from retry config
	backoffLimit := int32(task.Retry.MaxRetries)
	if backoffLimit < 0 {
		backoffLimit = 3 // Default retry limit
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: jm.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "task",
				"app.kubernetes.io/component":  "task-execution",
				"app.kubernetes.io/managed-by": "matey-task-scheduler",
				"mcp.matey.ai/task-id":        task.ID,
				"mcp.matey.ai/task-name":      task.Name,
				"mcp.matey.ai/scheduler":      "task-scheduler", // Will be updated by controller
			},
			Annotations: map[string]string{
				"mcp.matey.ai/task-description": task.Description,
				"mcp.matey.ai/submitted-at":     time.Now().Format(time.RFC3339),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          &backoffLimit,
			ActiveDeadlineSeconds: activeDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":       "task",
						"app.kubernetes.io/component":  "task-execution",
						"app.kubernetes.io/managed-by": "matey-task-scheduler",
						"mcp.matey.ai/task-id":        task.ID,
						"mcp.matey.ai/task-name":      task.Name,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "task",
							Image:           image,
							Command:         task.Command,
							Args:            task.Args,
							Env:             env,
							Resources:       resources,
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
				},
			},
		},
	}

	return job, nil
}

// enrichStatusWithPodInfo adds pod information and exit code to task status
func (jm *K8sJobManager) enrichStatusWithPodInfo(ctx context.Context, status *TaskStatus, job *batchv1.Job) error {
	// Find pods for this job
	pods, err := jm.client.CoreV1().Pods(jm.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", job.Name),
	})
	if err != nil {
		return err
	}

	if len(pods.Items) == 0 {
		return nil // No pods yet
	}

	// Use the latest pod
	pod := pods.Items[0]
	status.PodName = pod.Name

	// Get exit code from container status
	if len(pod.Status.ContainerStatuses) > 0 {
		containerStatus := pod.Status.ContainerStatuses[0]
		
		if containerStatus.State.Terminated != nil {
			status.ExitCode = &containerStatus.State.Terminated.ExitCode
			status.Reason = containerStatus.State.Terminated.Reason
			if status.Message == "" {
				status.Message = containerStatus.State.Terminated.Message
			}
		} else if containerStatus.State.Waiting != nil {
			status.Reason = containerStatus.State.Waiting.Reason
			if status.Message == "" {
				status.Message = containerStatus.State.Waiting.Message
			}
		}
	}

	return nil
}

// parseResourceQuantity parses a resource quantity string
func parseResourceQuantity(quantity string) resource.Quantity {
	q, err := resource.ParseQuantity(quantity)
	if err != nil {
		// Return a default quantity if parsing fails
		return resource.MustParse("100m") // 100 milliCPU as default
	}
	return q
}

// Statistics and monitoring methods

// GetTaskStatistics returns statistics about tasks managed by this scheduler
func (jm *K8sJobManager) GetTaskStatistics(ctx context.Context, schedulerName string) (*TaskStatistics, error) {
	jobs, err := jm.client.BatchV1().Jobs(jm.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("mcp.matey.ai/scheduler=%s", schedulerName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	stats := &TaskStatistics{
		TotalTasks: int64(len(jobs.Items)),
	}

	for _, job := range jobs.Items {
		if job.Status.Succeeded > 0 {
			stats.CompletedTasks++
		} else if job.Status.Failed > 0 {
			stats.FailedTasks++
		} else if job.Status.Active > 0 {
			stats.RunningTasks++
		} else {
			stats.ScheduledTasks++
		}
	}

	if len(jobs.Items) > 0 {
		// Find the most recent job
		var latestJob *batchv1.Job
		for i := range jobs.Items {
			if latestJob == nil || jobs.Items[i].CreationTimestamp.After(latestJob.CreationTimestamp.Time) {
				latestJob = &jobs.Items[i]
			}
		}
		
		if latestJob != nil {
			stats.LastTaskTime = latestJob.CreationTimestamp.Format(time.RFC3339)
		}
	}

	return stats, nil
}

// TaskStatistics represents task execution statistics
type TaskStatistics struct {
	TotalTasks      int64  `json:"totalTasks"`
	CompletedTasks  int64  `json:"completedTasks"`
	FailedTasks     int64  `json:"failedTasks"`
	RunningTasks    int64  `json:"runningTasks"`
	ScheduledTasks  int64  `json:"scheduledTasks"`
	LastTaskTime    string `json:"lastTaskTime"`
	AverageTaskTime string `json:"averageTaskTime"`
}