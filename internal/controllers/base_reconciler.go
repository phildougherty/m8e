// internal/controllers/base_reconciler.go
package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// reconcileResourceOptions configures how a desired child object is reconciled
// against its live counterpart. Each controller previously hand-rolled this
// create-or-update dance for every Deployment/Service/ConfigMap/Secret/PVC it
// owned; the drift between those copies (some updated specs, some didn't, some
// retried on conflict) lived here. Callers now express only the parts that
// genuinely differ per resource.
type reconcileResourceOptions[T client.Object] struct {
	// Owner is the CRD instance that should own the child object.
	Owner client.Object
	// Scheme is used to set the controller reference.
	Scheme *runtime.Scheme
	// Desired is the fully-built desired state of the child object.
	Desired T
	// Empty returns a fresh zero-valued object of type T to receive the Get
	// result. T cannot be instantiated generically, so the caller supplies it.
	Empty func() T
	// NeedsUpdate reports whether the live object (first arg) differs from the
	// desired object (second arg) in a way that requires an Update. If nil, the
	// resource is created if missing but never updated, matching the previous
	// behavior of controllers that treated a resource as create-only.
	NeedsUpdate func(found, desired T) bool
	// Mutate copies the desired fields onto the live object before Update. It is
	// only called when NeedsUpdate returns true and must be non-nil whenever
	// NeedsUpdate is non-nil.
	Mutate func(found, desired T)
}

// reconcileResource is the shared create-or-update primitive for owned child
// objects. It sets the controller reference, creates the object if absent, and
// otherwise updates it when NeedsUpdate says so. This replaces ~17 near-identical
// hand-written blocks across the five controllers.
func reconcileResource[T client.Object](ctx context.Context, c client.Client, opts reconcileResourceOptions[T]) error {
	if err := controllerutil.SetControllerReference(opts.Owner, opts.Desired, opts.Scheme); err != nil {
		return err
	}

	found := opts.Empty()
	key := client.ObjectKeyFromObject(opts.Desired)
	err := c.Get(ctx, key, found)
	if err != nil {
		if errors.IsNotFound(err) {
			return c.Create(ctx, opts.Desired)
		}

		return err
	}

	if opts.NeedsUpdate == nil || !opts.NeedsUpdate(found, opts.Desired) {
		return nil
	}
	opts.Mutate(found, opts.Desired)

	return c.Update(ctx, found)
}

// alwaysUpdate is a NeedsUpdate that unconditionally updates. It preserves the
// prior behavior of controllers that always wrote the spec back on every
// reconcile (MCPMemory and MCPTaskScheduler deployments).
func alwaysUpdate[T client.Object](_, _ T) bool { return true }

// fetchInstance fetches a CRD instance for a reconcile request and handles the
// universal "not found means deleted, stop reconciling" case. The bool return
// is false when the caller should return early with an empty result.
//
// Every controller opened its Reconcile with this exact block (only the
// resource name in the log line differed); it is centralized here.
func fetchInstance(ctx context.Context, c client.Client, req ctrl.Request, obj client.Object, resourceName string) (bool, error) {
	logger := log.FromContext(ctx)

	err := c.Get(ctx, req.NamespacedName, obj)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info(resourceName + " resource not found. Ignoring since object must be deleted")

			return false, nil
		}
		logger.Error(err, "Failed to get "+resourceName)

		return false, err
	}

	return true, nil
}
