// internal/scheduler/k8s_workflow_executor.go
package scheduler

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/task_scheduler"
)

// K8sWorkflowExecutor executes workflows as Kubernetes Jobs
type K8sWorkflowExecutor struct {
	jobManager *task_scheduler.K8sJobManager
	logger     logr.Logger
	namespace  string
	config     *config.ComposeConfig
}

// NewK8sWorkflowExecutor creates a new Kubernetes workflow executor
func NewK8sWorkflowExecutor(namespace string, config *config.ComposeConfig, logger logr.Logger) (*K8sWorkflowExecutor, error) {
	if namespace == "" {
		namespace = "default"
	}

	jobManager, err := task_scheduler.NewK8sJobManager(namespace, config, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create job manager: %w", err)
	}

	return &K8sWorkflowExecutor{
		jobManager: jobManager,
		logger:     logger,
		namespace:  namespace,
		config:     config,
	}, nil
}

// ExecuteWorkflowAsJob executes a workflow definition as a Kubernetes Job
func (kwe *K8sWorkflowExecutor) ExecuteWorkflowAsJob(ctx context.Context, workflowDef *crd.WorkflowDefinition, workflowName, executionID string) (*task_scheduler.TaskStatus, error) {
	kwe.logger.Info("Executing workflow as Kubernetes Job", 
		"workflow", workflowName, 
		"executionID", executionID,
		"steps", len(workflowDef.Steps))

	// Create task request for the workflow
	taskRequest := kwe.createWorkflowTaskRequest(workflowDef, workflowName, executionID)

	// Submit the task as a Kubernetes Job
	taskStatus, err := kwe.jobManager.SubmitTask(ctx, taskRequest)
	if err != nil {
		kwe.logger.Error(err, "Failed to submit workflow as Kubernetes Job", 
			"workflow", workflowName, "executionID", executionID)
		return nil, fmt.Errorf("failed to submit workflow job: %w", err)
	}

	kwe.logger.Info("Workflow submitted as Kubernetes Job", 
		"workflow", workflowName, 
		"executionID", executionID,
		"jobName", taskStatus.JobName)

	return taskStatus, nil
}

// createWorkflowTaskRequest converts a workflow definition to a TaskRequest
func (kwe *K8sWorkflowExecutor) createWorkflowTaskRequest(workflowDef *crd.WorkflowDefinition, workflowName, executionID string) *task_scheduler.TaskRequest {
	// For now, create a simple workflow execution script that runs all steps
	// In production, this could be enhanced to run steps separately or use init containers
	
	// Determine appropriate container image and commands based on workflow steps
	image, command, args := kwe.buildWorkflowExecution(workflowDef)

	// Set environment variables for workflow execution
	env := map[string]string{
		"WORKFLOW_NAME":         workflowName,
		"WORKFLOW_EXECUTION_ID": executionID,
		"KUBERNETES_NAMESPACE":  kwe.namespace,
	}

	// Set timeout from workflow definition
	timeout := 30 * time.Minute // Default timeout
	if workflowDef.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(workflowDef.Timeout); err == nil {
			timeout = parsedTimeout
		}
	}

	// Create shortened names for Kubernetes compliance (63 char limit)
	shortExecutionID := executionID
	if len(executionID) > 20 {
		shortExecutionID = fmt.Sprintf("%x", md5.Sum([]byte(executionID)))[:8]
	}
	
	shortWorkflowName := workflowName
	if len(workflowName) > 20 {
		shortWorkflowName = fmt.Sprintf("%x", md5.Sum([]byte(workflowName)))[:8]
	}

	// Set retry policy
	retryConfig := task_scheduler.TaskRetryConfig{
		MaxRetries:      3,
		RetryDelay:      30 * time.Second,
		BackoffStrategy: "exponential",
	}
	
	if workflowDef.RetryPolicy != nil {
		retryConfig.MaxRetries = int(workflowDef.RetryPolicy.MaxRetries)
		if workflowDef.RetryPolicy.RetryDelay != "" {
			if delay, err := time.ParseDuration(workflowDef.RetryPolicy.RetryDelay); err == nil {
				retryConfig.RetryDelay = delay
			}
		}
		retryConfig.BackoffStrategy = string(workflowDef.RetryPolicy.BackoffStrategy)
	}

	// Set resource requirements - WorkflowDefinition doesn't have Resources field
	// These would come from the MCPTaskScheduler spec or be configured elsewhere
	resources := task_scheduler.TaskResourceConfig{
		CPU:    "200m",
		Memory: "256Mi",
	}

	// Configure workspace volumes if enabled
	var volumes []task_scheduler.TaskVolumeConfig
	if kwe.shouldEnableWorkspace(workflowDef) {
		workspaceConfig := kwe.createWorkspaceVolume(workflowDef, workflowName, executionID, shortWorkflowName, shortExecutionID)
		volumes = append(volumes, workspaceConfig)

		// Add workspace path to environment variables
		workspacePath := kwe.getWorkspaceMountPath(workflowDef)
		env["WORKFLOW_WORKSPACE_PATH"] = workspacePath
		env["WORKSPACE_ENABLED"] = "true"
	}

	return &task_scheduler.TaskRequest{
		ID:          fmt.Sprintf("%s-%s", shortWorkflowName, shortExecutionID),
		Name:        fmt.Sprintf("workflow-%s", shortWorkflowName),
		Description: fmt.Sprintf("Workflow execution for %s (ID: %s)", workflowName, executionID),
		Command:     command,
		Args:        args,
		Image:       image,
		Env:         env,
		Timeout:     timeout,
		Retry:       retryConfig,
		Resources:   resources,
		Volumes:     volumes,
		Metadata: map[string]interface{}{
			"workflowName":    workflowName,
			"executionID":     executionID,
			"workflowType":    "scheduled",
			"stepCount":       len(workflowDef.Steps),
			"schedule":        workflowDef.Schedule,
			"timezone":        workflowDef.Timezone,
			"submittedAt":     time.Now().Format(time.RFC3339),
		},
	}
}

// buildWorkflowExecution creates the appropriate container image and command for executing the workflow
func (kwe *K8sWorkflowExecutor) buildWorkflowExecution(workflowDef *crd.WorkflowDefinition) (string, []string, []string) {
	// Use the standard workflow runner image for all workflows
	// This image contains all common tools: bash, curl, wget, jq, git, python3, nodejs, etc.
	
	if len(workflowDef.Steps) == 0 {
		return "mcp.robotrad.io/workflow-runner:latest", []string{}, []string{"echo 'No steps defined'"}
	}
	
	// Build the complete workflow script
	scriptCommands := kwe.buildShellScript(workflowDef)
	
	// The workflow-runner entrypoint expects the script as the first argument
	return "mcp.robotrad.io/workflow-runner:latest", []string{}, []string{scriptCommands}
}

// buildShellScript creates a shell script that executes all workflow steps
func (kwe *K8sWorkflowExecutor) buildShellScript(workflowDef *crd.WorkflowDefinition) string {
	script := "#!/bin/bash\n"
	script += "set -e\n" // Exit on error
	script += fmt.Sprintf("echo 'Starting workflow: %s'\n", workflowDef.Name)
	
	// Add workspace initialization if enabled
	if kwe.shouldEnableWorkspace(workflowDef) {
		workspacePath := kwe.getWorkspaceMountPath(workflowDef)
		script += fmt.Sprintf("echo 'Workspace enabled at: %s'\n", workspacePath)
		script += fmt.Sprintf("mkdir -p %s\n", workspacePath)
		script += fmt.Sprintf("cd %s\n", workspacePath)
		script += fmt.Sprintf("echo 'Working directory set to workspace'\n")
	}
	
	for i, step := range workflowDef.Steps {
		script += fmt.Sprintf("echo 'Step %d: %s'\n", i+1, step.Name)
		
		switch step.Tool {
		case "echo":
			message := "Hello World"
			if msg, ok := step.Parameters["message"].(string); ok {
				message = msg
			}
			script += fmt.Sprintf("echo '%s'\n", message)
			
		case "date":
			format := ""
			if fmt, ok := step.Parameters["format"].(string); ok {
				format = fmt
			}
			if format != "" {
				script += fmt.Sprintf("date '%s'\n", format)
			} else {
				script += "date\n"
			}
			
		case "sleep":
			duration := "1"
			if dur, ok := step.Parameters["duration"].(string); ok {
				duration = dur
			}
			script += fmt.Sprintf("sleep %s\n", duration)
			
		case "curl":
			url := ""
			method := "GET"
			headers := ""
			data := ""
			
			if u, ok := step.Parameters["url"].(string); ok {
				url = u
			}
			if m, ok := step.Parameters["method"].(string); ok {
				method = m
			}
			if h, ok := step.Parameters["headers"].(string); ok {
				headers = h
			}
			if d, ok := step.Parameters["data"].(string); ok {
				data = d
			}
			
			if url != "" {
				curlCmd := "curl -s"
				if method != "GET" {
					curlCmd += fmt.Sprintf(" -X %s", method)
				}
				if headers != "" {
					curlCmd += fmt.Sprintf(" -H '%s'", headers)
				}
				if data != "" {
					curlCmd += fmt.Sprintf(" -d '%s'", data)
				}
				curlCmd += fmt.Sprintf(" '%s'\n", url)
				script += curlCmd
			}
			
		case "wget":
			url := ""
			output := ""
			if u, ok := step.Parameters["url"].(string); ok {
				url = u
			}
			if o, ok := step.Parameters["output"].(string); ok {
				output = o
			}
			if url != "" {
				wgetCmd := "wget -q"
				if output != "" {
					wgetCmd += fmt.Sprintf(" -O '%s'", output)
				}
				wgetCmd += fmt.Sprintf(" '%s'\n", url)
				script += wgetCmd
			}
			
		case "python3", "python":
			code := ""
			file := ""
			if c, ok := step.Parameters["code"].(string); ok {
				code = c
			}
			if f, ok := step.Parameters["file"].(string); ok {
				file = f
			}
			
			if code != "" {
				script += fmt.Sprintf("python3 -c '%s'\n", code)
			} else if file != "" {
				script += fmt.Sprintf("python3 '%s'\n", file)
			}
			
		case "node", "nodejs":
			code := ""
			file := ""
			if c, ok := step.Parameters["code"].(string); ok {
				code = c
			}
			if f, ok := step.Parameters["file"].(string); ok {
				file = f
			}
			
			if code != "" {
				script += fmt.Sprintf("node -e '%s'\n", code)
			} else if file != "" {
				script += fmt.Sprintf("node '%s'\n", file)
			}
			
		case "jq":
			filter := "."
			input := ""
			if f, ok := step.Parameters["filter"].(string); ok {
				filter = f
			}
			if i, ok := step.Parameters["input"].(string); ok {
				input = i
			}
			
			if input != "" {
				script += fmt.Sprintf("echo '%s' | jq '%s'\n", input, filter)
			} else {
				script += fmt.Sprintf("jq '%s'\n", filter)
			}
			
		case "git":
			command := ""
			if cmd, ok := step.Parameters["command"].(string); ok {
				command = cmd
			}
			if command != "" {
				script += fmt.Sprintf("git %s\n", command)
			}
			
		case "bash", "sh":
			command := ""
			if cmd, ok := step.Parameters["command"].(string); ok {
				command = cmd
			}
			if command != "" {
				script += fmt.Sprintf("%s\n", command)
			}
			
		default:
			// Check if this is an MCP tool
			if strings.HasPrefix(step.Tool, "mcp__") {
				// Parse MCP tool name to extract server and tool
				parts := strings.Split(step.Tool, "__")
				if len(parts) >= 3 {
					server := parts[1]
					tool := strings.Join(parts[2:], "__")
					
					// Convert parameters to JSON, parsing string JSON values
					argsJSON := "{}"
					if len(step.Parameters) > 0 {
						// Convert string JSON values to proper types
						processedParams := make(map[string]interface{})
						for k, v := range step.Parameters {
							if strVal, ok := v.(string); ok {
								// Try to parse as JSON first
								var parsed interface{}
								if err := json.Unmarshal([]byte(strVal), &parsed); err == nil {
									processedParams[k] = parsed
								} else {
									// If not valid JSON, keep as string
									processedParams[k] = strVal
								}
							} else {
								processedParams[k] = v
							}
						}
						
						if argsBytes, err := json.Marshal(processedParams); err == nil {
							argsJSON = string(argsBytes)
						}
					}
					
					// Make direct HTTP call to MCP proxy
					script += fmt.Sprintf(`curl -s -X POST "${MCP_PROXY_URL:-http://matey-proxy.matey.svc.cluster.local:9876}/server/%s" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${MCP_PROXY_API_KEY:-myapikey}" \
  -d '{"method":"tools/call","params":{"name":"%s","arguments":%s}}' || echo 'MCP call failed: %s'`+"\n", 
						server, tool, argsJSON, step.Tool)
				} else {
					script += fmt.Sprintf("echo 'Invalid MCP tool format: %s'\n", step.Tool)
				}
			} else {
				// For unknown tools, try to execute them as commands
				command := step.Tool
				if cmd, ok := step.Parameters["command"].(string); ok {
					command = cmd
				} else if args, ok := step.Parameters["args"].(string); ok {
					command = fmt.Sprintf("%s %s", step.Tool, args)
				}
				script += fmt.Sprintf("%s || echo 'Command failed: %s'\n", command, command)
			}
		}
		
		script += fmt.Sprintf("echo 'Step %d completed'\n", i+1)
	}
	
	script += fmt.Sprintf("echo 'Workflow %s completed successfully'\n", workflowDef.Name)
	return script
}

// GetWorkflowJobStatus gets the status of a workflow job
func (kwe *K8sWorkflowExecutor) GetWorkflowJobStatus(ctx context.Context, workflowName, executionID string) (*task_scheduler.TaskStatus, error) {
	taskID := fmt.Sprintf("%s-%s", workflowName, executionID)
	return kwe.jobManager.GetTaskStatus(ctx, taskID)
}

// GetWorkflowJobLogs gets logs from a workflow job
func (kwe *K8sWorkflowExecutor) GetWorkflowJobLogs(ctx context.Context, workflowName, executionID string) (string, error) {
	taskID := fmt.Sprintf("%s-%s", workflowName, executionID)
	return kwe.jobManager.GetTaskLogs(ctx, taskID)
}

// CancelWorkflowJob cancels a running workflow job
func (kwe *K8sWorkflowExecutor) CancelWorkflowJob(ctx context.Context, workflowName, executionID string) error {
	taskID := fmt.Sprintf("%s-%s", workflowName, executionID)
	return kwe.jobManager.CancelTask(ctx, taskID)
}

// ListWorkflowJobs lists all workflow jobs
func (kwe *K8sWorkflowExecutor) ListWorkflowJobs(ctx context.Context) ([]*task_scheduler.TaskStatus, error) {
	return kwe.jobManager.ListTasks(ctx, "workflow-scheduler")
}

// CleanupOldWorkflowJobs removes old completed workflow jobs
func (kwe *K8sWorkflowExecutor) CleanupOldWorkflowJobs(ctx context.Context, olderThan time.Duration) error {
	return kwe.jobManager.CleanupCompletedTasks(ctx, "workflow-scheduler", olderThan)
}

// shouldEnableWorkspace determines if workspace should be enabled for a workflow
func (kwe *K8sWorkflowExecutor) shouldEnableWorkspace(workflowDef *crd.WorkflowDefinition) bool {
	// If workspace is explicitly configured, use that setting
	if workflowDef.Workspace != nil {
		return workflowDef.Workspace.Enabled
	}
	
	// Default: enable workspace for workflows with multiple steps
	return len(workflowDef.Steps) > 1
}

// getWorkspaceMountPath returns the mount path for the workspace
func (kwe *K8sWorkflowExecutor) getWorkspaceMountPath(workflowDef *crd.WorkflowDefinition) string {
	if workflowDef.Workspace != nil && workflowDef.Workspace.MountPath != "" {
		return workflowDef.Workspace.MountPath
	}
	return "/workspace" // Default mount path
}

// createWorkspaceVolume creates a volume configuration for the workflow workspace
func (kwe *K8sWorkflowExecutor) createWorkspaceVolume(workflowDef *crd.WorkflowDefinition, workflowName, executionID, shortWorkflowName, shortExecutionID string) task_scheduler.TaskVolumeConfig {
	// Get configuration with defaults
	workspace := workflowDef.Workspace
	if workspace == nil {
		workspace = &crd.WorkflowWorkspace{} // Use defaults
	}

	// Default values
	size := workspace.Size
	if size == "" {
		size = "1Gi"
	}

	mountPath := kwe.getWorkspaceMountPath(workflowDef)

	accessModes := workspace.AccessModes
	if len(accessModes) == 0 {
		accessModes = []string{"ReadWriteOnce"}
	}

	// Create unique volume name for this workflow execution (shortened to avoid K8s name limits)
	// Note: shortWorkflowName and shortExecutionID are created in createWorkflowTaskRequest
	volumeName := fmt.Sprintf("ws-%s-%s", shortWorkflowName, shortExecutionID)

	// Determine retention policy - use RetainWorkspace setting with ReclaimPolicy fallback
	autoDelete := true
	if workspace.RetainWorkspace || workspace.ReclaimPolicy == crd.WorkflowVolumeReclaimRetain {
		autoDelete = false
	}

	// Set default retention period
	retentionDays := workspace.WorkspaceRetentionDays
	if retentionDays == 0 {
		retentionDays = 7 // Default 7 days
	}

	return task_scheduler.TaskVolumeConfig{
		Name:      volumeName,
		MountPath: mountPath,
		Type:      "pvc",
		Source: task_scheduler.TaskVolumeSource{
			PVC: &task_scheduler.TaskVolumePVC{
				Size:         size,
				StorageClass: workspace.StorageClass,
				AccessModes:  accessModes,
				AutoDelete:   autoDelete,
			},
		},
	}
}