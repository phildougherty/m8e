// internal/cmd/install.go
package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/kube"
	"github.com/phildougherty/m8e/internal/service"
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
			if namespace == "" {
				namespace = constants.MateyNamespace
			}

			out := cmd.OutOrStdout()

			if dryRun {
				inst := service.NewInstaller(nil, namespace)
				fmt.Fprintln(out, "Dry run mode - showing what would be installed:")
				for _, line := range inst.DryRunPlan() {
					fmt.Fprintf(out, "✓ %s\n", line)
				}

				return nil
			}

			fmt.Fprintln(out, "Installing Matey CRDs...")

			k8sClient, err := createK8sClientWithCRDs()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			inst := service.NewInstaller(k8sClient, namespace)
			if err := inst.Install(context.Background(), func(msg string) {
				fmt.Fprintf(out, "✓ %s\n", msg)
			}); err != nil {
				return err
			}

			fmt.Fprintln(out, "\nMatey installation complete!")
			fmt.Fprintln(out, "You can now run 'matey up' to start your services.")

			return nil
		},
	}

	cmd.Flags().Bool("dry-run", false, "Print the resources that would be installed without actually installing them")
	// No local --namespace flag: install inherits the root persistent
	// --namespace flag (default constants.MateyNamespace) so install and
	// every other command agree on one namespace. A prior local flag here
	// shadowed the root flag and let install and up disagree silently.

	return cmd
}

// createK8sConfig creates a Kubernetes configuration.
func createK8sConfig() (*rest.Config, error) {
	config, err := kube.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
	}

	return config, nil
}

// createK8sClientWithCRDs creates a Kubernetes client with the CRD,
// apiextensions, core, and RBAC schemes registered. The installer needs the
// apiextensions and RBAC types that the shared newKubeClient() does not carry.
func createK8sClientWithCRDs() (client.Client, error) {
	config, err := createK8sConfig()
	if err != nil {
		return nil, err
	}

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

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return k8sClient, nil
}
