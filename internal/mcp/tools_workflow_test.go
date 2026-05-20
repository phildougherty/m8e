package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/crd"
)

// newTestWorkflowTools returns a workflowTools with no Kubernetes client wired,
// i.e. useK8sClient() is false. This is the only configuration exercisable
// without a live cluster.
func newTestWorkflowTools() *workflowTools {
	return newWorkflowTools(clusterDeps{namespace: "matey"})
}

// TestWorkflowTools_NoK8sClient_HonestErrors verifies that every workflow
// lifecycle operation returns a clear IsError result (not a stub or a
// fabricated success) when no Kubernetes client is available. Workflows live
// inside the MCPTaskScheduler CRD, so a cluster connection is mandatory.
func TestWorkflowTools_NoK8sClient_HonestErrors(t *testing.T) {
	w := newTestWorkflowTools()
	ctx := context.Background()

	cases := []struct {
		name string
		call func() (*ToolResult, error)
	}{
		{"create_workflow", func() (*ToolResult, error) {
			return w.createWorkflow(ctx, map[string]interface{}{
				"name": "wf",
				"steps": []interface{}{
					map[string]interface{}{"name": "s1", "tool": "echo"},
				},
			})
		}},
		{"list_workflows", func() (*ToolResult, error) {
			return w.listWorkflows(ctx, map[string]interface{}{})
		}},
		{"delete_workflow", func() (*ToolResult, error) {
			return w.deleteWorkflow(ctx, map[string]interface{}{"name": "wf"})
		}},
		{"execute_workflow", func() (*ToolResult, error) {
			return w.executeWorkflow(ctx, map[string]interface{}{"name": "wf"})
		}},
		{"workflow_logs", func() (*ToolResult, error) {
			return w.workflowLogs(ctx, map[string]interface{}{"name": "wf"})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.call()
			if err == nil {
				t.Fatalf("%s: expected an error when no k8s client is available", tc.name)
			}
			if result == nil || !result.IsError {
				t.Fatalf("%s: expected IsError result, got %+v", tc.name, result)
			}
			text := result.Content[0].Text
			// The error must be honest about the cause, not a "not implemented" stub.
			if strings.Contains(strings.ToLower(text), "not implemented") {
				t.Errorf("%s: error text still reads as a stub: %q", tc.name, text)
			}
			if !strings.Contains(text, "Kubernetes client") {
				t.Errorf("%s: error text should explain the missing k8s client: %q", tc.name, text)
			}
		})
	}
}

// TestWorkflowTools_ArgValidation ensures required-argument validation fires
// before any cluster access is attempted.
func TestWorkflowTools_ArgValidation(t *testing.T) {
	w := newTestWorkflowTools()
	ctx := context.Background()

	if _, err := w.createWorkflow(ctx, map[string]interface{}{}); err == nil {
		t.Error("createWorkflow: expected error for missing name")
	}
	if _, err := w.createWorkflow(ctx, map[string]interface{}{"name": "wf"}); err == nil {
		t.Error("createWorkflow: expected error for missing steps")
	}
	if _, err := w.deleteWorkflow(ctx, map[string]interface{}{}); err == nil {
		t.Error("deleteWorkflow: expected error for missing name")
	}
	if _, err := w.executeWorkflow(ctx, map[string]interface{}{}); err == nil {
		t.Error("executeWorkflow: expected error for missing name")
	}
	if _, err := w.workflowLogs(ctx, map[string]interface{}{}); err == nil {
		t.Error("workflowLogs: expected error for missing name")
	}
}

func TestWorkflowTools_CalculateWorkflowStats(t *testing.T) {
	w := newTestWorkflowTools()
	now := time.Now()
	d1 := 10 * time.Second
	d2 := 20 * time.Second
	executions := []crd.WorkflowExecution{
		{WorkflowName: "wf", Phase: crd.WorkflowPhaseSucceeded, StartTime: now.Add(-time.Hour), Duration: &d1},
		{WorkflowName: "wf", Phase: crd.WorkflowPhaseFailed, StartTime: now.Add(-30 * time.Minute), Duration: &d2},
		{WorkflowName: "wf", Phase: crd.WorkflowPhaseRunning, StartTime: now},
		{WorkflowName: "wf", Phase: crd.WorkflowPhasePending, StartTime: now.Add(-2 * time.Hour)},
	}

	stats := w.calculateWorkflowStats(executions)
	if stats.Total != 4 {
		t.Errorf("Total = %d, want 4", stats.Total)
	}
	if stats.Succeeded != 1 || stats.Failed != 1 || stats.Running != 1 || stats.Pending != 1 {
		t.Errorf("phase counts wrong: %+v", stats)
	}
	if stats.AvgDuration != (d1+d2)/2 {
		t.Errorf("AvgDuration = %v, want %v", stats.AvgDuration, (d1+d2)/2)
	}
	if stats.LastExecution == nil || !stats.LastExecution.Equal(now) {
		t.Errorf("LastExecution = %v, want %v", stats.LastExecution, now)
	}
}

func TestWorkflowTools_FormatWorkflowsList(t *testing.T) {
	w := newTestWorkflowTools()
	workflows := []map[string]interface{}{
		{
			"name": "daily-report", "namespace": "matey", "enabled": true,
			"schedule": "0 9 * * *", "description": "send report", "stepCount": 3,
		},
	}

	jsonResult, err := w.formatWorkflowsList(workflows, "json")
	if err != nil || jsonResult.IsError {
		t.Fatalf("json format failed: %v / %+v", err, jsonResult)
	}
	if !strings.Contains(jsonResult.Content[0].Text, "daily-report") {
		t.Errorf("json output missing workflow name: %q", jsonResult.Content[0].Text)
	}

	tableResult, err := w.formatWorkflowsList(workflows, "table")
	if err != nil || tableResult.IsError {
		t.Fatalf("table format failed: %v / %+v", err, tableResult)
	}
	if !strings.Contains(tableResult.Content[0].Text, "daily-report") {
		t.Errorf("table output missing workflow name: %q", tableResult.Content[0].Text)
	}
}

func TestWorkflowTools_GetStepSummary(t *testing.T) {
	w := newTestWorkflowTools()
	if got := w.getStepSummary(nil); got != "No steps" {
		t.Errorf("empty getStepSummary = %q, want %q", got, "No steps")
	}
	summary := w.getStepSummary(map[string]crd.StepResult{
		"a": {Phase: crd.StepPhaseSucceeded},
		"b": {Phase: crd.StepPhaseSucceeded},
		"c": {Phase: crd.StepPhaseFailed},
	})
	if !strings.Contains(summary, "SUCCESS:2") || !strings.Contains(summary, "FAILED:1") {
		t.Errorf("getStepSummary = %q, want SUCCESS:2 and FAILED:1", summary)
	}
}
