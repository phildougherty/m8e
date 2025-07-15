// internal/controllers/mcptaskscheduler_controller.go
package controllers

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
)

// MCPTaskSchedulerReconciler reconciles a MCPTaskScheduler object
type MCPTaskSchedulerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *logging.Logger
	Config *config.ComposeConfig
	
	// Event watching
	eventWatchers map[string]context.CancelFunc
}

//+kubebuilder:rbac:groups=mcp.matey.ai,resources=mcptaskschedulers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mcp.matey.ai,resources=mcptaskschedulers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mcp.matey.ai,resources=mcptaskschedulers/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch
//+kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *MCPTaskSchedulerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the MCPTaskScheduler instance
	taskScheduler := &crd.MCPTaskScheduler{}
	err := r.Get(ctx, req.NamespacedName, taskScheduler)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			logger.Info("MCPTaskScheduler resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get MCPTaskScheduler")
		return ctrl.Result{}, err
	}

	// Update status to reflect current phase
	if taskScheduler.Status.Phase == "" {
		taskScheduler.Status.Phase = crd.MCPTaskSchedulerPhasePending
		if err := r.Status().Update(ctx, taskScheduler); err != nil {
			logger.Error(err, "Failed to update MCPTaskScheduler status")
			return ctrl.Result{}, err
		}
	}

	// Handle reconciliation based on current phase
	switch taskScheduler.Status.Phase {
	case crd.MCPTaskSchedulerPhasePending:
		return r.reconcilePending(ctx, taskScheduler)
	case crd.MCPTaskSchedulerPhaseCreating:
		return r.reconcileCreating(ctx, taskScheduler)
	case crd.MCPTaskSchedulerPhaseStarting:
		return r.reconcileStarting(ctx, taskScheduler)
	case crd.MCPTaskSchedulerPhaseRunning:
		return r.reconcileRunning(ctx, taskScheduler)
	case crd.MCPTaskSchedulerPhaseFailed:
		return r.reconcileFailed(ctx, taskScheduler)
	case crd.MCPTaskSchedulerPhaseTerminating:
		return r.reconcileTerminating(ctx, taskScheduler)
	default:
		logger.Info("Unknown phase", "phase", taskScheduler.Status.Phase)
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
}

// reconcilePending handles the pending phase
func (r *MCPTaskSchedulerReconciler) reconcilePending(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling pending MCPTaskScheduler", "name", taskScheduler.Name)

	// Update phase to creating
	taskScheduler.Status.Phase = crd.MCPTaskSchedulerPhaseCreating
	if err := r.Status().Update(ctx, taskScheduler); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{Requeue: true}, nil
}

// reconcileCreating handles the creating phase - creates all required resources
func (r *MCPTaskSchedulerReconciler) reconcileCreating(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Creating MCPTaskScheduler resources", "name", taskScheduler.Name)

	// Create ConfigMap for task scheduler configuration
	if err := r.reconcileConfigMap(ctx, taskScheduler); err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap")
		return ctrl.Result{}, err
	}

	// Create Service for task scheduler
	if err := r.reconcileService(ctx, taskScheduler); err != nil {
		logger.Error(err, "Failed to reconcile Service")
		return ctrl.Result{}, err
	}

	// Create Deployment for task scheduler
	if err := r.reconcileDeployment(ctx, taskScheduler); err != nil {
		logger.Error(err, "Failed to reconcile Deployment")
		return ctrl.Result{}, err
	}

	// Update phase to starting
	taskScheduler.Status.Phase = crd.MCPTaskSchedulerPhaseStarting
	if err := r.Status().Update(ctx, taskScheduler); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Second * 10}, nil
}

// reconcileStarting handles the starting phase
func (r *MCPTaskSchedulerReconciler) reconcileStarting(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Checking MCPTaskScheduler startup", "name", taskScheduler.Name)

	// Check if deployment is ready
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      taskScheduler.Name,
		Namespace: taskScheduler.Namespace,
	}, deployment)
	if err != nil {
		logger.Error(err, "Failed to get Deployment")
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	// Check if deployment is ready
	if deployment.Status.ReadyReplicas > 0 {
		// Update phase to running
		taskScheduler.Status.Phase = crd.MCPTaskSchedulerPhaseRunning
		taskScheduler.Status.ReadyReplicas = deployment.Status.ReadyReplicas
		taskScheduler.Status.Replicas = deployment.Status.Replicas

		// Update conditions
		r.updateCondition(taskScheduler, crd.MCPTaskSchedulerConditionReady, metav1.ConditionTrue, "DeploymentReady", "Task scheduler deployment is ready")

		if err := r.Status().Update(ctx, taskScheduler); err != nil {
			return ctrl.Result{}, err
		}

		logger.Info("MCPTaskScheduler is now running", "name", taskScheduler.Name)
		return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
	}

	return ctrl.Result{RequeueAfter: time.Second * 10}, nil
}

// reconcileRunning handles the running phase - monitors health and manages tasks
func (r *MCPTaskSchedulerReconciler) reconcileRunning(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Update deployment if spec has changed
	if err := r.reconcileDeployment(ctx, taskScheduler); err != nil {
		logger.Error(err, "Failed to reconcile Deployment during running phase")
		return ctrl.Result{}, err
	}

	// Set up event watching for configured triggers
	if err := r.setupEventWatching(ctx, taskScheduler); err != nil {
		logger.Error(err, "Failed to setup event watching")
		// Don't fail reconciliation for event watching errors
	}

	// Update task statistics by checking for running jobs
	if err := r.updateTaskStatistics(ctx, taskScheduler); err != nil {
		logger.Error(err, "Failed to update task statistics")
		// Don't fail reconciliation for stats update errors
	}

	// Handle auto-scaling if enabled
	if taskScheduler.Spec.SchedulerConfig.AutoScaling.Enabled {
		if err := r.handleAutoScaling(ctx, taskScheduler); err != nil {
			logger.Error(err, "Failed to handle auto-scaling")
			// Don't fail reconciliation for auto-scaling errors
		}
	}

	// Check health of the deployment
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      taskScheduler.Name,
		Namespace: taskScheduler.Namespace,
	}, deployment)
	if err != nil {
		logger.Error(err, "Failed to get Deployment during health check")
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// Update status
	taskScheduler.Status.ReadyReplicas = deployment.Status.ReadyReplicas
	taskScheduler.Status.Replicas = deployment.Status.Replicas

	// Update health condition
	isHealthy := deployment.Status.ReadyReplicas > 0
	if isHealthy {
		r.updateCondition(taskScheduler, crd.MCPTaskSchedulerConditionHealthy, metav1.ConditionTrue, "Healthy", "Task scheduler is healthy")
	} else {
		r.updateCondition(taskScheduler, crd.MCPTaskSchedulerConditionHealthy, metav1.ConditionFalse, "Unhealthy", "Task scheduler has no ready replicas")
	}

	if err := r.Status().Update(ctx, taskScheduler); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute * 2}, nil
}

// reconcileFailed handles the failed phase
func (r *MCPTaskSchedulerReconciler) reconcileFailed(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("MCPTaskScheduler is in failed state", "name", taskScheduler.Name)

	// Try to restart by moving back to creating phase
	taskScheduler.Status.Phase = crd.MCPTaskSchedulerPhaseCreating
	if err := r.Status().Update(ctx, taskScheduler); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// reconcileTerminating handles the terminating phase
func (r *MCPTaskSchedulerReconciler) reconcileTerminating(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Terminating MCPTaskScheduler", "name", taskScheduler.Name)

	// Cancel event watchers
	if r.eventWatchers != nil {
		key := fmt.Sprintf("%s/%s", taskScheduler.Namespace, taskScheduler.Name)
		if cancelFunc, exists := r.eventWatchers[key]; exists {
			cancelFunc()
			delete(r.eventWatchers, key)
			logger.Info("Cancelled event watcher", "name", taskScheduler.Name)
		}
	}

	// Clean up any running jobs created by this scheduler
	if err := r.cleanupJobs(ctx, taskScheduler); err != nil {
		logger.Error(err, "Failed to cleanup jobs")
		return ctrl.Result{}, err
	}

	// The deployment and service will be automatically cleaned up by garbage collection
	// due to owner references

	return ctrl.Result{}, nil
}

// reconcileConfigMap creates or updates the ConfigMap for task scheduler configuration
func (r *MCPTaskSchedulerReconciler) reconcileConfigMap(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskScheduler.Name + "-config",
			Namespace: taskScheduler.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "task-scheduler",
				"app.kubernetes.io/instance":   taskScheduler.Name,
				"app.kubernetes.io/component":  "task-scheduler",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "task-scheduler",
			},
		},
		Data: map[string]string{
			"config.yaml": r.generateTaskSchedulerConfig(taskScheduler),
		},
	}

	// Set MCPTaskScheduler instance as the owner and controller
	if err := controllerutil.SetControllerReference(taskScheduler, configMap, r.Scheme); err != nil {
		return err
	}

	// Create or update the ConfigMap
	found := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, configMap)
	} else if err != nil {
		return err
	}

	// Update if needed
	if found.Data["config.yaml"] != configMap.Data["config.yaml"] {
		found.Data = configMap.Data
		return r.Update(ctx, found)
	}

	return nil
}

// reconcileService creates or updates the Service for the task scheduler
func (r *MCPTaskSchedulerReconciler) reconcileService(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskScheduler.Name,
			Namespace: taskScheduler.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "task-scheduler",
				"app.kubernetes.io/instance":   taskScheduler.Name,
				"app.kubernetes.io/component":  "task-scheduler",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "task-scheduler",
			},
			Annotations: taskScheduler.Spec.ServiceAnnotations,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     taskScheduler.Spec.Port,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"app.kubernetes.io/name":     "task-scheduler",
				"app.kubernetes.io/instance": taskScheduler.Name,
			},
		},
	}

	// Override service type if specified
	if taskScheduler.Spec.ServiceType != "" {
		service.Spec.Type = corev1.ServiceType(taskScheduler.Spec.ServiceType)
	}

	// Set MCPTaskScheduler instance as the owner and controller
	if err := controllerutil.SetControllerReference(taskScheduler, service, r.Scheme); err != nil {
		return err
	}

	// Create or update the Service
	found := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, service)
	} else if err != nil {
		return err
	}

	// Services are mostly immutable, so we don't update much
	return nil
}

// reconcileDeployment creates or updates the Deployment for the task scheduler
func (r *MCPTaskSchedulerReconciler) reconcileDeployment(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) error {
	replicas := int32(1)
	if taskScheduler.Spec.Replicas != nil {
		replicas = *taskScheduler.Spec.Replicas
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskScheduler.Name,
			Namespace: taskScheduler.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "task-scheduler",
				"app.kubernetes.io/instance":   taskScheduler.Name,
				"app.kubernetes.io/component":  "task-scheduler",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "task-scheduler",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "task-scheduler",
					"app.kubernetes.io/instance": taskScheduler.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":       "task-scheduler",
						"app.kubernetes.io/instance":   taskScheduler.Name,
						"app.kubernetes.io/component":  "task-scheduler",
						"app.kubernetes.io/managed-by": "matey",
						"mcp.matey.ai/role":           "task-scheduler",
					},
					Annotations: taskScheduler.Spec.PodAnnotations,
				},
				Spec: r.buildPodSpec(taskScheduler),
			},
		},
	}

	// Set MCPTaskScheduler instance as the owner and controller
	if err := controllerutil.SetControllerReference(taskScheduler, deployment, r.Scheme); err != nil {
		return err
	}

	// Create or update the Deployment
	found := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, deployment)
	} else if err != nil {
		return err
	}

	// Update the deployment if needed
	found.Spec = deployment.Spec
	return r.Update(ctx, found)
}

// buildPodSpec creates the pod specification for the task scheduler
func (r *MCPTaskSchedulerReconciler) buildPodSpec(taskScheduler *crd.MCPTaskScheduler) corev1.PodSpec {
	image := taskScheduler.Spec.Image
	if image == "" {
		image = "mcpcompose/task-scheduler:latest"
	}
	if r.Config != nil {
		image = r.Config.GetRegistryImage(image)
	}

	// Build environment variables
	env := []corev1.EnvVar{
		{Name: "MCP_CRON_SERVER_PORT", Value: fmt.Sprintf("%d", taskScheduler.Spec.Port)},
		{Name: "MCP_CRON_SERVER_ADDRESS", Value: taskScheduler.Spec.Host},
		{Name: "MCP_CRON_SERVER_TRANSPORT", Value: "sse"},
		{Name: "MCP_CRON_LOGGING_LEVEL", Value: taskScheduler.Spec.LogLevel},
		{Name: "MCP_CRON_DATABASE_PATH", Value: taskScheduler.Spec.DatabasePath},
		{Name: "MCP_CRON_DATABASE_ENABLED", Value: "true"},
		{Name: "KUBERNETES_MODE", Value: "true"},
		{Name: "NAMESPACE", Value: taskScheduler.Namespace},
	}

	// Add custom environment variables
	for key, value := range taskScheduler.Spec.Env {
		env = append(env, corev1.EnvVar{Name: key, Value: value})
	}

	// Add LLM configuration
	if taskScheduler.Spec.OpenRouterAPIKey != "" {
		env = append(env, corev1.EnvVar{Name: "OPENROUTER_API_KEY", Value: taskScheduler.Spec.OpenRouterAPIKey})
	}
	if taskScheduler.Spec.OpenRouterModel != "" {
		env = append(env, corev1.EnvVar{Name: "OPENROUTER_MODEL", Value: taskScheduler.Spec.OpenRouterModel})
	}
	if taskScheduler.Spec.OllamaURL != "" {
		env = append(env, corev1.EnvVar{Name: "MCP_CRON_OLLAMA_BASE_URL", Value: taskScheduler.Spec.OllamaURL})
	}
	if taskScheduler.Spec.OllamaModel != "" {
		env = append(env, corev1.EnvVar{Name: "MCP_CRON_OLLAMA_DEFAULT_MODEL", Value: taskScheduler.Spec.OllamaModel})
	}

	// Add MCP configuration
	if taskScheduler.Spec.MCPProxyURL != "" {
		env = append(env, corev1.EnvVar{Name: "MCP_PROXY_URL", Value: taskScheduler.Spec.MCPProxyURL})
	}
	if taskScheduler.Spec.MCPProxyAPIKey != "" {
		env = append(env, corev1.EnvVar{Name: "MCP_PROXY_API_KEY", Value: taskScheduler.Spec.MCPProxyAPIKey})
	}

	container := corev1.Container{
		Name:            "task-scheduler",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             env,
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: taskScheduler.Spec.Port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "config",
				MountPath: "/app/config",
				ReadOnly:  true,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/health",
					Port: intOrString(taskScheduler.Spec.Port + 1000),
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/ready",
					Port: intOrString(taskScheduler.Spec.Port + 1000),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
		},
	}

	// Apply resource requirements
	if taskScheduler.Spec.Resources.Limits != nil || taskScheduler.Spec.Resources.Requests != nil {
		container.Resources = corev1.ResourceRequirements{
			Limits:   convertTaskSchedulerResourceList(taskScheduler.Spec.Resources.Limits),
			Requests: convertTaskSchedulerResourceList(taskScheduler.Spec.Resources.Requests),
		}
	}

	// Apply command and args if specified
	if len(taskScheduler.Spec.Command) > 0 {
		container.Command = taskScheduler.Spec.Command
	}
	if len(taskScheduler.Spec.Args) > 0 {
		container.Args = taskScheduler.Spec.Args
	}

	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{container},
		Volumes: []corev1.Volume{
			{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: taskScheduler.Name + "-config",
						},
					},
				},
			},
		},
		RestartPolicy: corev1.RestartPolicyAlways,
	}

	// Apply security context
	if taskScheduler.Spec.Security != nil {
		podSpec.SecurityContext = &corev1.PodSecurityContext{}
		if taskScheduler.Spec.Security.RunAsUser != nil {
			podSpec.SecurityContext.RunAsUser = taskScheduler.Spec.Security.RunAsUser
		}
		if taskScheduler.Spec.Security.RunAsGroup != nil {
			podSpec.SecurityContext.RunAsGroup = taskScheduler.Spec.Security.RunAsGroup
		}
		// Set runAsNonRoot to true for security
		runAsNonRoot := true
		podSpec.SecurityContext.RunAsNonRoot = &runAsNonRoot
	}

	// Apply node selector
	if len(taskScheduler.Spec.NodeSelector) > 0 {
		podSpec.NodeSelector = taskScheduler.Spec.NodeSelector
	}

	// Apply service account
	if taskScheduler.Spec.ServiceAccount != "" {
		podSpec.ServiceAccountName = taskScheduler.Spec.ServiceAccount
	}

	return podSpec
}

// generateTaskSchedulerConfig generates the configuration file for the task scheduler
func (r *MCPTaskSchedulerReconciler) generateTaskSchedulerConfig(taskScheduler *crd.MCPTaskScheduler) string {
	config := fmt.Sprintf(`# Task Scheduler Configuration
port: %d
host: %s
log_level: %s
database_path: %s

# Kubernetes Configuration
kubernetes_mode: true
namespace: %s

# Task Configuration
scheduler:
  default_timeout: %s
  max_concurrent_tasks: %d
  retry_policy:
    max_retries: %d
    retry_delay: %s
    backoff_strategy: %s
  task_storage_enabled: %t
  task_history_limit: %d
  task_cleanup_policy: %s

# Activity webhook
activity_webhook: %s

# Event triggers
event_triggers:
  enabled: %t
  triggers: %s

# Conditional dependencies
conditional_dependencies:
  enabled: %t
  default_strategy: %s
  resolution_timeout: %s
  cross_workflow_enabled: %t

# Auto-scaling
auto_scaling:
  enabled: %t
  min_concurrent_tasks: %d
  max_concurrent_tasks: %d
  target_cpu_utilization: %d
  target_memory_utilization: %d
  scale_up_cooldown: %s
  scale_down_cooldown: %s
  metrics_interval: %s
`,
		taskScheduler.Spec.Port,
		taskScheduler.Spec.Host,
		taskScheduler.Spec.LogLevel,
		taskScheduler.Spec.DatabasePath,
		taskScheduler.Namespace,
		taskScheduler.Spec.SchedulerConfig.DefaultTimeout,
		taskScheduler.Spec.SchedulerConfig.MaxConcurrentTasks,
		taskScheduler.Spec.SchedulerConfig.RetryPolicy.MaxRetries,
		taskScheduler.Spec.SchedulerConfig.RetryPolicy.RetryDelay,
		taskScheduler.Spec.SchedulerConfig.RetryPolicy.BackoffStrategy,
		taskScheduler.Spec.SchedulerConfig.TaskStorageEnabled,
		taskScheduler.Spec.SchedulerConfig.TaskHistoryLimit,
		taskScheduler.Spec.SchedulerConfig.TaskCleanupPolicy,
		taskScheduler.Spec.SchedulerConfig.ActivityWebhook,
		len(taskScheduler.Spec.SchedulerConfig.EventTriggers) > 0,
		r.generateEventTriggersConfig(taskScheduler.Spec.SchedulerConfig.EventTriggers),
		taskScheduler.Spec.SchedulerConfig.ConditionalDependencies.Enabled,
		taskScheduler.Spec.SchedulerConfig.ConditionalDependencies.DefaultStrategy,
		taskScheduler.Spec.SchedulerConfig.ConditionalDependencies.ResolutionTimeout,
		taskScheduler.Spec.SchedulerConfig.ConditionalDependencies.CrossWorkflowEnabled,
		taskScheduler.Spec.SchedulerConfig.AutoScaling.Enabled,
		taskScheduler.Spec.SchedulerConfig.AutoScaling.MinConcurrentTasks,
		taskScheduler.Spec.SchedulerConfig.AutoScaling.MaxConcurrentTasks,
		taskScheduler.Spec.SchedulerConfig.AutoScaling.TargetCPUUtilization,
		taskScheduler.Spec.SchedulerConfig.AutoScaling.TargetMemoryUtilization,
		taskScheduler.Spec.SchedulerConfig.AutoScaling.ScaleUpCooldown,
		taskScheduler.Spec.SchedulerConfig.AutoScaling.ScaleDownCooldown,
		taskScheduler.Spec.SchedulerConfig.AutoScaling.MetricsInterval,
	)

	return config
}

// updateTaskStatistics updates the task statistics by counting running jobs
func (r *MCPTaskSchedulerReconciler) updateTaskStatistics(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) error {
	// List all jobs created by this task scheduler
	jobList := &batchv1.JobList{}
	listOpts := []client.ListOption{
		client.InNamespace(taskScheduler.Namespace),
		client.MatchingLabels{
			"mcp.matey.ai/scheduler": taskScheduler.Name,
		},
	}

	if err := r.List(ctx, jobList, listOpts...); err != nil {
		return err
	}

	// Count different job states
	var totalTasks, completedTasks, failedTasks, runningTasks int64
	for _, job := range jobList.Items {
		totalTasks++
		if job.Status.Succeeded > 0 {
			completedTasks++
		} else if job.Status.Failed > 0 {
			failedTasks++
		} else if job.Status.Active > 0 {
			runningTasks++
		}
	}

	// Update task statistics
	taskScheduler.Status.TaskStats = crd.TaskStatistics{
		TotalTasks:     totalTasks,
		CompletedTasks: completedTasks,
		FailedTasks:    failedTasks,
		RunningTasks:   runningTasks,
		LastTaskTime:   time.Now().Format(time.RFC3339),
	}

	return nil
}

// cleanupJobs removes jobs created by this task scheduler
func (r *MCPTaskSchedulerReconciler) cleanupJobs(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) error {
	jobList := &batchv1.JobList{}
	listOpts := []client.ListOption{
		client.InNamespace(taskScheduler.Namespace),
		client.MatchingLabels{
			"mcp.matey.ai/scheduler": taskScheduler.Name,
		},
	}

	if err := r.List(ctx, jobList, listOpts...); err != nil {
		return err
	}

	// Delete all jobs
	for _, job := range jobList.Items {
		if err := r.Delete(ctx, &job); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// updateCondition updates a condition in the task scheduler status
func (r *MCPTaskSchedulerReconciler) updateCondition(taskScheduler *crd.MCPTaskScheduler, conditionType crd.MCPTaskSchedulerConditionType, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	
	// Find existing condition
	for i, condition := range taskScheduler.Status.Conditions {
		if condition.Type == conditionType {
			if condition.Status != status {
				taskScheduler.Status.Conditions[i].Status = status
				taskScheduler.Status.Conditions[i].LastTransitionTime = now
				taskScheduler.Status.Conditions[i].Reason = reason
				taskScheduler.Status.Conditions[i].Message = message
			}
			return
		}
	}
	
	// Add new condition
	taskScheduler.Status.Conditions = append(taskScheduler.Status.Conditions, crd.MCPTaskSchedulerCondition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	})
}

// Helper functions

func intOrString(val int32) intstr.IntOrString {
	return intstr.FromInt32(val)
}

func convertTaskSchedulerResourceList(resources crd.ResourceList) corev1.ResourceList {
	if resources == nil {
		return nil
	}
	
	result := make(corev1.ResourceList)
	for key, value := range resources {
		quantity, err := resource.ParseQuantity(value)
		if err != nil {
			// Skip invalid quantities
			continue
		}
		result[corev1.ResourceName(key)] = quantity
	}
	return result
}

// generateEventTriggersConfig generates YAML configuration for event triggers
func (r *MCPTaskSchedulerReconciler) generateEventTriggersConfig(triggers []crd.EventTrigger) string {
	if len(triggers) == 0 {
		return "[]"
	}
	
	var config strings.Builder
	config.WriteString("[\n")
	for i, trigger := range triggers {
		config.WriteString(fmt.Sprintf(`    {
      "name": "%s",
      "type": "%s",
      "workflow": "%s",
      "cooldown_duration": "%s"`,
			trigger.Name,
			trigger.Type,
			trigger.Workflow,
			trigger.CooldownDuration,
		))
		
		if trigger.KubernetesEvent != nil {
			config.WriteString(fmt.Sprintf(`,
      "kubernetes_event": {
        "kind": "%s",
        "reason": "%s",
        "namespace": "%s",
        "label_selector": "%s",
        "field_selector": "%s"
      }`,
				trigger.KubernetesEvent.Kind,
				trigger.KubernetesEvent.Reason,
				trigger.KubernetesEvent.Namespace,
				trigger.KubernetesEvent.LabelSelector,
				trigger.KubernetesEvent.FieldSelector,
			))
		}
		
		config.WriteString("\n    }")
		if i < len(triggers)-1 {
			config.WriteString(",")
		}
		config.WriteString("\n")
	}
	config.WriteString("  ]")
	return config.String()
}

// setupEventWatching sets up event watchers for configured triggers
func (r *MCPTaskSchedulerReconciler) setupEventWatching(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) error {
	logger := log.FromContext(ctx)
	
	// Initialize event watchers map if not exists
	if r.eventWatchers == nil {
		r.eventWatchers = make(map[string]context.CancelFunc)
	}
	
	// Get the key for this task scheduler
	key := fmt.Sprintf("%s/%s", taskScheduler.Namespace, taskScheduler.Name)
	
	// Cancel existing watcher if it exists
	if cancelFunc, exists := r.eventWatchers[key]; exists {
		cancelFunc()
		delete(r.eventWatchers, key)
	}
	
	// Skip if no event triggers configured
	if len(taskScheduler.Spec.SchedulerConfig.EventTriggers) == 0 {
		return nil
	}
	
	// Create new context for this watcher
	watchCtx, cancel := context.WithCancel(ctx)
	r.eventWatchers[key] = cancel
	
	// Start watching events
	go r.watchEvents(watchCtx, taskScheduler)
	
	logger.Info("Event watching setup completed", "triggers", len(taskScheduler.Spec.SchedulerConfig.EventTriggers))
	return nil
}

// watchEvents watches for Kubernetes events and triggers workflows
func (r *MCPTaskSchedulerReconciler) watchEvents(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) {
	for _, trigger := range taskScheduler.Spec.SchedulerConfig.EventTriggers {
		if trigger.Type == "k8s-event" && trigger.KubernetesEvent != nil {
			go r.watchKubernetesEvents(ctx, taskScheduler, trigger)
		}
	}
}

// watchKubernetesEvents watches for specific Kubernetes events using polling
func (r *MCPTaskSchedulerReconciler) watchKubernetesEvents(ctx context.Context, taskScheduler *crd.MCPTaskScheduler, trigger crd.EventTrigger) {
	logger := log.FromContext(ctx)
	
	logger.Info("Started polling Kubernetes events", "trigger", trigger.Name, "kind", trigger.KubernetesEvent.Kind)
	
	// Use a ticker to poll for events periodically
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	var lastResourceVersion string
	
	for {
		select {
		case <-ctx.Done():
			logger.Info("Event watcher stopped", "trigger", trigger.Name)
			return
		case <-ticker.C:
			// List events to check for new ones
			eventList := &corev1.EventList{}
			listOpts := []client.ListOption{
				client.InNamespace(taskScheduler.Namespace),
			}
			
			if err := r.List(ctx, eventList, listOpts...); err != nil {
				logger.Error(err, "Failed to list events", "trigger", trigger.Name)
				continue
			}
			
			for _, event := range eventList.Items {
				// Skip events we've already processed
				if lastResourceVersion != "" && event.ResourceVersion <= lastResourceVersion {
					continue
				}
				
				if r.matchesEventTrigger(&event, trigger) {
					logger.Info("Event trigger matched", "trigger", trigger.Name, "event", event.Name)
					if err := r.triggerWorkflow(ctx, taskScheduler, trigger, &event); err != nil {
						logger.Error(err, "Failed to trigger workflow", "trigger", trigger.Name)
					}
				}
			}
			
			// Update last processed resource version
			if len(eventList.Items) > 0 {
				lastResourceVersion = eventList.Items[len(eventList.Items)-1].ResourceVersion
			}
		}
	}
}

// matchesEventTrigger checks if a Kubernetes event matches the trigger criteria
func (r *MCPTaskSchedulerReconciler) matchesEventTrigger(event *corev1.Event, trigger crd.EventTrigger) bool {
	k8sEvent := trigger.KubernetesEvent
	
	// Check kind
	if k8sEvent.Kind != "" && event.InvolvedObject.Kind != k8sEvent.Kind {
		return false
	}
	
	// Check reason
	if k8sEvent.Reason != "" && event.Reason != k8sEvent.Reason {
		return false
	}
	
	// Check namespace
	if k8sEvent.Namespace != "" && event.Namespace != k8sEvent.Namespace {
		return false
	}
	
	// Check conditions
	for _, condition := range trigger.Conditions {
		if !r.evaluateCondition(event, condition) {
			return false
		}
	}
	
	return true
}

// evaluateCondition evaluates a trigger condition against an event
func (r *MCPTaskSchedulerReconciler) evaluateCondition(event *corev1.Event, condition crd.TriggerCondition) bool {
	var fieldValue string
	
	switch condition.Field {
	case "reason":
		fieldValue = event.Reason
	case "message":
		fieldValue = event.Message
	case "type":
		fieldValue = event.Type
	case "namespace":
		fieldValue = event.Namespace
	case "name":
		fieldValue = event.Name
	default:
		return false
	}
	
	switch condition.Operator {
	case "equals":
		return fieldValue == condition.Value
	case "contains":
		return strings.Contains(fieldValue, condition.Value)
	case "startsWith":
		return strings.HasPrefix(fieldValue, condition.Value)
	case "endsWith":
		return strings.HasSuffix(fieldValue, condition.Value)
	default:
		return false
	}
}

// triggerWorkflow triggers a workflow based on an event
func (r *MCPTaskSchedulerReconciler) triggerWorkflow(ctx context.Context, taskScheduler *crd.MCPTaskScheduler, trigger crd.EventTrigger, event *corev1.Event) error {
	logger := log.FromContext(ctx)
	
	// Create a job to trigger the workflow
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%d", taskScheduler.Name, trigger.Name, time.Now().Unix()),
			Namespace: taskScheduler.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "task-scheduler",
				"app.kubernetes.io/instance":   taskScheduler.Name,
				"app.kubernetes.io/component":  "triggered-workflow",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/scheduler":       taskScheduler.Name,
				"mcp.matey.ai/trigger":         trigger.Name,
				"mcp.matey.ai/workflow":        trigger.Workflow,
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "workflow-trigger",
							Image: "mcpcompose/task-scheduler:latest",
							Command: []string{
								"/app/trigger-workflow",
								"--workflow", trigger.Workflow,
								"--scheduler", taskScheduler.Name,
								"--trigger", trigger.Name,
								"--event-name", event.Name,
								"--event-reason", event.Reason,
							},
							Env: []corev1.EnvVar{
								{Name: "NAMESPACE", Value: taskScheduler.Namespace},
								{Name: "SCHEDULER_URL", Value: fmt.Sprintf("http://%s:%d", taskScheduler.Name, taskScheduler.Spec.Port)},
							},
						},
					},
				},
			},
		},
	}
	
	// Set task scheduler as owner
	if err := controllerutil.SetControllerReference(taskScheduler, job, r.Scheme); err != nil {
		return err
	}
	
	// Create the job
	if err := r.Create(ctx, job); err != nil {
		return err
	}
	
	logger.Info("Triggered workflow job created", "job", job.Name, "workflow", trigger.Workflow)
	return nil
}

// handleAutoScaling handles auto-scaling based on resource utilization
func (r *MCPTaskSchedulerReconciler) handleAutoScaling(ctx context.Context, taskScheduler *crd.MCPTaskScheduler) error {
	logger := log.FromContext(ctx)
	
	autoScaling := taskScheduler.Spec.SchedulerConfig.AutoScaling
	
	// Get current resource utilization
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      taskScheduler.Name,
		Namespace: taskScheduler.Namespace,
	}, deployment); err != nil {
		return err
	}
	
	// Get current task statistics
	currentTasks := taskScheduler.Status.TaskStats.RunningTasks
	
	// Calculate desired concurrency based on utilization
	desiredConcurrency := r.calculateDesiredConcurrency(taskScheduler, int32(currentTasks))
	
	// Update max concurrent tasks if needed
	if desiredConcurrency != taskScheduler.Spec.SchedulerConfig.MaxConcurrentTasks {
		logger.Info("Auto-scaling adjustment needed", 
			"current", taskScheduler.Spec.SchedulerConfig.MaxConcurrentTasks,
			"desired", desiredConcurrency,
			"running_tasks", currentTasks,
			"target_cpu", autoScaling.TargetCPUUtilization,
		)
		
		// Update the task scheduler configuration
		taskScheduler.Spec.SchedulerConfig.MaxConcurrentTasks = desiredConcurrency
		
		// Update the ConfigMap to reflect the new configuration
		if err := r.reconcileConfigMap(ctx, taskScheduler); err != nil {
			return err
		}
	}
	
	return nil
}

// calculateDesiredConcurrency calculates the desired concurrency based on current load
func (r *MCPTaskSchedulerReconciler) calculateDesiredConcurrency(taskScheduler *crd.MCPTaskScheduler, currentTasks int32) int32 {
	autoScaling := taskScheduler.Spec.SchedulerConfig.AutoScaling
	current := taskScheduler.Spec.SchedulerConfig.MaxConcurrentTasks
	
	// Special case: if no tasks are running, start with minimum
	if currentTasks == 0 {
		return autoScaling.MinConcurrentTasks
	}
	
	// Calculate utilization percentage
	utilization := float32(currentTasks) / float32(current) * 100
	
	var desired int32
	
	// Scale up if utilization is high
	if utilization > float32(autoScaling.TargetCPUUtilization) {
		desired = current + int32(math.Floor(float64(current) * float64(config.DefaultAutoScalingMultiplier)))
	} else if utilization < float32(autoScaling.TargetCPUUtilization)/config.LowUtilizationDivisor {
		desired = current - int32(math.Ceil(float64(current) * float64(config.DefaultAutoScalingMultiplier)))
	} else {
		desired = current // No change
	}
	
	// Ensure within bounds
	if desired < autoScaling.MinConcurrentTasks {
		desired = autoScaling.MinConcurrentTasks
	}
	if desired > autoScaling.MaxConcurrentTasks {
		desired = autoScaling.MaxConcurrentTasks
	}
	
	return desired
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPTaskSchedulerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crd.MCPTaskScheduler{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}