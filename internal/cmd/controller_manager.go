// internal/cmd/controller_manager.go
package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	
	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/controllers"
	"github.com/phildougherty/m8e/internal/logging"
)

func NewControllerManagerCommand() *cobra.Command {
	var (
		configFile string
		namespace  string
		logLevel   string
	)

	cmd := &cobra.Command{
		Use:   "controller-manager",
		Short: "Run the Matey controller manager",
		Long: `Run the Matey controller manager as a standalone process.
This command is typically used when deploying the controller manager as a Kubernetes pod.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := logging.NewLogger(logLevel)
			
			logger.Info("Starting Matey controller manager")
			
			// Load configuration
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			
			// Create controller manager
			cm, err := controllers.NewControllerManager(namespace, cfg)
			if err != nil {
				return fmt.Errorf("failed to create controller manager: %w", err)
			}
			
			// Setup signal handling
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			
			// Start controller manager
			logger.Info("Controller manager starting...")
			if err := cm.Start(ctx); err != nil {
				return fmt.Errorf("failed to start controller manager: %w", err)
			}
			
			logger.Info("Controller manager stopped")
			return nil
		},
	}
	
	cmd.Flags().StringVar(&configFile, "config", "matey.yaml", "Path to configuration file")
	cmd.Flags().StringVar(&namespace, "namespace", "matey", "Kubernetes namespace to operate in")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	
	return cmd
}