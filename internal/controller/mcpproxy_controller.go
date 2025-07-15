// internal/controller/mcpproxy_controller.go
package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/phildougherty/m8e/internal/crd"
)

// MCPProxyReconciler reconciles a MCPProxy object
type MCPProxyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=mcp.phildougherty.com,resources=mcpproxies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mcp.phildougherty.com,resources=mcpproxies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mcp.phildougherty.com,resources=mcpproxies/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

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

	// Update status to show we're processing
	mcpProxy.Status.Phase = crd.MCPProxyPhaseCreating
	if err := r.Status().Update(ctx, &mcpProxy); err != nil {
		logger.Error(err, "Failed to update MCPProxy status")
		return ctrl.Result{}, err
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

	// Update status to running
	mcpProxy.Status.Phase = crd.MCPProxyPhaseRunning
	mcpProxy.Status.ObservedGeneration = mcpProxy.Generation
	if err := r.Status().Update(ctx, &mcpProxy); err != nil {
		logger.Error(err, "Failed to update MCPProxy status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled MCPProxy")
	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

func (r *MCPProxyReconciler) reconcileDeployment(ctx context.Context, mcpProxy *crd.MCPProxy) error {
	logger := log.FromContext(ctx)

	// Define the desired Deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpProxy.Name + "-proxy",
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
							Args:            r.getProxyArgs(mcpProxy),
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: r.getPort(mcpProxy),
									Name:          "http",
								},
							},
							Env: r.getEnvVars(mcpProxy),
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
		found.Spec = deployment.Spec
		found.ObjectMeta.Labels = deployment.ObjectMeta.Labels
		logger.Info("Updating Deployment", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)
		if err := r.Update(ctx, found); err != nil {
			logger.Error(err, "Failed to update Deployment", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)
			return err
		}
	}

	return nil
}

func (r *MCPProxyReconciler) reconcileService(ctx context.Context, mcpProxy *crd.MCPProxy) error {
	logger := log.FromContext(ctx)

	// Define the desired Service
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpProxy.Name + "-proxy",
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
			Name:        mcpProxy.Name + "-proxy",
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
											Name: mcpProxy.Name + "-proxy",
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

// SetupWithManager sets up the controller with the Manager.
func (r *MCPProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crd.MCPProxy{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}