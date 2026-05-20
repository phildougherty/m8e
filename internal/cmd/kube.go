// internal/cmd/kube.go
package cmd

import (
	"errors"
	"fmt"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/kube"
)

// newKubeConfig resolves a *rest.Config the same way kubectl does (in-cluster
// first, then $KUBECONFIG, then ~/.kube/config) but turns the notoriously
// opaque client-go failure modes into messages that tell an operator exactly
// what to fix. The precedence logic itself lives in internal/kube so every
// m8e site resolves credentials identically.
func newKubeConfig() (*rest.Config, error) {
	// Pre-check the kubeconfig file so a missing file yields an actionable
	// message instead of client-go's opaque deferred-loading error. This only
	// runs when not in-cluster; kube.LoadConfig still owns the real loading.
	if _, inClusterErr := rest.InClusterConfig(); inClusterErr != nil {
		kubeconfig := clientcmd.RecommendedHomeFile
		if env := os.Getenv("KUBECONFIG"); env != "" {
			kubeconfig = env
		}

		if _, statErr := os.Stat(kubeconfig); errors.Is(statErr, os.ErrNotExist) {
			return nil, fmt.Errorf(
				"no Kubernetes configuration found: not running in-cluster and kubeconfig %q does not exist; "+
					"set KUBECONFIG or run `kubectl config view` to confirm your cluster access",
				kubeconfig,
			)
		}

		cfg, err := kube.LoadConfig()
		if err != nil {
			return nil, fmt.Errorf(
				"failed to load kubeconfig %q: %w; "+
					"the file may be malformed or reference a missing context",
				kubeconfig, err,
			)
		}

		return cfg, nil
	}

	return kube.LoadConfig()
}

// newKubeClient builds a controller-runtime client with the core, apps and
// matey CRD schemes registered. Errors name the failed stage so an operator
// is not left guessing whether it was auth, networking, or scheme setup.
func newKubeClient() (client.Client, error) {
	cfg, err := newKubeConfig()
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to register core/v1 types in client scheme: %w", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to register apps/v1 types in client scheme: %w", err)
	}
	if err := crd.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to register matey CRD types in client scheme: %w", err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf(
			"failed to connect to Kubernetes API server at %s: %w; "+
				"check that the cluster is reachable and your credentials are valid",
			cfg.Host, err,
		)
	}

	return c, nil
}
