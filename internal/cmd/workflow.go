// internal/cmd/workflow.go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/scheduler"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewWorkflowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage workflows",
		Long:  "Create, list, get, delete, and manage workflow schedules and executions",
	}

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
		Short: "Create a new workflow",
		Long: `Create a new workflow from a file, template, or command line options.

Examples:
  # Create from file
  matey workflow create -f my-workflow.yaml

  # Create from template
  matey workflow create health-monitor --template=health-monitoring --param alert_channel=alerts

  # Create with inline schedule
  matey workflow create daily-backup --schedule="0 2 * * *" --template=data-backup
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
		Short: "List workflows",
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
		Short: "Get workflow details",
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
		Short: "Delete a workflow",
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
		Short: "Pause a workflow",
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
		Short: "Resume a paused workflow",
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
		Short: "Get workflow execution logs",
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
		Short: "Manually execute a workflow",
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

// Implementation functions

func createWorkflowFromFile(filename, namespace string, dryRun bool) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var workflow crd.Workflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		return fmt.Errorf("failed to parse workflow YAML: %w", err)
	}

	// Set namespace if not specified
	if workflow.Namespace == "" {
		workflow.Namespace = namespace
	}

	if dryRun {
		output, err := yaml.Marshal(&workflow)
		if err != nil {
			return fmt.Errorf("failed to marshal workflow: %w", err)
		}
		fmt.Print(string(output))
		return nil
	}

	k8sClient, err := getKubernetesClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	if err := k8sClient.Create(context.Background(), &workflow); err != nil {
		return fmt.Errorf("failed to create workflow: %w", err)
	}

	fmt.Printf("Workflow %s created in namespace %s\n", workflow.Name, workflow.Namespace)
	return nil
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

	// Create workflow from template
	spec, err := templateRegistry.CreateWorkflowFromTemplate(templateName, name, parameters)
	if err != nil {
		return fmt.Errorf("failed to create workflow from template: %w", err)
	}

	workflow := &crd.Workflow{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "mcp.matey.ai/v1",
			Kind:       "Workflow",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: *spec,
	}

	if dryRun {
		output, err := yaml.Marshal(workflow)
		if err != nil {
			return fmt.Errorf("failed to marshal workflow: %w", err)
		}
		fmt.Print(string(output))
		return nil
	}

	k8sClient, err := getKubernetesClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	if err := k8sClient.Create(context.Background(), workflow); err != nil {
		return fmt.Errorf("failed to create workflow: %w", err)
	}

	fmt.Printf("Workflow %s created from template %s in namespace %s\n", name, templateName, namespace)
	return nil
}

func listWorkflows(namespace string, allNamespaces bool, output string) error {
	k8sClient, err := getKubernetesClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	var workflowList crd.WorkflowList
	ctx := context.Background()

	if allNamespaces {
		if err := k8sClient.List(ctx, &workflowList); err != nil {
			return fmt.Errorf("failed to list workflows: %w", err)
		}
	} else {
		if err := k8sClient.List(ctx, &workflowList, client.InNamespace(namespace)); err != nil {
			return fmt.Errorf("failed to list workflows: %w", err)
		}
	}

	switch output {
	case "json":
		data, err := json.MarshalIndent(workflowList.Items, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(workflowList.Items)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
		fmt.Print(string(data))
	default:
		printWorkflowTable(workflowList.Items)
	}

	return nil
}

func getWorkflow(name, namespace, output string) error {
	k8sClient, err := getKubernetesClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	var workflow crd.Workflow
	key := client.ObjectKey{Name: name, Namespace: namespace}
	if err := k8sClient.Get(context.Background(), key, &workflow); err != nil {
		return fmt.Errorf("failed to get workflow: %w", err)
	}

	switch output {
	case "json":
		data, err := json.MarshalIndent(workflow, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(workflow)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
		fmt.Print(string(data))
	default:
		printWorkflowDetails(&workflow)
	}

	return nil
}

func deleteWorkflow(name, namespace string) error {
	k8sClient, err := getKubernetesClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	workflow := &crd.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	if err := k8sClient.Delete(context.Background(), workflow); err != nil {
		return fmt.Errorf("failed to delete workflow: %w", err)
	}

	fmt.Printf("Workflow %s deleted from namespace %s\n", name, namespace)
	return nil
}

func pauseWorkflow(name, namespace string) error {
	return updateWorkflowSuspend(name, namespace, true)
}

func resumeWorkflow(name, namespace string) error {
	return updateWorkflowSuspend(name, namespace, false)
}

func updateWorkflowSuspend(name, namespace string, suspend bool) error {
	k8sClient, err := getKubernetesClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	var workflow crd.Workflow
	key := client.ObjectKey{Name: name, Namespace: namespace}
	if err := k8sClient.Get(context.Background(), key, &workflow); err != nil {
		return fmt.Errorf("failed to get workflow: %w", err)
	}

	workflow.Spec.Suspend = suspend

	if err := k8sClient.Update(context.Background(), &workflow); err != nil {
		return fmt.Errorf("failed to update workflow: %w", err)
	}

	action := "resumed"
	if suspend {
		action = "paused"
	}
	fmt.Printf("Workflow %s %s\n", name, action)
	return nil
}

func getWorkflowLogs(name, namespace, step string, follow bool, tail int) error {
	fmt.Printf("Getting logs for workflow %s (step: %s, follow: %v, tail: %d)\n", name, step, follow, tail)
	fmt.Println("Note: Log implementation requires integration with Kubernetes Job logs")
	return nil
}

func executeWorkflow(name, namespace string, wait bool, timeout time.Duration) error {
	fmt.Printf("Manually executing workflow %s (wait: %v, timeout: %v)\n", name, wait, timeout)
	fmt.Println("Note: Manual execution implementation requires creating a Job resource")
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

// Helper functions

func getKubernetesClient() (client.Client, error) {
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	// Add our CRDs to the scheme
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		return nil, fmt.Errorf("failed to add core types to scheme: %w", err)
	}
	
	// Add our CRD types to the scheme
	if err := crd.AddToScheme(s); err != nil {
		return nil, fmt.Errorf("failed to add CRD types to scheme: %w", err)
	}

	c, err := client.New(config, client.Options{Scheme: s})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return c, nil
}

func printWorkflowTable(workflows []crd.Workflow) {
	fmt.Printf("%-20s %-15s %-10s %-15s %-20s %-10s\n", "NAME", "NAMESPACE", "ENABLED", "SCHEDULE", "LAST RUN", "PHASE")
	fmt.Printf("%-20s %-15s %-10s %-15s %-20s %-10s\n", "----", "---------", "-------", "--------", "--------", "-----")

	for _, wf := range workflows {
		name := wf.Name
		if len(name) > 20 {
			name = name[:17] + "..."
		}

		namespace := wf.Namespace
		if len(namespace) > 15 {
			namespace = namespace[:12] + "..."
		}

		enabled := "false"
		if wf.Spec.Enabled {
			enabled = "true"
		}

		schedule := wf.Spec.Schedule
		if len(schedule) > 15 {
			schedule = schedule[:12] + "..."
		}

		lastRun := "Never"
		if wf.Status.LastExecutionTime != nil {
			lastRun = wf.Status.LastExecutionTime.Format("15:04:05")
		}

		phase := string(wf.Status.Phase)
		if phase == "" {
			phase = "Unknown"
		}

		fmt.Printf("%-20s %-15s %-10s %-15s %-20s %-10s\n", name, namespace, enabled, schedule, lastRun, phase)
	}
}

func printWorkflowDetails(workflow *crd.Workflow) {
	fmt.Printf("Name:        %s\n", workflow.Name)
	fmt.Printf("Namespace:   %s\n", workflow.Namespace)
	fmt.Printf("Schedule:    %s\n", workflow.Spec.Schedule)
	fmt.Printf("Timezone:    %s\n", workflow.Spec.Timezone)
	fmt.Printf("Enabled:     %v\n", workflow.Spec.Enabled)
	fmt.Printf("Suspended:   %v\n", workflow.Spec.Suspend)
	fmt.Printf("Phase:       %s\n", workflow.Status.Phase)

	if workflow.Status.LastExecutionTime != nil {
		fmt.Printf("Last Run:    %s\n", workflow.Status.LastExecutionTime.Format(time.RFC3339))
	}

	if workflow.Status.NextScheduleTime != nil {
		fmt.Printf("Next Run:    %s\n", workflow.Status.NextScheduleTime.Format(time.RFC3339))
	}

	fmt.Printf("\nSteps:\n")
	for i, step := range workflow.Spec.Steps {
		fmt.Printf("  %d. %s (%s)\n", i+1, step.Name, step.Tool)
	}

	if len(workflow.Status.StepStatuses) > 0 {
		fmt.Printf("\nLast Execution Status:\n")
		for stepName, status := range workflow.Status.StepStatuses {
			fmt.Printf("  %s: %s\n", stepName, status.Phase)
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