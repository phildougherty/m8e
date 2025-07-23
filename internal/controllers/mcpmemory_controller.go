// internal/controllers/mcpmemory_controller.go
package controllers

import (
	"context"
	"fmt"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
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

// MCPMemoryReconciler reconciles a MCPMemory object
type MCPMemoryReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *logging.Logger
	Config *config.ComposeConfig
}

//+kubebuilder:rbac:groups=mcp.matey.ai,resources=mcpmemories,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mcp.matey.ai,resources=mcpmemories/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mcp.matey.ai,resources=mcpmemories/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *MCPMemoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the MCPMemory instance
	memory := &crd.MCPMemory{}
	err := r.Get(ctx, req.NamespacedName, memory)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			logger.Info("MCPMemory resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get MCPMemory")
		return ctrl.Result{}, err
	}

	// Update status to reflect current phase
	if memory.Status.Phase == "" {
		memory.Status.Phase = crd.MCPMemoryPhasePending
		if err := r.Status().Update(ctx, memory); err != nil {
			logger.Error(err, "Failed to update MCPMemory status")
			return ctrl.Result{}, err
		}
	}

	// Handle reconciliation based on current phase
	switch memory.Status.Phase {
	case crd.MCPMemoryPhasePending:
		return r.reconcilePending(ctx, memory)
	case crd.MCPMemoryPhaseCreating:
		return r.reconcileCreating(ctx, memory)
	case crd.MCPMemoryPhaseStarting:
		return r.reconcileStarting(ctx, memory)
	case crd.MCPMemoryPhaseRunning:
		return r.reconcileRunning(ctx, memory)
	case crd.MCPMemoryPhaseFailed:
		return r.reconcileFailed(ctx, memory)
	case crd.MCPMemoryPhaseTerminating:
		return r.reconcileTerminating(ctx, memory)
	default:
		logger.Info("Unknown phase", "phase", memory.Status.Phase)
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
}

// reconcilePending handles the pending phase
func (r *MCPMemoryReconciler) reconcilePending(ctx context.Context, memory *crd.MCPMemory) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling pending MCPMemory", "name", memory.Name)

	// Update phase to creating
	memory.Status.Phase = crd.MCPMemoryPhaseCreating
	if err := r.Status().Update(ctx, memory); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{Requeue: true}, nil
}

// reconcileCreating handles the creating phase - creates all required resources
func (r *MCPMemoryReconciler) reconcileCreating(ctx context.Context, memory *crd.MCPMemory) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Creating MCPMemory resources", "name", memory.Name)

	// Create PostgreSQL resources first if enabled
	if memory.Spec.PostgresEnabled {
		if err := r.reconcilePostgresResources(ctx, memory); err != nil {
			logger.Error(err, "Failed to reconcile PostgreSQL resources")
			return ctrl.Result{}, err
		}
	}

	// Create Secret for sensitive data
	if err := r.reconcileSecret(ctx, memory); err != nil {
		logger.Error(err, "Failed to reconcile Secret")
		return ctrl.Result{}, err
	}

	// Create ConfigMap for memory server configuration
	if err := r.reconcileConfigMap(ctx, memory); err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap")
		return ctrl.Result{}, err
	}

	// Create Service for memory server
	if err := r.reconcileService(ctx, memory); err != nil {
		logger.Error(err, "Failed to reconcile Service")
		return ctrl.Result{}, err
	}

	// Create Deployment for memory server
	if err := r.reconcileDeployment(ctx, memory); err != nil {
		logger.Error(err, "Failed to reconcile Deployment")
		return ctrl.Result{}, err
	}

	// Update phase to starting
	memory.Status.Phase = crd.MCPMemoryPhaseStarting
	if err := r.Status().Update(ctx, memory); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Second * 10}, nil
}

// reconcileStarting handles the starting phase
func (r *MCPMemoryReconciler) reconcileStarting(ctx context.Context, memory *crd.MCPMemory) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Checking MCPMemory startup", "name", memory.Name)

	// Check if PostgreSQL is ready (if enabled)
	if memory.Spec.PostgresEnabled {
		postgresReady, err := r.isPostgresReady(ctx, memory)
		if err != nil {
			logger.Error(err, "Failed to check PostgreSQL status")
			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		}
		if !postgresReady {
			logger.Info("Waiting for PostgreSQL to become ready")
			r.updateCondition(memory, crd.MCPMemoryConditionPostgresReady, metav1.ConditionFalse, "PostgresNotReady", "PostgreSQL is not ready yet")
			if err := r.Status().Update(ctx, memory); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		}
		r.updateCondition(memory, crd.MCPMemoryConditionPostgresReady, metav1.ConditionTrue, "PostgresReady", "PostgreSQL is ready")
	}

	// Check if memory server deployment is ready
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      memory.Name,
		Namespace: memory.Namespace,
	}, deployment)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Deployment not found, going back to creating phase")
			memory.Status.Phase = crd.MCPMemoryPhaseCreating
			if err := r.Status().Update(ctx, memory); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Error(err, "Failed to get Deployment")
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	// Check if deployment is ready
	if deployment.Status.ReadyReplicas > 0 {
		// Update phase to running
		memory.Status.Phase = crd.MCPMemoryPhaseRunning
		memory.Status.ReadyReplicas = deployment.Status.ReadyReplicas
		memory.Status.Replicas = deployment.Status.Replicas

		// Update conditions
		r.updateCondition(memory, crd.MCPMemoryConditionReady, metav1.ConditionTrue, "DeploymentReady", "Memory server deployment is ready")
		r.updateCondition(memory, crd.MCPMemoryConditionHealthy, metav1.ConditionTrue, "Healthy", "Memory server is healthy")

		if err := r.Status().Update(ctx, memory); err != nil {
			return ctrl.Result{}, err
		}

		logger.Info("MCPMemory is now running", "name", memory.Name)
		return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
	}

	return ctrl.Result{RequeueAfter: time.Second * 10}, nil
}

// reconcileRunning handles the running phase - monitors health
func (r *MCPMemoryReconciler) reconcileRunning(ctx context.Context, memory *crd.MCPMemory) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Update deployment if spec has changed
	if err := r.reconcileDeployment(ctx, memory); err != nil {
		logger.Error(err, "Failed to reconcile Deployment during running phase")
		return ctrl.Result{}, err
	}

	// Check health of the deployment
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      memory.Name,
		Namespace: memory.Namespace,
	}, deployment)
	if err != nil {
		logger.Error(err, "Failed to get Deployment during health check")
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// Update status
	memory.Status.ReadyReplicas = deployment.Status.ReadyReplicas
	memory.Status.Replicas = deployment.Status.Replicas

	// Update health condition
	isHealthy := deployment.Status.ReadyReplicas > 0
	if isHealthy {
		r.updateCondition(memory, crd.MCPMemoryConditionHealthy, metav1.ConditionTrue, "Healthy", "Memory server is healthy")
	} else {
		r.updateCondition(memory, crd.MCPMemoryConditionHealthy, metav1.ConditionFalse, "Unhealthy", "Memory server has no ready replicas")
	}

	// Check PostgreSQL health if enabled
	if memory.Spec.PostgresEnabled {
		postgresHealthy, _ := r.isPostgresReady(ctx, memory)
		if postgresHealthy {
			r.updateCondition(memory, crd.MCPMemoryConditionPostgresReady, metav1.ConditionTrue, "PostgresHealthy", "PostgreSQL is healthy")
			memory.Status.PostgresStatus = "healthy"
		} else {
			r.updateCondition(memory, crd.MCPMemoryConditionPostgresReady, metav1.ConditionFalse, "PostgresUnhealthy", "PostgreSQL is not healthy")
			memory.Status.PostgresStatus = "unhealthy"
		}
	}

	if err := r.Status().Update(ctx, memory); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute * 2}, nil
}

// reconcileFailed handles the failed phase
func (r *MCPMemoryReconciler) reconcileFailed(ctx context.Context, memory *crd.MCPMemory) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("MCPMemory is in failed state", "name", memory.Name)

	// Try to restart by moving back to creating phase
	memory.Status.Phase = crd.MCPMemoryPhaseCreating
	if err := r.Status().Update(ctx, memory); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// reconcileTerminating handles the terminating phase
func (r *MCPMemoryReconciler) reconcileTerminating(ctx context.Context, memory *crd.MCPMemory) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Terminating MCPMemory", "name", memory.Name)

	// The deployment, service, and postgres resources will be automatically cleaned up by garbage collection
	// due to owner references

	return ctrl.Result{}, nil
}

// reconcileSecret creates or updates the Secret for sensitive data
func (r *MCPMemoryReconciler) reconcileSecret(ctx context.Context, memory *crd.MCPMemory) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      memory.Name + "-secret",
			Namespace: memory.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "memory",
				"app.kubernetes.io/instance":   memory.Name,
				"app.kubernetes.io/component":  "memory",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "memory",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"postgres-password": []byte(memory.Spec.PostgresPassword),
		},
	}

	// Set MCPMemory instance as the owner and controller
	if err := controllerutil.SetControllerReference(memory, secret, r.Scheme); err != nil {
		return err
	}

	// Create or update the Secret
	found := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, secret)
	} else if err != nil {
		return err
	}

	// Update if needed
	found.Data = secret.Data
	return r.Update(ctx, found)
}

// reconcileConfigMap creates or updates the ConfigMap for memory server configuration
func (r *MCPMemoryReconciler) reconcileConfigMap(ctx context.Context, memory *crd.MCPMemory) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      memory.Name + "-config",
			Namespace: memory.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "memory",
				"app.kubernetes.io/instance":   memory.Name,
				"app.kubernetes.io/component":  "memory",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "memory",
			},
		},
		Data: map[string]string{
			"config.yaml": r.generateMemoryConfig(memory),
		},
	}

	// Set MCPMemory instance as the owner and controller
	if err := controllerutil.SetControllerReference(memory, configMap, r.Scheme); err != nil {
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

// reconcileService creates or updates the Service for the memory server
func (r *MCPMemoryReconciler) reconcileService(ctx context.Context, memory *crd.MCPMemory) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      memory.Name,
			Namespace: memory.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "memory",
				"app.kubernetes.io/instance":   memory.Name,
				"app.kubernetes.io/component":  "memory",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "server",
				"mcp.matey.ai/server-name":    memory.Name,
				"mcp.matey.ai/protocol":       "http",
				"mcp.matey.ai/capabilities":   "tools",
			},
			Annotations: memory.Spec.ServiceAnnotations,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     memory.Spec.Port,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"app.kubernetes.io/name":     "memory",
				"app.kubernetes.io/instance": memory.Name,
			},
		},
	}

	// Override service type if specified
	if memory.Spec.ServiceType != "" {
		service.Spec.Type = corev1.ServiceType(memory.Spec.ServiceType)
	}

	// Set MCPMemory instance as the owner and controller
	if err := controllerutil.SetControllerReference(memory, service, r.Scheme); err != nil {
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

	return nil
}

// reconcileDeployment creates or updates the Deployment for the memory server
func (r *MCPMemoryReconciler) reconcileDeployment(ctx context.Context, memory *crd.MCPMemory) error {
	replicas := int32(1)
	if memory.Spec.Replicas != nil {
		replicas = *memory.Spec.Replicas
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      memory.Name,
			Namespace: memory.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "memory",
				"app.kubernetes.io/instance":   memory.Name,
				"app.kubernetes.io/component":  "memory",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "memory",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "memory",
					"app.kubernetes.io/instance": memory.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":       "memory",
						"app.kubernetes.io/instance":   memory.Name,
						"app.kubernetes.io/component":  "memory",
						"app.kubernetes.io/managed-by": "matey",
						"mcp.matey.ai/role":           "memory",
					},
					Annotations: memory.Spec.PodAnnotations,
				},
				Spec: r.buildMemoryPodSpec(memory),
			},
		},
	}

	// Set MCPMemory instance as the owner and controller
	if err := controllerutil.SetControllerReference(memory, deployment, r.Scheme); err != nil {
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

// reconcilePostgresResources creates PostgreSQL deployment and service
func (r *MCPMemoryReconciler) reconcilePostgresResources(ctx context.Context, memory *crd.MCPMemory) error {
	// Create PostgreSQL PVC
	if err := r.reconcilePostgresPVC(ctx, memory); err != nil {
		return err
	}

	// Create PostgreSQL Service
	if err := r.reconcilePostgresService(ctx, memory); err != nil {
		return err
	}

	// Create PostgreSQL Deployment
	return r.reconcilePostgresDeployment(ctx, memory)
}

// reconcilePostgresPVC creates the PVC for PostgreSQL
func (r *MCPMemoryReconciler) reconcilePostgresPVC(ctx context.Context, memory *crd.MCPMemory) error {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      memory.Name + "-postgres-data",
			Namespace: memory.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "postgres",
				"app.kubernetes.io/instance":   memory.Name + "-postgres",
				"app.kubernetes.io/component":  "database",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "database",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	// Set MCPMemory instance as the owner and controller
	if err := controllerutil.SetControllerReference(memory, pvc, r.Scheme); err != nil {
		return err
	}

	// Create PVC if it doesn't exist
	found := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, pvc)
	}
	return err
}

// reconcilePostgresService creates the PostgreSQL service
func (r *MCPMemoryReconciler) reconcilePostgresService(ctx context.Context, memory *crd.MCPMemory) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      memory.Name + "-postgres",
			Namespace: memory.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "postgres",
				"app.kubernetes.io/instance":   memory.Name + "-postgres",
				"app.kubernetes.io/component":  "database",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "database",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:     "postgres",
					Port:     memory.Spec.PostgresPort,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"app.kubernetes.io/name":     "postgres",
				"app.kubernetes.io/instance": memory.Name + "-postgres",
			},
		},
	}

	// Set MCPMemory instance as the owner and controller
	if err := controllerutil.SetControllerReference(memory, service, r.Scheme); err != nil {
		return err
	}

	// Create or update the Service
	found := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, service)
	}
	return err
}

// reconcilePostgresDeployment creates the PostgreSQL deployment
func (r *MCPMemoryReconciler) reconcilePostgresDeployment(ctx context.Context, memory *crd.MCPMemory) error {
	replicas := int32(1)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      memory.Name + "-postgres",
			Namespace: memory.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "postgres",
				"app.kubernetes.io/instance":   memory.Name + "-postgres",
				"app.kubernetes.io/component":  "database",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "database",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "postgres",
					"app.kubernetes.io/instance": memory.Name + "-postgres",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":       "postgres",
						"app.kubernetes.io/instance":   memory.Name + "-postgres",
						"app.kubernetes.io/component":  "database",
						"app.kubernetes.io/managed-by": "matey",
						"mcp.matey.ai/role":           "database",
					},
				},
				Spec: r.buildPostgresPodSpec(memory),
			},
		},
	}

	// Set MCPMemory instance as the owner and controller
	if err := controllerutil.SetControllerReference(memory, deployment, r.Scheme); err != nil {
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

// buildMemoryPodSpec creates the pod specification for the memory server
func (r *MCPMemoryReconciler) buildMemoryPodSpec(memory *crd.MCPMemory) corev1.PodSpec {
	image := "mcpcompose/memory:latest"
	if r.Config != nil {
		image = r.Config.GetRegistryImage(image)
	}

	// Build environment variables
	env := []corev1.EnvVar{
		{Name: "NODE_ENV", Value: "production"},
		{Name: "PORT", Value: strconv.Itoa(int(memory.Spec.Port))},
		{Name: "HOST", Value: memory.Spec.Host},
	}

	// Add database URL
	if memory.Spec.DatabaseURL != "" {
		env = append(env, corev1.EnvVar{Name: "DATABASE_URL", Value: memory.Spec.DatabaseURL})
	} else if memory.Spec.PostgresEnabled {
		// Build database URL from components
		dbURL := fmt.Sprintf("postgresql://%s@%s-postgres:%d/%s?sslmode=disable",
			memory.Spec.PostgresUser,
			memory.Name,
			memory.Spec.PostgresPort,
			memory.Spec.PostgresDB)
		env = append(env, corev1.EnvVar{Name: "DATABASE_URL", Value: dbURL})
	}

	// Add PostgreSQL password from secret
	if memory.Spec.PostgresEnabled {
		env = append(env, corev1.EnvVar{
			Name: "POSTGRES_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: memory.Name + "-secret",
					},
					Key: "postgres-password",
				},
			},
		})
	}

	container := corev1.Container{
		Name:            "memory",
		Image:           image,
		ImagePullPolicy: corev1.PullAlways,
		Env:             env,
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: memory.Spec.Port,
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
					Port: intstr.FromInt32(memory.Spec.Port),
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/health",
					Port: intstr.FromInt32(memory.Spec.Port),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
		},
	}

	// Apply resource requirements
	if memory.Spec.CPUs != "" || memory.Spec.Memory != "" {
		container.Resources = corev1.ResourceRequirements{
			Requests: make(corev1.ResourceList),
			Limits:   make(corev1.ResourceList),
		}

		if memory.Spec.CPUs != "" {
			if qty, err := resource.ParseQuantity(memory.Spec.CPUs); err == nil {
				container.Resources.Requests[corev1.ResourceCPU] = qty
				container.Resources.Limits[corev1.ResourceCPU] = qty
			}
		}
		if memory.Spec.Memory != "" {
			if qty, err := resource.ParseQuantity(memory.Spec.Memory); err == nil {
				container.Resources.Requests[corev1.ResourceMemory] = qty
				container.Resources.Limits[corev1.ResourceMemory] = qty
			}
		}
	}

	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{container},
		Volumes: []corev1.Volume{
			{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: memory.Name + "-config",
						},
					},
				},
			},
		},
		RestartPolicy: corev1.RestartPolicyAlways,
	}

	// Apply node selector
	if len(memory.Spec.NodeSelector) > 0 {
		podSpec.NodeSelector = memory.Spec.NodeSelector
	}

	// Apply service account
	if memory.Spec.ServiceAccount != "" {
		podSpec.ServiceAccountName = memory.Spec.ServiceAccount
	}

	return podSpec
}

// buildPostgresPodSpec creates the pod specification for PostgreSQL
func (r *MCPMemoryReconciler) buildPostgresPodSpec(memory *crd.MCPMemory) corev1.PodSpec {
	container := corev1.Container{
		Name:            "postgres",
		Image:           "postgres:15-alpine",
		ImagePullPolicy: corev1.PullAlways,
		Env: []corev1.EnvVar{
			{Name: "POSTGRES_DB", Value: memory.Spec.PostgresDB},
			{Name: "POSTGRES_USER", Value: memory.Spec.PostgresUser},
			{
				Name: "POSTGRES_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: memory.Name + "-secret",
						},
						Key: "postgres-password",
					},
				},
			},
			{Name: "PGDATA", Value: "/var/lib/postgresql/data/pgdata"},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "postgres",
				ContainerPort: memory.Spec.PostgresPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "postgres-data",
				MountPath: "/var/lib/postgresql/data",
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"pg_isready", "-U", memory.Spec.PostgresUser},
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"pg_isready", "-U", memory.Spec.PostgresUser},
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
		},
	}

	// Apply PostgreSQL resource requirements
	if memory.Spec.PostgresCPUs != "" || memory.Spec.PostgresMemory != "" {
		container.Resources = corev1.ResourceRequirements{
			Requests: make(corev1.ResourceList),
			Limits:   make(corev1.ResourceList),
		}

		if memory.Spec.PostgresCPUs != "" {
			if qty, err := resource.ParseQuantity(memory.Spec.PostgresCPUs); err == nil {
				container.Resources.Requests[corev1.ResourceCPU] = qty
				container.Resources.Limits[corev1.ResourceCPU] = qty
			}
		}
		if memory.Spec.PostgresMemory != "" {
			if qty, err := resource.ParseQuantity(memory.Spec.PostgresMemory); err == nil {
				container.Resources.Requests[corev1.ResourceMemory] = qty
				container.Resources.Limits[corev1.ResourceMemory] = qty
			}
		}
	}

	return corev1.PodSpec{
		Containers: []corev1.Container{container},
		Volumes: []corev1.Volume{
			{
				Name: "postgres-data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: memory.Name + "-postgres-data",
					},
				},
			},
		},
		RestartPolicy: corev1.RestartPolicyAlways,
	}
}

// generateMemoryConfig generates the configuration file for the memory server
func (r *MCPMemoryReconciler) generateMemoryConfig(memory *crd.MCPMemory) string {
	config := fmt.Sprintf(`# Memory Server Configuration
port: %d
host: %s

# Database Configuration
postgres_enabled: %t
database_url: %s

# Kubernetes Configuration
kubernetes_mode: true
namespace: %s
`,
		memory.Spec.Port,
		memory.Spec.Host,
		memory.Spec.PostgresEnabled,
		memory.Spec.DatabaseURL,
		memory.Namespace,
	)

	return config
}

// isPostgresReady checks if PostgreSQL is ready
func (r *MCPMemoryReconciler) isPostgresReady(ctx context.Context, memory *crd.MCPMemory) (bool, error) {
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      memory.Name + "-postgres",
		Namespace: memory.Namespace,
	}, deployment)
	if err != nil {
		return false, err
	}

	return deployment.Status.ReadyReplicas > 0, nil
}

// updateCondition updates a condition in the memory status
func (r *MCPMemoryReconciler) updateCondition(memory *crd.MCPMemory, conditionType crd.MCPMemoryConditionType, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()

	// Find existing condition
	for i, condition := range memory.Status.Conditions {
		if condition.Type == conditionType {
			if condition.Status != status {
				memory.Status.Conditions[i].Status = status
				memory.Status.Conditions[i].LastTransitionTime = now
				memory.Status.Conditions[i].Reason = reason
				memory.Status.Conditions[i].Message = message
			}
			return
		}
	}

	// Add new condition
	memory.Status.Conditions = append(memory.Status.Conditions, crd.MCPMemoryCondition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPMemoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crd.MCPMemory{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Complete(r)
}