package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ReconcileConfigMapParams contains parameters for ConfigMap reconciliation
type ReconcileConfigMapParams struct {
	Name      string
	Namespace string
	Labels    map[string]string
	Data      map[string]string
	Owner     client.Object
	DataKey   string // Key to check for updates (e.g., "config.yaml", "matey.yaml")
}

// ReconcileConfigMap is a shared function to create or update ConfigMaps
func ReconcileConfigMap(ctx context.Context, c client.Client, scheme *runtime.Scheme, params ReconcileConfigMapParams) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      params.Name,
			Namespace: params.Namespace,
			Labels:    params.Labels,
		},
		Data: params.Data,
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(params.Owner, configMap, scheme); err != nil {
		return err
	}

	// Create or update the ConfigMap
	found := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return c.Create(ctx, configMap)
	} else if err != nil {
		return err
	}

	// Update if needed
	if found.Data[params.DataKey] != configMap.Data[params.DataKey] {
		found.Data = configMap.Data
		return c.Update(ctx, found)
	}

	return nil
}

// ReconcileStandardPattern handles the common pattern of fetching a resource and handling not found errors
func ReconcileStandardPattern[T client.Object](ctx context.Context, c client.Client, req ctrl.Request, resourceName string) (T, ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var resource T
	
	err := c.Get(ctx, req.NamespacedName, resource)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info(fmt.Sprintf("%s resource not found. Ignoring since object must be deleted", resourceName))
			return resource, ctrl.Result{}, nil
		}
		logger.Error(err, fmt.Sprintf("Failed to get %s", resourceName))
		return resource, ctrl.Result{}, err
	}

	return resource, ctrl.Result{}, nil
}

// CreateOrUpdateResource handles the common create-or-update pattern for Kubernetes resources
func CreateOrUpdateResource[T client.Object](ctx context.Context, c client.Client, desired T, current T) error {
	namespacedName := types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}
	
	err := c.Get(ctx, namespacedName, current)
	if err != nil && errors.IsNotFound(err) {
		return c.Create(ctx, desired)
	} else if err != nil {
		return err
	}
	
	// For most resources, we'd need specific update logic here
	// This is a simplified version
	return c.Update(ctx, desired)
}

// ReconcileWithStatusPhase handles the common pattern for phase-based reconciliation
// This function encapsulates the standard pattern used in both MCPMemory and MCPTaskScheduler
func ReconcileWithStatusPhase(ctx context.Context, c client.Client, req ctrl.Request, resourceName string, 
	getResource func() client.Object, 
	getPhase func(client.Object) string, 
	setPhase func(client.Object, string), 
	pendingPhase string,
	phaseHandler func(context.Context, client.Object, string) (ctrl.Result, error)) (ctrl.Result, error) {
	
	logger := log.FromContext(ctx)
	
	// Fetch the resource instance
	resource := getResource()
	err := c.Get(ctx, req.NamespacedName, resource)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info(fmt.Sprintf("%s resource not found. Ignoring since object must be deleted", resourceName))
			return ctrl.Result{}, nil
		}
		logger.Error(err, fmt.Sprintf("Failed to get %s", resourceName))
		return ctrl.Result{}, err
	}

	// Update status to reflect current phase if not set
	currentPhase := getPhase(resource)
	if currentPhase == "" {
		setPhase(resource, pendingPhase)
		if statusClient, ok := c.(client.StatusClient); ok {
			if err := statusClient.Status().Update(ctx, resource); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to update %s status", resourceName))
				return ctrl.Result{}, err
			}
		}
		currentPhase = pendingPhase
	}

	// Delegate to phase-specific reconciliation
	return phaseHandler(ctx, resource, currentPhase)
}