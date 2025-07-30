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

Available services:
  memory, task-scheduler, controller-manager, proxy, mcp-server
  
Examples:
  matey down                       # Stop all services
  matey down memory                # Stop only memory service  
  matey down memory task-scheduler # Stop memory and task scheduler
  matey down proxy                 # Stop proxy service
  matey down controller-manager    # Stop controller manager`,
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
