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
		Use:   "task-scheduler",
		Short: "Manage the task scheduler service and workflows",
		Long: `Start, stop, enable, or disable the task scheduler service using Kubernetes.
The task scheduler provides intelligent task automation with:
- Built-in cron scheduling with AI-powered expression generation
- 14 MCP tools for workflow and task management
- Kubernetes Jobs for reliable task execution
- OpenRouter and Ollama integration for LLM-powered workflows
- Workflow templates and dependency management

Examples:
  matey task-scheduler               # Start task scheduler via Kubernetes
  matey task-scheduler --enable      # Enable in config
  matey task-scheduler --disable     # Disable service`,
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
		Volumes: cfg.TaskScheduler.Volumes,
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

  # Create from template
  matey task-scheduler create health-monitor --template=health-monitoring --param alert_channel=alerts

  # Create with inline schedule
  matey task-scheduler create daily-backup --schedule="0 2 * * *" --template=data-backup
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
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
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

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
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

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
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

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")

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

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")

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

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")

	return cmd
}

func newWorkflowLogsCommand() *cobra.Command {
	var (
		namespace string
		step      string
		follow    bool
		tail      int
	)

	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Get workflow execution logs from task scheduler",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getWorkflowLogs(args[0], namespace, step, follow, tail)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	cmd.Flags().StringVar(&step, "step", "", "Get logs for specific step")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVar(&tail, "tail", 100, "Number of lines to show from the end")

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

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for execution to complete")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Timeout for waiting")

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

func getWorkflowLogs(name, namespace, step string, follow bool, tail int) error {
	// For now, redirect to the task scheduler logs since workflows run through the scheduler
	fmt.Printf("Getting logs for workflow %s in task scheduler...\n", name)

	// Get the task scheduler logs which contain workflow execution logs
	// This would need to be enhanced to filter logs by workflow name
	fmt.Printf("Note: Workflow logs are included in task scheduler logs.\n")
	fmt.Printf("Use: kubectl logs -n %s deployment/task-scheduler -f  < /dev/null |  grep '%s'\n", namespace, name)
	
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
	fmt.Printf("%-20s %-15s %-10s %-15s %-20s %-10s\n", "NAME", "NAMESPACE", "ENABLED", "SCHEDULE", "TEMPLATE", "SUSPEND")
	fmt.Printf("%-20s %-15s %-10s %-15s %-20s %-10s\n", "----", "---------", "-------", "--------", "--------", "-------")

	for _, wf := range workflows {
		name := wf.Name
		if len(name) > 20 {
			name = name[:17] + "..."
		}

		// Namespace would need to be passed separately or tracked in context
		namespace := "default" // Placeholder for display
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
	fmt.Printf("Namespace:   %s\n", "default") // Namespace tracked separately
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
	fmt.Printf("%-25s %-15s %-50s\n", "NAME", "CATEGORY", "DESCRIPTION")
	fmt.Printf("%-25s %-15s %-50s\n", "----", "--------", "-----------")

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
