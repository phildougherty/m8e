// internal/service/installer.go
package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
)

// Installer owns the orchestration of installing Matey CRDs, RBAC, the
// namespace, and the built-in resources into a cluster. It contains no
// cobra/stdout concerns: every step reports progress through the Reporter
// callback so the caller decides how to render it.
type Installer struct {
	client    client.Client
	namespace string
	// crdDir and deploymentFile are injectable so tests can point at fixtures
	// instead of the repo-relative paths the CLI uses.
	crdDir         string
	deploymentFile string
}

// Reporter receives a human-readable progress line for each completed step.
// The CLI passes a closure that writes to cmd.OutOrStdout().
type Reporter func(msg string)

// NewInstaller builds an Installer for the given namespace. An empty namespace
// is not defaulted here; callers resolve the namespace before construction.
func NewInstaller(c client.Client, namespace string) *Installer {
	return &Installer{
		client:         c,
		namespace:      namespace,
		crdDir:         "config/crd",
		deploymentFile: "k8s/matey-mcp-server-deployment.yaml",
	}
}

// crdFileNames is the ordered set of CRD manifests installed from disk.
var crdFileNames = []string{
	"mcpserver.yaml",
	"mcpmemory.yaml",
	"mcptaskscheduler.yaml",
	"mcpproxy.yaml",
	"mcppostgres.yaml",
}

// DryRunPlan returns the ordered list of resources that Install would create,
// so the caller can show a plan without touching the cluster.
func (i *Installer) DryRunPlan() []string {
	return []string{
		"MCPServer CRD (mcp.matey.ai/v1)",
		"MCPMemory CRD (mcp.matey.ai/v1)",
		"MCPTaskScheduler CRD (mcp.matey.ai/v1)",
		"MCPProxy CRD (mcp.matey.ai/v1)",
		"MCPPostgres CRD (mcp.matey.ai/v1)",
		fmt.Sprintf("ServiceAccount: matey-controller (namespace: %s)", i.namespace),
		"ClusterRole: matey-controller",
		fmt.Sprintf("ClusterRoleBinding: matey-controller (namespace: %s)", i.namespace),
		fmt.Sprintf("ServiceAccount: matey-mcp-server (namespace: %s)", i.namespace),
		"ClusterRole: matey-mcp-server",
		fmt.Sprintf("ClusterRoleBinding: matey-mcp-server (namespace: %s)", i.namespace),
		fmt.Sprintf("ServiceAccount: task-scheduler (namespace: %s)", i.namespace),
		"ClusterRole: matey-task-scheduler",
		fmt.Sprintf("ClusterRoleBinding: matey-task-scheduler (namespace: %s)", i.namespace),
		fmt.Sprintf("Matey MCP Server deployment (namespace: %s)", i.namespace),
	}
}

// Install performs the full installation, invoking report after each step.
// A nil report is tolerated.
func (i *Installer) Install(ctx context.Context, report Reporter) error {
	if report == nil {
		report = func(string) {}
	}

	if err := i.installCRDsFromYAML(ctx, report); err != nil {
		return fmt.Errorf("failed to install CRDs: %w", err)
	}

	if err := i.createNamespace(ctx); err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}
	report(fmt.Sprintf("Namespace %s created", i.namespace))

	if err := i.installControllerRBAC(ctx); err != nil {
		return fmt.Errorf("failed to install ServiceAccount: %w", err)
	}
	report(fmt.Sprintf("ServiceAccount installed in namespace %s", i.namespace))
	report("ClusterRole installed")
	report("ClusterRoleBinding installed")

	if err := i.installMCPServerRBAC(ctx); err != nil {
		return fmt.Errorf("failed to install MCP Server RBAC: %w", err)
	}
	report(fmt.Sprintf("MCP Server RBAC installed in namespace %s", i.namespace))

	if err := i.installTaskSchedulerRBAC(ctx); err != nil {
		return fmt.Errorf("failed to install Task Scheduler RBAC: %w", err)
	}
	report(fmt.Sprintf("Task Scheduler RBAC installed in namespace %s", i.namespace))

	if err := i.installMateyMCPServer(ctx); err != nil {
		return fmt.Errorf("failed to install Matey MCP Server: %w", err)
	}
	report(fmt.Sprintf("Matey MCP Server installed in namespace %s", i.namespace))

	if err := i.installSharedPostgres(ctx); err != nil {
		return fmt.Errorf("failed to install shared postgres: %w", err)
	}
	report(fmt.Sprintf("Shared matey-postgres installed in namespace %s", i.namespace))

	if err := i.createImagePullSecret(ctx, report); err != nil {
		return fmt.Errorf("failed to create image pull secret: %w", err)
	}

	if err := i.installDefaultMCPProxy(ctx, report); err != nil {
		return fmt.Errorf("failed to install default MCPProxy: %w", err)
	}
	report(fmt.Sprintf("Default MCPProxy installed in namespace %s", i.namespace))

	return nil
}

func (i *Installer) installCRDsFromYAML(ctx context.Context, report Reporter) error {
	for _, fileName := range crdFileNames {
		yamlData, err := os.ReadFile(filepath.Join(i.crdDir, fileName))
		if err != nil {
			return fmt.Errorf("failed to read CRD file %s: %w", fileName, err)
		}

		crdObj := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal(yamlData, crdObj); err != nil {
			return fmt.Errorf("failed to unmarshal CRD %s: %w", fileName, err)
		}

		err = i.client.Create(ctx, crdObj)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create CRD %s: %w", crdObj.Name, err)
		}

		report(fmt.Sprintf("%s CRD installed", crdObj.Spec.Names.Kind))
	}

	return nil
}

func (i *Installer) createNamespace(ctx context.Context) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: i.namespace},
	}

	err := i.client.Create(ctx, ns)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (i *Installer) installControllerRBAC(ctx context.Context) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-controller",
			Namespace: i.namespace,
		},
	}
	if err := i.client.Create(ctx, sa); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "matey-controller"},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "services", "endpoints", "configmaps", "secrets", "persistentvolumeclaims", "namespaces"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"deployments", "replicasets"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"batch"},
				Resources: []string{"jobs", "cronjobs"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"networking.k8s.io"},
				Resources: []string{"ingresses", "networkpolicies"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"policy"},
				Resources: []string{"poddisruptionbudgets"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"get", "list", "watch", "create"},
			},
			{
				APIGroups: []string{"metrics.k8s.io"},
				Resources: []string{"pods", "nodes"},
				Verbs:     []string{"get", "list"},
			},
			{
				APIGroups: []string{"mcp.matey.ai"},
				Resources: []string{"mcpservers", "mcpmemories", "mcptaskschedulers", "mcpproxies", "mcppostgres"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"mcp.matey.ai"},
				Resources: []string{"mcpservers/status", "mcpmemories/status", "mcptaskschedulers/status", "mcpproxies/status", "mcppostgres/status"},
				Verbs:     []string{"get", "list", "watch", "update", "patch"},
			},
		},
	}
	if err := i.client.Create(ctx, cr); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "matey-controller"},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "matey-controller",
				Namespace: i.namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "matey-controller",
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	if err := i.client.Create(ctx, crb); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (i *Installer) installMCPServerRBAC(ctx context.Context) error {
	labels := map[string]string{
		"app.kubernetes.io/name":      "matey",
		"app.kubernetes.io/component": "mcp-server",
	}

	mcpSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-mcp-server",
			Namespace: i.namespace,
			Labels:    labels,
		},
	}
	if err := i.client.Create(ctx, mcpSA); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	mcpCR := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "matey-mcp-server",
			Labels: labels,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"services", "endpoints", "pods", "configmaps", "secrets", "namespaces", "persistentvolumeclaims"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"deployments", "replicasets", "daemonsets", "statefulsets"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"mcp.matey.ai"},
				Resources: []string{"mcpservers", "mcpmemories", "mcptaskschedulers", "mcpproxies", "mcppostgres"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"mcp.matey.ai"},
				Resources: []string{"mcpservers/status", "mcpmemories/status", "mcptaskschedulers/status", "mcpproxies/status", "mcppostgres/status"},
				Verbs:     []string{"get", "update", "patch"},
			},
			{
				APIGroups: []string{"batch"},
				Resources: []string{"jobs", "cronjobs"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"networking.k8s.io"},
				Resources: []string{"ingresses"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}
	if err := i.client.Create(ctx, mcpCR); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	mcpCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "matey-mcp-server",
			Labels: labels,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "matey-mcp-server",
				Namespace: i.namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "matey-mcp-server",
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	if err := i.client.Create(ctx, mcpCRB); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (i *Installer) installTaskSchedulerRBAC(ctx context.Context) error {
	labels := map[string]string{
		"app.kubernetes.io/name":      "matey",
		"app.kubernetes.io/component": "task-scheduler",
	}

	taskSchedulerSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-scheduler",
			Namespace: i.namespace,
			Labels:    labels,
		},
	}
	if err := i.client.Create(ctx, taskSchedulerSA); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	taskSchedulerCR := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "matey-task-scheduler",
			Labels: labels,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumeclaims"},
				Verbs:     []string{"get", "list", "watch", "create", "delete"},
			},
			{
				APIGroups: []string{"batch"},
				Resources: []string{"jobs"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"mcp.matey.ai"},
				Resources: []string{"mcptaskschedulers"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"mcp.matey.ai"},
				Resources: []string{"mcptaskschedulers/status"},
				Verbs:     []string{"get", "update", "patch"},
			},
		},
	}
	if err := i.client.Create(ctx, taskSchedulerCR); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	taskSchedulerCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "matey-task-scheduler",
			Labels: labels,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "task-scheduler",
				Namespace: i.namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "matey-task-scheduler",
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	if err := i.client.Create(ctx, taskSchedulerCRB); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (i *Installer) installMateyMCPServer(ctx context.Context) error {
	yamlData, err := os.ReadFile(i.deploymentFile)
	if err != nil {
		return fmt.Errorf("failed to read matey-mcp-server deployment file: %w", err)
	}

	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(yamlData)), 4096)
	for {
		var obj map[string]interface{}
		err := decoder.Decode(&obj)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}

			return fmt.Errorf("failed to decode YAML: %w", err)
		}

		if obj == nil {
			continue
		}

		unstructuredObj := &unstructured.Unstructured{Object: obj}

		if unstructuredObj.GetKind() != "ClusterRole" && unstructuredObj.GetKind() != "ClusterRoleBinding" {
			unstructuredObj.SetNamespace(i.namespace)
		}

		// RBAC objects are installed explicitly by installMCPServerRBAC; skip
		// any duplicates embedded in the deployment manifest.
		if unstructuredObj.GetKind() == "ServiceAccount" ||
			unstructuredObj.GetKind() == "ClusterRole" ||
			unstructuredObj.GetKind() == "ClusterRoleBinding" {
			continue
		}

		err = i.client.Create(ctx, unstructuredObj)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create resource %s/%s: %w",
				unstructuredObj.GetKind(), unstructuredObj.GetName(), err)
		}
	}

	return nil
}

func (i *Installer) installSharedPostgres(ctx context.Context) error {
	initConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-postgres-init",
			Namespace: i.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "matey",
				"app.kubernetes.io/component":  "postgres",
				"app.kubernetes.io/managed-by": "matey",
			},
		},
		Data: map[string]string{
			"01-create-memory-db.sql": `-- Create memory_graph database for memory service
CREATE DATABASE memory_graph;
GRANT ALL PRIVILEGES ON DATABASE memory_graph TO postgres;`,
		},
	}
	if err := i.client.Create(ctx, initConfigMap); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create matey-postgres-init ConfigMap: %w", err)
	}

	mateyPostgres := &crd.MCPPostgres{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-postgres",
			Namespace: i.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "matey",
				"app.kubernetes.io/component":  "postgres",
				"app.kubernetes.io/managed-by": "matey",
			},
		},
		Spec: crd.MCPPostgresSpec{
			Replicas:    1,
			Port:        5432,
			Database:    "matey",
			User:        "postgres",
			Password:    "password",
			StorageSize: "10Gi",
		},
	}
	if err := i.client.Create(ctx, mateyPostgres); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create matey-postgres: %w", err)
	}

	return nil
}

// createImagePullSecret creates a docker registry secret from matey.yaml
// registry credentials, falling back to environment variables.
func (i *Installer) createImagePullSecret(ctx context.Context, report Reporter) error {
	cfg, err := config.LoadConfig("matey.yaml")
	if err != nil {
		return i.createImagePullSecretFromEnv(ctx, report)
	}

	if cfg.Registry.Username == "" || cfg.Registry.Password == "" {
		return i.createImagePullSecretFromEnv(ctx, report)
	}

	registryURL := cfg.Registry.URL
	if registryURL == "" {
		registryURL = "ghcr.io"
	}

	return i.applyImagePullSecret(ctx, report, registryURL, cfg.Registry.Username, cfg.Registry.Password)
}

func (i *Installer) createImagePullSecretFromEnv(ctx context.Context, report Reporter) error {
	registryURL := os.Getenv("MATEY_REGISTRY_URL")
	username := os.Getenv("MATEY_REGISTRY_USERNAME")
	password := os.Getenv("MATEY_REGISTRY_PASSWORD")

	if registryURL == "" && username == "" && password == "" {
		registryURL = "ghcr.io"
		username = os.Getenv("GITHUB_USERNAME")
		if username == "" {
			username = os.Getenv("GITHUB_ACTOR")
		}
		password = os.Getenv("GITHUB_TOKEN")
	}

	if username == "" || password == "" {
		report("WARNING: No registry credentials found, skipping image pull secret creation")
		report("  Configure registry credentials in matey.yaml or set environment variables:")
		report("  - MATEY_REGISTRY_USERNAME and MATEY_REGISTRY_PASSWORD")
		report("  - or GITHUB_USERNAME/GITHUB_ACTOR and GITHUB_TOKEN for GHCR")

		return nil
	}

	if registryURL == "" {
		registryURL = "ghcr.io"
	}

	return i.applyImagePullSecret(ctx, report, registryURL, username, password)
}

func (i *Installer) applyImagePullSecret(ctx context.Context, report Reporter, registryURL, username, password string) error {
	dockerConfigJSON := fmt.Sprintf(`{
		"auths": {
			"%s": {
				"username": "%s",
				"password": "%s",
				"auth": "%s"
			}
		}
	}`, registryURL, username, password,
		base64.StdEncoding.EncodeToString([]byte(username+":"+password)))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "registry-secret",
			Namespace: i.namespace,
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: []byte(dockerConfigJSON),
		},
	}

	err := i.client.Create(ctx, secret)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create image pull secret: %w", err)
	}

	if !errors.IsAlreadyExists(err) {
		report(fmt.Sprintf("Image pull secret created for registry %s", registryURL))
	}

	return nil
}

func (i *Installer) installDefaultMCPProxy(ctx context.Context, report Reporter) error {
	replicas := int32(1)
	mcpProxy := &crd.MCPProxy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-proxy",
			Namespace: i.namespace,
			Labels: map[string]string{
				"app":                          "matey",
				"app.kubernetes.io/name":       "matey",
				"app.kubernetes.io/instance":   "matey-proxy",
				"app.kubernetes.io/component":  "proxy",
				"app.kubernetes.io/managed-by": "matey",
			},
		},
		Spec: crd.MCPProxySpec{
			Port:        int32(9876),
			Replicas:    &replicas,
			ServiceType: "NodePort",
			Auth: &crd.ProxyAuthConfig{
				Enabled: false,
				APIKey:  "",
			},
			ServiceAccount: "matey-controller",
			Ingress: &crd.IngressConfig{
				Enabled: false,
			},
		},
	}

	err := i.client.Create(ctx, mcpProxy)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			existing := &crd.MCPProxy{}
			if err := i.client.Get(ctx, client.ObjectKeyFromObject(mcpProxy), existing); err != nil {
				return fmt.Errorf("failed to get existing MCPProxy: %w", err)
			}
			// Leave an operator's existing proxy configuration intact.
			report("MCPProxy already exists, skipping creation")

			return nil
		}

		return fmt.Errorf("failed to create MCPProxy: %w", err)
	}

	return nil
}
