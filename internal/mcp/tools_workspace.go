package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// workspaceTools owns the workspace-facing tools: workspace_files (list/read/
// mount/unmount/stats against workflow PVCs), search_in_files (content grep),
// and manage_todos. The TODO operations are intentionally grouped here because
// they share the "agent scratch space" concern and have no k8s dependency.
//
// The TODO list is in-process state — agentic sessions use it as a working
// scratchpad within one run of the matey-mcp-server. It does not persist
// across pod restarts by design; an agent that needs durable TODOs should
// commit them to the memory service or a workflow artefact.
type workspaceTools struct {
	workspaceManager *WorkspaceManager

	todoMu sync.Mutex
	todos  *TodoList
}

func newWorkspaceTools(wm *WorkspaceManager) *workspaceTools {
	return &workspaceTools{
		workspaceManager: wm,
		todos:            &TodoList{},
	}
}

// manageTodos handles consolidated TODO management operations
func (ws *workspaceTools) manageTodos(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	action, _ := arguments["action"].(string)
	if action == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: action parameter is required"}},
			IsError: true,
		}, fmt.Errorf("action parameter is required")
	}

	switch action {
	case "create":
		return ws.createTodos(ctx, arguments)
	case "list":
		return ws.listTodos(ctx, arguments)
	case "update":
		return ws.updateTodoStatus(ctx, arguments)
	case "clear":
		return ws.clearCompletedTodos(ctx, arguments)
	case "stats":
		return ws.getTodoStats(ctx, arguments)
	default:
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Unknown action: %s", action)}},
			IsError: true,
		}, fmt.Errorf("unknown action: %s", action)
	}
}

// workspaceFiles handles consolidated workspace file operations
func (ws *workspaceTools) workspaceFiles(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	action, _ := arguments["action"].(string)
	if action == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: action parameter is required"}},
			IsError: true,
		}, fmt.Errorf("action parameter is required")
	}

	// Extract common parameters
	workflowName, _ := arguments["workflowName"].(string)
	executionID, _ := arguments["executionID"].(string)

	switch action {
	case "list":
		subPath, _ := arguments["subPath"].(string)
		return ws.listWorkspaceFiles(ctx, workflowName, executionID, subPath)
	case "read":
		filePath, _ := arguments["filePath"].(string)
		maxSize, _ := arguments["maxSize"].(int)
		if maxSize == 0 {
			maxSize = 1048576 // 1MB default
		}
		return ws.readWorkspaceFile(ctx, workflowName, executionID, filePath, maxSize)
	case "mount":
		return ws.mountWorkspace(ctx, workflowName, executionID)
	case "unmount":
		return ws.unmountWorkspace(ctx, workflowName, executionID)
	case "stats":
		return ws.getWorkspaceStats(ctx)
	default:
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Unknown action: %s", action)}},
			IsError: true,
		}, fmt.Errorf("unknown action: %s", action)
	}
}

func (ws *workspaceTools) createTodos(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	todosInterface, ok := arguments["todos"]
	if !ok {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: todos array is required"}},
			IsError: true,
		}, fmt.Errorf("todos array is required")
	}

	todosArray, ok := todosInterface.([]interface{})
	if !ok {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: todos must be an array"}},
			IsError: true,
		}, fmt.Errorf("todos must be an array")
	}

	ws.todoMu.Lock()
	defer ws.todoMu.Unlock()

	var createdItems []TodoItem
	for _, todoInterface := range todosArray {
		todoMap, ok := todoInterface.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := todoMap["content"].(string)
		if !ok || content == "" {
			continue
		}
		priority := TodoPriorityMedium
		if p, ok := todoMap["priority"].(string); ok {
			priority = normalizeTodoPriority(p)
		}

		id := ws.todos.AddItem(content, priority)
		// AddItem appends; the freshly-added item is the last one.
		createdItems = append(createdItems, ws.todos.Items[len(ws.todos.Items)-1])
		_ = id
	}

	payload, _ := json.MarshalIndent(map[string]interface{}{
		"created": createdItems,
		"total":   len(createdItems),
	}, "", "  ")

	return &ToolResult{
		Content: []Content{{Type: "text", Text: string(payload)}},
		IsError: false,
	}, nil
}

func (ws *workspaceTools) listTodos(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	statusFilter := normalizeTodoStatus(stringArg(arguments, "status"))
	priorityFilter := normalizeTodoPriority(stringArg(arguments, "priority"))

	ws.todoMu.Lock()
	items := append([]TodoItem(nil), ws.todos.Items...) // snapshot
	ws.todoMu.Unlock()

	filtered := items[:0:0]
	for _, item := range items {
		if statusFilter != "" && item.Status != statusFilter {
			continue
		}
		if priorityFilter != "" && item.Priority != priorityFilter {
			continue
		}
		filtered = append(filtered, item)
	}
	// Stable order: most-recent first by UpdatedAt; ties by CreatedAt.
	sort.SliceStable(filtered, func(i, j int) bool {
		if !filtered[i].UpdatedAt.Equal(filtered[j].UpdatedAt) {
			return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	payload, _ := json.MarshalIndent(map[string]interface{}{
		"items": filtered,
		"total": len(filtered),
	}, "", "  ")

	return &ToolResult{
		Content: []Content{{Type: "text", Text: string(payload)}},
		IsError: false,
	}, nil
}

func (ws *workspaceTools) updateTodoStatus(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	id, ok := arguments["id"].(string)
	if !ok || id == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: id is required"}},
			IsError: true,
		}, fmt.Errorf("id is required")
	}

	rawStatus, ok := arguments["status"].(string)
	if !ok || rawStatus == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: status is required"}},
			IsError: true,
		}, fmt.Errorf("status is required")
	}
	status := normalizeTodoStatus(rawStatus)
	switch status {
	case TodoStatusPending, TodoStatusInProgress, TodoStatusCompleted, TodoStatusCancelled:
	default:
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: status must be one of pending|in_progress|completed|cancelled"}},
			IsError: true,
		}, fmt.Errorf("invalid status %q", rawStatus)
	}

	ws.todoMu.Lock()
	updated := ws.todos.UpdateItemStatus(id, status)
	ws.todoMu.Unlock()

	if !updated {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: TODO item %q not found", id)}},
			IsError: true,
		}, fmt.Errorf("todo %q not found", id)
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf("Updated TODO item %s status to %s", id, status)}},
		IsError: false,
	}, nil
}

func (ws *workspaceTools) getTodoStats(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	ws.todoMu.Lock()
	stats := ws.todos.GetStats()
	ws.todoMu.Unlock()
	payload, _ := json.MarshalIndent(stats, "", "  ")

	return &ToolResult{
		Content: []Content{{Type: "text", Text: string(payload)}},
		IsError: false,
	}, nil
}

func (ws *workspaceTools) clearCompletedTodos(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	ws.todoMu.Lock()
	removed := ws.todos.ClearCompleted()
	ws.todoMu.Unlock()
	result := fmt.Sprintf("Cleared %d completed TODO items", removed)

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
		IsError: false,
	}, nil
}

func (ws *workspaceTools) searchInFiles(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	pattern, ok := arguments["pattern"].(string)
	if !ok || pattern == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: pattern is required"}},
			IsError: true,
		}, fmt.Errorf("pattern is required")
	}

	// Get search parameters
	files, _ := arguments["files"].([]interface{})
	filePattern, _ := arguments["file_pattern"].(string)
	isRegex := getBoolArg(arguments, "regex", false)
	caseSensitive := getBoolArg(arguments, "case_sensitive", false)
	maxResults := getIntArg(arguments, "max_results", 100)
	contextLines := getIntArg(arguments, "context_lines", 2)

	var searchFiles []string

	// Determine files to search
	if len(files) > 0 {
		for _, f := range files {
			if filepath, ok := f.(string); ok {
				searchFiles = append(searchFiles, filepath)
			}
		}
	} else if filePattern != "" {
		// Use glob pattern to find files
		matches, err := filepath.Glob(filePattern)
		if err == nil {
			searchFiles = matches
		}
	} else {
		// Default to searching current directory Go files
		matches, _ := filepath.Glob("*.go")
		searchFiles = matches
	}

	if len(searchFiles) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "No files found to search"}},
			IsError: false,
		}, nil
	}

	// Compile regex if needed
	var regex *regexp.Regexp
	var err error
	if isRegex {
		flags := ""
		if !caseSensitive {
			flags = "(?i)"
		}
		regex, err = regexp.Compile(flags + pattern)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Invalid regex pattern: %v", err)}},
				IsError: true,
			}, err
		}
	}

	var results []string
	totalMatches := 0

	for _, filePath := range searchFiles {
		if totalMatches >= maxResults {
			break
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			continue // Skip files that can't be read
		}

		lines := strings.Split(string(content), "\n")
		var fileMatches []string

		for lineNum, line := range lines {
			if totalMatches >= maxResults {
				break
			}

			var matched bool
			if isRegex {
				matched = regex.MatchString(line)
			} else {
				searchLine := line
				searchPattern := pattern
				if !caseSensitive {
					searchLine = strings.ToLower(line)
					searchPattern = strings.ToLower(pattern)
				}
				matched = strings.Contains(searchLine, searchPattern)
			}

			if matched {
				// Add context lines
				start := lineNum - contextLines
				end := lineNum + contextLines + 1
				if start < 0 {
					start = 0
				}
				if end > len(lines) {
					end = len(lines)
				}

				match := fmt.Sprintf("%s:%d:", filePath, lineNum+1)
				for i := start; i < end; i++ {
					prefix := "  "
					if i == lineNum {
						prefix = "> "
					}
					match += fmt.Sprintf("\n%s%d: %s", prefix, i+1, lines[i])
				}
				fileMatches = append(fileMatches, match)
				totalMatches++
			}
		}

		results = append(results, fileMatches...)
	}

	if len(results) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("No matches found for pattern: %s", pattern)}},
			IsError: false,
		}, nil
	}

	result := fmt.Sprintf("Found %d matches:\n\n%s", totalMatches, strings.Join(results, "\n\n"))
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
		IsError: false,
	}, nil
}

// mountWorkspace mounts a workspace PVC for chat agent access
func (ws *workspaceTools) mountWorkspace(ctx context.Context, workflowName, executionID string) (*ToolResult, error) {
	if ws.workspaceManager == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Workspace manager not configured"}},
			IsError: true,
		}, fmt.Errorf("workspace manager not configured")
	}

	mountPath, err := ws.workspaceManager.MountWorkspacePVC(workflowName, executionID)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Failed to mount workspace: %v", err)}},
			IsError: true,
		}, err
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf("Workspace mounted successfully at: %s", mountPath)}},
	}, nil
}

// listWorkspaceFiles lists files in a mounted workspace
func (ws *workspaceTools) listWorkspaceFiles(ctx context.Context, workflowName, executionID, subPath string) (*ToolResult, error) {
	if ws.workspaceManager == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Workspace manager not configured"}},
			IsError: true,
		}, fmt.Errorf("workspace manager not configured")
	}

	// Check if workspace is mounted
	mountPath, mounted := ws.workspaceManager.GetMountPath(workflowName, executionID)
	if !mounted {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Workspace %s-%s is not mounted. Use mount_workspace first", workflowName, executionID)}},
			IsError: true,
		}, fmt.Errorf("workspace not mounted")
	}

	// Update access time
	ws.workspaceManager.UpdateAccessTime(workflowName, executionID)

	// Build full path
	fullPath := mountPath
	if subPath != "" {
		fullPath = filepath.Join(mountPath, subPath)
	}

	// Check if path exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Path does not exist: %s", subPath)}},
			IsError: true,
		}, err
	}

	// List files
	files, err := os.ReadDir(fullPath)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Failed to list files: %v", err)}},
			IsError: true,
		}, err
	}

	// Format file list
	var fileList []string
	for _, file := range files {
		if file.IsDir() {
			fileList = append(fileList, fmt.Sprintf("%s/ (directory)", file.Name()))
		} else {
			// Get file info for size
			info, err := file.Info()
			if err != nil {
				fileList = append(fileList, fmt.Sprintf("%s (unknown size)", file.Name()))
			} else {
				fileList = append(fileList, fmt.Sprintf("%s (%d bytes)", file.Name(), info.Size()))
			}
		}
	}

	result := fmt.Sprintf("Files in %s:\n", filepath.Join(workflowName, executionID, subPath))
	if len(fileList) == 0 {
		result += "No files found"
	} else {
		for _, file := range fileList {
			result += fmt.Sprintf("- %s\n", file)
		}
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
	}, nil
}

// readWorkspaceFile reads a file from a mounted workspace
func (ws *workspaceTools) readWorkspaceFile(ctx context.Context, workflowName, executionID, filePath string, maxSize int) (*ToolResult, error) {
	if ws.workspaceManager == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Workspace manager not configured"}},
			IsError: true,
		}, fmt.Errorf("workspace manager not configured")
	}

	// Check if workspace is mounted
	mountPath, mounted := ws.workspaceManager.GetMountPath(workflowName, executionID)
	if !mounted {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Workspace %s-%s is not mounted. Use mount_workspace first", workflowName, executionID)}},
			IsError: true,
		}, fmt.Errorf("workspace not mounted")
	}

	// Update access time
	ws.workspaceManager.UpdateAccessTime(workflowName, executionID)

	// Build full file path
	fullPath := filepath.Join(mountPath, filePath)

	// Check if file exists
	fileInfo, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("File does not exist: %s", filePath)}},
			IsError: true,
		}, err
	}

	if fileInfo.IsDir() {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Path is a directory, not a file: %s", filePath)}},
			IsError: true,
		}, fmt.Errorf("path is directory")
	}

	// Check file size
	if fileInfo.Size() > int64(maxSize) {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("File too large (%d bytes, max %d bytes): %s", fileInfo.Size(), maxSize, filePath)}},
			IsError: true,
		}, fmt.Errorf("file too large")
	}

	// Read file content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Failed to read file: %v", err)}},
			IsError: true,
		}, err
	}

	result := fmt.Sprintf("Content of %s (%d bytes):\n\n%s", filePath, len(content), string(content))

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
	}, nil
}

// unmountWorkspace unmounts a workspace PVC
func (ws *workspaceTools) unmountWorkspace(ctx context.Context, workflowName, executionID string) (*ToolResult, error) {
	if ws.workspaceManager == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Workspace manager not configured"}},
			IsError: true,
		}, fmt.Errorf("workspace manager not configured")
	}

	err := ws.workspaceManager.UnmountWorkspacePVC(workflowName, executionID)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Failed to unmount workspace: %v", err)}},
			IsError: true,
		}, err
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf("Workspace %s-%s unmounted successfully", workflowName, executionID)}},
	}, nil
}

// getWorkspaceStats gets statistics about workspace PVCs and retention policies
func (ws *workspaceTools) getWorkspaceStats(ctx context.Context) (*ToolResult, error) {
	if ws.workspaceManager == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Workspace manager not configured"}},
			IsError: true,
		}, fmt.Errorf("workspace manager not configured")
	}

	mounts := ws.workspaceManager.ListMountedWorkspaces()

	// Basic stats
	totalMounts := len(mounts)
	totalAccessCount := int64(0)

	for _, mount := range mounts {
		totalAccessCount += mount.AccessCount
	}

	result := "Workspace Statistics:\n\n"
	result += fmt.Sprintf("Total mounted workspaces: %d\n", totalMounts)
	result += fmt.Sprintf("Total access count: %d\n", totalAccessCount)

	if totalMounts > 0 {
		avgAccess := float64(totalAccessCount) / float64(totalMounts)
		result += fmt.Sprintf("Average accesses per workspace: %.1f\n", avgAccess)
	}

	result += "Retention policy: 2 hours since last access\n"
	result += "Base mount path: /tmp/matey-workspaces\n"

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
	}, nil
}

// stringArg pulls a string argument with empty-string fallback.
func stringArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)

	return v
}

// normalizeTodoStatus coerces caller-supplied status strings (e.g. "in_progress",
// "InProgress", "IN-PROGRESS") into the canonical TodoStatus values, returning
// "" for empty input so callers can use it as a "no filter" sentinel.
func normalizeTodoStatus(s string) TodoStatus {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "_")
	switch s {
	case "":
		return ""
	case "pending":
		return TodoStatusPending
	case "in_progress", "inprogress":
		return TodoStatusInProgress
	case "completed", "done":
		return TodoStatusCompleted
	case "cancelled", "canceled":
		return TodoStatusCancelled
	default:
		return TodoStatus(s) // pass through unrecognised values so validation can surface them
	}
}

// normalizeTodoPriority maps caller-supplied priority strings to the canonical
// TodoPriority values. Empty input returns "" for "no filter"; unknown values
// pass through so a downstream check can reject them.
func normalizeTodoPriority(s string) TodoPriority {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "":
		return ""
	case "low":
		return TodoPriorityLow
	case "medium", "med":
		return TodoPriorityMedium
	case "high":
		return TodoPriorityHigh
	case "urgent", "critical":
		return TodoPriorityUrgent
	default:
		return TodoPriority(s)
	}
}
