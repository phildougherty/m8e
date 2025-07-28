// internal/cmd/scheduler_execute_workflow.go
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
	"github.com/phildougherty/m8e/internal/scheduler"
)

// schedulerExecuteWorkflowCmd executes a specific workflow within a Kubernetes Job
var schedulerExecuteWorkflowCmd = &cobra.Command{
	Use:    "scheduler-execute-workflow",
	Short:  "Execute a workflow within a Kubernetes Job (internal command)",
	Hidden: true, // Hidden from help as this is an internal command
	RunE:   runSchedulerExecuteWorkflow,
}

var (
	workflowNameFlag    string
	executionIDFlag     string
	workflowNamespace   string
)

func init() {
	schedulerExecuteWorkflowCmd.Flags().StringVar(&workflowNameFlag, "workflow-name", "", "Name of the workflow to execute")
	schedulerExecuteWorkflowCmd.Flags().StringVar(&executionIDFlag, "execution-id", "", "Execution ID for this workflow run")
	schedulerExecuteWorkflowCmd.Flags().StringVar(&workflowNamespace, "namespace", "matey", "Kubernetes namespace")
	
	schedulerExecuteWorkflowCmd.MarkFlagRequired("workflow-name")
	schedulerExecuteWorkflowCmd.MarkFlagRequired("execution-id")

	// This command will be added by the controller when needed
	// rootCmd.AddCommand(schedulerExecuteWorkflowCmd)
}

func runSchedulerExecuteWorkflow(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Load configuration
	cfg, err := config.LoadConfig("matey.yaml")
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Create Kubernetes client
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Get the workflow definition from the MCPTaskScheduler CRD
	taskScheduler := &crd.MCPTaskScheduler{}
	err = k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: workflowNamespace,
	}, taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Find the specific workflow
	var workflowDef *crd.WorkflowDefinition
	for _, workflow := range taskScheduler.Spec.Workflows {
		if workflow.Name == workflowNameFlag {
			workflowDef = &workflow
			break
		}
	}

	if workflowDef == nil {
		return fmt.Errorf("workflow %q not found in task scheduler", workflowNameFlag)
	}

	fmt.Printf("Starting workflow execution: %s (ID: %s)\n", workflowNameFlag, executionIDFlag)
	fmt.Printf("Workflow has %d steps\n", len(workflowDef.Steps))

	// Initialize logger
	logLevel := "info"
	if cfg.TaskScheduler != nil && cfg.TaskScheduler.LogLevel != "" {
		logLevel = cfg.TaskScheduler.LogLevel
	}
	logger := logging.NewLogger(logLevel)
	logr := logger.GetLogr()

	// Create workflow engine
	mcpProxyURL := os.Getenv("MCP_PROXY_URL")
	if mcpProxyURL == "" {
		mcpProxyURL = fmt.Sprintf("http://matey-proxy.%s.svc.cluster.local:9876", workflowNamespace)
	}
	
	mcpProxyAPIKey := os.Getenv("MCP_PROXY_API_KEY")
	if mcpProxyAPIKey == "" && cfg.Servers != nil {
		if _, exists := cfg.Servers["matey-proxy"]; exists {
			// Extract API key from authentication config if available
			mcpProxyAPIKey = "proxy-api-key" // Placeholder
		}
	}

	workflowEngine := scheduler.NewWorkflowEngine(mcpProxyURL, mcpProxyAPIKey, logr)

	// Execute the workflow
	execution, err := workflowEngine.ExecuteWorkflow(ctx, workflowDef, workflowNameFlag, workflowNamespace)
	if err != nil {
		fmt.Printf("Workflow execution failed: %v\n", err)
		
		// Update the MCPTaskScheduler status with the failure
		if updateErr := updateWorkflowExecutionStatus(ctx, k8sClient, workflowNamespace, execution); updateErr != nil {
			fmt.Printf("Failed to update workflow status: %v\n", updateErr)
		}
		
		return fmt.Errorf("workflow execution failed: %w", err)
	}

	// Update the MCPTaskScheduler status with the results
	if err := updateWorkflowExecutionStatus(ctx, k8sClient, workflowNamespace, execution); err != nil {
		fmt.Printf("Warning: Failed to update workflow status: %v\n", err)
		// Don't fail the workflow just because we couldn't update status
	}

	// Print execution summary
	fmt.Printf("\n=== Workflow Execution Summary ===\n")
	fmt.Printf("Workflow: %s\n", execution.WorkflowName)
	fmt.Printf("Execution ID: %s\n", execution.ID)
	fmt.Printf("Status: %s\n", execution.Status)
	fmt.Printf("Start Time: %s\n", execution.StartTime.Format(time.RFC3339))
	if execution.EndTime != nil {
		fmt.Printf("End Time: %s\n", execution.EndTime.Format(time.RFC3339))
		fmt.Printf("Duration: %s\n", execution.EndTime.Sub(execution.StartTime))
	}

	fmt.Printf("\nStep Results:\n")
	for i, step := range execution.Steps {
		fmt.Printf("  %d. %s (%s): %s", i+1, step.Name, step.Tool, step.Status)
		if step.Error != "" {
			fmt.Printf(" - Error: %s", step.Error)
		}
		if step.Skipped {
			fmt.Printf(" - Skipped: %s", step.SkipReason)
		}
		fmt.Printf(" (Duration: %s, Attempts: %d)\n", step.Duration, step.Attempts)
	}

	if execution.Status == scheduler.WorkflowExecutionStatusSucceeded {
		fmt.Printf("\n✅ Workflow completed successfully!\n")
		return nil
	} else {
		fmt.Printf("\n❌ Workflow failed: %s\n", execution.Error)
		return fmt.Errorf("workflow failed: %s", execution.Error)
	}
}

// updateWorkflowExecutionStatus updates the MCPTaskScheduler CRD with execution results
func updateWorkflowExecutionStatus(ctx context.Context, k8sClient client.Client, namespace string, execution *scheduler.WorkflowExecution) error {
	// Get the current MCPTaskScheduler
	taskScheduler := &crd.MCPTaskScheduler{}
	err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to get MCPTaskScheduler for status update: %w", err)
	}

	// Convert WorkflowExecution to WorkflowExecution status
	statusExecution := crd.WorkflowExecution{
		ID:           execution.ID,
		WorkflowName: execution.WorkflowName,
		StartTime:    execution.StartTime,
		Phase:        crd.WorkflowPhase(execution.Status),
		StepResults:  make(map[string]crd.StepResult),
	}

	if execution.EndTime != nil {
		statusExecution.EndTime = execution.EndTime
		duration := execution.EndTime.Sub(execution.StartTime)
		statusExecution.Duration = &duration
	}

	if execution.Error != "" {
		statusExecution.Message = execution.Error
	}

	// Convert step executions to step results
	for _, step := range execution.Steps {
		stepResult := crd.StepResult{
			Phase:    crd.StepPhase(step.Status),
			Output:   step.Output,
			Duration: step.Duration,
			Attempts: int32(step.Attempts),
		}
		if step.Error != "" {
			stepResult.Error = step.Error
		}
		statusExecution.StepResults[step.Name] = stepResult
	}

	// Update the status - limit to most recent 10 executions per workflow
	if taskScheduler.Status.WorkflowExecutions == nil {
		taskScheduler.Status.WorkflowExecutions = make([]crd.WorkflowExecution, 0)
	}

	// Add new execution
	taskScheduler.Status.WorkflowExecutions = append(taskScheduler.Status.WorkflowExecutions, statusExecution)

	// Keep only the most recent 10 executions per workflow
	executionsByWorkflow := make(map[string][]crd.WorkflowExecution)
	for _, exec := range taskScheduler.Status.WorkflowExecutions {
		executionsByWorkflow[exec.WorkflowName] = append(executionsByWorkflow[exec.WorkflowName], exec)
	}

	// Trim each workflow's execution history to 10 most recent
	var allExecutions []crd.WorkflowExecution
	for workflowName, executions := range executionsByWorkflow {
		if len(executions) > 10 {
			// Sort by start time (most recent first) and take first 10
			// For simplicity, just take the last 10 (they're likely already in order)
			executions = executions[len(executions)-10:]
		}
		allExecutions = append(allExecutions, executions...)
		fmt.Printf("Keeping %d execution(s) for workflow %s\n", len(executions), workflowName)
	}
	
	taskScheduler.Status.WorkflowExecutions = allExecutions

	// Update the status
	err = k8sClient.Status().Update(ctx, taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to update workflow execution status: %w", err)
	}

	fmt.Printf("Updated workflow execution status: %s (ID: %s, Status: %s)\n", 
		execution.WorkflowName, execution.ID, execution.Status)
	return nil
}