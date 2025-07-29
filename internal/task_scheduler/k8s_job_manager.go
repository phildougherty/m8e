// internal/task_scheduler/k8s_job_manager.go
package task_scheduler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
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
	Volumes     []TaskVolumeConfig     `json:"volumes,omitempty"`
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

// TaskVolumeConfig defines volume configuration for tasks
type TaskVolumeConfig struct {
	// Name of the volume
	Name string `json:"name"`
	// Mount path in the container
	MountPath string `json:"mountPath"`
	// Volume type (pvc, configmap, secret, emptyDir)
	Type string `json:"type"`
	// Source configuration (depends on type)
	Source TaskVolumeSource `json:"source"`
	// ReadOnly mount
	ReadOnly bool `json:"readOnly,omitempty"`
}

// TaskVolumeSource defines the source of a volume
type TaskVolumeSource struct {
	// PVC configuration (for type: pvc)
	PVC *TaskVolumePVC `json:"pvc,omitempty"`
	// EmptyDir configuration (for type: emptyDir)
	EmptyDir *TaskVolumeEmptyDir `json:"emptyDir,omitempty"`
	// ConfigMap configuration (for type: configmap)
	ConfigMap *TaskVolumeConfigMap `json:"configMap,omitempty"`
}

// TaskVolumePVC defines PVC volume source
type TaskVolumePVC struct {
	// Claim name (if existing) or auto-generated if empty
	ClaimName string `json:"claimName,omitempty"`
	// Size for auto-created PVC
	Size string `json:"size,omitempty"`
	// Storage class for auto-created PVC
	StorageClass string `json:"storageClass,omitempty"`
	// Access modes for auto-created PVC
	AccessModes []string `json:"accessModes,omitempty"`
	// Auto-delete PVC after job completion
	AutoDelete bool `json:"autoDelete,omitempty"`
}

// TaskVolumeEmptyDir defines emptyDir volume source
type TaskVolumeEmptyDir struct {
	// Size limit for emptyDir
	SizeLimit string `json:"sizeLimit,omitempty"`
	// Medium (Memory for tmpfs)
	Medium string `json:"medium,omitempty"`
}

// TaskVolumeConfigMap defines configMap volume source
type TaskVolumeConfigMap struct {
	// ConfigMap name
	Name string `json:"name"`
	// Items to project from configMap
	Items []TaskVolumeConfigMapItem `json:"items,omitempty"`
}

// TaskVolumeConfigMapItem defines a configMap item projection
type TaskVolumeConfigMapItem struct {
	// Key in configMap
	Key string `json:"key"`
	// Path to mount the key
	Path string `json:"path"`
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
	job, err := jm.createJobSpec(ctx, task)
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
		jm.logger.Warning("Failed to get pod info for task %s: %v", taskID, err)
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
				jm.logger.Error("Failed to get status for task %s: %v", taskID, err)
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
	defer func() {
		if err := logs.Close(); err != nil {
			log.Printf("Warning: Failed to close log stream: %v", err)
		}
	}()

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
					jm.logger.Error("Failed to delete job %s: %v", job.Name, err)
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
func (jm *K8sJobManager) createJobSpec(ctx context.Context, task *TaskRequest) (*batchv1.Job, error) {
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

	// Build volume mounts and volumes
	var volumeMounts []corev1.VolumeMount
	var volumes []corev1.Volume
	var pvcCreated []string

	for _, volConfig := range task.Volumes {
		// Create volume mount
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volConfig.Name,
			MountPath: volConfig.MountPath,
			ReadOnly:  volConfig.ReadOnly,
		})

		// Create volume based on type
		var volume corev1.Volume
		switch volConfig.Type {
		case "pvc":
			pvcName := volConfig.Source.PVC.ClaimName
			if pvcName == "" {
				// Auto-generate PVC name using a deterministic but reusable identifier
				// Priority: 1) explicit workflow metadata, 2) task name + image hash, 3) task name only
				workflowName := ""
				if workflow, ok := task.Metadata["workflow"]; ok {
					if workflowStr, ok := workflow.(string); ok {
						workflowName = workflowStr
					}
				}
				
				if workflowName == "" {
					// Create a stable identifier based on task characteristics
					// This allows reuse for tasks with same name/image but prevents collisions with different images
					if task.Image != "" {
						// Use task name + short hash of image for uniqueness while allowing reuse
						imageHash := fmt.Sprintf("%x", sha256.Sum256([]byte(task.Image)))[:8]
						workflowName = fmt.Sprintf("%s-%s", task.Name, imageHash)
					} else {
						// Fallback to just task name (less ideal but better than "default")
						workflowName = task.Name
					}
				}
				
				// Ensure we have a valid name
				if workflowName == "" {
					workflowName = "unnamed-task"
				}
				
				pvcName = fmt.Sprintf("workflow-%s-%s", workflowName, volConfig.Name)
			}

			volume = corev1.Volume{
				Name: volConfig.Name,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvcName,
					},
				},
			}

			// Auto-create PVC if needed
			if volConfig.Source.PVC.ClaimName == "" {
				err := jm.createPVCForTask(ctx, pvcName, volConfig.Source.PVC)
				if err != nil {
					return nil, fmt.Errorf("failed to create PVC %s: %w", pvcName, err)
				}
				pvcCreated = append(pvcCreated, pvcName)
			}

		case "emptyDir":
			volumeSource := corev1.EmptyDirVolumeSource{}
			if volConfig.Source.EmptyDir != nil {
				if volConfig.Source.EmptyDir.SizeLimit != "" {
					sizeLimit := parseResourceQuantity(volConfig.Source.EmptyDir.SizeLimit)
					volumeSource.SizeLimit = &sizeLimit
				}
				if volConfig.Source.EmptyDir.Medium != "" {
					volumeSource.Medium = corev1.StorageMedium(volConfig.Source.EmptyDir.Medium)
				}
			}
			volume = corev1.Volume{
				Name: volConfig.Name,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &volumeSource,
				},
			}

		case "configmap":
			if volConfig.Source.ConfigMap == nil {
				return nil, fmt.Errorf("configMap source required for volume %s", volConfig.Name)
			}
			
			configMapSource := &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: volConfig.Source.ConfigMap.Name,
				},
			}

			if len(volConfig.Source.ConfigMap.Items) > 0 {
				for _, item := range volConfig.Source.ConfigMap.Items {
					configMapSource.Items = append(configMapSource.Items, corev1.KeyToPath{
						Key:  item.Key,
						Path: item.Path,
					})
				}
			}

			volume = corev1.Volume{
				Name: volConfig.Name,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: configMapSource,
				},
			}

		default:
			return nil, fmt.Errorf("unsupported volume type: %s", volConfig.Type)
		}

		volumes = append(volumes, volume)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: jm.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "task",
				"app.kubernetes.io/component":  "task-execution",
				"app.kubernetes.io/managed-by": "matey-task-scheduler",
				"mcp.matey.ai/task-id":        shortenForK8sLabel(task.ID),
				"mcp.matey.ai/task-name":      shortenForK8sLabel(task.Name),
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
						"mcp.matey.ai/task-id":        shortenForK8sLabel(task.ID),
						"mcp.matey.ai/task-name":      shortenForK8sLabel(task.Name),
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
							ImagePullPolicy: corev1.PullAlways,
							VolumeMounts:    volumeMounts,
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	// Store PVC list for cleanup if auto-delete is enabled
	if len(pvcCreated) > 0 {
		if job.Annotations == nil {
			job.Annotations = make(map[string]string)
		}
		job.Annotations["mcp.matey.ai/auto-created-pvcs"] = strings.Join(pvcCreated, ",")
	}

	return job, nil
}

// createPVCForTask creates a PersistentVolumeClaim for a task
func (jm *K8sJobManager) createPVCForTask(ctx context.Context, pvcName string, config *TaskVolumePVC) error {
	// Default values
	size := config.Size
	if size == "" {
		size = "1Gi"
	}

	accessModes := config.AccessModes
	if len(accessModes) == 0 {
		accessModes = []string{"ReadWriteOnce"}
	}

	// Convert access modes to Kubernetes types
	k8sAccessModes := make([]corev1.PersistentVolumeAccessMode, len(accessModes))
	for i, mode := range accessModes {
		k8sAccessModes[i] = corev1.PersistentVolumeAccessMode(mode)
	}

	// Extract workspace metadata from PVC name pattern (workspace-{workflow-name}-{execution-id})
	labels := map[string]string{
		"app.kubernetes.io/name":       "workflow-workspace",
		"app.kubernetes.io/component":  "storage",
		"app.kubernetes.io/managed-by": "matey-task-scheduler",
	}
	
	annotations := map[string]string{
		"mcp.matey.ai/created-at": time.Now().Format(time.RFC3339),
	}
	
	// Add workspace-specific labels if this is a workspace PVC
	if strings.HasPrefix(pvcName, "workspace-") {
		parts := strings.SplitN(pvcName, "-", 3)
		if len(parts) >= 3 {
			workflowName := parts[1]
			executionID := parts[2]
			
			labels["mcp.matey.ai/workspace-type"] = "workflow"
			labels["mcp.matey.ai/workflow-name"] = workflowName
			labels["mcp.matey.ai/execution-id"] = executionID
			labels["mcp.matey.ai/retention-policy"] = "workspace"
			
			// Add retention information to annotations
			annotations["mcp.matey.ai/workspace-retention-days"] = "7" // Default
			annotations["mcp.matey.ai/auto-delete"] = fmt.Sprintf("%t", config.AutoDelete)
		}
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        pvcName,
			Namespace:   jm.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: k8sAccessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: parseResourceQuantity(size),
				},
			},
		},
	}

	// Set storage class if specified
	if config.StorageClass != "" {
		pvc.Spec.StorageClassName = &config.StorageClass
	}

	// Add auto-delete annotation if enabled
	if config.AutoDelete {
		pvc.Annotations["mcp.matey.ai/auto-delete"] = "true"
	}

	// Check if PVC already exists before creating
	existingPVC, err := jm.client.CoreV1().PersistentVolumeClaims(jm.namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err == nil {
		// PVC already exists, check if it's compatible
		if existingPVC.Status.Phase == corev1.ClaimBound || existingPVC.Status.Phase == corev1.ClaimPending {
			jm.logger.Info("Reusing existing PVC %s", pvcName)
			return nil
		}
	}

	// Create PVC only if it doesn't exist or isn't usable
	_, err = jm.client.CoreV1().PersistentVolumeClaims(jm.namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		// If creation fails due to AlreadyExists, that's okay - another process created it
		if strings.Contains(err.Error(), "already exists") {
			jm.logger.Info("PVC %s already exists, continuing", pvcName)
			return nil
		}
		return fmt.Errorf("failed to create PVC: %w", err)
	}

	jm.logger.Info("Created PVC %s for task storage", pvcName)
	return nil
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

// shortenForK8sLabel ensures a string fits Kubernetes label requirements (63 chars max)
func shortenForK8sLabel(s string) string {
	if len(s) <= 63 {
		return s
	}
	// Use first 55 chars + SHA-256 hash (8 chars) = 63 chars total
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(s)))[:8]
	return s[:55] + hash
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