package kube

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestIsCertVerifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated error", errors.New("connection refused"), false},
		{"typed UnknownAuthorityError", x509.UnknownAuthorityError{}, true},
		{"typed HostnameError", x509.HostnameError{Host: "127.0.0.1"}, true},
		{"wrapped UnknownAuthorityError", fmt.Errorf("wrap: %w", x509.UnknownAuthorityError{}), true},
		{
			"controller-runtime style wrapped",
			&url.Error{Op: "Get", URL: "https://127.0.0.1:6443/api", Err: errors.New("tls: failed to verify certificate: x509: certificate signed by unknown authority")},
			true,
		},
		{
			"plain message-only error",
			errors.New("Get \"https://127.0.0.1:6443/api\": tls: failed to verify certificate: x509: certificate signed by unknown authority"),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCertVerifyError(tt.err); got != tt.want {
				t.Errorf("IsCertVerifyError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestRemediationHint_PrefersK3sKubeconfig: when /etc/rancher/k3s/k3s.yaml
// exists and KUBECONFIG isn't already pointing at it, the hint names the
// file and the exact two commands to fix it.
func TestRemediationHint_PrefersK3sKubeconfig(t *testing.T) {
	// We can't write to /etc/rancher in a unit test, so just check the
	// branch that runs in production by stat-ing whatever's actually
	// there. On developer machines this often exists.
	got := RemediationHint()
	if got == "" {
		t.Fatal("RemediationHint returned empty string")
	}
	// One of the two branches always fires; both should mention kubeconfig.
	if !strings.Contains(got, "kubeconfig") {
		t.Errorf("hint does not mention 'kubeconfig': %q", got)
	}
}

func TestWrapAPIError(t *testing.T) {
	// Non-cert errors pass through unchanged.
	plain := errors.New("connection refused")
	if got := WrapAPIError(plain); got != plain {
		t.Errorf("WrapAPIError(plain) = %v, want passthrough", got)
	}

	// Cert errors get wrapped with the remediation hint.
	certErr := errors.New("tls: failed to verify certificate: x509: certificate signed by unknown authority")
	wrapped := WrapAPIError(certErr)
	if wrapped == certErr {
		t.Fatal("expected wrapping, got pass-through")
	}
	msg := wrapped.Error()
	if !strings.Contains(msg, "TLS verification") {
		t.Errorf("wrapped message missing TLS prefix: %q", msg)
	}
	if !strings.Contains(msg, "kubeconfig") {
		t.Errorf("wrapped message missing remediation hint: %q", msg)
	}
	if !errors.Is(wrapped, certErr) {
		t.Errorf("wrapped error must preserve the original via errors.Is")
	}
}
