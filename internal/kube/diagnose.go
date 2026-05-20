package kube

import (
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
)

// IsCertVerifyError reports whether err is, or wraps, a TLS certificate
// verification failure — the classic symptom of a kubeconfig whose CA bundle
// no longer matches the cluster (the most common cause: a k3s install was
// torn down and reinstalled, regenerating its CA, while ~/.kube/config still
// holds the old one).
//
// We try the typed `crypto/x509` errors first and fall back to substring
// matching on the message, because the error often arrives wrapped inside a
// `*net/url.Error` or controller-runtime's own error chain where the typed
// match no longer works.
func IsCertVerifyError(err error) bool {
	if err == nil {
		return false
	}

	var ua x509.UnknownAuthorityError
	if errors.As(err, &ua) {
		return true
	}
	var hn x509.HostnameError
	if errors.As(err, &hn) {
		return true
	}
	var ci x509.CertificateInvalidError
	if errors.As(err, &ci) {
		return true
	}

	msg := err.Error()

	return strings.Contains(msg, "certificate signed by unknown authority") ||
		strings.Contains(msg, "x509: certificate") ||
		strings.Contains(msg, "tls: failed to verify certificate")
}

// RemediationHint returns a context-aware suggestion for resolving a TLS
// cert-verify failure. If a fresh k3s kubeconfig is on disk at the
// canonical path and the caller isn't already using it, we name the file —
// because in practice that's the fix 9 times out of 10.
func RemediationHint() string {
	const k3sKubeconfig = "/etc/rancher/k3s/k3s.yaml"

	if info, err := os.Stat(k3sKubeconfig); err == nil && !info.IsDir() {
		// Only suggest it if it's not already what the caller is using.
		if os.Getenv("KUBECONFIG") != k3sKubeconfig {
			return fmt.Sprintf(`Hint: a fresh k3s kubeconfig is at %s but your shell points elsewhere
(KUBECONFIG=%q, ~/.kube/config likely has the old CA from a previous k3s install).
Fix with either:
  export KUBECONFIG=%s
  # or, to make 'kubectl' and 'matey' both pick it up by default:
  mkdir -p ~/.kube && sudo cp %s ~/.kube/config && sudo chown $(id -u):$(id -g) ~/.kube/config`,
				k3sKubeconfig, os.Getenv("KUBECONFIG"), k3sKubeconfig, k3sKubeconfig)
		}
	}

	return `Hint: your kubeconfig's CA bundle does not match the cluster's certificate.
Check the active context with 'kubectl config view --minify' and replace the
kubeconfig with a current one from the cluster (e.g. 'gcloud container clusters
get-credentials ...', 'aws eks update-kubeconfig ...', or copying the cluster's
admin kubeconfig).`
}

// WrapAPIError returns err unchanged when it isn't a cert-verify failure;
// otherwise it returns a wrapped error whose message names the root cause
// and includes the remediation hint, so the operator sees ONE actionable
// message instead of N repeated TLS warnings.
func WrapAPIError(err error) error {
	if !IsCertVerifyError(err) {
		return err
	}

	return fmt.Errorf("Kubernetes API request failed TLS verification: %w\n\n%s", err, RemediationHint())
}
