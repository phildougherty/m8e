package controllers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/discovery"
	"github.com/phildougherty/m8e/internal/logging"
)

// MCPProxyReconciler reconciles a MCPProxy object
type MCPProxyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *logging.Logger
	
	// Enhanced service discovery integration
	ServiceDiscovery *discovery.K8sServiceDiscovery
	ConnectionManager *discovery.DynamicConnectionManager
	
	// Configuration for reliability features
	MaxRetries      int
	RetryDelay      time.Duration
	CircuitBreaker  bool
	HealthInterval  time.Duration
}

//+kubebuilder:rbac:groups=mcp.phildougherty.com,resources=mcpproxies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mcp.phildougherty.com,resources=mcpproxies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mcp.phildougherty.com,resources=mcpproxies/finalizers,verbs=update
//+kubebuilder:rbac:groups=mcp.phildougherty.com,resources=mcpservers,verbs=get;list;watch
//+kubebuilder:rbac:groups=mcp.phildougherty.com,resources=mcpmemories,verbs=get;list;watch
//+kubebuilder:rbac:groups=mcp.phildougherty.com,resources=mcptoolboxes,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *MCPProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the MCPProxy instance
	var mcpProxy crd.MCPProxy
	if err := r.Get(ctx, req.NamespacedName, &mcpProxy); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			logger.Info("MCPProxy resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get MCPProxy")
		return ctrl.Result{}, err
	}

	// Only update status if it's not already in a processing state
	if mcpProxy.Status.Phase != crd.MCPProxyPhaseCreating && mcpProxy.Status.Phase != crd.MCPProxyPhaseRunning {
		mcpProxy.Status.Phase = crd.MCPProxyPhaseCreating
		if err := r.Status().Update(ctx, &mcpProxy); err != nil {
			logger.Error(err, "Failed to update MCPProxy status")
			return ctrl.Result{}, err
		}
	}

	// Reconcile Deployment
	if err := r.reconcileDeployment(ctx, &mcpProxy); err != nil {
		logger.Error(err, "Failed to reconcile Deployment")
		mcpProxy.Status.Phase = crd.MCPProxyPhaseFailed
		r.Status().Update(ctx, &mcpProxy)
		return ctrl.Result{}, err
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, &mcpProxy); err != nil {
		logger.Error(err, "Failed to reconcile Service")
		mcpProxy.Status.Phase = crd.MCPProxyPhaseFailed
		r.Status().Update(ctx, &mcpProxy)
		return ctrl.Result{}, err
	}

	// Reconcile Ingress if enabled
	if mcpProxy.Spec.Ingress != nil && mcpProxy.Spec.Ingress.Enabled {
		if err := r.reconcileIngress(ctx, &mcpProxy); err != nil {
			logger.Error(err, "Failed to reconcile Ingress")
			mcpProxy.Status.Phase = crd.MCPProxyPhaseFailed
			r.Status().Update(ctx, &mcpProxy)
			return ctrl.Result{}, err
		}
	}

	// Reconcile ConfigMap for dynamic configuration
	if err := r.reconcileConfigMap(ctx, &mcpProxy); err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap")
		return ctrl.Result{}, err
	}

	// Reconcile Pod Disruption Budget for high availability
	if err := r.reconcilePodDisruptionBudget(ctx, &mcpProxy); err != nil {
		logger.Error(err, "Failed to reconcile PodDisruptionBudget")
		return ctrl.Result{}, err
	}

	// Enhanced service discovery integration
	if err := r.reconcileServiceDiscovery(ctx, &mcpProxy); err != nil {
		logger.Error(err, "Failed to reconcile service discovery")
		return ctrl.Result{}, err
	}

	// Cross-controller integration
	if err := r.reconcileCrossControllerIntegration(ctx, &mcpProxy); err != nil {
		logger.Error(err, "Failed to reconcile cross-controller integration")
		return ctrl.Result{}, err
	}

	// Check deployment status and update ReadyReplicas
	if err := r.updateDeploymentStatus(ctx, &mcpProxy); err != nil {
		logger.Error(err, "Failed to update deployment status")
		return ctrl.Result{}, err // Let controller-runtime handle retry
	}

	logger.V(1).Info("Successfully reconciled MCPProxy")
	return ctrl.Result{}, nil // Event-driven: only reconcile when resources change
}

func (r *MCPProxyReconciler) reconcileDeployment(ctx context.Context, mcpProxy *crd.MCPProxy) error {
	logger := log.FromContext(ctx)

	// Define the desired Deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpProxy.Name,
			Namespace: mcpProxy.Namespace,
			Labels:    r.labelsForMCPProxy(mcpProxy),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: mcpProxy.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: r.labelsForMCPProxy(mcpProxy),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: r.labelsForMCPProxy(mcpProxy),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: r.getServiceAccount(mcpProxy),
					Containers: []corev1.Container{
						{
							Name:            "proxy",
							Image:           "mcp.robotrad.io/matey:latest",
							ImagePullPolicy: corev1.PullAlways,
							Command:         []string{"./matey", "serve-proxy"},
							Args:            r.getProxyArgsWithConfig(mcpProxy),
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: r.getPort(mcpProxy),
									Name:          "http",
								},
							},
							Env: r.getEnvVars(mcpProxy),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: "/etc/matey",
									ReadOnly:  true,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: r.getMemoryRequest(mcpProxy),
									corev1.ResourceCPU:    r.getCPURequest(mcpProxy),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: r.getMemoryLimit(mcpProxy),
									corev1.ResourceCPU:    r.getCPULimit(mcpProxy),
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(int(r.getPort(mcpProxy))),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(int(r.getPort(mcpProxy))),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: mcpProxy.Name + "-config",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Set MCPProxy instance as the owner and controller
	if err := controllerutil.SetControllerReference(mcpProxy, deployment, r.Scheme); err != nil {
		return err
	}

	// Check if this Deployment already exists
	found := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Creating a new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
		if err := r.Create(ctx, deployment); err != nil {
			logger.Error(err, "Failed to create new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
			return err
		}
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to get Deployment")
		return err
	}

	// Update the found object and write the result back if there are any changes
	if !r.deploymentEqual(deployment, found) {
		// Retry update with exponential backoff on conflicts
		retries := 3
		for i := 0; i < retries; i++ {
			// Get fresh copy of the deployment to ensure we have the latest resource version
			if i > 0 {
				if err := r.Get(ctx, client.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}, found); err != nil {
					logger.Error(err, "Failed to get fresh Deployment for retry", "attempt", i+1)
					return err
				}
			}
			
			found.Spec = deployment.Spec
			found.ObjectMeta.Labels = deployment.ObjectMeta.Labels
			logger.Info("Updating Deployment", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name, "attempt", i+1)
			
			if err := r.Update(ctx, found); err != nil {
				if errors.IsConflict(err) && i < retries-1 {
					logger.Info("Deployment update conflict, retrying", "attempt", i+1, "error", err)
					time.Sleep(time.Duration(i+1) * 100 * time.Millisecond) // Exponential backoff
					continue
				}
				logger.Error(err, "Failed to update Deployment", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)
				return err
			}
			break // Success
		}
	}

	return nil
}

func (r *MCPProxyReconciler) updateDeploymentStatus(ctx context.Context, mcpProxy *crd.MCPProxy) error {
	logger := log.FromContext(ctx)

	// Get the deployment
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{
		Name:      mcpProxy.Name,
		Namespace: mcpProxy.Namespace,
	}, deployment)
	if err != nil {
		logger.Error(err, "Failed to get Deployment for status check")
		return err
	}

	// Check if status needs updating to avoid unnecessary updates
	newReplicas := deployment.Status.Replicas
	newReadyReplicas := deployment.Status.ReadyReplicas
	newObservedGeneration := mcpProxy.Generation
	
	var newPhase crd.MCPProxyPhase
	if deployment.Status.ReadyReplicas > 0 {
		newPhase = crd.MCPProxyPhaseRunning
	} else {
		newPhase = crd.MCPProxyPhaseStarting
	}

	// Only update status if something actually changed
	statusChanged := mcpProxy.Status.Replicas != newReplicas ||
		mcpProxy.Status.ReadyReplicas != newReadyReplicas ||
		mcpProxy.Status.ObservedGeneration != newObservedGeneration ||
		mcpProxy.Status.Phase != newPhase

	if statusChanged {
		mcpProxy.Status.Replicas = newReplicas
		mcpProxy.Status.ReadyReplicas = newReadyReplicas
		mcpProxy.Status.ObservedGeneration = newObservedGeneration
		mcpProxy.Status.Phase = newPhase

		if err := r.Status().Update(ctx, mcpProxy); err != nil {
			logger.Error(err, "Failed to update MCPProxy status")
			return err
		}
		logger.V(1).Info("Updated MCPProxy status", "phase", newPhase, "readyReplicas", newReadyReplicas)
	}

	return nil
}

func (r *MCPProxyReconciler) reconcileService(ctx context.Context, mcpProxy *crd.MCPProxy) error {
	logger := log.FromContext(ctx)

	// Define the desired Service
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpProxy.Name,
			Namespace: mcpProxy.Namespace,
			Labels:    r.labelsForMCPProxy(mcpProxy),
		},
		Spec: corev1.ServiceSpec{
			Selector: r.labelsForMCPProxy(mcpProxy),
			Ports: []corev1.ServicePort{
				{
					Port:       r.getPort(mcpProxy),
					TargetPort: intstr.FromInt(int(r.getPort(mcpProxy))),
					NodePort:   r.getNodePort(mcpProxy),
					Protocol:   corev1.ProtocolTCP,
					Name:       "http",
				},
			},
			Type: r.getServiceType(mcpProxy),
		},
	}

	// Set MCPProxy instance as the owner and controller
	if err := controllerutil.SetControllerReference(mcpProxy, service, r.Scheme); err != nil {
		return err
	}

	// Check if this Service already exists
	found := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Creating a new Service", "Service.Namespace", service.Namespace, "Service.Name", service.Name)
		if err := r.Create(ctx, service); err != nil {
			logger.Error(err, "Failed to create new Service", "Service.Namespace", service.Namespace, "Service.Name", service.Name)
			return err
		}
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to get Service")
		return err
	}

	// Update the found object if needed
	if found.Spec.Type != service.Spec.Type || len(found.Spec.Ports) != len(service.Spec.Ports) {
		found.Spec.Type = service.Spec.Type
		found.Spec.Ports = service.Spec.Ports
		logger.Info("Updating Service", "Service.Namespace", found.Namespace, "Service.Name", found.Name)
		if err := r.Update(ctx, found); err != nil {
			logger.Error(err, "Failed to update Service", "Service.Namespace", found.Namespace, "Service.Name", found.Name)
			return err
		}
	}

	return nil
}

func (r *MCPProxyReconciler) reconcileIngress(ctx context.Context, mcpProxy *crd.MCPProxy) error {
	logger := log.FromContext(ctx)

	if mcpProxy.Spec.Ingress == nil || !mcpProxy.Spec.Ingress.Enabled {
		return nil
	}

	pathType := networkingv1.PathTypePrefix
	// Define the desired Ingress
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        mcpProxy.Name,
			Namespace:   mcpProxy.Namespace,
			Labels:      r.labelsForMCPProxy(mcpProxy),
			Annotations: mcpProxy.Spec.Ingress.Annotations,
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: mcpProxy.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: mcpProxy.Name,
											Port: networkingv1.ServiceBackendPort{
												Number: r.getPort(mcpProxy),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Set MCPProxy instance as the owner and controller
	if err := controllerutil.SetControllerReference(mcpProxy, ingress, r.Scheme); err != nil {
		return err
	}

	// Check if this Ingress already exists
	found := &networkingv1.Ingress{}
	err := r.Get(ctx, client.ObjectKey{Name: ingress.Name, Namespace: ingress.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Creating a new Ingress", "Ingress.Namespace", ingress.Namespace, "Ingress.Name", ingress.Name)
		if err := r.Create(ctx, ingress); err != nil {
			logger.Error(err, "Failed to create new Ingress", "Ingress.Namespace", ingress.Namespace, "Ingress.Name", ingress.Name)
			return err
		}
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to get Ingress")
		return err
	}

	// Update if needed
	found.Spec = ingress.Spec
	found.ObjectMeta.Annotations = ingress.ObjectMeta.Annotations
	logger.Info("Updating Ingress", "Ingress.Namespace", found.Namespace, "Ingress.Name", found.Name)
	if err := r.Update(ctx, found); err != nil {
		logger.Error(err, "Failed to update Ingress", "Ingress.Namespace", found.Namespace, "Ingress.Name", found.Name)
		return err
	}

	return nil
}

// Helper functions

func (r *MCPProxyReconciler) labelsForMCPProxy(mcpProxy *crd.MCPProxy) map[string]string {
	return map[string]string{
		"app":                          "matey-proxy",
		"app.kubernetes.io/name":       "matey",
		"app.kubernetes.io/instance":   mcpProxy.Name,
		"app.kubernetes.io/component":  "proxy",
		"app.kubernetes.io/created-by": "mcp-controller",
	}
}

func (r *MCPProxyReconciler) getServiceAccount(mcpProxy *crd.MCPProxy) string {
	if mcpProxy.Spec.ServiceAccount != "" {
		return mcpProxy.Spec.ServiceAccount
	}
	return "matey-controller"
}

func (r *MCPProxyReconciler) getPort(mcpProxy *crd.MCPProxy) int32 {
	if mcpProxy.Spec.Port != 0 {
		return mcpProxy.Spec.Port
	}
	return 8080
}

func (r *MCPProxyReconciler) getServiceType(mcpProxy *crd.MCPProxy) corev1.ServiceType {
	if mcpProxy.Spec.ServiceType != "" {
		return corev1.ServiceType(mcpProxy.Spec.ServiceType)
	}
	return corev1.ServiceTypeClusterIP
}

func (r *MCPProxyReconciler) getNodePort(mcpProxy *crd.MCPProxy) int32 {
	if mcpProxy.Spec.ServiceType == "NodePort" {
		port := r.getPort(mcpProxy)
		// NodePort range is 30000-32767
		// Map common proxy ports to valid NodePorts
		switch port {
		case 8080:
			return 30080
		case 9876:
			return 30086
		case 3000:
			return 30000
		case 8000:
			return 30800
		default:
			// For other ports, map to NodePort range
			if port >= 30000 && port <= 32767 {
				return port
			}
			// Hash the port to get a consistent NodePort in range
			return 30000 + (port % 2767)
		}
	}
	return 0
}

func (r *MCPProxyReconciler) getProxyArgs(mcpProxy *crd.MCPProxy) []string {
	args := []string{
		"--port", fmt.Sprintf("%d", r.getPort(mcpProxy)),
		"--namespace", mcpProxy.Namespace,
	}
	
	if mcpProxy.Spec.Auth != nil && mcpProxy.Spec.Auth.APIKey != "" {
		args = append(args, "--api-key", mcpProxy.Spec.Auth.APIKey)
	}
	
	return args
}

func (r *MCPProxyReconciler) getProxyArgsWithConfig(mcpProxy *crd.MCPProxy) []string {
	args := []string{
		"--port", fmt.Sprintf("%d", r.getPort(mcpProxy)),
		"--namespace", mcpProxy.Namespace,
		"--file", "/etc/matey/config.yaml", // Mount point for ConfigMap
	}
	
	if mcpProxy.Spec.Auth != nil && mcpProxy.Spec.Auth.APIKey != "" {
		args = append(args, "--api-key", mcpProxy.Spec.Auth.APIKey)
	}
	
	return args
}

func (r *MCPProxyReconciler) getEnvVars(mcpProxy *crd.MCPProxy) []corev1.EnvVar {
	envVars := []corev1.EnvVar{}
	
	if mcpProxy.Spec.Auth != nil && mcpProxy.Spec.Auth.APIKey != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "MCP_API_KEY",
			Value: mcpProxy.Spec.Auth.APIKey,
		})
	}
	
	return envVars
}

func (r *MCPProxyReconciler) getMemoryRequest(mcpProxy *crd.MCPProxy) resource.Quantity {
	if mcpProxy.Spec.Resources.Requests != nil {
		if memStr, exists := mcpProxy.Spec.Resources.Requests["memory"]; exists {
			if quantity, err := resource.ParseQuantity(memStr); err == nil {
				return quantity
			}
		}
	}
	return resource.MustParse("128Mi")
}

func (r *MCPProxyReconciler) getCPURequest(mcpProxy *crd.MCPProxy) resource.Quantity {
	if mcpProxy.Spec.Resources.Requests != nil {
		if cpuStr, exists := mcpProxy.Spec.Resources.Requests["cpu"]; exists {
			if quantity, err := resource.ParseQuantity(cpuStr); err == nil {
				return quantity
			}
		}
	}
	return resource.MustParse("100m")
}

func (r *MCPProxyReconciler) getMemoryLimit(mcpProxy *crd.MCPProxy) resource.Quantity {
	if mcpProxy.Spec.Resources.Limits != nil {
		if memStr, exists := mcpProxy.Spec.Resources.Limits["memory"]; exists {
			if quantity, err := resource.ParseQuantity(memStr); err == nil {
				return quantity
			}
		}
	}
	return resource.MustParse("512Mi")
}

func (r *MCPProxyReconciler) getCPULimit(mcpProxy *crd.MCPProxy) resource.Quantity {
	if mcpProxy.Spec.Resources.Limits != nil {
		if cpuStr, exists := mcpProxy.Spec.Resources.Limits["cpu"]; exists {
			if quantity, err := resource.ParseQuantity(cpuStr); err == nil {
				return quantity
			}
		}
	}
	return resource.MustParse("500m")
}

func (r *MCPProxyReconciler) deploymentEqual(desired, actual *appsv1.Deployment) bool {
	// Simple comparison - in production you'd want more sophisticated comparison
	return desired.Spec.Replicas != nil && actual.Spec.Replicas != nil && 
		   *desired.Spec.Replicas == *actual.Spec.Replicas
}

// reconcileConfigMap creates/updates ConfigMap for dynamic configuration
func (r *MCPProxyReconciler) reconcileConfigMap(ctx context.Context, mcpProxy *crd.MCPProxy) error {
	logger := log.FromContext(ctx)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpProxy.Name + "-config",
			Namespace: mcpProxy.Namespace,
			Labels:    r.labelsForMCPProxy(mcpProxy),
		},
		Data: r.buildProxyConfig(mcpProxy),
	}

	// Set MCPProxy instance as the owner and controller
	if err := controllerutil.SetControllerReference(mcpProxy, configMap, r.Scheme); err != nil {
		return err
	}

	// Check if this ConfigMap already exists
	found := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: configMap.Name, Namespace: configMap.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Creating a new ConfigMap", "ConfigMap.Namespace", configMap.Namespace, "ConfigMap.Name", configMap.Name)
		if err := r.Create(ctx, configMap); err != nil {
			logger.Error(err, "Failed to create new ConfigMap", "ConfigMap.Namespace", configMap.Namespace, "ConfigMap.Name", configMap.Name)
			return err
		}
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to get ConfigMap")
		return err
	}

	// Update if configuration has changed
	if !r.configMapEqual(configMap, found) {
		found.Data = configMap.Data
		logger.Info("Updating ConfigMap", "ConfigMap.Namespace", found.Namespace, "ConfigMap.Name", found.Name)
		if err := r.Update(ctx, found); err != nil {
			logger.Error(err, "Failed to update ConfigMap", "ConfigMap.Namespace", found.Namespace, "ConfigMap.Name", found.Name)
			return err
		}
	}

	return nil
}

// reconcilePodDisruptionBudget creates/updates PodDisruptionBudget for high availability
func (r *MCPProxyReconciler) reconcilePodDisruptionBudget(ctx context.Context, mcpProxy *crd.MCPProxy) error {
	logger := log.FromContext(ctx)

	// Only create PDB if we have multiple replicas
	if mcpProxy.Spec.Replicas == nil || *mcpProxy.Spec.Replicas <= 1 {
		return nil
	}

	minAvailable := intstr.FromInt(1)
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpProxy.Name + "-pdb",
			Namespace: mcpProxy.Namespace,
			Labels:    r.labelsForMCPProxy(mcpProxy),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: r.labelsForMCPProxy(mcpProxy),
			},
		},
	}

	// Set MCPProxy instance as the owner and controller
	if err := controllerutil.SetControllerReference(mcpProxy, pdb, r.Scheme); err != nil {
		return err
	}

	// Check if this PDB already exists
	found := &policyv1.PodDisruptionBudget{}
	err := r.Get(ctx, client.ObjectKey{Name: pdb.Name, Namespace: pdb.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Creating a new PodDisruptionBudget", "PDB.Namespace", pdb.Namespace, "PDB.Name", pdb.Name)
		if err := r.Create(ctx, pdb); err != nil {
			logger.Error(err, "Failed to create new PodDisruptionBudget", "PDB.Namespace", pdb.Namespace, "PDB.Name", pdb.Name)
			return err
		}
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to get PodDisruptionBudget")
		return err
	}

	// Update if needed
	found.Spec = pdb.Spec
	logger.Info("Updating PodDisruptionBudget", "PDB.Namespace", found.Namespace, "PDB.Name", found.Name)
	if err := r.Update(ctx, found); err != nil {
		logger.Error(err, "Failed to update PodDisruptionBudget", "PDB.Namespace", found.Namespace, "PDB.Name", found.Name)
		return err
	}

	return nil
}

// reconcileServiceDiscovery integrates with enhanced service discovery
func (r *MCPProxyReconciler) reconcileServiceDiscovery(ctx context.Context, mcpProxy *crd.MCPProxy) error {
	logger := log.FromContext(ctx)

	// Initialize service discovery if not already done
	if r.ServiceDiscovery == nil {
		sd, err := discovery.NewK8sServiceDiscovery(mcpProxy.Namespace, r.Logger)
		if err != nil {
			logger.Error(err, "Failed to initialize service discovery")
			return err
		}
		r.ServiceDiscovery = sd

		// Initialize connection manager
		r.ConnectionManager = discovery.NewDynamicConnectionManager(r.ServiceDiscovery, r.Logger)
	}

	// Discover MCP services and update status
	services, err := r.ServiceDiscovery.DiscoverMCPServices(ctx)
	if err != nil {
		logger.Error(err, "Failed to discover MCP services")
		return err
	}

	// Update proxy status with discovered servers
	discoveredServers := make([]crd.DiscoveredServer, 0, len(services))
	for _, svc := range services {
		// Filter based on server selector if configured
		if r.shouldIncludeService(mcpProxy, svc) {
			discoveredServers = append(discoveredServers, crd.DiscoveredServer{
				Name:           svc.Name,
				Namespace:      svc.Namespace,
				Endpoint:       svc.Endpoint,
				Protocol:       svc.Protocol,
				Capabilities:   svc.Capabilities,
				Status:         svc.HealthStatus,
				LastDiscovered: metav1.NewTime(svc.LastDiscovered),
				HealthStatus:   svc.HealthStatus,
			})
		}
	}

	mcpProxy.Status.DiscoveredServers = discoveredServers
	logger.Info("Service discovery complete", "services", len(discoveredServers))

	return nil
}

// reconcileCrossControllerIntegration coordinates with other MCP controllers
func (r *MCPProxyReconciler) reconcileCrossControllerIntegration(ctx context.Context, mcpProxy *crd.MCPProxy) error {
	logger := log.FromContext(ctx)

	// Discover MCPServers in the cluster
	mcpServers := &crd.MCPServerList{}
	if err := r.List(ctx, mcpServers, client.InNamespace(mcpProxy.Namespace)); err != nil {
		logger.Error(err, "Failed to list MCPServers")
		return err
	}

	// Discover MCPMemory services
	mcpMemories := &crd.MCPMemoryList{}
	if err := r.List(ctx, mcpMemories, client.InNamespace(mcpProxy.Namespace)); err != nil {
		logger.Error(err, "Failed to list MCPMemory services")
		return err
	}

	// Discover MCPToolboxes
	mcpToolboxes := &crd.MCPToolboxList{}
	if err := r.List(ctx, mcpToolboxes, client.InNamespace(mcpProxy.Namespace)); err != nil {
		logger.Error(err, "Failed to list MCPToolboxes")
		return err
	}

	// Update proxy endpoints based on discovered resources
	endpoints := r.buildProxyEndpoints(mcpServers, mcpMemories, mcpToolboxes)
	mcpProxy.Status.Endpoints = endpoints

	logger.Info("Cross-controller integration complete", 
		"mcpServers", len(mcpServers.Items),
		"mcpMemories", len(mcpMemories.Items), 
		"mcpToolboxes", len(mcpToolboxes.Items))

	return nil
}

// Helper methods for new functionality

func (r *MCPProxyReconciler) buildProxyConfig(mcpProxy *crd.MCPProxy) map[string]string {
	config := map[string]string{
		"port":      fmt.Sprintf("%d", r.getPort(mcpProxy)),
		"namespace": mcpProxy.Namespace,
	}

	if mcpProxy.Spec.Auth != nil && mcpProxy.Spec.Auth.APIKey != "" {
		config["api-key"] = mcpProxy.Spec.Auth.APIKey
	}

	// Add reliability configuration
	config["max-retries"] = fmt.Sprintf("%d", r.MaxRetries)
	config["retry-delay"] = r.RetryDelay.String()
	config["health-interval"] = r.HealthInterval.String()
	config["circuit-breaker"] = fmt.Sprintf("%t", r.CircuitBreaker)

	return config
}

func (r *MCPProxyReconciler) configMapEqual(desired, actual *corev1.ConfigMap) bool {
	if len(desired.Data) != len(actual.Data) {
		return false
	}
	for key, value := range desired.Data {
		if actual.Data[key] != value {
			return false
		}
	}
	return true
}

func (r *MCPProxyReconciler) shouldIncludeService(mcpProxy *crd.MCPProxy, svc discovery.ServiceEndpoint) bool {
	// If no selector specified, include all services
	if mcpProxy.Spec.ServerSelector == nil {
		return true
	}

	selector := mcpProxy.Spec.ServerSelector

	// Check exclude list first
	for _, excluded := range selector.ExcludeServers {
		if svc.Name == excluded {
			return false
		}
	}

	// Check include list if specified
	if len(selector.IncludeServers) > 0 {
		for _, included := range selector.IncludeServers {
			if svc.Name == included {
				return true
			}
		}
		return false // Not in include list
	}

	// TODO: Implement label selector matching
	return true
}

func (r *MCPProxyReconciler) buildProxyEndpoints(mcpServers *crd.MCPServerList, mcpMemories *crd.MCPMemoryList, mcpToolboxes *crd.MCPToolboxList) []crd.ProxyEndpoint {
	var endpoints []crd.ProxyEndpoint

	// Add MCPServer endpoints
	for _, server := range mcpServers.Items {
		endpoint := crd.ProxyEndpoint{
			Name:     server.Name,
			Protocol: server.Spec.Protocol,
		}
		if server.Spec.HttpPort > 0 {
			endpoint.Port = server.Spec.HttpPort
			endpoint.URL = fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", server.Name, server.Namespace, server.Spec.HttpPort)
		}
		endpoints = append(endpoints, endpoint)
	}

	// Add MCPMemory endpoints
	for _, memory := range mcpMemories.Items {
		endpoint := crd.ProxyEndpoint{
			Name:     memory.Name + "-memory",
			Protocol: "http",
		}
		if memory.Spec.Port > 0 {
			endpoint.Port = memory.Spec.Port
			endpoint.URL = fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", memory.Name, memory.Namespace, memory.Spec.Port)
		}
		endpoints = append(endpoints, endpoint)
	}

	// Add MCPToolbox endpoints
	for _, toolbox := range mcpToolboxes.Items {
		endpoint := crd.ProxyEndpoint{
			Name:     toolbox.Name + "-toolbox",
			Protocol: "http",
			URL:      fmt.Sprintf("http://%s.%s.svc.cluster.local", toolbox.Name, toolbox.Namespace),
		}
		endpoints = append(endpoints, endpoint)
	}

	return endpoints
}

// mapToMCPProxyRequests maps related resource changes to MCPProxy reconcile requests
func (r *MCPProxyReconciler) mapToMCPProxyRequests(ctx context.Context, obj client.Object) []reconcile.Request {
	// Find all MCPProxy instances in the same namespace that might be affected
	proxyList := &crd.MCPProxyList{}
	if err := r.List(ctx, proxyList, client.InNamespace(obj.GetNamespace())); err != nil {
		if r.Logger != nil {
			r.Logger.Error("Failed to list MCPProxy resources for cross-controller integration: %v", err)
		}
		return nil
	}
	
	// Create reconcile requests for all MCPProxy instances in the namespace
	requests := make([]reconcile.Request, 0, len(proxyList.Items))
	for _, proxy := range proxyList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: proxy.Namespace,
				Name:      proxy.Name,
			},
		})
	}
	
	if r.Logger != nil && len(requests) > 0 {
		r.Logger.Debug("Cross-controller trigger: enqueuing %d MCPProxy reconcile requests", len(requests))
	}
	
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create a mapper function that can access the reconciler instance
	mapperFunc := func(ctx context.Context, obj client.Object) []reconcile.Request {
		return r.mapToMCPProxyRequests(ctx, obj)
	}
	
	return ctrl.NewControllerManagedBy(mgr).
		For(&crd.MCPProxy{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Watches(&crd.MCPServer{}, handler.EnqueueRequestsFromMapFunc(mapperFunc)).
		Watches(&crd.MCPMemory{}, handler.EnqueueRequestsFromMapFunc(mapperFunc)).
		Watches(&crd.MCPToolbox{}, handler.EnqueueRequestsFromMapFunc(mapperFunc)).
		Complete(r)
}