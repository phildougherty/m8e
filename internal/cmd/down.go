// internal/cmd/down.go
package cmd

import (
	"github.com/phildougherty/m8e/internal/compose"

	"github.com/spf13/cobra"
)

func NewDownCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "down [SERVICE...]",
		Short: "Stop and remove MCP services and their Kubernetes resources",
		Long: `Stop and remove MCP services and their Kubernetes resources.

This command deletes the Kubernetes Deployments, Services, ConfigMaps, and other resources
for the specified services. If no services are specified, all services will be stopped.

Examples:
  matey down                  # Stop all services
  matey down memory           # Stop only memory service
  matey down memory task-scheduler  # Stop memory and task scheduler`,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")
			namespace, _ := cmd.Flags().GetString("namespace")

			composer, err := compose.NewK8sComposer(file, namespace)
			if err != nil {
				return err
			}

			return composer.Down(args)
		},
	}

	return cmd
}
