// internal/cmd/install.go
package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
the necessary CRDs (MCPServer, MCPMemory, MCPTaskScheduler, MCPProxy) and RBAC resources into the cluster.

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
		fmt.Println("✓ ServiceAccount: matey-controller")
		fmt.Println("✓ ClusterRole: matey-controller")
		fmt.Println("✓ ClusterRoleBinding: matey-controller")
		return nil
	}

	// Note: config creation is handled by createK8sClientWithCRDs

	// Note: Using controller-runtime client instead of deprecated apiextensions client

	fmt.Println("Installing Matey CRDs...")

	// Create CRD client
	k8sClient, err := createK8sClientWithCRDs()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Install MCPServer CRD
	err = installMCPServerCRD(ctx, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to install MCPServer CRD: %w", err)
	}
	fmt.Println("✓ MCPServer CRD installed")

	// Install MCPMemory CRD
	err = installMCPMemoryCRD(ctx, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to install MCPMemory CRD: %w", err)
	}
	fmt.Println("✓ MCPMemory CRD installed")

	// Install MCPTaskScheduler CRD
	err = installMCPTaskSchedulerCRD(ctx, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to install MCPTaskScheduler CRD: %w", err)
	}
	fmt.Println("✓ MCPTaskScheduler CRD installed")

	// Install MCPProxy CRD
	err = installMCPProxyCRD(ctx, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to install MCPProxy CRD: %w", err)
	}
	fmt.Println("✓ MCPProxy CRD installed")

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

	fmt.Println("\n🎉 Matey installation complete!")
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

// installMCPServerCRD installs the MCPServer CRD
func installMCPServerCRD(ctx context.Context, k8sClient client.Client) error {
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mcpservers.mcp.matey.ai",
			Annotations: map[string]string{
				"controller-gen.kubebuilder.io/version": "v0.12.0",
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "mcp.matey.ai",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "MCPServer",
				ListKind: "MCPServerList",
				Plural:   "mcpservers",
				Singular: "mcpserver",
				ShortNames: []string{"mcpsrv"},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": {
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"image":    {Type: "string"},
										"port":     {Type: "integer", Format: "int32"},
										"protocol": {Type: "string"},
										"replicas": {Type: "integer", Format: "int32"},
									},
								},
								"status": {
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"phase":         {Type: "string"},
										"replicas":      {Type: "integer", Format: "int32"},
										"readyReplicas": {Type: "integer", Format: "int32"},
									},
								},
							},
						},
					},
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
				},
			},
		},
	}

	err := k8sClient.Create(ctx, crd)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// installMCPMemoryCRD installs the MCPMemory CRD
func installMCPMemoryCRD(ctx context.Context, k8sClient client.Client) error {
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mcpmemories.mcp.matey.ai",
			Annotations: map[string]string{
				"controller-gen.kubebuilder.io/version": "v0.12.0",
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "mcp.matey.ai",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "MCPMemory",
				ListKind: "MCPMemoryList",
				Plural:   "mcpmemories",
				Singular: "mcpmemory",
				ShortNames: []string{"mcpmem"},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": {
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"host":              {Type: "string"},
										"port":              {Type: "integer", Format: "int32"},
										"replicas":          {Type: "integer", Format: "int32"},
										"databaseURL":       {Type: "string"},
										"postgresEnabled":   {Type: "boolean"},
										"postgresUser":      {Type: "string"},
										"postgresPassword":  {Type: "string"},
										"postgresDB":        {Type: "string"},
										"postgresPort":      {Type: "integer", Format: "int32"},
									},
								},
								"status": {
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"phase":          {Type: "string"},
										"replicas":       {Type: "integer", Format: "int32"},
										"readyReplicas":  {Type: "integer", Format: "int32"},
										"postgresStatus": {Type: "string"},
									},
								},
							},
						},
					},
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
				},
			},
		},
	}

	err := k8sClient.Create(ctx, crd)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// installMCPTaskSchedulerCRD installs the MCPTaskScheduler CRD
func installMCPTaskSchedulerCRD(ctx context.Context, k8sClient client.Client) error {
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mcptaskschedulers.mcp.matey.ai",
			Annotations: map[string]string{
				"controller-gen.kubebuilder.io/version": "v0.12.0",
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "mcp.matey.ai",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "MCPTaskScheduler",
				ListKind: "MCPTaskSchedulerList",
				Plural:   "mcptaskschedulers",
				Singular: "mcptaskscheduler",
				ShortNames: []string{"mcpts"},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": {
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"image":             {Type: "string"},
										"port":              {Type: "integer", Format: "int32"},
										"host":              {Type: "string"},
										"replicas":          {Type: "integer", Format: "int32"},
										"databasePath":      {Type: "string"},
										"logLevel":          {Type: "string"},
										"mcpProxyURL":       {Type: "string"},
										"mcpProxyAPIKey":    {Type: "string"},
										"ollamaURL":         {Type: "string"},
										"ollamaModel":       {Type: "string"},
										"openRouterAPIKey":  {Type: "string"},
										"openRouterModel":   {Type: "string"},
									},
								},
								"status": {
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"phase":         {Type: "string"},
										"replicas":      {Type: "integer", Format: "int32"},
										"readyReplicas": {Type: "integer", Format: "int32"},
									},
								},
							},
						},
					},
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
				},
			},
		},
	}

	err := k8sClient.Create(ctx, crd)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// installMCPProxyCRD installs the MCPProxy CRD
func installMCPProxyCRD(ctx context.Context, k8sClient client.Client) error {
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mcpproxies.mcp.matey.ai",
			Annotations: map[string]string{
				"controller-gen.kubebuilder.io/version": "v0.12.0",
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "mcp.matey.ai",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "MCPProxy",
				ListKind: "MCPProxyList",
				Plural:   "mcpproxies",
				Singular: "mcpproxy",
				ShortNames: []string{"mcpproxy"},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": {
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"port":        {Type: "integer", Format: "int32"},
										"host":        {Type: "string"},
										"replicas":    {Type: "integer", Format: "int32"},
										"serviceType": {Type: "string"},
										"auth": {
											Type: "object",
											Properties: map[string]apiextensionsv1.JSONSchemaProps{
												"enabled": {Type: "boolean"},
												"apiKey":  {Type: "string"},
											},
										},
										"ingress": {
											Type: "object",
											Properties: map[string]apiextensionsv1.JSONSchemaProps{
												"enabled": {Type: "boolean"},
												"host":    {Type: "string"},
											},
										},
										"serviceAccount": {Type: "string"},
									},
								},
								"status": {
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"phase":         {Type: "string"},
										"replicas":      {Type: "integer", Format: "int32"},
										"readyReplicas": {Type: "integer", Format: "int32"},
									},
								},
							},
						},
					},
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
				},
			},
		},
	}

	err := k8sClient.Create(ctx, crd)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
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
				APIGroups: []string{"mcp.matey.ai"},
				Resources: []string{"mcpservers", "mcpmemories", "mcptaskschedulers", "mcpproxies"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"mcp.matey.ai"},
				Resources: []string{"mcpservers/status", "mcpmemories/status", "mcptaskschedulers/status", "mcpproxies/status"},
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
