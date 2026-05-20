// Package kube provides a single canonical loader for the Kubernetes client
// config so every m8e site resolves credentials the same way kubectl and helm
// do — instead of hardcoding ~/.kube/config and ignoring $KUBECONFIG.
package kube

import (
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// LoadConfig returns the Kubernetes client config, honouring (in order):
// an in-cluster service account, the $KUBECONFIG environment variable
// (including colon-separated lists), and finally ~/.kube/config — the same
// precedence kubectl and helm use.
func LoadConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}

	rules := clientcmd.NewDefaultClientConfigLoadingRules() // respects $KUBECONFIG
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, &clientcmd.ConfigOverrides{},
	).ClientConfig()
}
