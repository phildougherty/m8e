// internal/cmd/install.go
package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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
the necessary CRDs (MCPServer, MCPMemory, MCPTaskScheduler) into the cluster.

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

	fmt.Println("\n🎉 Matey installation complete!")
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