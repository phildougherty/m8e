// internal/cmd/up.go
package cmd

import (
	"github.com/phildougherty/m8e/internal/compose"

	"github.com/spf13/cobra"
)

func NewUpCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up [SERVICE...]",
		Short: "Create and start MCP services using Kubernetes resources",
		Long: `Create and start MCP services using system resources.

This command deploys services as Kubernetes Deployments, Services, and ConfigMaps.
If no services are specified, all enabled services in the configuration will be started.

Examples:
  matey up                    # Start all enabled services
  matey up memory             # Start only memory service
  matey up memory task-scheduler  # Start memory and task scheduler`,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")
			namespace, _ := cmd.Flags().GetString("namespace")

			composer, err := compose.NewK8sComposer(file, namespace)
			if err != nil {
				return err
			}

			return composer.Up(args)
		},
	}

	return cmd
}
