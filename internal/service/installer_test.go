// internal/service/installer_test.go
package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/phildougherty/m8e/internal/crd"
)

func installerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("corev1 scheme: %v", err)
	}
	if err := rbacv1.AddToScheme(s); err != nil {
		t.Fatalf("rbacv1 scheme: %v", err)
	}
	if err := apiextensionsv1.AddToScheme(s); err != nil {
		t.Fatalf("apiextensions scheme: %v", err)
	}
	if err := crd.AddToScheme(s); err != nil {
		t.Fatalf("crd scheme: %v", err)
	}

	return s
}

// writeCRDFixtures writes a minimal valid CRD manifest per expected file name
// into dir so installCRDsFromYAML has something to read.
func writeCRDFixtures(t *testing.T, dir string) {
	t.Helper()
	kinds := map[string]string{
		"mcpserver.yaml":        "MCPServer",
		"mcpmemory.yaml":        "MCPMemory",
		"mcptaskscheduler.yaml": "MCPTaskScheduler",
		"mcpproxy.yaml":         "MCPProxy",
		"mcppostgres.yaml":      "MCPPostgres",
	}
	for file, kind := range kinds {
		manifest := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: ` + lower(kind) + `s.mcp.matey.ai
spec:
  group: mcp.matey.ai
  scope: Namespaced
  names:
    kind: ` + kind + `
    plural: ` + lower(kind) + `s
    singular: ` + lower(kind) + `
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`
		if err := os.WriteFile(filepath.Join(dir, file), []byte(manifest), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", file, err)
		}
	}
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}

	return string(b)
}

func TestInstaller_DryRunPlan(t *testing.T) {
	inst := NewInstaller(nil, "matey")
	plan := inst.DryRunPlan()
	if len(plan) != 15 {
		t.Fatalf("expected 15 plan items, got %d", len(plan))
	}
	// The namespace must be threaded into namespaced resource descriptions.
	found := false
	for _, line := range plan {
		if line == "ServiceAccount: matey-controller (namespace: matey)" {
			found = true
		}
	}
	if !found {
		t.Errorf("dry-run plan missing namespaced ServiceAccount line: %v", plan)
	}
}

func TestInstaller_Install_CreatesCoreResources(t *testing.T) {
	scheme := installerScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	crdDir := t.TempDir()
	writeCRDFixtures(t, crdDir)

	deployFile := filepath.Join(t.TempDir(), "deploy.yaml")
	deployManifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: matey-mcp-server
spec:
  selector:
    matchLabels:
      app: matey-mcp-server
  template:
    metadata:
      labels:
        app: matey-mcp-server
    spec:
      containers:
        - name: matey-mcp-server
          image: matey:latest
`
	if err := os.WriteFile(deployFile, []byte(deployManifest), 0o644); err != nil {
		t.Fatalf("write deploy fixture: %v", err)
	}

	inst := &Installer{
		client:         c,
		namespace:      "matey",
		crdDir:         crdDir,
		deploymentFile: deployFile,
	}

	var reported []string
	err := inst.Install(context.Background(), func(msg string) {
		reported = append(reported, msg)
	})
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	ctx := context.Background()

	// CRDs created.
	for _, name := range []string{
		"mcpservers.mcp.matey.ai", "mcpmemorys.mcp.matey.ai",
		"mcptaskschedulers.mcp.matey.ai", "mcpproxys.mcp.matey.ai",
		"mcppostgress.mcp.matey.ai",
	} {
		got := &apiextensionsv1.CustomResourceDefinition{}
		if err := c.Get(ctx, types.NamespacedName{Name: name}, got); err != nil {
			t.Errorf("expected CRD %s to exist: %v", name, err)
		}
	}

	// Namespace created.
	ns := &corev1.Namespace{}
	if err := c.Get(ctx, types.NamespacedName{Name: "matey"}, ns); err != nil {
		t.Errorf("expected namespace matey: %v", err)
	}

	// Controller RBAC.
	sa := &corev1.ServiceAccount{}
	if err := c.Get(ctx, types.NamespacedName{Name: "matey-controller", Namespace: "matey"}, sa); err != nil {
		t.Errorf("expected matey-controller ServiceAccount: %v", err)
	}
	cr := &rbacv1.ClusterRole{}
	if err := c.Get(ctx, types.NamespacedName{Name: "matey-controller"}, cr); err != nil {
		t.Errorf("expected matey-controller ClusterRole: %v", err)
	}
	crb := &rbacv1.ClusterRoleBinding{}
	if err := c.Get(ctx, types.NamespacedName{Name: "matey-controller"}, crb); err != nil {
		t.Errorf("expected matey-controller ClusterRoleBinding: %v", err)
	}
	if len(crb.Subjects) != 1 || crb.Subjects[0].Namespace != "matey" {
		t.Errorf("ClusterRoleBinding subject namespace not threaded: %+v", crb.Subjects)
	}

	// Task scheduler RBAC.
	tsSA := &corev1.ServiceAccount{}
	if err := c.Get(ctx, types.NamespacedName{Name: "task-scheduler", Namespace: "matey"}, tsSA); err != nil {
		t.Errorf("expected task-scheduler ServiceAccount: %v", err)
	}

	// Shared postgres + default proxy.
	pg := &crd.MCPPostgres{}
	if err := c.Get(ctx, types.NamespacedName{Name: "matey-postgres", Namespace: "matey"}, pg); err != nil {
		t.Errorf("expected matey-postgres MCPPostgres: %v", err)
	}
	proxy := &crd.MCPProxy{}
	if err := c.Get(ctx, types.NamespacedName{Name: "matey-proxy", Namespace: "matey"}, proxy); err != nil {
		t.Errorf("expected matey-proxy MCPProxy: %v", err)
	}

	if len(reported) == 0 {
		t.Error("expected progress to be reported")
	}
}

func TestInstaller_Install_IdempotentOnExisting(t *testing.T) {
	scheme := installerScheme(t)

	// Pre-create the namespace so the second install path hits AlreadyExists.
	existingNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "matey"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingNS).Build()

	crdDir := t.TempDir()
	writeCRDFixtures(t, crdDir)
	deployFile := filepath.Join(t.TempDir(), "deploy.yaml")
	if err := os.WriteFile(deployFile, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: noop\n"), 0o644); err != nil {
		t.Fatalf("write deploy fixture: %v", err)
	}

	inst := &Installer{client: c, namespace: "matey", crdDir: crdDir, deploymentFile: deployFile}

	if err := inst.Install(context.Background(), nil); err != nil {
		t.Fatalf("Install over existing namespace should not error: %v", err)
	}
}

func TestInstaller_Install_MissingCRDDir(t *testing.T) {
	scheme := installerScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	inst := &Installer{
		client:         c,
		namespace:      "matey",
		crdDir:         filepath.Join(t.TempDir(), "does-not-exist"),
		deploymentFile: "unused",
	}

	err := inst.Install(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error when CRD directory is missing")
	}
}

// ensure apierrors stays referenced if future edits drop its use.
var _ = apierrors.IsAlreadyExists
