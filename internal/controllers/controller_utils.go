package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

// ReconcileConfigMap is a shared function to create or update ConfigMaps.
// It is kept as a dedicated helper (rather than going through reconcileResource)
// because its update trigger is a single-key comparison driven by DataKey,
// which several controllers rely on.
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
