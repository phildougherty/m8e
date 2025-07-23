// internal/cmd/install.go
package cmd

import (
	"context"
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

	"github.com/phildougherty/m8e/internal/crd"
)

func NewInstallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install Matey CRDs and required Kubernetes resources",
		Long: `Install Matey Custom Resource Definitions (CRDs) and required Kubernetes resources.

This command must be run before using 'matey up' for the first time to install
the necessary CRDs (MCPServer, MCPMemory, MCPTaskScheduler, MCPProxy, MCPToolbox, MCPPostgres, Workflow) and RBAC resources into the cluster.

Examples:
  matey install                    # Install all CRDs and resources
  matey install --dry-run          # Show what would be installed`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			return installCRDs(dryRun)
		},
	}

	cmd.Flags().Bool("dry-run", false, "Print the resources that would be installed without actually installing them")

	return cmd
}

func installCRDs(dryRun bool) error {
	ctx := context.Background()

	if dryRun {
		fmt.Println("Dry run mode - showing what would be installed:")
		fmt.Println("✓ MCPServer CRD (mcp.matey.ai/v1)")
		fmt.Println("✓ MCPMemory CRD (mcp.matey.ai/v1)")
		fmt.Println("✓ MCPTaskScheduler CRD (mcp.matey.ai/v1)")
		fmt.Println("✓ MCPProxy CRD (mcp.matey.ai/v1)")
		fmt.Println("✓ MCPPostgres CRD (mcp.matey.ai/v1)")
		fmt.Println("✓ ServiceAccount: matey-controller")
		fmt.Println("✓ ClusterRole: matey-controller")
		fmt.Println("✓ ClusterRoleBinding: matey-controller")
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

	// Install RBAC resources
	err = installServiceAccount(ctx, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to install ServiceAccount: %w", err)
	}
	fmt.Println("✓ ServiceAccount installed")

	err = installClusterRole(ctx, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to install ClusterRole: %w", err)
	}
	fmt.Println("✓ ClusterRole installed")

	err = installClusterRoleBinding(ctx, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to install ClusterRoleBinding: %w", err)
	}
	fmt.Println("✓ ClusterRoleBinding installed")

	// Install built-in matey-mcp-server
	err = installMateaMCPServer(ctx, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to install Matey MCP Server: %w", err)
	}
	fmt.Println("✓ Matey MCP Server installed")

	fmt.Println("\nMatey installation complete!")
	fmt.Println("You can now run 'matey up' or 'matey proxy' to start your services.")

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
		"mcptoolbox.yaml",
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

// installServiceAccount installs the matey-controller service account
func installServiceAccount(ctx context.Context, k8sClient client.Client) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-controller",
			Namespace: "default",
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
				Resources: []string{"pods", "services", "endpoints", "persistentvolumeclaims", "events", "configmaps", "secrets", "serviceaccounts"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"deployments", "daemonsets", "replicasets", "statefulsets"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"networking.k8s.io"},
				Resources: []string{"ingresses"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"batch"},
				Resources: []string{"jobs", "cronjobs"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"policy"},
				Resources: []string{"poddisruptionbudgets"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"mcp.matey.ai"},
				Resources: []string{"mcpservers", "mcpmemories", "mcptaskschedulers", "mcpproxies", "mcptoolboxes", "mcppostgres"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"mcp.matey.ai"},
				Resources: []string{"mcpservers/status", "mcpmemories/status", "mcptaskschedulers/status", "mcpproxies/status", "mcptoolboxes/status", "mcppostgres/status"},
				Verbs:     []string{"get", "update", "patch"},
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
func installClusterRoleBinding(ctx context.Context, k8sClient client.Client) error {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "matey-controller",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "matey-controller",
				Namespace: "default",
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

// installMateaMCPServer installs the built-in Matey MCP Server deployment
func installMateaMCPServer(ctx context.Context, k8sClient client.Client) error {
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
		
		// Apply the resource
		err = k8sClient.Create(ctx, unstructuredObj)
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create resource %s/%s: %w", 
				unstructuredObj.GetKind(), unstructuredObj.GetName(), err)
		}
	}

	return nil
}