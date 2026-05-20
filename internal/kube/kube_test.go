package kube

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigHonorsKUBECONFIG asserts that LoadConfig reads the file pointed
// at by $KUBECONFIG rather than the hardcoded ~/.kube/config — the bug this
// package fixes.
func TestLoadConfigHonorsKUBECONFIG(t *testing.T) {
	const wantServer = "https://kubeconfig-env-test.example:6443"

	kubeconfig := `apiVersion: v1
kind: Config
clusters:
- name: test-cluster
  cluster:
    server: ` + wantServer + `
    insecure-skip-tls-verify: true
contexts:
- name: test-context
  context:
    cluster: test-cluster
    user: test-user
current-context: test-context
users:
- name: test-user
  user:
    token: test-token
`

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(kubeconfig), 0600); err != nil {
		t.Fatalf("writing temp kubeconfig: %v", err)
	}

	t.Setenv("KUBECONFIG", path)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.Host != wantServer {
		t.Errorf("LoadConfig() Host = %q, want %q (did it ignore $KUBECONFIG?)", cfg.Host, wantServer)
	}
	if cfg.BearerToken != "test-token" {
		t.Errorf("LoadConfig() BearerToken = %q, want %q", cfg.BearerToken, "test-token")
	}
}
