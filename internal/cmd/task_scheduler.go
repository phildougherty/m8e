// internal/cmd/task_scheduler.go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/scheduler"
	"github.com/phildougherty/m8e/internal/task_scheduler"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewTaskSchedulerCommand() *cobra.Command {
	var enable bool
	var disable bool

	cmd := &cobra.Command{
		Use:     "task-scheduler",
		Aliases: []string{"ts"},
		Short:   "Manage the task scheduler service and workflows",
		Long: `Start, stop, enable, or disable the task scheduler service using Kubernetes.
The task scheduler provides intelligent task automation with:
- Built-in cron scheduling with AI-powered expression generation
- 14 MCP tools for workflow and task management
- Kubernetes Jobs for reliable task execution
- OpenRouter and Ollama integration for LLM-powered workflows
- Workflow templates and dependency management

Examples:
  matey task-scheduler               # Start task scheduler via Kubernetes
  matey ts                           # Same as above (alias)
  matey task-scheduler --enable      # Enable in config
  matey ts --disable                 # Disable service (using alias)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("file")
			namespace, _ := cmd.Flags().GetString("namespace")
			
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if enable {
				return enableTaskScheduler(configFile, cfg)
			}

			if disable {
				return disableTaskScheduler(configFile, cfg, namespace)
			}

			// Check if task scheduler is enabled in config
			if !cfg.TaskScheduler.Enabled {
				fmt.Println("Task scheduler is not enabled in configuration.")
				fmt.Println("Use --enable flag to enable it first.")
				return nil
			}

			// Start the task scheduler using Kubernetes
			return startK8sTaskScheduler(cfg, namespace)
		},
	}

	cmd.Flags().BoolVar(&enable, "enable", false, "Enable the task scheduler in config")
	cmd.Flags().BoolVar(&disable, "disable", false, "Disable the task scheduler")

	// Add workflow subcommands
	cmd.AddCommand(
		newWorkflowCreateCommand(),
		newWorkflowListCommand(),
		newWorkflowGetCommand(),
		newWorkflowDeleteCommand(),
		newWorkflowPauseCommand(),
		newWorkflowResumeCommand(),
		newWorkflowLogsCommand(),
		newWorkflowTemplatesCommand(),
		newWorkflowExecuteCommand(),
		newWorkflowWorkspaceCommand(),
	)

	return cmd
}

func enableTaskScheduler(configFile string, cfg *config.ComposeConfig) error {
	fmt.Println("Enabling task scheduler...")

	// Enable in the built-in task scheduler section
	cfg.TaskScheduler.Enabled = true
	if cfg.TaskScheduler.Port == 0 {
		cfg.TaskScheduler.Port = 8018
	}
	if cfg.TaskScheduler.Host == "" {
		cfg.TaskScheduler.Host = "0.0.0.0"
	}
	// Enable PostgreSQL by default
	if !cfg.TaskScheduler.PostgresEnabled {
		cfg.TaskScheduler.PostgresEnabled = true
	}
	if cfg.TaskScheduler.DatabaseURL == "" {
		cfg.TaskScheduler.DatabaseURL = "postgresql://postgres:password@matey-postgres.matey.svc.cluster.local:5432/matey?sslmode=disable"
	}
	// Keep SQLite as fallback
	if cfg.TaskScheduler.DatabasePath == "" {
		cfg.TaskScheduler.DatabasePath = "/data/task-scheduler.db"
	}
	if cfg.TaskScheduler.LogLevel == "" {
		cfg.TaskScheduler.LogLevel = "info"
	}
	if cfg.TaskScheduler.Workspace == "" {
		cfg.TaskScheduler.Workspace = "/home/phil"
	}
	if cfg.TaskScheduler.CPUs == "" {
		cfg.TaskScheduler.CPUs = "2.0"
	}
	if cfg.TaskScheduler.Memory == "" {
		cfg.TaskScheduler.Memory = "1g"
	}
	if len(cfg.TaskScheduler.Volumes) == 0 {
		cfg.TaskScheduler.Volumes = []string{"/home/phil:/workspace:rw", "/tmp:/tmp:rw"}
	}

	// Also add to servers section for proxy discovery
	if cfg.Servers == nil {
		cfg.Servers = make(map[string]config.ServerConfig)
	}

	// Add task scheduler server to servers config
	cfg.Servers["task-scheduler"] = config.ServerConfig{
		Build: config.BuildConfig{
			Context:    "github.com/phildougherty/m8e-task-scheduler.git",
			Dockerfile: "Dockerfile",
		},
		Command:      "./matey-task-scheduler",
		Args:         []string{"--host", "0.0.0.0", "--port", "8018"},
		Protocol:     "http",
		HttpPort:     constants.TaskSchedulerDefaultPort,
		User:         "root",
		ReadOnly:     false,
		Privileged:   false,
		SecurityOpt:  []string{"no-new-privileges:true"},
		Capabilities: []string{"tools"},
		Env: map[string]string{
			"NODE_ENV":           "production",
			"DATABASE_PATH":      cfg.TaskScheduler.DatabasePath,
			"DATABASE_URL":       cfg.TaskScheduler.DatabaseURL,
			"POSTGRES_ENABLED":   fmt.Sprintf("%t", cfg.TaskScheduler.PostgresEnabled),
			"MCP_PROXY_URL":      cfg.TaskScheduler.MCPProxyURL,
			"MCP_PROXY_API_KEY":  cfg.TaskScheduler.MCPProxyAPIKey,
			"OPENROUTER_API_KEY": cfg.TaskScheduler.OpenRouterAPIKey,
			"OPENROUTER_MODEL":   cfg.TaskScheduler.OpenRouterModel,
			"OLLAMA_URL":         cfg.TaskScheduler.OllamaURL,
			"OLLAMA_MODEL":       cfg.TaskScheduler.OllamaModel,
		},
		Networks: []string{"mcp-net"},
		Authentication: &config.ServerAuthConfig{
			Enabled:       true,
			RequiredScope: "mcp:tools",
			OptionalAuth:  false,
			AllowAPIKey:   &[]bool{true}[0],
		},
		// DependsOn removed - postgres should be managed as MCPPostgres resource
		Volumes: cfg.TaskScheduler.Volumes,
	}

	// Ensure postgres resource exists (don't add to servers config)
	if err := EnsurePostgresResource(); err != nil {
		fmt.Printf("Warning: Failed to ensure postgres resource: %v\n", err)
	}

	fmt.Printf("Task scheduler enabled in both built-in config and servers list (port: %d).\n", cfg.TaskScheduler.Port)

	return config.SaveConfig(configFile, cfg)
}

func disableTaskScheduler(configFile string, cfg *config.ComposeConfig, namespace string) error {
	fmt.Println("Disabling task scheduler...")

	// Create Kubernetes client
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Stop the Kubernetes resources
	taskSchedulerManager := task_scheduler.NewK8sManager(cfg, k8sClient, namespace)
	if err := taskSchedulerManager.Stop(); err != nil {
		fmt.Printf("Warning: %v\n", err)
	}

	// Disable in config
	cfg.TaskScheduler.Enabled = false

	fmt.Println("Task scheduler disabled.")

	return config.SaveConfig(configFile, cfg)
}

// startK8sTaskScheduler starts the task scheduler using Kubernetes
func startK8sTaskScheduler(cfg *config.ComposeConfig, namespace string) error {
	fmt.Println("Creating MCP task scheduler...")
	fmt.Printf("Namespace: %s\n", namespace)
	
	// Create Kubernetes client
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create task scheduler manager and start (non-blocking)
	taskSchedulerManager := task_scheduler.NewK8sManager(cfg, k8sClient, namespace)
	if err := taskSchedulerManager.Start(); err != nil {
		return fmt.Errorf("failed to create MCPTaskScheduler resource: %w", err)
	}

	fmt.Println("MCPTaskScheduler resource created successfully")
	fmt.Println("The controller will deploy the task scheduler service automatically")
	fmt.Printf("Check deployment status with: kubectl get mcptaskscheduler -n %s\n", namespace)
	
	return nil
}

// Workflow subcommand implementations

func newWorkflowCreateCommand() *cobra.Command {
	var (
		filename    string
		template    string
		namespace   string
		parameters  []string
		schedule    string
		timezone    string
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new workflow in the task scheduler",
		Long: `Create a new workflow and add it to the MCPTaskScheduler.

Examples:
  # Create from file
  matey task-scheduler create -f my-workflow.yaml
  matey ts create -f my-workflow.yaml

  # Create from template
  matey ts create health-monitor --template=health-monitoring --param alert_channel=alerts

  # Create with inline schedule
  matey ts create daily-backup --schedule="0 2 * * *" --template=data-backup
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if filename != "" {
				return createWorkflowFromFile(filename, namespace, dryRun)
			}

			if template != "" {
				if len(args) == 0 {
					return fmt.Errorf("workflow name is required when using template")
				}
				return createWorkflowFromTemplate(args[0], template, namespace, parameters, schedule, timezone, dryRun)
			}

			return fmt.Errorf("either --file or --template must be specified")
		},
	}

	cmd.Flags().StringVarP(&filename, "file", "f", "", "Workflow definition file")
	cmd.Flags().StringVar(&template, "template", "", "Template name to use")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace")
	cmd.Flags().StringArrayVar(&parameters, "param", []string{}, "Template parameters (key=value)")
	cmd.Flags().StringVar(&schedule, "schedule", "", "Cron schedule expression")
	cmd.Flags().StringVar(&timezone, "timezone", "", "Timezone for schedule")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the workflow without creating it")

	return cmd
}

func newWorkflowListCommand() *cobra.Command {
	var (
		namespace string
		output    string
		allNamespaces bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workflows in task schedulers",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return listWorkflows(namespace, allNamespaces, output)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace")
	cmd.Flags().StringVarP(&output, "output", "o", "table", "Output format (table, json, yaml)")
	cmd.Flags().BoolVar(&allNamespaces, "all-namespaces", false, "List workflows from all namespaces")

	return cmd
}

func newWorkflowGetCommand() *cobra.Command {
	var (
		namespace string
		output    string
	)

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get workflow details from task scheduler",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getWorkflow(args[0], namespace, output)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace")
	cmd.Flags().StringVarP(&output, "output", "o", "yaml", "Output format (table, json, yaml)")

	return cmd
}

func newWorkflowDeleteCommand() *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a workflow from task scheduler",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteWorkflow(args[0], namespace)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace")

	return cmd
}

func newWorkflowPauseCommand() *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "pause <name>",
		Short: "Pause a workflow in task scheduler",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return pauseWorkflow(args[0], namespace)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace")

	return cmd
}

func newWorkflowResumeCommand() *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "resume <name>",
		Short: "Resume a paused workflow in task scheduler",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return resumeWorkflow(args[0], namespace)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace")

	return cmd
}

func newWorkflowLogsCommand() *cobra.Command {
	var (
		namespace   string
		step        string
		follow      bool
		tail        int
		executionID string
		since       string
	)

	cmd := &cobra.Command{
		Use:   "logs <workflow-name-or-execution-id>",
		Short: "Get consolidated workflow execution logs",
		Long: `Get consolidated logs from all steps of a workflow execution.
If no execution ID is specified, shows logs from the most recent execution.

Examples:
  # Get logs from most recent execution of workflow
  matey ts logs my-workflow

  # Get logs from specific execution ID  
  matey ts logs abc123-def456

  # Get logs for specific step only
  matey ts logs my-workflow --step=step-1

  # Follow logs in real-time
  matey ts logs my-workflow -f`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getWorkflowLogs(args[0], namespace, executionID, step, follow, tail, since)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace")
	cmd.Flags().StringVar(&executionID, "execution-id", "", "Specific execution ID (if not provided, uses most recent)")
	cmd.Flags().StringVar(&step, "step", "", "Get logs for specific step only")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVar(&tail, "tail", 100, "Number of lines to show from the end")
	cmd.Flags().StringVar(&since, "since", "", "Show logs since time (e.g. '1h', '30m', '2h30m')")

	return cmd
}

func newWorkflowTemplatesCommand() *cobra.Command {
	var (
		category string
		output   string
	)

	cmd := &cobra.Command{
		Use:   "templates",
		Short: "List available workflow templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listTemplates(category, output)
		},
	}

	cmd.Flags().StringVar(&category, "category", "", "Filter templates by category")
	cmd.Flags().StringVarP(&output, "output", "o", "table", "Output format (table, json, yaml)")

	return cmd
}

func newWorkflowExecuteCommand() *cobra.Command {
	var (
		namespace string
		wait      bool
		timeout   time.Duration
	)

	cmd := &cobra.Command{
		Use:   "execute <name>",
		Short: "Manually execute a workflow from task scheduler",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeWorkflow(args[0], namespace, wait, timeout)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for execution to complete")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Timeout for waiting")

	return cmd
}

func newWorkflowWorkspaceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage workflow workspaces and persistent volumes",
		Long: `Manage workflow workspaces including listing available PVCs and mounting them locally.
Workspaces contain files and data produced by workflow executions and persist beyond the execution.`,
	}

	cmd.AddCommand(
		newWorkspaceListCommand(),
		newWorkspaceMountCommand(),
		newWorkspaceUnmountCommand(),
	)

	return cmd
}

func newWorkspaceListCommand() *cobra.Command {
	var (
		namespace string
		output    string
		showAll   bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available workflow workspace PVCs",
		Long: `List all workspace persistent volume claims created by workflow executions.
Shows workspace status, size, retention policy, and age.`,
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return listWorkspaces(namespace, output, showAll)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace")
	cmd.Flags().StringVarP(&output, "output", "o", "table", "Output format (table, json, yaml)")
	cmd.Flags().BoolVar(&showAll, "all", false, "Show all workspaces including auto-delete ones")

	return cmd
}

func newWorkspaceMountCommand() *cobra.Command {
	var (
		namespace string
		mountPath string
	)

	cmd := &cobra.Command{
		Use:   "mount <execution-id> [local-path]",
		Short: "Mount a workflow workspace PVC locally",
		Long: `Mount a workflow workspace PVC to a local directory for inspection and manipulation.
If no local path is provided, mounts to /tmp/matey-workspaces/<execution-id>.

Examples:
  # Mount workspace to default location
  matey ts workspace mount workflow-123-abc456

  # Mount workspace to custom location  
  matey ts workspace mount workflow-123-abc456 /mnt/my-workspace`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			executionID := args[0]
			if len(args) > 1 {
				mountPath = args[1]
			}
			return mountWorkspace(executionID, namespace, mountPath)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace")

	return cmd
}

func newWorkspaceUnmountCommand() *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "unmount <execution-id>",
		Short: "Unmount a workflow workspace PVC",  
		Long: `Unmount a previously mounted workflow workspace PVC and clean up the mount directory.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return unmountWorkspace(args[0], namespace)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace")

	return cmd
}



// Workflow implementation functions for unified MCPTaskScheduler

func createWorkflowFromFile(filename, namespace string, dryRun bool) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var workflowDef crd.WorkflowDefinition
	if err := yaml.Unmarshal(data, &workflowDef); err != nil {
		return fmt.Errorf("failed to parse workflow YAML: %w", err)
	}

	if dryRun {
		output, err := yaml.Marshal(&workflowDef)
		if err != nil {
			return fmt.Errorf("failed to marshal workflow: %w", err)
		}
		fmt.Print(string(output))
		return nil
	}

	return addWorkflowToTaskScheduler(workflowDef, namespace)
}

func createWorkflowFromTemplate(name, templateName, namespace string, paramStrings []string, schedule, timezone string, dryRun bool) error {
	templateRegistry := scheduler.NewTemplateRegistry()

	// Parse parameters
	parameters := make(map[string]interface{})
	for _, paramStr := range paramStrings {
		parts := strings.SplitN(paramStr, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid parameter format: %s (expected key=value)", paramStr)
		}
		parameters[parts[0]] = parts[1]
	}

	// Override schedule and timezone if provided
	if schedule != "" {
		parameters["schedule"] = schedule
	}
	if timezone != "" {
		parameters["timezone"] = timezone
	}

	// Create workflow from template - now returns WorkflowDefinition
	workflowDef, err := templateRegistry.CreateWorkflowFromTemplate(templateName, name, parameters)
	if err != nil {
		return fmt.Errorf("failed to create workflow from template: %w", err)
	}

	if dryRun {
		output, err := yaml.Marshal(workflowDef)
		if err != nil {
			return fmt.Errorf("failed to marshal workflow: %w", err)
		}
		fmt.Print(string(output))
		return nil
	}

	return addWorkflowToTaskScheduler(*workflowDef, namespace)
}

func addWorkflowToTaskScheduler(workflowDef crd.WorkflowDefinition, namespace string) error {
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Find the task scheduler in this namespace
	taskScheduler := &crd.MCPTaskScheduler{}
	err = k8sClient.Get(context.Background(), types.NamespacedName{
		Name: "task-scheduler", Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Add the workflow to the task scheduler
	taskScheduler.Spec.Workflows = append(taskScheduler.Spec.Workflows, workflowDef)

	if err := k8sClient.Update(context.Background(), taskScheduler); err != nil {
		return fmt.Errorf("failed to update MCPTaskScheduler: %w", err)
	}

	fmt.Printf("Workflow %s added to task scheduler in namespace %s\n", workflowDef.Name, namespace)
	return nil
}

func listWorkflows(namespace string, allNamespaces bool, output string) error {
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	var taskSchedulerList crd.MCPTaskSchedulerList
	ctx := context.Background()

	if allNamespaces {
		if err := k8sClient.List(ctx, &taskSchedulerList); err != nil {
			return fmt.Errorf("failed to list task schedulers: %w", err)
		}
	} else {
		if err := k8sClient.List(ctx, &taskSchedulerList, client.InNamespace(namespace)); err != nil {
			return fmt.Errorf("failed to list task schedulers: %w", err)
		}
	}

	// Collect all workflows from all task schedulers
	var allWorkflows []crd.WorkflowDefinition
	for _, ts := range taskSchedulerList.Items {
		for _, wf := range ts.Spec.Workflows {
			// Add workflow with namespace context
			wfCopy := wf
			// Note: namespace is tracked separately for display
			allWorkflows = append(allWorkflows, wfCopy)
		}
	}

	switch output {
	case "json":
		data, err := json.MarshalIndent(allWorkflows, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(allWorkflows)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
		fmt.Print(string(data))
	default:
		printWorkflowTable(allWorkflows)
	}

	return nil
}

func getWorkflow(name, namespace, output string) error {
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	taskScheduler := &crd.MCPTaskScheduler{}
	err = k8sClient.Get(context.Background(), types.NamespacedName{
		Name: "task-scheduler", Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Find the workflow
	var foundWorkflow *crd.WorkflowDefinition
	for i, wf := range taskScheduler.Spec.Workflows {
		if wf.Name == name {
			foundWorkflow = &taskScheduler.Spec.Workflows[i]
			break
		}
	}

	if foundWorkflow == nil {
		return fmt.Errorf("workflow %s not found in task scheduler", name)
	}

	// Note: namespace is tracked separately for display context

	switch output {
	case "json":
		data, err := json.MarshalIndent(foundWorkflow, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(foundWorkflow)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
		fmt.Print(string(data))
	default:
		printWorkflowDetails(foundWorkflow, taskScheduler)
	}

	return nil
}

func deleteWorkflow(name, namespace string) error {
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	taskScheduler := &crd.MCPTaskScheduler{}
	err = k8sClient.Get(context.Background(), types.NamespacedName{
		Name: "task-scheduler", Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Find and remove the workflow
	workflowIndex := -1
	for i, wf := range taskScheduler.Spec.Workflows {
		if wf.Name == name {
			workflowIndex = i
			break
		}
	}

	if workflowIndex == -1 {
		return fmt.Errorf("workflow %s not found in task scheduler", name)
	}

	// Remove the workflow from slice
	taskScheduler.Spec.Workflows = append(
		taskScheduler.Spec.Workflows[:workflowIndex],
		taskScheduler.Spec.Workflows[workflowIndex+1:]...,
	)

	if err := k8sClient.Update(context.Background(), taskScheduler); err != nil {
		return fmt.Errorf("failed to update MCPTaskScheduler: %w", err)
	}

	fmt.Printf("Workflow %s deleted from task scheduler in namespace %s\n", name, namespace)
	return nil
}

func pauseWorkflow(name, namespace string) error {
	return updateWorkflowSuspend(name, namespace, true)
}

func resumeWorkflow(name, namespace string) error {
	return updateWorkflowSuspend(name, namespace, false)
}

func updateWorkflowSuspend(name, namespace string, suspend bool) error {
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	taskScheduler := &crd.MCPTaskScheduler{}
	err = k8sClient.Get(context.Background(), types.NamespacedName{
		Name: "task-scheduler", Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Find and update the workflow
	workflowFound := false
	for i, wf := range taskScheduler.Spec.Workflows {
		if wf.Name == name {
			// Note: WorkflowDefinition doesn't have Suspend field - using Enabled instead
			taskScheduler.Spec.Workflows[i].Enabled = !suspend
			workflowFound = true
			break
		}
	}

	if !workflowFound {
		return fmt.Errorf("workflow %s not found in task scheduler", name)
	}

	if err := k8sClient.Update(context.Background(), taskScheduler); err != nil {
		return fmt.Errorf("failed to update MCPTaskScheduler: %w", err)
	}

	action := "resumed"
	if suspend {
		action = "paused"
	}
	fmt.Printf("Workflow %s %s\n", name, action)
	return nil
}

// Note: The file was truncated in the git show output, so I'll add placeholder functions for the remaining workflow functions
func getWorkflowLogs(nameOrExecutionID, namespace, executionID, step string, follow bool, tail int, since string) error {
	// Implementation would go here - this was truncated in the git output
	return fmt.Errorf("getWorkflowLogs implementation not fully restored from git")
}

func executeWorkflow(name, namespace string, wait bool, timeout time.Duration) error {
	// Implementation would go here - this was truncated in the git output
	return fmt.Errorf("executeWorkflow implementation not fully restored from git")
}

func listTemplates(category, output string) error {
	// Implementation would go here - this was truncated in the git output
	return fmt.Errorf("listTemplates implementation not fully restored from git")
}

func printWorkflowTable(workflows []crd.WorkflowDefinition) {
	// Implementation would go here - this was truncated in the git output
	fmt.Printf("printWorkflowTable implementation not fully restored from git\n")
}

func printWorkflowDetails(workflow *crd.WorkflowDefinition, taskScheduler *crd.MCPTaskScheduler) {
	// Implementation would go here - this was truncated in the git output
	fmt.Printf("printWorkflowDetails implementation not fully restored from git\n")
}

func listWorkspaces(namespace, output string, showAll bool) error {
	// Implementation would go here - this was truncated in the git output
	return fmt.Errorf("listWorkspaces implementation not fully restored from git")
}

func mountWorkspace(executionID, namespace, mountPath string) error {
	// Implementation would go here - this was truncated in the git output
	return fmt.Errorf("mountWorkspace implementation not fully restored from git")
}

func unmountWorkspace(executionID, namespace string) error {
	// Implementation would go here - this was truncated in the git output
	return fmt.Errorf("unmountWorkspace implementation not fully restored from git")
}
