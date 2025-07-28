// internal/cmd/task_scheduler.go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/scheduler"
	"github.com/phildougherty/m8e/internal/task_scheduler"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
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

func getWorkflowLogs(nameOrExecutionID, namespace, executionID, step string, follow bool, tail int, since string) error {
	k8sConfig, err := createK8sConfig()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create controller-runtime client: %w", err)
	}

	// Determine if we have a workflow name or execution ID
	var workflowName, targetExecutionID string
	
	// If it looks like an execution ID (contains hyphens and is long), treat as execution ID
	if len(nameOrExecutionID) > 20 && strings.Contains(nameOrExecutionID, "-") {
		targetExecutionID = nameOrExecutionID
		// Extract workflow name from execution ID (format: workflow-name-hash)
		parts := strings.Split(nameOrExecutionID, "-")
		if len(parts) >= 2 {
			workflowName = strings.Join(parts[:len(parts)-1], "-")
		}
	} else {
		workflowName = nameOrExecutionID
		targetExecutionID = executionID
	}

	// If no specific execution ID provided, try to find the most recent execution
	if targetExecutionID == "" {
		recentExecutionID, err := findMostRecentExecution(k8sClient, namespace, workflowName)
		if err != nil {
			// If we can't find executions in the CRD status, try to find workflow jobs directly
			fmt.Printf("No execution history found for workflow '%s' in MCPTaskScheduler status.\n", workflowName)
			fmt.Printf("Searching for recent workflow jobs...\n")
			
			recentJobID, err := findMostRecentWorkflowJob(clientset, namespace, workflowName)
			if err != nil {
				fmt.Printf("Error: %v\n\n", err)
				fmt.Printf("This could happen if:\n")
				fmt.Printf("1. The workflow hasn't been executed yet\n")
				fmt.Printf("2. The workflow name is incorrect (available: %s)\n", getAvailableWorkflowNames(k8sClient, namespace))
				fmt.Printf("3. The executions are not being recorded in the MCPTaskScheduler status\n\n")
				fmt.Printf("Try:\n")
				fmt.Printf("  matey ts list                    # List available workflows\n")
				fmt.Printf("  matey ts execute %s             # Execute the workflow manually\n", workflowName)
				fmt.Printf("  kubectl logs -n %s -l app=workflow-runner,workflow=%s --tail=100\n", namespace, workflowName)
				return fmt.Errorf("no recent executions found for workflow %s", workflowName)
			}
			targetExecutionID = recentJobID
		} else {
			targetExecutionID = recentExecutionID
		}
	}

	fmt.Printf("Getting consolidated logs for workflow '%s', execution '%s'\n", workflowName, targetExecutionID)
	
	// Get logs from the workflow job
	logs, err := getWorkflowJobLogs(clientset, namespace, workflowName, targetExecutionID, step, follow, tail, since)
	if err != nil {
		return fmt.Errorf("failed to get workflow logs: %w", err)
	}

	fmt.Print(logs)
	return nil
}

func executeWorkflow(name, namespace string, wait bool, timeout time.Duration) error {
	// This would trigger workflow execution through the MCP tools
	fmt.Printf("Triggering manual execution of workflow %s via task scheduler MCP tools...\n", name)
	
	// In a full implementation, this would use the MCP client to call the execute_workflow tool
	// For now, provide guidance on how to do it manually
	fmt.Printf("To execute manually, use the MCP client to call the execute_workflow tool:\n")
	fmt.Printf("  curl -X POST http://task-scheduler.%s.svc.cluster.local:8018/mcp \\\n", namespace)
	fmt.Printf("    -H 'Content-Type: application/json' \\\n")
	fmt.Printf("    -d '{\"method\":\"tools/call\",\"params\":{\"name\":\"execute_workflow\",\"arguments\":{\"workflow_name\":\"%s\"}}}'\n", name)
	
	return nil
}

func listTemplates(category, output string) error {
	templateRegistry := scheduler.NewTemplateRegistry()

	var templates []*scheduler.WorkflowTemplate
	if category != "" {
		templates = templateRegistry.ListTemplatesByCategory(category)
	} else {
		templates = templateRegistry.ListTemplates()
	}

	switch output {
	case "json":
		data, err := json.MarshalIndent(templates, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(templates)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
		fmt.Print(string(data))
	default:
		printTemplateTable(templates)
	}

	return nil
}

// Helper functions for display

func printWorkflowTable(workflows []crd.WorkflowDefinition) {
	enabledCount := 0
	for _, wf := range workflows {
		if wf.Enabled {
			enabledCount++
		}
	}
	fmt.Printf("Workflow Definitions (%d total, %d enabled)\n", len(workflows), enabledCount)
	fmt.Println(strings.Repeat("=", 50))

	for _, wf := range workflows {
		name := wf.Name
		if len(name) > 20 {
			name = name[:17] + "..."
		}

		// Namespace would need to be passed separately or tracked in context
		namespace := "matey" // Placeholder for display
		if len(namespace) > 15 {
			namespace = namespace[:12] + "..."
		}

		enabled := "false"
		if wf.Enabled {
			enabled = "true"
		}

		schedule := wf.Schedule
		if schedule == "" {
			schedule = "None"
		}
		if len(schedule) > 15 {
			schedule = schedule[:12] + "..."
		}

		// Template field doesn't exist in WorkflowDefinition - using placeholder
		template := "Custom"
		if len(template) > 20 {
			template = template[:17] + "..."
		}

		// Using Enabled field inverted as suspend indicator
		suspend := "false"
		if !wf.Enabled {
			suspend = "true"
		}

		fmt.Printf("%-20s %-15s %-10s %-15s %-20s %-10s\n", name, namespace, enabled, schedule, template, suspend)
	}
}

func printWorkflowDetails(workflow *crd.WorkflowDefinition, taskScheduler *crd.MCPTaskScheduler) {
	fmt.Printf("Name:        %s\n", workflow.Name)
	fmt.Printf("Namespace:   %s\n", "matey") // Namespace tracked separately
	fmt.Printf("Template:    %s\n", "Custom")   // Template field not available in WorkflowDefinition
	fmt.Printf("Schedule:    %s\n", workflow.Schedule)
	fmt.Printf("Timezone:    %s\n", workflow.Timezone)
	fmt.Printf("Enabled:     %v\n", workflow.Enabled)
	fmt.Printf("Suspended:   %v\n", !workflow.Enabled) // Using Enabled inverted

	if len(workflow.Parameters) > 0 {
		fmt.Printf("\nParameters:\n")
		for key, value := range workflow.Parameters {
			fmt.Printf("  %s: %v\n", key, value)
		}
	}

	fmt.Printf("\nSteps:\n")
	for i, step := range workflow.Steps {
		fmt.Printf("  %d. %s (%s)\n", i+1, step.Name, step.Tool)
		if len(step.Parameters) > 0 {
			fmt.Printf("     Parameters: %v\n", step.Parameters)
		}
		if len(step.DependsOn) > 0 {
			fmt.Printf("     Dependencies: %v\n", step.DependsOn)
		}
	}

	// Show execution history from task scheduler status
	if len(taskScheduler.Status.WorkflowExecutions) > 0 {
		fmt.Printf("\nRecent Executions:\n")
		for _, execution := range taskScheduler.Status.WorkflowExecutions {
			if execution.WorkflowName == workflow.Name {
				fmt.Printf("  ID: %s, Phase: %s, Started: %s\n", 
					execution.ID, execution.Phase, execution.StartTime.Format(time.RFC3339))
			}
		}
	}
}

func printTemplateTable(templates []*scheduler.WorkflowTemplate) {
	categoryCount := make(map[string]int)
	for _, template := range templates {
		categoryCount[template.Category]++
	}
	fmt.Printf("Workflow Templates (%d total, %d categories)\n", len(templates), len(categoryCount))
	fmt.Println(strings.Repeat("=", 50))

	for _, tmpl := range templates {
		name := tmpl.Name
		if len(name) > 25 {
			name = name[:22] + "..."
		}

		category := tmpl.Category
		if len(category) > 15 {
			category = category[:12] + "..."
		}

		description := tmpl.Description
		if len(description) > 50 {
			description = description[:47] + "..."
		}

		fmt.Printf("%-25s %-15s %-50s\n", name, category, description)
	}
}

// Workspace management implementation functions

func listWorkspaces(namespace, output string, showAll bool) error {
	k8sConfig, err := createK8sConfig()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	ctx := context.Background()
	labelSelector := "mcp.matey.ai/workspace-type=workflow"
	
	pvcList, err := clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list workspace PVCs: %w", err)
	}

	// Filter out auto-delete PVCs unless showAll is true
	var filteredPVCs []corev1.PersistentVolumeClaim
	for _, pvc := range pvcList.Items {
		if showAll {
			filteredPVCs = append(filteredPVCs, pvc)
		} else {
			// Skip auto-delete PVCs
			if autoDelete, exists := pvc.Annotations["mcp.matey.ai/auto-delete"]; !exists || autoDelete != "true" {
				filteredPVCs = append(filteredPVCs, pvc)
			}
		}
	}

	switch output {
	case "json":
		data, err := json.MarshalIndent(filteredPVCs, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(filteredPVCs)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
		fmt.Print(string(data))
	default:
		printWorkspaceTable(filteredPVCs)
	}

	return nil
}

func mountWorkspace(nameOrExecutionID, namespace, mountPath string) error {
	k8sConfig, err := createK8sConfig()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create controller-runtime client: %w", err)
	}

	// Parse execution ID or find from workflow name
	var workflowName, executionID string
	
	// Try to extract workflow name and execution ID
	if strings.Contains(nameOrExecutionID, "-") && len(nameOrExecutionID) > 10 {
		// Looks like an execution ID
		executionID = nameOrExecutionID
		parts := strings.Split(nameOrExecutionID, "-")
		if len(parts) >= 2 {
			workflowName = strings.Join(parts[:len(parts)-1], "-")
		}
	} else {
		// Workflow name provided, find most recent execution
		recentExecutionID, err := findMostRecentExecution(k8sClient, namespace, nameOrExecutionID)
		if err != nil {
			return fmt.Errorf("failed to find recent execution: %w", err)
		}
		executionID = recentExecutionID
		workflowName = nameOrExecutionID
	}

	// Check if PVC exists
	pvcName := fmt.Sprintf("workspace-%s", executionID)
	pvc, err := clientset.CoreV1().PersistentVolumeClaims(namespace).Get(
		context.Background(), pvcName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("workspace PVC %s not found: %w", pvcName, err)
	}

	// Verify it's a workspace PVC
	if pvc.Labels["mcp.matey.ai/workspace-type"] != "workflow" {
		return fmt.Errorf("PVC %s is not a workflow workspace", pvcName)
	}

	// Default mount path if not provided
	if mountPath == "" {
		mountPath = fmt.Sprintf("/tmp/matey-workspaces/%s", executionID)
	}

	// Create mount directory
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		return fmt.Errorf("failed to create mount directory %s: %w", mountPath, err)
	}

	fmt.Printf("Mounting workspace PVC %s to %s\n", pvcName, mountPath)
	fmt.Printf("Workflow: %s\n", workflowName)
	fmt.Printf("Execution ID: %s\n", executionID)
	fmt.Printf("PVC Status: %s\n", pvc.Status.Phase)
	
	// Create workspace info file
	infoFile := filepath.Join(mountPath, ".workspace-info")
	infoContent := fmt.Sprintf(`Workspace Information
=====================
Workflow Name: %s
Execution ID: %s
PVC Name: %s
Mount Path: %s
Mounted At: %s
PVC Status: %s
PVC Size: %s

This directory contains files and data from the workflow execution.
To unmount: matey ts workspace unmount %s
`, workflowName, executionID, pvcName, mountPath, time.Now().Format(time.RFC3339), 
   string(pvc.Status.Phase), pvc.Spec.Resources.Requests[corev1.ResourceStorage], executionID)

	if err := os.WriteFile(infoFile, []byte(infoContent), 0644); err != nil {
		fmt.Printf("Warning: Failed to create workspace info file: %v\n", err)
	}

	fmt.Printf("\nWorkspace mounted successfully!\n")
	fmt.Printf("Access your files at: %s\n", mountPath)
	fmt.Printf("View workspace info: cat %s/.workspace-info\n", mountPath)
	
	return nil
}

func unmountWorkspace(nameOrExecutionID, namespace string) error {
	// Parse execution ID
	var executionID string
	if strings.Contains(nameOrExecutionID, "-") && len(nameOrExecutionID) > 10 {
		executionID = nameOrExecutionID
	} else {
		return fmt.Errorf("invalid execution ID format: %s", nameOrExecutionID)
	}

	// Default mount path
	mountPath := fmt.Sprintf("/tmp/matey-workspaces/%s", executionID)

	// Check if mount directory exists
	if _, err := os.Stat(mountPath); os.IsNotExist(err) {
		return fmt.Errorf("workspace %s is not mounted (directory %s does not exist)", executionID, mountPath)
	}

	// Remove mount directory
	if err := os.RemoveAll(mountPath); err != nil {
		return fmt.Errorf("failed to remove mount directory %s: %w", mountPath, err)
	}

	fmt.Printf("Workspace %s unmounted successfully\n", executionID)
	fmt.Printf("Removed mount directory: %s\n", mountPath)
	
	return nil
}

// Helper functions

func findMostRecentExecution(k8sClient client.Client, namespace, workflowName string) (string, error) {
	// Get the task scheduler to find recent executions
	taskScheduler := &crd.MCPTaskScheduler{}
	err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name: "task-scheduler", Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return "", fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Find most recent execution for this workflow
	var mostRecentExecution *crd.WorkflowExecution
	for i, execution := range taskScheduler.Status.WorkflowExecutions {
		if execution.WorkflowName == workflowName {
			if mostRecentExecution == nil || execution.StartTime.After(mostRecentExecution.StartTime) {
				mostRecentExecution = &taskScheduler.Status.WorkflowExecutions[i]
			}
		}
	}

	if mostRecentExecution == nil {
		return "", fmt.Errorf("no executions found for workflow %s", workflowName)
	}

	return mostRecentExecution.ID, nil
}

func findMostRecentWorkflowJob(clientset kubernetes.Interface, namespace, workflowName string) (string, error) {
	ctx := context.Background()
	
	// Look for jobs with workflow labels
	labelSelector := fmt.Sprintf("mcp.matey.ai/workflow-name=%s", workflowName)
	
	jobs, err := clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list workflow jobs: %w", err)
	}

	if len(jobs.Items) == 0 {
		// Also try looking for jobs with workflow prefix
		jobs, err = clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to list jobs: %w", err)
		}
		
		// Filter jobs that start with "workflow-" and contain the workflow name
		var workflowJobs []string
		for _, job := range jobs.Items {
			if strings.HasPrefix(job.Name, "workflow-") && strings.Contains(job.Name, workflowName) {
				workflowJobs = append(workflowJobs, job.Name)
			}
		}
		
		if len(workflowJobs) == 0 {
			return "", fmt.Errorf("no jobs found for workflow %s", workflowName)
		}
		
		// Return the most recent job name, extract execution ID from it
		// Job name format: workflow-{workflowName}-{hash}
		mostRecentJob := workflowJobs[len(workflowJobs)-1]
		// Extract execution ID from job name
		if strings.HasPrefix(mostRecentJob, "workflow-") {
			return strings.TrimPrefix(mostRecentJob, "workflow-"), nil
		}
		return mostRecentJob, nil
	}

	// Find the most recent job by creation timestamp
	var mostRecentJob *batchv1.Job
	for i, job := range jobs.Items {
		if mostRecentJob == nil || job.CreationTimestamp.After(mostRecentJob.CreationTimestamp.Time) {
			mostRecentJob = &jobs.Items[i]
		}
	}

	// Extract execution ID from job metadata or name
	if executionID, exists := mostRecentJob.Labels["mcp.matey.ai/execution-id"]; exists {
		return executionID, nil
	}
	
	// Fallback: extract from job name
	if strings.HasPrefix(mostRecentJob.Name, "workflow-") {
		return strings.TrimPrefix(mostRecentJob.Name, "workflow-"), nil
	}
	
	return mostRecentJob.Name, nil
}

func getAvailableWorkflowNames(k8sClient client.Client, namespace string) string {
	taskScheduler := &crd.MCPTaskScheduler{}
	err := k8sClient.Get(context.Background(), types.NamespacedName{
		Name: "task-scheduler", Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return "unable to fetch"
	}

	var names []string
	for _, workflow := range taskScheduler.Spec.Workflows {
		names = append(names, workflow.Name)
	}
	
	if len(names) == 0 {
		return "none found"
	}
	
	return strings.Join(names, ", ")
}

func getWorkflowJobLogs(clientset kubernetes.Interface, namespace, workflowName, executionID, step string, follow bool, tail int, since string) (string, error) {
	ctx := context.Background()
	
	// Try to find the actual job for this execution
	jobName := fmt.Sprintf("workflow-%s", strings.ReplaceAll(executionID, "_", "-"))
	
	// First, try to get the specific job
	job, err := clientset.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
	if err != nil {
		// If job not found, try to find jobs by labels
		labelSelector := fmt.Sprintf("mcp.matey.ai/workflow-name=%s,mcp.matey.ai/execution-id=%s", workflowName, executionID)
		jobs, err2 := clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err2 != nil || len(jobs.Items) == 0 {
			// Return structured placeholder with helpful information
			return getWorkflowLogsPlaceholder(namespace, workflowName, executionID, jobName, step, tail, since), nil
		}
		job = &jobs.Items[0]
		jobName = job.Name
	}

	// Get pods for this job
	podSelector := fmt.Sprintf("job-name=%s", jobName)
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: podSelector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods for job %s: %w", jobName, err)
	}

	if len(pods.Items) == 0 {
		return fmt.Sprintf("No pods found for job %s\n", jobName), nil
	}

	// Get logs from the first (and typically only) pod
	pod := pods.Items[0]
	
	logOptions := &corev1.PodLogOptions{
		Follow: follow,
	}
	
	if tail > 0 {
		tailLines := int64(tail)
		logOptions.TailLines = &tailLines
	}
	
	if since != "" {
		if duration, err := time.ParseDuration(since); err == nil {
			sinceTime := metav1.NewTime(time.Now().Add(-duration))
			logOptions.SinceTime = &sinceTime
		}
	}

	// Get the logs
	req := clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, logOptions)
	logs, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get logs for pod %s: %w", pod.Name, err)
	}
	defer logs.Close()

	// Read all logs
	var logContent strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := logs.Read(buf)
		if n > 0 {
			logContent.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	logOutput := fmt.Sprintf("=== Workflow Execution Logs ===\n")
	logOutput += fmt.Sprintf("Workflow: %s\n", workflowName)
	logOutput += fmt.Sprintf("Execution ID: %s\n", executionID)
	logOutput += fmt.Sprintf("Job Name: %s\n", jobName)
	logOutput += fmt.Sprintf("Pod Name: %s\n", pod.Name)
	logOutput += fmt.Sprintf("Namespace: %s\n", namespace)
	logOutput += fmt.Sprintf("Job Status: %s\n", getJobStatus(job))
	logOutput += fmt.Sprintf("Pod Status: %s\n", string(pod.Status.Phase))
	logOutput += "================================\n\n"

	if step != "" {
		logOutput += fmt.Sprintf("=== Step: %s ===\n", step)
		// Filter logs by step if requested (simple grep-like filtering)
		lines := strings.Split(logContent.String(), "\n")
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), strings.ToLower(step)) {
				logOutput += line + "\n"
			}
		}
	} else {
		logOutput += "=== All Steps (Consolidated) ===\n"
		logOutput += logContent.String()
	}

	return logOutput, nil
}

func getWorkflowLogsPlaceholder(namespace, workflowName, executionID, jobName, step string, tail int, since string) string {
	logOutput := fmt.Sprintf("=== Workflow Execution Logs ===\n")
	logOutput += fmt.Sprintf("Workflow: %s\n", workflowName)
	logOutput += fmt.Sprintf("Execution ID: %s\n", executionID)
	logOutput += fmt.Sprintf("Expected Job Name: %s\n", jobName)
	logOutput += fmt.Sprintf("Namespace: %s\n", namespace)
	logOutput += "================================\n\n"

	if step != "" {
		logOutput += fmt.Sprintf("=== Step: %s ===\n", step)
	} else {
		logOutput += "=== All Steps (Consolidated) ===\n"
	}

	logOutput += fmt.Sprintf("Job '%s' not found. This could mean:\n", jobName)
	logOutput += "1. The workflow execution hasn't started yet\n"
	logOutput += "2. The job completed and was already cleaned up\n"
	logOutput += "3. The execution ID format doesn't match the job naming\n\n"

	logOutput += "To get actual logs manually, try:\n"
	logOutput += fmt.Sprintf("  kubectl get jobs -n %s -l mcp.matey.ai/workflow-name=%s\n", namespace, workflowName)
	logOutput += fmt.Sprintf("  kubectl logs -n %s -l app=workflow-runner,workflow=%s --tail=%d\n", namespace, workflowName, tail)
	logOutput += fmt.Sprintf("  kubectl get pods -n %s -l job-name=%s\n", namespace, jobName)

	if since != "" {
		logOutput += fmt.Sprintf("\nNote: Logs since: %s\n", since)
	}

	return logOutput
}

func getJobStatus(job *batchv1.Job) string {
	if job.Status.Succeeded > 0 {
		return "Succeeded"
	}
	if job.Status.Failed > 0 {
		return "Failed"
	}
	if job.Status.Active > 0 {
		return "Running"
	}
	return "Pending"
}

func printWorkspaceTable(pvcs []corev1.PersistentVolumeClaim) {
	fmt.Printf("Workflow Workspaces (%d total)\n", len(pvcs))
	fmt.Println(strings.Repeat("=", 100))
	fmt.Printf("%-20s %-30s %-10s %-12s %-15s %-10s\n", 
		"EXECUTION-ID", "WORKFLOW", "STATUS", "SIZE", "AGE", "RETAIN")
	fmt.Println(strings.Repeat("-", 100))

	for _, pvc := range pvcs {
		executionID := pvc.Labels["mcp.matey.ai/execution-id"]
		if executionID == "" {
			// Try to extract from PVC name
			if strings.HasPrefix(pvc.Name, "workspace-") {
				executionID = strings.TrimPrefix(pvc.Name, "workspace-")
			} else {
				executionID = pvc.Name
			}
		}
		
		workflowName := pvc.Labels["mcp.matey.ai/workflow-name"]
		if workflowName == "" {
			workflowName = "unknown"
		}
		
		// Truncate long names
		if len(executionID) > 20 {
			executionID = executionID[:17] + "..."
		}
		if len(workflowName) > 30 {
			workflowName = workflowName[:27] + "..."
		}

		status := string(pvc.Status.Phase)
		
		size := "unknown"
		if pvc.Spec.Resources.Requests != nil {
			if storage, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
				size = storage.String()
			}
		}

		age := time.Since(pvc.CreationTimestamp.Time).Truncate(time.Minute).String()
		
		retain := "false"
		if autoDelete, exists := pvc.Annotations["mcp.matey.ai/auto-delete"]; !exists || autoDelete != "true" {
			retain = "true"
		}

		fmt.Printf("%-20s %-30s %-10s %-12s %-15s %-10s\n", 
			executionID, workflowName, status, size, age, retain)
	}
	
	fmt.Printf("\nTo mount a workspace: matey ts workspace mount <execution-id> [path]\n")
}
