package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestWorkspaceTools_TodoLifecycle exercises the manage_todos surface end to
// end against the real in-process TodoList: create → list → update → stats →
// clear. Previously every method returned a hardcoded placeholder string and
// the agent's working-memory tool was unusable.
func TestWorkspaceTools_TodoLifecycle(t *testing.T) {
	ws := newWorkspaceTools(nil)
	ctx := context.Background()

	// create three todos with mixed priorities.
	createRes, err := ws.manageTodos(ctx, map[string]interface{}{
		"action": "create",
		"todos": []interface{}{
			map[string]interface{}{"content": "wire audit logger", "priority": "high"},
			map[string]interface{}{"content": "fix docs", "priority": "low"},
			map[string]interface{}{"content": "review chart", "priority": "high"},
		},
	})
	if err != nil || createRes.IsError {
		t.Fatalf("create: unexpected error: err=%v result=%+v", err, createRes)
	}

	// list returns the created items; not the placeholder string.
	listRes, err := ws.manageTodos(ctx, map[string]interface{}{"action": "list"})
	if err != nil || listRes.IsError {
		t.Fatalf("list: %v %+v", err, listRes)
	}
	got := listRes.Content[0].Text
	if strings.Contains(got, "requires persistent storage") {
		t.Fatalf("listTodos still returns the placeholder string: %q", got)
	}
	var listed struct {
		Items []TodoItem `json:"items"`
		Total int        `json:"total"`
	}
	if err := json.Unmarshal([]byte(got), &listed); err != nil {
		t.Fatalf("list result is not JSON: %v\n%s", err, got)
	}
	if listed.Total != 3 {
		t.Fatalf("expected 3 items, got %d", listed.Total)
	}

	// list with a priority filter narrows to the high-priority entries.
	highRes, _ := ws.manageTodos(ctx, map[string]interface{}{"action": "list", "priority": "high"})
	var high struct {
		Items []TodoItem `json:"items"`
		Total int        `json:"total"`
	}
	_ = json.Unmarshal([]byte(highRes.Content[0].Text), &high)
	if high.Total != 2 {
		t.Errorf("priority filter: expected 2 high-priority items, got %d", high.Total)
	}

	// update an item's status — pick the first listed.
	firstID := listed.Items[0].ID
	updateRes, err := ws.manageTodos(ctx, map[string]interface{}{
		"action": "update",
		"id":     firstID,
		"status": "in_progress",
	})
	if err != nil || updateRes.IsError {
		t.Fatalf("update: %v %+v", err, updateRes)
	}

	// stats reflects the transition.
	statsRes, _ := ws.manageTodos(ctx, map[string]interface{}{"action": "stats"})
	if statsRes.IsError {
		t.Fatalf("stats failed: %+v", statsRes)
	}
	var stats map[string]interface{}
	_ = json.Unmarshal([]byte(statsRes.Content[0].Text), &stats)
	if stats["in_progress"].(float64) != 1 {
		t.Errorf("expected 1 in_progress after update, got %v", stats["in_progress"])
	}
	if stats["pending"].(float64) != 2 {
		t.Errorf("expected 2 pending, got %v", stats["pending"])
	}

	// complete one and clear it.
	_, _ = ws.manageTodos(ctx, map[string]interface{}{
		"action": "update", "id": firstID, "status": "completed",
	})
	clearRes, _ := ws.manageTodos(ctx, map[string]interface{}{"action": "clear"})
	if clearRes.IsError {
		t.Fatalf("clear failed: %+v", clearRes)
	}
	if !strings.Contains(clearRes.Content[0].Text, "Cleared 1 completed") {
		t.Errorf("expected clear to report 1 removed, got %q", clearRes.Content[0].Text)
	}

	// after clear: 2 remain.
	postClear, _ := ws.manageTodos(ctx, map[string]interface{}{"action": "list"})
	var remaining struct {
		Total int `json:"total"`
	}
	_ = json.Unmarshal([]byte(postClear.Content[0].Text), &remaining)
	if remaining.Total != 2 {
		t.Errorf("expected 2 items remaining after clear, got %d", remaining.Total)
	}
}

// TestWorkspaceTools_TodoUpdateUnknownID returns an honest "not found" rather
// than the prior cheerful "Updated TODO item X" lie.
func TestWorkspaceTools_TodoUpdateUnknownID(t *testing.T) {
	ws := newWorkspaceTools(nil)
	res, err := ws.manageTodos(context.Background(), map[string]interface{}{
		"action": "update",
		"id":     "todo_does_not_exist",
		"status": "completed",
	})
	if err == nil {
		t.Fatal("expected error for unknown id")
	}
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
	if !strings.Contains(res.Content[0].Text, "not found") {
		t.Errorf("expected honest 'not found' message, got %q", res.Content[0].Text)
	}
}

// TestWorkspaceTools_TodoUpdateInvalidStatus rejects unknown statuses.
func TestWorkspaceTools_TodoUpdateInvalidStatus(t *testing.T) {
	ws := newWorkspaceTools(nil)
	_, _ = ws.manageTodos(context.Background(), map[string]interface{}{
		"action": "create",
		"todos":  []interface{}{map[string]interface{}{"content": "x"}},
	})
	res, err := ws.manageTodos(context.Background(), map[string]interface{}{
		"action": "update",
		"id":     "todo_1",
		"status": "almost-done",
	})
	if err == nil || !res.IsError {
		t.Fatalf("expected error for invalid status, got %v %+v", err, res)
	}
}

// TestNormalizeTodoStatus_AndPriority covers the input normalisation that
// makes the tool tolerant of common variants (Pascal-Case, dashes, synonyms).
func TestNormalizeTodoStatus_AndPriority(t *testing.T) {
	statusCases := map[string]TodoStatus{
		"":            "",
		"pending":     TodoStatusPending,
		"In-Progress": TodoStatusInProgress,
		"InProgress":  TodoStatusInProgress,
		"done":        TodoStatusCompleted,
		"completed":   TodoStatusCompleted,
		"canceled":    TodoStatusCancelled,
	}
	for input, want := range statusCases {
		if got := normalizeTodoStatus(input); got != want {
			t.Errorf("normalizeTodoStatus(%q) = %q, want %q", input, got, want)
		}
	}

	priorityCases := map[string]TodoPriority{
		"":         "",
		"low":      TodoPriorityLow,
		"MED":      TodoPriorityMedium,
		"high":     TodoPriorityHigh,
		"URGENT":   TodoPriorityUrgent,
		"critical": TodoPriorityUrgent,
	}
	for input, want := range priorityCases {
		if got := normalizeTodoPriority(input); got != want {
			t.Errorf("normalizeTodoPriority(%q) = %q, want %q", input, got, want)
		}
	}
}
