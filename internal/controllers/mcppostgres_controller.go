// internal/controllers/mcppostgres_controller.go
package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/phildougherty/m8e/internal/crd"
)

// MCPPostgresReconciler reconciles a MCPPostgres object
type MCPPostgresReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=mcp.matey.ai,resources=mcppostgres,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mcp.matey.ai,resources=mcppostgres/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mcp.matey.ai,resources=mcppostgres/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

func (r *MCPPostgresReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("mcppostgres", req.NamespacedName)

	// Fetch the MCPPostgres instance
	postgres := &crd.MCPPostgres{}
	err := r.Get(ctx, req.NamespacedName, postgres)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("MCPPostgres resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get MCPPostgres")
		return ctrl.Result{}, err
	}

	// Set default values
	r.applyDefaults(postgres)

	// Handle deletion
	if postgres.GetDeletionTimestamp() != nil {
		return r.handleDeletion(ctx, postgres, log)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(postgres, "postgres.finalizer.mcp.matey.ai") {
		controllerutil.AddFinalizer(postgres, "postgres.finalizer.mcp.matey.ai")
		return ctrl.Result{}, r.Update(ctx, postgres)
	}

	// Update status to Creating if it's empty
	if postgres.Status.Phase == "" {
		postgres.Status.Phase = crd.PostgresPhaseCreating
		postgres.Status.ObservedGeneration = postgres.Generation
		if err := r.Status().Update(ctx, postgres); err != nil {
			log.Error(err, "Failed to update status to Creating")
			return ctrl.Result{}, err
		}
	}

	// Create or update resources
	if err := r.reconcileSecret(ctx, postgres, log); err != nil {
		return r.updateStatusAndRequeue(ctx, postgres, crd.PostgresPhaseFailed, fmt.Sprintf("Failed to reconcile secret: %v", err), log)
	}

	if err := r.reconcilePVC(ctx, postgres, log); err != nil {
		return r.updateStatusAndRequeue(ctx, postgres, crd.PostgresPhaseFailed, fmt.Sprintf("Failed to reconcile PVC: %v", err), log)
	}

	if err := r.reconcileService(ctx, postgres, log); err != nil {
		return r.updateStatusAndRequeue(ctx, postgres, crd.PostgresPhaseFailed, fmt.Sprintf("Failed to reconcile service: %v", err), log)
	}

	if err := r.reconcileDeployment(ctx, postgres, log); err != nil {
		return r.updateStatusAndRequeue(ctx, postgres, crd.PostgresPhaseFailed, fmt.Sprintf("Failed to reconcile deployment: %v", err), log)
	}

	// Check deployment status and update accordingly
	return r.checkAndUpdateStatus(ctx, postgres, log)
}

func (r *MCPPostgresReconciler) applyDefaults(postgres *crd.MCPPostgres) {
	if postgres.Spec.Database == "" {
		postgres.Spec.Database = "matey"
	}
	if postgres.Spec.User == "" {
		postgres.Spec.User = "postgres"
	}
	if postgres.Spec.Password == "" {
		postgres.Spec.Password = "password"
	}
	if postgres.Spec.Port == 0 {
		postgres.Spec.Port = 5432
	}
	if postgres.Spec.StorageSize == "" {
		postgres.Spec.StorageSize = "10Gi"
	}
	if postgres.Spec.Version == "" {
		postgres.Spec.Version = "15-alpine"
	}
	if postgres.Spec.Replicas == 0 {
		postgres.Spec.Replicas = 1
	}
	if postgres.Spec.Resources == nil {
		postgres.Spec.Resources = &crd.ResourceRequirements{
			Limits: crd.ResourceList{
				"cpu":    "1000m",
				"memory": "1Gi",
			},
			Requests: crd.ResourceList{
				"cpu":    "500m",
				"memory": "512Mi",
			},
		}
	}
}

func (r *MCPPostgresReconciler) reconcileSecret(ctx context.Context, postgres *crd.MCPPostgres, log logr.Logger) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgres.Name + "-secret",
			Namespace: postgres.Namespace,
			Labels:    r.getLabels(postgres),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"postgres-password": []byte(postgres.Spec.Password),
		},
	}

	if err := controllerutil.SetControllerReference(postgres, secret, r.Scheme); err != nil {
		return err
	}

	foundSecret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKeyFromObject(secret), foundSecret)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating secret", "secret", secret.Name)
		return r.Create(ctx, secret)
	} else if err != nil {
		return err
	}

	// Update secret if needed
	if string(foundSecret.Data["postgres-password"]) != postgres.Spec.Password {
		foundSecret.Data["postgres-password"] = []byte(postgres.Spec.Password)
		log.Info("Updating secret", "secret", secret.Name)
		return r.Update(ctx, foundSecret)
	}

	return nil
}

func (r *MCPPostgresReconciler) reconcilePVC(ctx context.Context, postgres *crd.MCPPostgres, log logr.Logger) error {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgres.Name + "-data",
			Namespace: postgres.Namespace,
			Labels:    r.getLabels(postgres),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(postgres.Spec.StorageSize),
				},
			},
		},
	}

	if postgres.Spec.StorageClassName != "" {
		pvc.Spec.StorageClassName = &postgres.Spec.StorageClassName
	}

	if err := controllerutil.SetControllerReference(postgres, pvc, r.Scheme); err != nil {
		return err
	}

	foundPVC := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, client.ObjectKeyFromObject(pvc), foundPVC)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating PVC", "pvc", pvc.Name)
		return r.Create(ctx, pvc)
	} else if err != nil {
		return err
	}

	return nil
}

func (r *MCPPostgresReconciler) reconcileService(ctx context.Context, postgres *crd.MCPPostgres, log logr.Logger) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgres.Name,
			Namespace: postgres.Namespace,
			Labels:    r.getLabels(postgres),
		},
		Spec: corev1.ServiceSpec{
			Selector: r.getSelectorLabels(postgres),
			Ports: []corev1.ServicePort{
				{
					Name:       "postgres",
					Port:       postgres.Spec.Port,
					TargetPort: intstr.FromInt(int(postgres.Spec.Port)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	if err := controllerutil.SetControllerReference(postgres, service, r.Scheme); err != nil {
		return err
	}

	foundService := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKeyFromObject(service), foundService)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating service", "service", service.Name)
		return r.Create(ctx, service)
	} else if err != nil {
		return err
	}

	// Update service if needed
	if foundService.Spec.Ports[0].Port != postgres.Spec.Port {
		foundService.Spec.Ports[0].Port = postgres.Spec.Port
		foundService.Spec.Ports[0].TargetPort = intstr.FromInt(int(postgres.Spec.Port))
		log.Info("Updating service", "service", service.Name)
		return r.Update(ctx, foundService)
	}

	return nil
}

func (r *MCPPostgresReconciler) reconcileDeployment(ctx context.Context, postgres *crd.MCPPostgres, log logr.Logger) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgres.Name,
			Namespace: postgres.Namespace,
			Labels:    r.getLabels(postgres),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &postgres.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: r.getSelectorLabels(postgres),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: r.getLabels(postgres),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "postgres",
							Image: fmt.Sprintf("postgres:%s", postgres.Spec.Version),
							Ports: []corev1.ContainerPort{
								{
									Name:          "postgres",
									ContainerPort: postgres.Spec.Port,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: r.getEnvVars(postgres),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "postgres-data",
									MountPath: "/var/lib/postgresql/data",
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"pg_isready", "-U", postgres.Spec.User},
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
								TimeoutSeconds:      1,
								FailureThreshold:    3,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"pg_isready", "-U", postgres.Spec.User},
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
								TimeoutSeconds:      1,
								FailureThreshold:    3,
							},
							Resources: r.getResourceRequirements(postgres),
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "postgres-data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: postgres.Name + "-data",
								},
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(postgres, deployment, r.Scheme); err != nil {
		return err
	}

	foundDeployment := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKeyFromObject(deployment), foundDeployment)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating deployment", "deployment", deployment.Name)
		return r.Create(ctx, deployment)
	} else if err != nil {
		return err
	}

	// Update deployment if needed
	if r.needsDeploymentUpdate(foundDeployment, deployment) {
		foundDeployment.Spec = deployment.Spec
		log.Info("Updating deployment", "deployment", deployment.Name)
		return r.Update(ctx, foundDeployment)
	}

	return nil
}

func (r *MCPPostgresReconciler) needsDeploymentUpdate(found, desired *appsv1.Deployment) bool {
	// Simple comparison - in production you'd want more sophisticated comparison
	if *found.Spec.Replicas != *desired.Spec.Replicas {
		return true
	}
	if found.Spec.Template.Spec.Containers[0].Image != desired.Spec.Template.Spec.Containers[0].Image {
		return true
	}
	return false
}

func (r *MCPPostgresReconciler) checkAndUpdateStatus(ctx context.Context, postgres *crd.MCPPostgres, log logr.Logger) (ctrl.Result, error) {
	// Get deployment status
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Name: postgres.Name, Namespace: postgres.Namespace}, deployment)
	if err != nil {
		return r.updateStatusAndRequeue(ctx, postgres, crd.PostgresPhaseFailed, fmt.Sprintf("Failed to get deployment: %v", err), log)
	}

	// Update status based on deployment
	readyReplicas := deployment.Status.ReadyReplicas
	totalReplicas := deployment.Status.Replicas

	postgres.Status.ReadyReplicas = readyReplicas
	postgres.Status.Replicas = totalReplicas
	postgres.Status.ServiceEndpoint = fmt.Sprintf("%s.%s.svc.cluster.local", postgres.Name, postgres.Namespace)
	postgres.Status.ServicePort = postgres.Spec.Port
	postgres.Status.StorageSize = postgres.Spec.StorageSize
	postgres.Status.ObservedGeneration = postgres.Generation

	// Determine phase
	var phase crd.PostgresPhase
	var message string

	if readyReplicas == postgres.Spec.Replicas && totalReplicas == postgres.Spec.Replicas {
		phase = crd.PostgresPhaseRunning
		postgres.Status.DatabaseReady = true
		message = "PostgreSQL is running and ready"
	} else if readyReplicas > 0 {
		phase = crd.PostgresPhaseDegraded
		message = fmt.Sprintf("PostgreSQL partially ready: %d/%d replicas", readyReplicas, postgres.Spec.Replicas)
	} else {
		phase = crd.PostgresPhaseCreating
		message = "PostgreSQL is starting up"
	}

	postgres.Status.Phase = phase
	postgres.Status.DatabaseError = ""

	// Update conditions
	now := metav1.Now()
	r.updateCondition(postgres, crd.PostgresConditionReady, readyReplicas > 0, "PostgresReady", message, now)
	r.updateCondition(postgres, crd.PostgresConditionDatabaseReady, readyReplicas > 0, "DatabaseReady", message, now)

	if err := r.Status().Update(ctx, postgres); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	// Requeue if not fully ready
	if phase != crd.PostgresPhaseRunning {
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

func (r *MCPPostgresReconciler) updateCondition(postgres *crd.MCPPostgres, conditionType crd.PostgresConditionType, status bool, reason, message string, now metav1.Time) {
	condStatus := metav1.ConditionFalse
	if status {
		condStatus = metav1.ConditionTrue
	}

	// Find existing condition
	for i, condition := range postgres.Status.Conditions {
		if condition.Type == conditionType {
			if condition.Status != condStatus {
				postgres.Status.Conditions[i].Status = condStatus
				postgres.Status.Conditions[i].LastTransitionTime = now
				postgres.Status.Conditions[i].Reason = reason
				postgres.Status.Conditions[i].Message = message
			}
			return
		}
	}

	// Add new condition
	postgres.Status.Conditions = append(postgres.Status.Conditions, crd.PostgresCondition{
		Type:               conditionType,
		Status:             condStatus,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	})
}

func (r *MCPPostgresReconciler) updateStatusAndRequeue(ctx context.Context, postgres *crd.MCPPostgres, phase crd.PostgresPhase, message string, log logr.Logger) (ctrl.Result, error) {
	postgres.Status.Phase = phase
	postgres.Status.DatabaseError = message
	postgres.Status.ObservedGeneration = postgres.Generation

	if err := r.Status().Update(ctx, postgres); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Second * 30}, nil
}

func (r *MCPPostgresReconciler) handleDeletion(ctx context.Context, postgres *crd.MCPPostgres, log logr.Logger) (ctrl.Result, error) {
	log.Info("Handling PostgreSQL deletion")

	// Update status to terminating
	if postgres.Status.Phase != crd.PostgresPhaseTerminating {
		postgres.Status.Phase = crd.PostgresPhaseTerminating
		if err := r.Status().Update(ctx, postgres); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Remove finalizer to allow deletion
	controllerutil.RemoveFinalizer(postgres, "postgres.finalizer.mcp.matey.ai")
	return ctrl.Result{}, r.Update(ctx, postgres)
}

func (r *MCPPostgresReconciler) getLabels(postgres *crd.MCPPostgres) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "postgres",
		"app.kubernetes.io/instance":   postgres.Name,
		"app.kubernetes.io/component":  "database",
		"app.kubernetes.io/managed-by": "matey",
		"mcp.matey.ai/role":           "database",
	}
}

func (r *MCPPostgresReconciler) getSelectorLabels(postgres *crd.MCPPostgres) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     "postgres",
		"app.kubernetes.io/instance": postgres.Name,
	}
}

func (r *MCPPostgresReconciler) getEnvVars(postgres *crd.MCPPostgres) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{
			Name:  "POSTGRES_DB",
			Value: postgres.Spec.Database,
		},
		{
			Name:  "POSTGRES_USER",
			Value: postgres.Spec.User,
		},
		{
			Name: "POSTGRES_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: postgres.Name + "-secret",
					},
					Key: "postgres-password",
				},
			},
		},
		{
			Name:  "PGDATA",
			Value: "/var/lib/postgresql/data/pgdata",
		},
	}

	// Add custom postgres config
	for key, value := range postgres.Spec.PostgresConfig {
		envVars = append(envVars, corev1.EnvVar{
			Name:  key,
			Value: value,
		})
	}

	return envVars
}

func (r *MCPPostgresReconciler) getResourceRequirements(postgres *crd.MCPPostgres) corev1.ResourceRequirements {
	resources := corev1.ResourceRequirements{}

	if postgres.Spec.Resources != nil {
		if len(postgres.Spec.Resources.Limits) > 0 {
			resources.Limits = corev1.ResourceList{}
			if postgres.Spec.Resources.Limits["cpu"] != "" {
				resources.Limits[corev1.ResourceCPU] = resource.MustParse(postgres.Spec.Resources.Limits["cpu"])
			}
			if postgres.Spec.Resources.Limits["memory"] != "" {
				resources.Limits[corev1.ResourceMemory] = resource.MustParse(postgres.Spec.Resources.Limits["memory"])
			}
		}
		if len(postgres.Spec.Resources.Requests) > 0 {
			resources.Requests = corev1.ResourceList{}
			if postgres.Spec.Resources.Requests["cpu"] != "" {
				resources.Requests[corev1.ResourceCPU] = resource.MustParse(postgres.Spec.Resources.Requests["cpu"])
			}
			if postgres.Spec.Resources.Requests["memory"] != "" {
				resources.Requests[corev1.ResourceMemory] = resource.MustParse(postgres.Spec.Resources.Requests["memory"])
			}
		}
	}

	return resources
}

// SetupWithManager sets up the controller with the Manager
func (r *MCPPostgresReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crd.MCPPostgres{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Complete(r)
}