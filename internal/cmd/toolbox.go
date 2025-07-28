// internal/cmd/toolbox.go
package cmd

import (
	"github.com/spf13/cobra"
	"github.com/phildougherty/m8e/internal/compose"
)

func NewToolboxCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "toolbox",
		Short: "Manage MCP toolboxes - collections of servers working together",
		Long: `Manage MCP toolboxes, which are collections of MCP servers configured to work together as cohesive AI workflows.

Toolboxes provide enterprise features like team collaboration, OAuth integration, dependency management, and pre-built templates for common AI workflows.

Examples:
  matey toolbox create coding-assistant --template=coding-assistant
  matey toolbox list
  matey toolbox up coding-assistant
  matey toolbox logs coding-assistant
  matey toolbox delete coding-assistant`,
	}

	// Add subcommands
	cmd.AddCommand(NewToolboxCreateCommand())
	cmd.AddCommand(NewToolboxListCommand())
	cmd.AddCommand(NewToolboxUpCommand())
	cmd.AddCommand(NewToolboxDownCommand())
	cmd.AddCommand(NewToolboxDeleteCommand())
	cmd.AddCommand(NewToolboxLogsCommand())
	cmd.AddCommand(NewToolboxStatusCommand())
	cmd.AddCommand(NewToolboxTemplatesCommand())

	return cmd
}

func NewToolboxCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [NAME]",
		Short: "Create a new MCP toolbox",
		Long: `Create a new MCP toolbox with the specified configuration.

You can use pre-built templates or create custom toolboxes by specifying servers, dependencies, and configuration.

Templates available:
  - coding-assistant: Complete coding assistant with filesystem, git, web search, and memory
  - rag-stack: Retrieval Augmented Generation with memory, search, and document processing
  - research-agent: Research workflow with web search, memory, and document analysis

Examples:
  matey toolbox create my-assistant --template=coding-assistant
  matey toolbox create my-rag --template=rag-stack
  matey toolbox create custom --file=toolbox.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			template, _ := cmd.Flags().GetString("template")
			file, _ := cmd.Flags().GetString("file")
			namespace, _ := cmd.Flags().GetString("namespace")

			return compose.CreateToolbox(name, template, file, namespace)
		},
	}

	cmd.Flags().StringP("template", "t", "", "Use a pre-built template (coding-assistant, rag-stack, research-agent)")
	cmd.Flags().StringP("file", "f", "", "Create from toolbox configuration file")
	cmd.Flags().StringP("description", "d", "", "Description of the toolbox")

	return cmd
}

func NewToolboxListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all MCP toolboxes",
		Long: `List all MCP toolboxes with their status, server count, and template information.

Shows comprehensive information about each toolbox including:
- Phase (Pending, Starting, Running, Degraded, Failed)
- Number of servers and how many are ready
- Template used (if any)
- Creation time

Examples:
  matey toolbox list
  matey toolbox list --format=json
  matey toolbox list --namespace=production`,
		RunE: func(cmd *cobra.Command, args []string) error {
			namespace, _ := cmd.Flags().GetString("namespace")
			format, _ := cmd.Flags().GetString("format")

			return compose.ListToolboxes(namespace, format)
		},
	}

	cmd.Flags().StringP("format", "f", "table", "Output format (table, json, yaml)")

	return cmd
}

func NewToolboxUpCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up [TOOLBOX_NAME]",
		Short: "Start an MCP toolbox",
		Long: `Start an MCP toolbox, which will deploy all its configured servers in dependency order.

The toolbox will:
1. Apply the template if specified
2. Create all server resources
3. Start servers in dependency order
4. Set up networking and shared volumes
5. Configure monitoring and security

Examples:
  matey toolbox up coding-assistant
  matey toolbox up my-rag --wait
  matey toolbox up my-toolbox --timeout=300s`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			namespace, _ := cmd.Flags().GetString("namespace")
			wait, _ := cmd.Flags().GetBool("wait")
			timeout, _ := cmd.Flags().GetDuration("timeout")

			return compose.StartToolbox(name, namespace, wait, timeout)
		},
	}

	cmd.Flags().BoolP("wait", "w", false, "Wait for toolbox to be ready")
	cmd.Flags().Duration("timeout", 0, "Timeout for waiting (e.g., 300s, 5m)")

	return cmd
}

func NewToolboxDownCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "down [TOOLBOX_NAME]",
		Short: "Stop an MCP toolbox",
		Long: `Stop an MCP toolbox and all its servers.

This will:
1. Stop all servers in the toolbox
2. Clean up networking resources
3. Optionally remove persistent volumes
4. Delete the toolbox resource

Examples:
  matey toolbox down coding-assistant
  matey toolbox down my-rag --remove-volumes
  matey toolbox down my-toolbox --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			namespace, _ := cmd.Flags().GetString("namespace")
			removeVolumes, _ := cmd.Flags().GetBool("remove-volumes")
			force, _ := cmd.Flags().GetBool("force")

			return compose.StopToolbox(name, namespace, removeVolumes, force)
		},
	}

	cmd.Flags().Bool("remove-volumes", false, "Remove persistent volumes")
	cmd.Flags().Bool("force", false, "Force deletion without confirmation")

	return cmd
}

func NewToolboxDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete [TOOLBOX_NAME]",
		Short: "Delete an MCP toolbox",
		Long: `Delete an MCP toolbox and all its resources.

This will permanently remove:
1. The toolbox resource
2. All associated servers
3. Networking resources
4. Optionally persistent volumes

Examples:
  matey toolbox delete coding-assistant
  matey toolbox delete my-rag --remove-volumes
  matey toolbox delete my-toolbox --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			namespace, _ := cmd.Flags().GetString("namespace")
			removeVolumes, _ := cmd.Flags().GetBool("remove-volumes")
			force, _ := cmd.Flags().GetBool("force")

			return compose.DeleteToolbox(name, namespace, removeVolumes, force)
		},
	}

	cmd.Flags().Bool("remove-volumes", false, "Remove persistent volumes")
	cmd.Flags().Bool("force", false, "Force deletion without confirmation")

	return cmd
}

func NewToolboxLogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs [TOOLBOX_NAME]",
		Short: "Show logs from a toolbox",
		Long: `Show logs from all servers in a toolbox.

You can view logs from:
- All servers in the toolbox
- Specific servers within the toolbox
- Follow logs in real-time
- Filter by log level or time range

Examples:
  matey toolbox logs coding-assistant
  matey toolbox logs my-rag --server=memory
  matey toolbox logs my-toolbox --follow --tail=100`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			namespace, _ := cmd.Flags().GetString("namespace")
			server, _ := cmd.Flags().GetString("server")
			follow, _ := cmd.Flags().GetBool("follow")
			tail, _ := cmd.Flags().GetInt("tail")

			return compose.ToolboxLogs(name, namespace, server, follow, tail)
		},
	}

	cmd.Flags().StringP("server", "s", "", "Show logs from specific server only")
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")
	cmd.Flags().Int("tail", 50, "Number of lines to show from the end of the logs")

	return cmd
}

func NewToolboxStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [TOOLBOX_NAME]",
		Short: "Show detailed status of a toolbox",
		Long: `Show detailed status information for a specific toolbox.

Includes:
- Overall toolbox phase and health
- Individual server statuses
- Resource usage
- Network endpoints
- Recent events and conditions

Examples:
  matey toolbox status coding-assistant
  matey toolbox status my-rag --format=json
  matey toolbox status my-toolbox --watch`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			namespace, _ := cmd.Flags().GetString("namespace")
			format, _ := cmd.Flags().GetString("format")
			watch, _ := cmd.Flags().GetBool("watch")

			return compose.ToolboxStatus(name, namespace, format, watch)
		},
	}

	cmd.Flags().StringP("format", "f", "table", "Output format (table, json, yaml)")
	cmd.Flags().BoolP("watch", "w", false, "Watch for status changes")

	return cmd
}

func NewToolboxTemplatesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "templates",
		Short: "List available toolbox templates",
		Long: `List all available pre-built toolbox templates with their descriptions and included servers.

Templates provide ready-to-use configurations for common AI workflows:
- coding-assistant: Complete development environment
- rag-stack: Retrieval Augmented Generation setup
- research-agent: Research and analysis workflow

Examples:
  matey toolbox templates
  matey toolbox templates --format=json
  matey toolbox templates --template=coding-assistant`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("format")
			template, _ := cmd.Flags().GetString("template")

			return compose.ListToolboxTemplates(format, template)
		},
	}

	cmd.Flags().StringP("format", "f", "table", "Output format (table, json, yaml)")
	cmd.Flags().StringP("template", "t", "", "Show details for specific template")

	return cmd
}