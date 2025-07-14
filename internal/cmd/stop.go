// internal/cmd/stop.go
package cmd

import (
	"fmt"
	"github.com/phildougherty/m8e/internal/compose"

	"github.com/spf13/cobra"
)

func NewStopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop [SERVICE...]",
		Short: "Stop specific MCP services using Kubernetes resources",
		Long: `Stop specific MCP services using Kubernetes-native resources.

This command stops the specified services by scaling down their Kubernetes 
Deployments and removing related resources.

Examples:
  matey stop memory             # Stop only memory service
  matey stop memory task-scheduler  # Stop memory and task scheduler`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("no services specified to stop")
			}

			file, _ := cmd.Flags().GetString("file")
			namespace, _ := cmd.Flags().GetString("namespace")

			composer, err := compose.NewK8sComposer(file, namespace)
			if err != nil {
				return err
			}

			return composer.Stop(args)
		},
	}

	return cmd
}
