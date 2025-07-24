package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/crd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WorkflowStats represents calculated statistics for a specific workflow
type WorkflowStats struct {
	Total         int
	Succeeded     int
	Failed        int
	Running       int
	Pending       int
	AvgDuration   time.Duration
	LastExecution *time.Time
}

// createWorkflow creates a workflow in the MCPTaskScheduler
func (m *MateyMCPServer) createWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	steps, ok := args["steps"].([]interface{})
	if !ok || len(steps) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "steps are required"}},
			IsError: true,
		}, fmt.Errorf("steps are required")
	}
	
	// Process steps for MCPTaskScheduler WorkflowDefinition
	workflowSteps := make([]map[string]interface{}, 0, len(steps))
	for i, step := range steps {
		stepMap, ok := step.(map[string]interface{})
		if !ok {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("step %d is not a valid object", i)}},
				IsError: true,
			}, fmt.Errorf("step %d is not a valid object", i)
		}
		
		// Validate required fields
		stepName, nameOk := stepMap["name"].(string)
		stepTool, toolOk := stepMap["tool"].(string)
		if !nameOk || !toolOk {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("step %d is missing required 'name' or 'tool' field", i)}},
				IsError: true,
			}, fmt.Errorf("step %d is missing required 'name' or 'tool' field", i)
		}
		
		// Create WorkflowStep structure
		workflowStep := map[string]interface{}{
			"name": stepName,
			"tool": stepTool,
		}
		
		// Add optional fields
		if parameters, ok := stepMap["parameters"].(map[string]interface{}); ok {
			workflowStep["parameters"] = parameters
		}
		if condition, ok := stepMap["condition"].(string); ok && condition != "" {
			workflowStep["condition"] = condition
		}
		if continueOnError, ok := stepMap["continueOnError"].(bool); ok {
			workflowStep["continueOnError"] = continueOnError
		}
		if dependsOn, ok := stepMap["dependsOn"].([]interface{}); ok && len(dependsOn) > 0 {
			workflowStep["dependsOn"] = dependsOn
		}
		if timeout, ok := stepMap["timeout"].(string); ok && timeout != "" {
			workflowStep["timeout"] = timeout
		}
		
		workflowSteps = append(workflowSteps, workflowStep)
	}
	
	// Build workflow definition for MCPTaskScheduler
	workflowDef := map[string]interface{}{
		"name":    name,
		"steps":   workflowSteps,
		"enabled": true,
	}
	
	// Add optional fields
	if description, ok := args["description"].(string); ok && description != "" {
		workflowDef["description"] = description
	}
	if schedule, ok := args["schedule"].(string); ok && schedule != "" {
		workflowDef["schedule"] = schedule
	}
	if timezone, ok := args["timezone"].(string); ok && timezone != "" {
		workflowDef["timezone"] = timezone
	}
	if enabled, ok := args["enabled"].(bool); ok {
		workflowDef["enabled"] = enabled
	}
	if concurrencyPolicy, ok := args["concurrencyPolicy"].(string); ok && concurrencyPolicy != "" {
		workflowDef["concurrencyPolicy"] = concurrencyPolicy
	}
	if timeout, ok := args["timeout"].(string); ok && timeout != "" {
		workflowDef["timeout"] = timeout
	}
	if retryPolicy, ok := args["retryPolicy"].(map[string]interface{}); ok {
		workflowDef["retryPolicy"] = retryPolicy
	}
	
	// Try to get existing MCPTaskScheduler and add workflow to it
	if m.useK8sClient() {
		result, err := m.addWorkflowToTaskScheduler(ctx, workflowDef)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Kubernetes workflow creation failed: %v", err)}},
				IsError: true,
			}, err
		}
		return result, nil
	}
	
	// Fall back to binary execution
	return m.createWorkflowWithBinary(ctx, args)
}

// addWorkflowToTaskScheduler adds a workflow to an existing MCPTaskScheduler
func (m *MateyMCPServer) addWorkflowToTaskScheduler(ctx context.Context, workflowDef map[string]interface{}) (*ToolResult, error) {
	// First, get the current MCPTaskScheduler to retrieve existing workflows
	var taskScheduler crd.MCPTaskScheduler
	taskSchedulerName := "task-scheduler"
	
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      taskSchedulerName,
		Namespace: m.namespace,
	}, &taskScheduler)
	
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting task scheduler '%s': %v", taskSchedulerName, err)}},
			IsError: true,
		}, err
	}
	
	// Convert workflow definition to proper WorkflowDefinition struct
	workflowName, _ := workflowDef["name"].(string)
	
	// Check if workflow with same name already exists
	for _, existingWorkflow := range taskScheduler.Spec.Workflows {
		if existingWorkflow.Name == workflowName {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Workflow '%s' already exists. Use update operation to modify it.", workflowName)}},
				IsError: true,
			}, fmt.Errorf("workflow '%s' already exists", workflowName)
		}
	}
	
	// Convert map to WorkflowDefinition struct
	newWorkflow := crd.WorkflowDefinition{
		Name:    workflowName,
		Enabled: true,
	}
	
	// Set optional fields
	if description, ok := workflowDef["description"].(string); ok {
		newWorkflow.Description = description
	}
	if schedule, ok := workflowDef["schedule"].(string); ok {
		newWorkflow.Schedule = schedule
	}
	if timezone, ok := workflowDef["timezone"].(string); ok {
		newWorkflow.Timezone = timezone
	}
	if enabled, ok := workflowDef["enabled"].(bool); ok {
		newWorkflow.Enabled = enabled
	}
	if concurrencyPolicy, ok := workflowDef["concurrencyPolicy"].(string); ok {
		newWorkflow.ConcurrencyPolicy = crd.WorkflowConcurrencyPolicy(concurrencyPolicy)
	}
	if timeout, ok := workflowDef["timeout"].(string); ok {
		newWorkflow.Timeout = timeout
	}
	
	// Convert steps
	if stepsInterface, ok := workflowDef["steps"].([]map[string]interface{}); ok {
		for _, stepMap := range stepsInterface {
			step := crd.WorkflowStep{
				Name: stepMap["name"].(string),
				Tool: stepMap["tool"].(string),
			}
			
			if parameters, ok := stepMap["parameters"].(map[string]interface{}); ok {
				step.Parameters = parameters
			}
			if condition, ok := stepMap["condition"].(string); ok {
				step.Condition = condition
			}
			if continueOnError, ok := stepMap["continueOnError"].(bool); ok {
				step.ContinueOnError = continueOnError
			}
			if dependsOnInterface, ok := stepMap["dependsOn"].([]interface{}); ok {
				dependsOn := make([]string, len(dependsOnInterface))
				for i, dep := range dependsOnInterface {
					if depStr, ok := dep.(string); ok {
						dependsOn[i] = depStr
					}
				}
				step.DependsOn = dependsOn
			}
			if timeout, ok := stepMap["timeout"].(string); ok {
				step.Timeout = timeout
			}
			
			newWorkflow.Steps = append(newWorkflow.Steps, step)
		}
	}
	
	// Handle retry policy
	if retryPolicyMap, ok := workflowDef["retryPolicy"].(map[string]interface{}); ok {
		retryPolicy := &crd.WorkflowRetryPolicy{}
		if maxRetries, ok := retryPolicyMap["maxRetries"].(float64); ok {
			maxRetriesInt := int32(maxRetries)
			retryPolicy.MaxRetries = maxRetriesInt
		}
		if retryDelay, ok := retryPolicyMap["retryDelay"].(string); ok {
			retryPolicy.RetryDelay = retryDelay
		}
		if backoffStrategy, ok := retryPolicyMap["backoffStrategy"].(string); ok {
			retryPolicy.BackoffStrategy = crd.WorkflowBackoffStrategy(backoffStrategy)
		}
		if maxRetryDelay, ok := retryPolicyMap["maxRetryDelay"].(string); ok {
			retryPolicy.MaxRetryDelay = maxRetryDelay
		}
		if backoffMultiplier, ok := retryPolicyMap["backoffMultiplier"].(float64); ok {
			retryPolicy.BackoffMultiplier = backoffMultiplier
		}
		newWorkflow.RetryPolicy = retryPolicy
	}
	
	// Add the new workflow to the existing list
	taskScheduler.Spec.Workflows = append(taskScheduler.Spec.Workflows, newWorkflow)
	
	// Update the MCPTaskScheduler
	err = m.k8sClient.Update(ctx, &taskScheduler)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error updating task scheduler with new workflow: %v", err)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf("Successfully created workflow '%s' in task scheduler. Total workflows: %d", workflowName, len(taskScheduler.Spec.Workflows))}},
	}, nil
}

// createWorkflowWithBinary falls back to binary execution
func (m *MateyMCPServer) createWorkflowWithBinary(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow creation via binary not yet implemented. Please ensure MCPTaskScheduler is deployed and try again."}},
		IsError: true,
	}, fmt.Errorf("workflow creation via binary not implemented")
}

// listWorkflows lists all workflows in the task scheduler
func (m *MateyMCPServer) listWorkflows(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.useK8sClient() {
		var taskSchedulers crd.MCPTaskSchedulerList
		namespace := m.namespace
		if allNamespaces, ok := args["all_namespaces"].(bool); ok && allNamespaces {
			namespace = ""
		}
		
		listOpts := &client.ListOptions{}
		if namespace != "" {
			listOpts.Namespace = namespace
		}
		
		err := m.k8sClient.List(ctx, &taskSchedulers, listOpts)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error listing task schedulers: %v", err)}},
				IsError: true,
			}, err
		}
		
		// Extract workflows from all task schedulers
		var allWorkflows []map[string]interface{}
		for _, ts := range taskSchedulers.Items {
			for _, workflow := range ts.Spec.Workflows {
				workflowInfo := map[string]interface{}{
					"name":        workflow.Name,
					"namespace":   ts.Namespace,
					"enabled":     workflow.Enabled,
					"schedule":    workflow.Schedule,
					"timezone":    workflow.Timezone,
					"description": workflow.Description,
					"stepCount":   len(workflow.Steps),
				}
				
				// Filter by enabled_only if specified
				if enabledOnly, ok := args["enabled_only"].(bool); ok && enabledOnly && !workflow.Enabled {
					continue
				}
				
				// Filter by tags if specified
				if filterTags, ok := args["tags"].([]interface{}); ok && len(filterTags) > 0 {
					hasTag := false
					for _, filterTag := range filterTags {
						if filterTagStr, ok := filterTag.(string); ok {
							for _, workflowTag := range workflow.Tags {
								if workflowTag == filterTagStr {
									hasTag = true
									break
								}
							}
							if hasTag {
								break
							}
						}
					}
					if !hasTag {
						continue
					}
				}
				
				allWorkflows = append(allWorkflows, workflowInfo)
			}
		}
		
		// Format output
		outputFormat := "table"
		if format, ok := args["output_format"].(string); ok && format != "" {
			outputFormat = format
		}
		
		return m.formatWorkflowsList(allWorkflows, outputFormat)
	}
	
	return m.listWorkflowsWithBinary(ctx, args)
}

// formatWorkflowsList formats workflow list output
func (m *MateyMCPServer) formatWorkflowsList(workflows []map[string]interface{}, outputFormat string) (*ToolResult, error) {
	switch outputFormat {
	case "json":
		jsonBytes, err := json.MarshalIndent(workflows, "", "  ")
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error formatting workflows as JSON: %v", err)}},
				IsError: true,
			}, err
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: string(jsonBytes)}},
		}, nil
	case "yaml":
		var yamlOutput strings.Builder
		for _, workflow := range workflows {
			yamlOutput.WriteString(fmt.Sprintf("- name: %v\n", workflow["name"]))
			yamlOutput.WriteString(fmt.Sprintf("  namespace: %v\n", workflow["namespace"]))
			yamlOutput.WriteString(fmt.Sprintf("  enabled: %v\n", workflow["enabled"]))
			if schedule := workflow["schedule"]; schedule != nil && schedule != "" {
				yamlOutput.WriteString(fmt.Sprintf("  schedule: %v\n", schedule))
			}
			if description := workflow["description"]; description != nil && description != "" {
				yamlOutput.WriteString(fmt.Sprintf("  description: %v\n", description))
			}
			yamlOutput.WriteString(fmt.Sprintf("  steps: %v\n", workflow["stepCount"]))
			yamlOutput.WriteString("\n")
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: yamlOutput.String()}},
		}, nil
	default: // table format
		var output strings.Builder
		output.WriteString("NAME                 NAMESPACE       ENABLED    SCHEDULE        STEPS    DESCRIPTION\n")
		output.WriteString("----                 ---------       -------    --------        -----    -----------\n")
		
		for _, workflow := range workflows {
			name := fmt.Sprintf("%v", workflow["name"])
			if len(name) > 20 {
				name = name[:17] + "..."
			}
			
			namespace := fmt.Sprintf("%v", workflow["namespace"])
			if len(namespace) > 15 {
				namespace = namespace[:12] + "..."
			}
			
			enabled := fmt.Sprintf("%v", workflow["enabled"])
			schedule := fmt.Sprintf("%v", workflow["schedule"])
			if schedule == "" || schedule == "<nil>" {
				schedule = "Manual"
			}
			if len(schedule) > 15 {
				schedule = schedule[:12] + "..."
			}
			
			stepCount := fmt.Sprintf("%v", workflow["stepCount"])
			
			description := fmt.Sprintf("%v", workflow["description"])
			if len(description) > 20 {
				description = description[:17] + "..."
			}
			if description == "<nil>" {
				description = "-"
			}
			
			output.WriteString(fmt.Sprintf("%-20s %-15s %-10s %-15s %-8s %-20s\n",
				name, namespace, enabled, schedule, stepCount, description))
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: output.String()}},
		}, nil
	}
}

// listWorkflowsWithBinary falls back to binary execution
func (m *MateyMCPServer) listWorkflowsWithBinary(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow listing via binary not yet implemented. Please ensure MCPTaskScheduler is deployed and try using the k8s client."}},
		IsError: true,
	}, fmt.Errorf("workflow listing via binary not implemented")
}

// getWorkflow gets details of a specific workflow
func (m *MateyMCPServer) getWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	if m.useK8sClient() {
		var taskSchedulers crd.MCPTaskSchedulerList
		listOpts := &client.ListOptions{}
		if m.namespace != "" {
			listOpts.Namespace = m.namespace
		}
		
		err := m.k8sClient.List(ctx, &taskSchedulers, listOpts)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error listing task schedulers: %v", err)}},
				IsError: true,
			}, err
		}
		
		// Find the workflow in any task scheduler
		for _, ts := range taskSchedulers.Items {
			for _, workflow := range ts.Spec.Workflows {
				if workflow.Name == name {
					outputFormat := "table"
					if format, ok := args["output_format"].(string); ok && format != "" {
						outputFormat = format
					}
					
					return m.formatSingleWorkflowFromTaskScheduler(workflow, ts.Namespace, outputFormat)
				}
			}
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Workflow '%s' not found", name)}},
			IsError: true,
		}, fmt.Errorf("workflow %s not found", name)
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow details via binary not yet implemented."}},
		IsError: true,
	}, fmt.Errorf("workflow details via binary not implemented")
}

// deleteWorkflow deletes a workflow from the task scheduler
func (m *MateyMCPServer) deleteWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	if m.useK8sClient() {
		var taskScheduler crd.MCPTaskScheduler
		taskSchedulerName := "task-scheduler"
		err := m.k8sClient.Get(ctx, client.ObjectKey{
			Name:      taskSchedulerName,
			Namespace: m.namespace,
		}, &taskScheduler)
		
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting task scheduler '%s': %v", taskSchedulerName, err)}},
				IsError: true,
			}, err
		}
		
		// Find and remove the workflow
		workflowFound := false
		var updatedWorkflows []crd.WorkflowDefinition
		for _, workflow := range taskScheduler.Spec.Workflows {
			if workflow.Name != name {
				updatedWorkflows = append(updatedWorkflows, workflow)
			} else {
				workflowFound = true
			}
		}
		
		if !workflowFound {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Workflow '%s' not found", name)}},
				IsError: true,
			}, fmt.Errorf("workflow '%s' not found", name)
		}
		
		taskScheduler.Spec.Workflows = updatedWorkflows
		err = m.k8sClient.Update(ctx, &taskScheduler)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error removing workflow from task scheduler: %v", err)}},
				IsError: true,
			}, err
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Successfully deleted workflow '%s' from task scheduler. Remaining workflows: %d", name, len(updatedWorkflows))}},
		}, nil
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow deletion via binary not yet implemented."}},
		IsError: true,
	}, fmt.Errorf("workflow deletion via binary not implemented")
}

// executeWorkflow manually executes a workflow
func (m *MateyMCPServer) executeWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	// Try to execute via direct MCP call to task scheduler service
	if m.useK8sClient() {
		return m.executeWorkflowViaMCP(ctx, name, args)
	}
	
	// Fall back to binary execution
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow execution via binary not yet implemented. Please ensure MCPTaskScheduler is deployed and try again."}},
		IsError: true,
	}, fmt.Errorf("workflow execution via binary not implemented")
}

// executeWorkflowViaMCP executes a workflow by triggering it through the MCPTaskScheduler CRD
func (m *MateyMCPServer) executeWorkflowViaMCP(ctx context.Context, workflowName string, args map[string]interface{}) (*ToolResult, error) {
	// Get the current MCPTaskScheduler
	var taskScheduler crd.MCPTaskScheduler
	taskSchedulerName := "task-scheduler"
	
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      taskSchedulerName,
		Namespace: m.namespace,
	}, &taskScheduler)
	
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting task scheduler '%s': %v", taskSchedulerName, err)}},
			IsError: true,
		}, err
	}
	
	// Check if workflow exists and is enabled for manual execution
	var targetWorkflow *crd.WorkflowDefinition
	for i, workflow := range taskScheduler.Spec.Workflows {
		if workflow.Name == workflowName {
			targetWorkflow = &taskScheduler.Spec.Workflows[i]
			break
		}
	}
	
	if targetWorkflow == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Workflow '%s' not found in task scheduler", workflowName)}},
			IsError: true,
		}, fmt.Errorf("workflow '%s' not found", workflowName)
	}
	
	// Check if manual execution is allowed
	// For now, allow manual execution by default (we can make this configurable later)
	// In the future, we might want to check if ManualExecution was explicitly set to false
	manualExecutionAllowed := true // Default to allowing manual execution
	
	if !manualExecutionAllowed {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Manual execution is disabled for workflow '%s'", workflowName)}},
			IsError: true,
		}, fmt.Errorf("manual execution disabled for workflow '%s'", workflowName)
	}
	
	// Generate unique execution ID for manual execution with microsecond precision and random component
	// This ensures no collision with scheduled executions or other manual executions
	now := time.Now()
	executionID := fmt.Sprintf("%s-manual-%d-%06d", workflowName, now.Unix(), now.Nanosecond()/1000)
	
	// Add annotation to trigger manual execution
	if taskScheduler.Annotations == nil {
		taskScheduler.Annotations = make(map[string]string)
	}
	
	// Use a special annotation to signal manual execution request
	annotationKey := fmt.Sprintf("mcp.matey.ai/manual-execution-%s", workflowName)
	taskScheduler.Annotations[annotationKey] = executionID
	
	// Also add a general manual execution timestamp for the controller
	taskScheduler.Annotations["mcp.matey.ai/manual-execution-requested"] = time.Now().Format(time.RFC3339)
	
	// Update the MCPTaskScheduler with the annotation
	err = m.k8sClient.Update(ctx, &taskScheduler)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error triggering manual execution: %v", err)}},
			IsError: true,
		}, err
	}
	
	// Return success message with execution details
	resultText := fmt.Sprintf("Manual execution triggered for workflow '%s'\n", workflowName)
	resultText += fmt.Sprintf("🆔 Execution ID: %s\n", executionID)
	resultText += fmt.Sprintf("⏰ Requested at: %s\n", time.Now().Format("2006-01-02 15:04:05 MST"))
	resultText += fmt.Sprintf("🔄 Status: Queued for execution by task-scheduler controller\n")
	resultText += fmt.Sprintf("\n💡 Use 'workflow_logs' with name '%s' to monitor execution progress", workflowName)
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: resultText}},
	}, nil
}

// pauseWorkflow pauses a workflow
func (m *MateyMCPServer) pauseWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	return m.updateWorkflowEnabledStatus(ctx, name, false)
}

// resumeWorkflow resumes a paused workflow
func (m *MateyMCPServer) resumeWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	return m.updateWorkflowEnabledStatus(ctx, name, true)
}

// updateWorkflowEnabledStatus updates the enabled status of a workflow
func (m *MateyMCPServer) updateWorkflowEnabledStatus(ctx context.Context, name string, enabled bool) (*ToolResult, error) {
	if m.useK8sClient() {
		var taskScheduler crd.MCPTaskScheduler
		taskSchedulerName := "task-scheduler"
		err := m.k8sClient.Get(ctx, client.ObjectKey{
			Name:      taskSchedulerName,
			Namespace: m.namespace,
		}, &taskScheduler)
		
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting task scheduler '%s': %v", taskSchedulerName, err)}},
				IsError: true,
			}, err
		}
		
		// Find and update the workflow
		workflowFound := false
		for i, workflow := range taskScheduler.Spec.Workflows {
			if workflow.Name == name {
				taskScheduler.Spec.Workflows[i].Enabled = enabled
				workflowFound = true
				break
			}
		}
		
		if !workflowFound {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Workflow '%s' not found", name)}},
				IsError: true,
			}, fmt.Errorf("workflow '%s' not found", name)
		}
		
		err = m.k8sClient.Update(ctx, &taskScheduler)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error updating workflow enabled status: %v", err)}},
				IsError: true,
			}, err
		}
		
		status := "paused"
		if enabled {
			status = "resumed"
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Successfully %s workflow '%s'", status, name)}},
		}, nil
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow status update via binary not yet implemented."}},
		IsError: true,
	}, fmt.Errorf("workflow status update via binary not implemented")
}

// workflowLogs gets workflow execution logs
func (m *MateyMCPServer) workflowLogs(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	executionID, _ := args["execution_id"].(string)
	step, _ := args["step"].(string)
	tailLines, _ := args["tail"].(float64)
	if tailLines == 0 {
		tailLines = 10
	}
	
	if !m.useK8sClient() {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot retrieve workflow execution history."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	return m.getWorkflowExecutionHistory(ctx, name, executionID, step, int(tailLines))
}

// workflowTemplates lists available workflow templates
func (m *MateyMCPServer) workflowTemplates(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.useK8sClient() {
		var taskSchedulers crd.MCPTaskSchedulerList
		listOpts := &client.ListOptions{}
		if m.namespace != "" {
			listOpts.Namespace = m.namespace
		}
		
		err := m.k8sClient.List(ctx, &taskSchedulers, listOpts)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error listing task schedulers: %v", err)}},
				IsError: true,
			}, err
		}
		
		// Extract templates from all task schedulers
		var allTemplates []map[string]interface{}
		category := ""
		if cat, ok := args["category"].(string); ok {
			category = cat
		}
		
		for _, ts := range taskSchedulers.Items {
			for _, template := range ts.Spec.Templates {
				if category != "" && template.Category != category {
					continue
				}
				
				templateInfo := map[string]interface{}{
					"name":        template.Name,
					"category":    template.Category,
					"description": template.Description,
					"version":     template.Version,
					"parameters":  len(template.Parameters),
					"steps":       len(template.Workflow.Steps),
				}
				allTemplates = append(allTemplates, templateInfo)
			}
		}
		
		outputFormat := "table"
		if format, ok := args["output_format"].(string); ok && format != "" {
			outputFormat = format
		}
		
		return m.formatTemplatesList(allTemplates, outputFormat)
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow templates listing not yet implemented via binary."}},
		IsError: true,
	}, fmt.Errorf("workflow templates not implemented")
}

// formatSingleWorkflowFromTaskScheduler formats a single workflow from MCPTaskScheduler
func (m *MateyMCPServer) formatSingleWorkflowFromTaskScheduler(workflow crd.WorkflowDefinition, namespace, outputFormat string) (*ToolResult, error) {
	switch outputFormat {
	case "json":
		jsonBytes, err := json.MarshalIndent(workflow, "", "  ")
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error formatting workflow as JSON: %v", err)}},
				IsError: true,
			}, err
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: string(jsonBytes)}},
		}, nil
	case "yaml":
		var yamlOutput strings.Builder
		yamlOutput.WriteString(fmt.Sprintf("name: %s\n", workflow.Name))
		yamlOutput.WriteString(fmt.Sprintf("namespace: %s\n", namespace))
		yamlOutput.WriteString(fmt.Sprintf("enabled: %v\n", workflow.Enabled))
		if workflow.Schedule != "" {
			yamlOutput.WriteString(fmt.Sprintf("schedule: %s\n", workflow.Schedule))
		}
		if workflow.Timezone != "" {
			yamlOutput.WriteString(fmt.Sprintf("timezone: %s\n", workflow.Timezone))
		}
		if workflow.Description != "" {
			yamlOutput.WriteString(fmt.Sprintf("description: %s\n", workflow.Description))
		}
		yamlOutput.WriteString(fmt.Sprintf("steps: %d\n", len(workflow.Steps)))
		return &ToolResult{
			Content: []Content{{Type: "text", Text: yamlOutput.String()}},
		}, nil
	default: // table format
		var output strings.Builder
		output.WriteString("=== Workflow Details ===\n")
		output.WriteString(fmt.Sprintf("Name: %s\n", workflow.Name))
		output.WriteString(fmt.Sprintf("Namespace: %s\n", namespace))
		output.WriteString(fmt.Sprintf("Enabled: %v\n", workflow.Enabled))
		if workflow.Schedule != "" {
			output.WriteString(fmt.Sprintf("Schedule: %s\n", workflow.Schedule))
		}
		if workflow.Timezone != "" {
			output.WriteString(fmt.Sprintf("Timezone: %s\n", workflow.Timezone))
		}
		if workflow.Description != "" {
			output.WriteString(fmt.Sprintf("Description: %s\n", workflow.Description))
		}
		if workflow.ConcurrencyPolicy != "" {
			output.WriteString(fmt.Sprintf("Concurrency Policy: %s\n", workflow.ConcurrencyPolicy))
		}
		if workflow.Timeout != "" {
			output.WriteString(fmt.Sprintf("Timeout: %s\n", workflow.Timeout))
		}
		
		if len(workflow.Steps) > 0 {
			output.WriteString("\n=== Steps ===\n")
			for i, step := range workflow.Steps {
				output.WriteString(fmt.Sprintf("%d. %s (tool: %s)\n", i+1, step.Name, step.Tool))
				if len(step.DependsOn) > 0 {
					output.WriteString(fmt.Sprintf("   Depends On: %v\n", step.DependsOn))
				}
				if step.Condition != "" {
					output.WriteString(fmt.Sprintf("   Condition: %s\n", step.Condition))
				}
				if step.Timeout != "" {
					output.WriteString(fmt.Sprintf("   Timeout: %s\n", step.Timeout))
				}
			}
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: output.String()}},
		}, nil
	}
}

// getWorkflowExecutionHistory retrieves comprehensive workflow execution history from Kubernetes
func (m *MateyMCPServer) getWorkflowExecutionHistory(ctx context.Context, workflowName, executionID, step string, tailLines int) (*ToolResult, error) {
	// Get the MCPTaskScheduler to retrieve execution history
	var taskScheduler crd.MCPTaskScheduler
	taskSchedulerName := "task-scheduler"
	
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      taskSchedulerName,
		Namespace: m.namespace,
	}, &taskScheduler)
	
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting task scheduler '%s': %v", taskSchedulerName, err)}},
			IsError: true,
		}, err
	}
	
	// Filter executions by workflow name
	var matchingExecutions []crd.WorkflowExecution
	for _, execution := range taskScheduler.Status.WorkflowExecutions {
		if execution.WorkflowName == workflowName {
			matchingExecutions = append(matchingExecutions, execution)
		}
	}
	
	if len(matchingExecutions) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("No execution history found for workflow '%s'", workflowName)}},
		}, nil
	}
	
	// If specific execution ID requested, show detailed view
	if executionID != "" {
		for _, execution := range matchingExecutions {
			if execution.ID == executionID {
				return m.formatDetailedExecution(execution, step)
			}
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Execution ID '%s' not found for workflow '%s'", executionID, workflowName)}},
			IsError: true,
		}, fmt.Errorf("execution not found")
	}
	
	// Sort executions by start time (most recent first) and apply tail limit
	executions := make([]crd.WorkflowExecution, len(matchingExecutions))
	copy(executions, matchingExecutions)
	
	// Sort by start time (most recent first)
	for i := 0; i < len(executions)-1; i++ {
		for j := i + 1; j < len(executions); j++ {
			if executions[j].StartTime.After(executions[i].StartTime) {
				executions[i], executions[j] = executions[j], executions[i]
			}
		}
	}
	
	// Apply tail limit
	if tailLines > 0 && len(executions) > tailLines {
		executions = executions[:tailLines]
	}
	
	return m.formatExecutionHistory(executions, workflowName, &taskScheduler.Status)
}

// formatDetailedExecution formats detailed information for a single execution
func (m *MateyMCPServer) formatDetailedExecution(execution crd.WorkflowExecution, specificStep string) (*ToolResult, error) {
	var result strings.Builder
	
	result.WriteString(fmt.Sprintf("📊 **Workflow Execution Details**\n\n"))
	result.WriteString(fmt.Sprintf("**Workflow**: %s\n", execution.WorkflowName))
	result.WriteString(fmt.Sprintf("**Execution ID**: %s\n", execution.ID))
	result.WriteString(fmt.Sprintf("**Status**: %s %s\n", m.getStatusIcon(string(execution.Phase)), execution.Phase))
	result.WriteString(fmt.Sprintf("**Started**: %s\n", execution.StartTime.Format("2006-01-02 15:04:05 MST")))
	
	if execution.EndTime != nil {
		result.WriteString(fmt.Sprintf("**Completed**: %s\n", execution.EndTime.Format("2006-01-02 15:04:05 MST")))
		if execution.Duration != nil {
			result.WriteString(fmt.Sprintf("**Duration**: %v\n", *execution.Duration))
		}
	} else if execution.Phase == "Running" {
		duration := time.Since(execution.StartTime)
		result.WriteString(fmt.Sprintf("**Running for**: %v\n", duration.Truncate(time.Second)))
	}
	
	if execution.Message != "" {
		result.WriteString(fmt.Sprintf("**Message**: %s\n", execution.Message))
	}
	
	result.WriteString("\n**📝 Step Results:**\n")
	if len(execution.StepResults) == 0 {
		result.WriteString("  No step results available\n")
	} else {
		stepCount := 0
		for stepName, stepResult := range execution.StepResults {
			// If specific step requested, only show that step
			if specificStep != "" && stepName != specificStep {
				continue
			}
			
			stepCount++
			result.WriteString(fmt.Sprintf("\n  **%d. Step: %s** %s\n", stepCount, stepName, m.getStatusIcon(string(stepResult.Phase))))
			result.WriteString(fmt.Sprintf("     Status: %s\n", stepResult.Phase))
			result.WriteString(fmt.Sprintf("     Duration: %v\n", stepResult.Duration.Truncate(time.Millisecond)))
			result.WriteString(fmt.Sprintf("     Attempts: %d\n", stepResult.Attempts))
			
			if stepResult.Output != nil {
				outputText := m.formatStepOutput(stepResult.Output)
				if len(outputText) > 200 {
					outputText = outputText[:200] + "... (truncated)"
				}
				result.WriteString(fmt.Sprintf("     Output: %s\n", outputText))
			}
			
			if stepResult.Error != "" {
				result.WriteString(fmt.Sprintf("     Error: %s\n", stepResult.Error))
			}
		}
		
		if specificStep != "" && stepCount == 0 {
			result.WriteString(fmt.Sprintf("  ERROR: Step '%s' not found in this execution\n", specificStep))
		}
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// formatExecutionHistory formats summary information for multiple executions with statistics
func (m *MateyMCPServer) formatExecutionHistory(executions []crd.WorkflowExecution, workflowName string, status *crd.MCPTaskSchedulerStatus) (*ToolResult, error) {
	var result strings.Builder
	
	result.WriteString(fmt.Sprintf("📊 **Workflow Execution History: %s**\n\n", workflowName))
	
	// Calculate workflow-specific statistics
	workflowStats := m.calculateWorkflowStats(executions)
	
	result.WriteString("**📈 Workflow Statistics:**\n")
	result.WriteString(fmt.Sprintf("  Total Executions: %d\n", workflowStats.Total))
	result.WriteString(fmt.Sprintf("  Succeeded: %d\n", workflowStats.Succeeded))
	result.WriteString(fmt.Sprintf("  Failed: %d\n", workflowStats.Failed))
	result.WriteString(fmt.Sprintf("  🔄 Running: %d\n", workflowStats.Running))
	result.WriteString(fmt.Sprintf("  ⏳ Pending: %d\n", workflowStats.Pending))
	
	if workflowStats.AvgDuration > 0 {
		result.WriteString(fmt.Sprintf("  Average Duration: %v\n", workflowStats.AvgDuration.Truncate(time.Second)))
	}
	if workflowStats.LastExecution != nil {
		result.WriteString(fmt.Sprintf("  Last Execution: %s\n", workflowStats.LastExecution.Format("2006-01-02 15:04:05")))
	}
	
	result.WriteString("\n**📋 Recent Executions:**\n")
	
	if len(executions) == 0 {
		result.WriteString("  No recent executions found\n")
	} else {
		for i, execution := range executions {
			statusIcon := m.getStatusIcon(string(execution.Phase))
			result.WriteString(fmt.Sprintf("\n  **%d. %s %s** (`%s`)\n", i+1, statusIcon, execution.Phase, execution.ID))
			
			result.WriteString(fmt.Sprintf("     📅 Started: %s", execution.StartTime.Format("2006-01-02 15:04:05")))
			
			if execution.EndTime != nil {
				result.WriteString(fmt.Sprintf(" → %s", execution.EndTime.Format("15:04:05")))
				if execution.Duration != nil {
					result.WriteString(fmt.Sprintf(" ⏱️ %v", execution.Duration.Truncate(time.Second)))
				}
			} else if execution.Phase == "Running" {
				runningTime := time.Since(execution.StartTime)
				result.WriteString(fmt.Sprintf(" 🔄 Running for %v", runningTime.Truncate(time.Second)))
			}
			result.WriteString("\n")
			
			if execution.Message != "" {
				result.WriteString(fmt.Sprintf("     💬 %s\n", execution.Message))
			}
			
			// Step summary
			if len(execution.StepResults) > 0 {
				stepSummary := m.getStepSummary(execution.StepResults)
				result.WriteString(fmt.Sprintf("     📝 Steps: %s\n", stepSummary))
			}
		}
	}
	
	result.WriteString("\n**💡 Usage Tips:**\n")
	result.WriteString("  • Use `workflow_logs` with `execution_id` to get detailed step information\n")
	result.WriteString("  • Add `step` parameter to focus on a specific step\n")
	result.WriteString("  • Use `tail` parameter to limit number of executions shown\n")
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// calculateWorkflowStats calculates statistics for a specific workflow from its executions
func (m *MateyMCPServer) calculateWorkflowStats(executions []crd.WorkflowExecution) WorkflowStats {
	stats := WorkflowStats{
		Total: len(executions),
	}
	
	var totalDuration time.Duration
	completedCount := 0
	
	for _, exec := range executions {
		switch exec.Phase {
		case "Succeeded":
			stats.Succeeded++
		case "Failed":
			stats.Failed++
		case "Running":
			stats.Running++
		case "Pending":
			stats.Pending++
		}
		
		// Track duration for completed workflows
		if exec.Duration != nil {
			totalDuration += *exec.Duration
			completedCount++
		}
		
		// Track most recent execution
		if stats.LastExecution == nil || exec.StartTime.After(*stats.LastExecution) {
			stats.LastExecution = &exec.StartTime
		}
	}
	
	// Calculate average duration
	if completedCount > 0 {
		stats.AvgDuration = totalDuration / time.Duration(completedCount)
	}
	
	return stats
}

// getStatusIcon returns an emoji icon for the workflow/step phase
func (m *MateyMCPServer) getStatusIcon(phase string) string {
	switch phase {
	case "Succeeded":
		return "SUCCESS"
	case "Failed":
		return "FAILED"
	case "Running":
		return "🔄"
	case "Pending":
		return "⏳"
	case "Cancelled":
		return "⏹️"
	case "Skipped":
		return "⏭️"
	case "Retrying":
		return "🔄"
	default:
		return "📋"
	}
}

// getStepSummary returns a summary of step results with counts and icons
func (m *MateyMCPServer) getStepSummary(stepResults map[string]crd.StepResult) string {
	if len(stepResults) == 0 {
		return "No steps"
	}
	
	counts := make(map[crd.StepPhase]int)
	for _, result := range stepResults {
		counts[result.Phase]++
	}
	
	var parts []string
	if count := counts["Succeeded"]; count > 0 {
		parts = append(parts, fmt.Sprintf("SUCCESS:%d", count))
	}
	if count := counts["Failed"]; count > 0 {
		parts = append(parts, fmt.Sprintf("FAILED:%d", count))
	}
	if count := counts["Running"]; count > 0 {
		parts = append(parts, fmt.Sprintf("🔄%d", count))
	}
	if count := counts["Retrying"]; count > 0 {
		parts = append(parts, fmt.Sprintf("🔄%d", count))
	}
	if count := counts["Pending"]; count > 0 {
		parts = append(parts, fmt.Sprintf("⏳%d", count))
	}
	if count := counts["Skipped"]; count > 0 {
		parts = append(parts, fmt.Sprintf("⏭️%d", count))
	}
	
	if len(parts) == 0 {
		return fmt.Sprintf("%d steps", len(stepResults))
	}
	
	return strings.Join(parts, " ")
}

// formatStepOutput formats step output for display
func (m *MateyMCPServer) formatStepOutput(output interface{}) string {
	if output == nil {
		return "(no output)"
	}
	
	switch v := output.(type) {
	case string:
		return v
	case map[string]interface{}:
		if jsonBytes, err := json.MarshalIndent(v, "", "  "); err == nil {
			return string(jsonBytes)
		}
		return fmt.Sprintf("%+v", v)
	case []interface{}:
		if jsonBytes, err := json.MarshalIndent(v, "", "  "); err == nil {
			return string(jsonBytes)
		}
		return fmt.Sprintf("%+v", v)
	default:
		return fmt.Sprintf("%+v", v)
	}
}

// formatTemplatesList formats templates list output
func (m *MateyMCPServer) formatTemplatesList(templates []map[string]interface{}, outputFormat string) (*ToolResult, error) {
	switch outputFormat {
	case "json":
		jsonBytes, err := json.MarshalIndent(templates, "", "  ")
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error formatting templates as JSON: %v", err)}},
				IsError: true,
			}, err
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: string(jsonBytes)}},
		}, nil
	case "yaml":
		var yamlOutput strings.Builder
		for _, template := range templates {
			yamlOutput.WriteString(fmt.Sprintf("- name: %v\n", template["name"]))
			yamlOutput.WriteString(fmt.Sprintf("  category: %v\n", template["category"]))
			yamlOutput.WriteString(fmt.Sprintf("  description: %v\n", template["description"]))
			yamlOutput.WriteString(fmt.Sprintf("  version: %v\n", template["version"]))
			yamlOutput.WriteString(fmt.Sprintf("  parameters: %v\n", template["parameters"]))
			yamlOutput.WriteString(fmt.Sprintf("  steps: %v\n", template["steps"]))
			yamlOutput.WriteString("\n")
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: yamlOutput.String()}},
		}, nil
	default: // table format
		var output strings.Builder
		output.WriteString("NAME                 CATEGORY        VERSION    PARAMS   STEPS    DESCRIPTION\n")
		output.WriteString("----                 --------        -------    ------   -----    -----------\n")
		
		for _, template := range templates {
			name := fmt.Sprintf("%v", template["name"])
			if len(name) > 20 {
				name = name[:17] + "..."
			}
			
			category := fmt.Sprintf("%v", template["category"])
			if len(category) > 15 {
				category = category[:12] + "..."
			}
			
			version := fmt.Sprintf("%v", template["version"])
			if len(version) > 10 {
				version = version[:7] + "..."
			}
			
			params := fmt.Sprintf("%v", template["parameters"])
			steps := fmt.Sprintf("%v", template["steps"])
			
			description := fmt.Sprintf("%v", template["description"])
			if len(description) > 20 {
				description = description[:17] + "..."
			}
			
			output.WriteString(fmt.Sprintf("%-20s %-15s %-10s %-8s %-8s %-20s\n",
				name, category, version, params, steps, description))
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: output.String()}},
		}, nil
	}
}