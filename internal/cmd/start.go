// internal/cmd/start.go
package cmd

import (
	"github.com/phildougherty/m8e/internal/compose"

	"github.com/spf13/cobra"
)

func NewStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start [SERVICE...]",
		Short: "Start specific MCP services using Kubernetes resources",
		Args:  cobra.MinimumNArgs(1),
		Long: `Start specific MCP services using system resources.

This command starts the specified services by creating or updating their 
Kubernetes Deployments and related resources.

Examples:
  matey start memory             # Start only memory service
  matey start memory task-scheduler  # Start memory and task scheduler`,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")
			namespace, _ := cmd.Flags().GetString("namespace")

			composer, err := compose.NewK8sComposer(file, namespace)
			if err != nil {
				return err
			}

			return composer.Start(args)
		},
	}

	return cmd
}
