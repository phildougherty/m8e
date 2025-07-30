// internal/cmd/install.go
package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
)

func NewInstallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install Matey CRDs and required Kubernetes resources",
		Long: `Install Matey Custom Resource Definitions (CRDs) and required Kubernetes resources.

This command must be run before using 'matey up' for the first time to install
the necessary CRDs (MCPServer, MCPMemory, MCPTaskScheduler, MCPProxy, MCPPostgres, Workflow) and RBAC resources into the cluster.

Examples:
  matey install                    # Install all CRDs and resources
  matey install --dry-run          # Show what would be installed`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			namespace, _ := cmd.Flags().GetString("namespace")

			return installCRDs(dryRun, namespace)
		},
	}

	cmd.Flags().Bool("dry-run", false, "Print the resources that would be installed without actually installing them")
	cmd.Flags().String("namespace", "matey", "Namespace to install resources into")

	return cmd
}

func installCRDs(dryRun bool, namespace string) error {
	if namespace == "" {
		namespace = "matey" // Default to matey namespace
	}
	ctx := context.Background()

	if dryRun {
		fmt.Println("Dry run mode - showing what would be installed:")
		fmt.Println("✓ MCPServer CRD (mcp.matey.ai/v1)")
		fmt.Println("✓ MCPMemory CRD (mcp.matey.ai/v1)")
		fmt.Println("✓ MCPTaskScheduler CRD (mcp.matey.ai/v1)")
		fmt.Println("✓ MCPProxy CRD (mcp.matey.ai/v1)")
		fmt.Println("✓ MCPPostgres CRD (mcp.matey.ai/v1)")
		fmt.Printf("✓ ServiceAccount: matey-controller (namespace: %s)\n", namespace)
		fmt.Println("✓ ClusterRole: matey-controller")
		fmt.Printf("✓ ClusterRoleBinding: matey-controller (namespace: %s)\n", namespace)
		fmt.Printf("✓ ServiceAccount: matey-mcp-server (namespace: %s)\n", namespace)
		fmt.Println("✓ ClusterRole: matey-mcp-server")
		fmt.Printf("✓ ClusterRoleBinding: matey-mcp-server (namespace: %s)\n", namespace)
		fmt.Printf("✓ ServiceAccount: task-scheduler (namespace: %s)\n", namespace)
		fmt.Println("✓ ClusterRole: matey-task-scheduler")
		fmt.Printf("✓ ClusterRoleBinding: matey-task-scheduler (namespace: %s)\n", namespace)
		fmt.Printf("✓ Matey MCP Server deployment (namespace: %s)\n", namespace)
		return nil
	}

	fmt.Println("Installing Matey CRDs...")

	// Create CRD client
	k8sClient, err := createK8sClientWithCRDs()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Install CRDs from YAML files
	err = installCRDsFromYAML(ctx, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to install CRDs: %w", err)
	}

	// Create namespace first
	err = createNamespace(ctx, k8sClient, namespace)
	if err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}
	fmt.Printf("✓ Namespace %s created\n", namespace)

	// Install RBAC resources
	err = installServiceAccount(ctx, k8sClient, namespace)
	if err != nil {
		return fmt.Errorf("failed to install ServiceAccount: %w", err)
	}
	fmt.Printf("✓ ServiceAccount installed in namespace %s\n", namespace)

	err = installClusterRole(ctx, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to install ClusterRole: %w", err)
	}
	fmt.Println("✓ ClusterRole installed")

	err = installClusterRoleBinding(ctx, k8sClient, namespace)
	if err != nil {
		return fmt.Errorf("failed to install ClusterRoleBinding: %w", err)
	}
	fmt.Println("✓ ClusterRoleBinding installed")

	// Install MCP Server RBAC
	err = installMCPServerRBAC(ctx, k8sClient, namespace)
	if err != nil {
		return fmt.Errorf("failed to install MCP Server RBAC: %w", err)
	}
	fmt.Printf("✓ MCP Server RBAC installed in namespace %s\n", namespace)

	// Install Task Scheduler RBAC
	err = installTaskSchedulerRBAC(ctx, k8sClient, namespace)
	if err != nil {
		return fmt.Errorf("failed to install Task Scheduler RBAC: %w", err)
	}
	fmt.Printf("✓ Task Scheduler RBAC installed in namespace %s\n", namespace)

	// Install built-in matey-mcp-server
	err = installMateaMCPServer(ctx, k8sClient, namespace)
	if err != nil {
		return fmt.Errorf("failed to install Matey MCP Server: %w", err)
	}
	fmt.Printf("✓ Matey MCP Server installed in namespace %s\n", namespace)

	// Install shared matey-postgres resource
	err = installSharedPostgres(ctx, k8sClient, namespace)
	if err != nil {
		return fmt.Errorf("failed to install shared postgres: %w", err)
	}
	fmt.Printf("✓ Shared matey-postgres installed in namespace %s\n", namespace)

	// Create image pull secret if registry credentials are configured
	err = createImagePullSecret(ctx, k8sClient, namespace)
	if err != nil {
		return fmt.Errorf("failed to create image pull secret: %w", err)
	}

	// Create default MCPProxy resource to enable proxy functionality
	err = installDefaultMCPProxy(ctx, k8sClient, namespace)
	if err != nil {
		return fmt.Errorf("failed to install default MCPProxy: %w", err)
	}
	fmt.Printf("✓ Default MCPProxy installed in namespace %s\n", namespace)

	fmt.Println("\nMatey installation complete!")
	fmt.Println("You can now run 'matey up' to start your services.")

	return nil
}

// createK8sConfig creates a Kubernetes configuration
func createK8sConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}
	return config, nil
}

// createK8sClientWithCRDs creates a Kubernetes client with CRD scheme
func createK8sClientWithCRDs() (client.Client, error) {
	config, err := createK8sConfig()
	if err != nil {
		return nil, err
	}

	// Create the scheme with CRDs and apiextensions
	scheme := runtime.NewScheme()
	if err := crd.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add CRD scheme: %w", err)
	}
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add apiextensions scheme: %w", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add corev1 scheme: %w", err)
	}
	if err := rbacv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add rbacv1 scheme: %w", err)
	}

	// Create the client
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return k8sClient, nil
}

// installCRDsFromYAML installs CRDs from embedded YAML files
func installCRDsFromYAML(ctx context.Context, k8sClient client.Client) error {
	crdFileNames := []string{
		"mcpserver.yaml",
		"mcpmemory.yaml",
		"mcptaskscheduler.yaml",
		"mcpproxy.yaml",
		"mcppostgres.yaml",
	}

	for _, fileName := range crdFileNames {
		// Read the YAML file
		yamlData, err := os.ReadFile(filepath.Join("config/crd", fileName))
		if err != nil {
			return fmt.Errorf("failed to read CRD file %s: %w", fileName, err)
		}

		// Parse the YAML into a CRD object
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal(yamlData, crd); err != nil {
			return fmt.Errorf("failed to unmarshal CRD %s: %w", fileName, err)
		}

		// Create or update the CRD
		err = k8sClient.Create(ctx, crd)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create CRD %s: %w", crd.Name, err)
		}

		fmt.Printf("✓ %s CRD installed\n", crd.Spec.Names.Kind)
	}

	return nil
}

// createNamespace creates the namespace if it doesn't exist
func createNamespace(ctx context.Context, k8sClient client.Client, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	err := k8sClient.Create(ctx, ns)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// installServiceAccount installs the matey-controller service account
func installServiceAccount(ctx context.Context, k8sClient client.Client, namespace string) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-controller",
			Namespace: namespace,
		},
	}

	err := k8sClient.Create(ctx, sa)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// installClusterRole installs the matey-controller cluster role
func installClusterRole(ctx context.Context, k8sClient client.Client) error {
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "matey-controller",
		},
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

	err := k8sClient.Create(ctx, cr)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// installClusterRoleBinding installs the matey-controller cluster role binding
func installClusterRoleBinding(ctx context.Context, k8sClient client.Client, namespace string) error {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "matey-controller",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "matey-controller",
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "matey-controller",
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	err := k8sClient.Create(ctx, crb)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// installMCPServerRBAC installs RBAC resources for the MCP server
func installMCPServerRBAC(ctx context.Context, k8sClient client.Client, namespace string) error {
	// Create MCP Server ServiceAccount
	mcpSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-mcp-server",
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "matey",
				"app.kubernetes.io/component": "mcp-server",
			},
		},
	}
	
	err := k8sClient.Create(ctx, mcpSA)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	// Create MCP Server ClusterRole
	mcpCR := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "matey-mcp-server",
			Labels: map[string]string{
				"app.kubernetes.io/name":      "matey",
				"app.kubernetes.io/component": "mcp-server",
			},
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

	err = k8sClient.Create(ctx, mcpCR)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	// Create MCP Server ClusterRoleBinding
	mcpCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "matey-mcp-server",
			Labels: map[string]string{
				"app.kubernetes.io/name":      "matey",
				"app.kubernetes.io/component": "mcp-server",
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "matey-mcp-server",
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "matey-mcp-server",
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	err = k8sClient.Create(ctx, mcpCRB)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

// installTaskSchedulerRBAC installs RBAC resources for the Task Scheduler
func installTaskSchedulerRBAC(ctx context.Context, k8sClient client.Client, namespace string) error {
	// Create Task Scheduler ServiceAccount
	taskSchedulerSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-scheduler",
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "matey",
				"app.kubernetes.io/component": "task-scheduler",
			},
		},
	}
	
	err := k8sClient.Create(ctx, taskSchedulerSA)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	// Create Task Scheduler ClusterRole
	taskSchedulerCR := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "matey-task-scheduler",
			Labels: map[string]string{
				"app.kubernetes.io/name":      "matey",
				"app.kubernetes.io/component": "task-scheduler",
			},
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

	err = k8sClient.Create(ctx, taskSchedulerCR)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	// Create Task Scheduler ClusterRoleBinding
	taskSchedulerCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "matey-task-scheduler",
			Labels: map[string]string{
				"app.kubernetes.io/name":      "matey",
				"app.kubernetes.io/component": "task-scheduler",
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "task-scheduler",
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "matey-task-scheduler",
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	err = k8sClient.Create(ctx, taskSchedulerCRB)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

// installMateaMCPServer installs the built-in Matey MCP Server deployment
func installMateaMCPServer(ctx context.Context, k8sClient client.Client, namespace string) error {
	// Read the YAML file
	yamlData, err := os.ReadFile("k8s/matey-mcp-server-deployment.yaml")
	if err != nil {
		return fmt.Errorf("failed to read matey-mcp-server deployment file: %w", err)
	}

	// Parse and apply each resource in the YAML file
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

		// Convert to unstructured object
		unstructuredObj := &unstructured.Unstructured{Object: obj}
		
		// Override namespace for namespaced resources
		if unstructuredObj.GetKind() != "ClusterRole" && unstructuredObj.GetKind() != "ClusterRoleBinding" {
			unstructuredObj.SetNamespace(namespace)
		}
		
		// Skip creating RBAC resources as they're handled separately
		if unstructuredObj.GetKind() == "ServiceAccount" || 
		   unstructuredObj.GetKind() == "ClusterRole" || 
		   unstructuredObj.GetKind() == "ClusterRoleBinding" {
			continue
		}
		
		// Apply the resource
		err = k8sClient.Create(ctx, unstructuredObj)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create resource %s/%s: %w", 
				unstructuredObj.GetKind(), unstructuredObj.GetName(), err)
		}
	}

	return nil
}

// installSharedPostgres creates the shared matey-postgres MCPPostgres resource and required ConfigMap
func installSharedPostgres(ctx context.Context, k8sClient client.Client, namespace string) error {
	// Create the init scripts ConfigMap first
	initConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-postgres-init",
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "matey",
				"app.kubernetes.io/component": "postgres",
				"app.kubernetes.io/managed-by": "matey",
			},
		},
		Data: map[string]string{
			"01-create-memory-db.sql": `-- Create memory_graph database for memory service
CREATE DATABASE memory_graph;
GRANT ALL PRIVILEGES ON DATABASE memory_graph TO postgres;`,
		},
	}

	err := k8sClient.Create(ctx, initConfigMap)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create matey-postgres-init ConfigMap: %w", err)
	}

	// Create the MCPPostgres resource
	mateyPostgres := &crd.MCPPostgres{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-postgres",
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "matey",
				"app.kubernetes.io/component": "postgres",
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

	err = k8sClient.Create(ctx, mateyPostgres)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create matey-postgres: %w", err)
	}

	return nil
}

// createImagePullSecret creates a docker registry secret if credentials are configured
func createImagePullSecret(ctx context.Context, k8sClient client.Client, namespace string) error {
	// Try to load config to get registry credentials
	cfg, err := config.LoadConfig("matey.yaml")
	if err != nil {
		// If no config file or error loading, check environment variables
		return createImagePullSecretFromEnv(ctx, k8sClient, namespace)
	}

	// Check if registry credentials are configured
	if cfg.Registry.Username == "" || cfg.Registry.Password == "" {
		// Try environment variables as fallback
		return createImagePullSecretFromEnv(ctx, k8sClient, namespace)
	}

	// Determine registry URL
	registryURL := cfg.Registry.URL
	if registryURL == "" {
		registryURL = "ghcr.io" // Default to GitHub Container Registry
	}

	// Create the image pull secret
	secretName := "registry-secret"
	dockerConfigJSON := fmt.Sprintf(`{
		"auths": {
			"%s": {
				"username": "%s",
				"password": "%s",
				"auth": "%s"
			}
		}
	}`, registryURL, cfg.Registry.Username, cfg.Registry.Password, 
		base64.StdEncoding.EncodeToString([]byte(cfg.Registry.Username+":"+cfg.Registry.Password)))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: []byte(dockerConfigJSON),
		},
	}

	err = k8sClient.Create(ctx, secret)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create image pull secret: %w", err)
	}

	if !errors.IsAlreadyExists(err) {
		fmt.Printf("✓ Image pull secret created for registry %s\n", registryURL)
	}

	return nil
}

// createImagePullSecretFromEnv creates image pull secret from environment variables
func createImagePullSecretFromEnv(ctx context.Context, k8sClient client.Client, namespace string) error {
	// Check environment variables
	registryURL := os.Getenv("MATEY_REGISTRY_URL")
	username := os.Getenv("MATEY_REGISTRY_USERNAME") 
	password := os.Getenv("MATEY_REGISTRY_PASSWORD")
	
	// Also check for GitHub-specific env vars
	if registryURL == "" && username == "" && password == "" {
		registryURL = "ghcr.io"
		username = os.Getenv("GITHUB_USERNAME")
		if username == "" {
			username = os.Getenv("GITHUB_ACTOR")
		}
		password = os.Getenv("GITHUB_TOKEN")
	}

	if username == "" || password == "" {
		// No credentials found, skip creating secret
		fmt.Printf("⚠ No registry credentials found, skipping image pull secret creation\n")
		fmt.Printf("  Configure registry credentials in matey.yaml or set environment variables:\n")
		fmt.Printf("  - MATEY_REGISTRY_USERNAME and MATEY_REGISTRY_PASSWORD\n")
		fmt.Printf("  - or GITHUB_USERNAME/GITHUB_ACTOR and GITHUB_TOKEN for GHCR\n")
		return nil
	}

	if registryURL == "" {
		registryURL = "ghcr.io"
	}

	// Create the image pull secret
	secretName := "registry-secret"
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
			Name:      secretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: []byte(dockerConfigJSON),
		},
	}

	err := k8sClient.Create(ctx, secret)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create image pull secret: %w", err)
	}

	if !errors.IsAlreadyExists(err) {
		fmt.Printf("✓ Image pull secret created for registry %s\n", registryURL)
	}

	return nil
}

// installDefaultMCPProxy creates a default MCPProxy resource
func installDefaultMCPProxy(ctx context.Context, k8sClient client.Client, namespace string) error {
	// Create MCPProxy resource
	replicas := int32(1)
	mcpProxy := &crd.MCPProxy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-proxy",
			Namespace: namespace,
			Labels: map[string]string{
				"app":                        "matey",
				"app.kubernetes.io/name":     "matey",
				"app.kubernetes.io/instance": "matey-proxy",
				"app.kubernetes.io/component": "proxy",
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
				Enabled: false, // Disabled by default during install
			},
		},
	}

	// Create the resource
	err := k8sClient.Create(ctx, mcpProxy)
	if err != nil {
		// Check if it already exists
		if errors.IsAlreadyExists(err) {
			existing := &crd.MCPProxy{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(mcpProxy), existing); err != nil {
				return fmt.Errorf("failed to get existing MCPProxy: %w", err)
			}
			// Don't update existing proxy - leave user configuration intact
			fmt.Printf("MCPProxy already exists, skipping creation\n")
			return nil
		}
		return fmt.Errorf("failed to create MCPProxy: %w", err)
	}

	return nil
}
