// internal/cmd/create-config.go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/service"
)

func NewCreateConfigCommand() *cobra.Command {
	var outputDir string
	var clientType string
	cmd := &cobra.Command{
		Use:   "create-config",
		Short: "Create client configuration for MCP servers",
		Long: `Generate ready-to-use configuration files for MCP servers that can be
imported directly into LLM clients like Claude Desktop, Anthropic API clients,
or OpenAI compatible clients.
This makes it easy to use your MCP servers with popular LLM client applications.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")

			if outputDir == "" {
				outputDir = "client-configs"
			}
			if err := os.MkdirAll(outputDir, constants.DefaultDirMode); err != nil {
				return fmt.Errorf("failed to create output directory: %w", err)
			}

			cfg, err := config.LoadConfig(file)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			out := cmd.OutOrStdout()
			gen := service.NewConfigGenerator(cfg, outputDir, func(msg string) {
				fmt.Fprintln(os.Stderr, msg)
			})

			return gen.Generate(clientType, func(msg string) {
				// Callback signature is func(string); writes to cobra's output
				// stream are not actionable here. SIGPIPE handles broken pipes.
				_, _ = fmt.Fprintln(out, msg)
			})
		},
	}
	// Use different flag names to avoid conflict with the global -c flag
	cmd.Flags().StringVarP(&outputDir, "output", "o", "client-configs", "Directory to output client configurations")
	cmd.Flags().StringVarP(&clientType, "type", "t", "all", "Client type (claude, claude-code, gemini, anthropic, openai, opencode, all)")

	return cmd
}
