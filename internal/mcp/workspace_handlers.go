// internal/mcp/workspace_handlers.go
package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// mountWorkspace mounts a workspace PVC for chat agent access
func (m *MateyMCPServer) mountWorkspace(ctx context.Context, workflowName, executionID string) (*ToolResult, error) {
	if m.workspaceManager == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Workspace manager not configured"}},
			IsError: true,
		}, fmt.Errorf("workspace manager not configured")
	}

	mountPath, err := m.workspaceManager.MountWorkspacePVC(workflowName, executionID)
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
func (m *MateyMCPServer) listWorkspaceFiles(ctx context.Context, workflowName, executionID, subPath string) (*ToolResult, error) {
	if m.workspaceManager == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Workspace manager not configured"}},
			IsError: true,
		}, fmt.Errorf("workspace manager not configured")
	}

	// Check if workspace is mounted
	mountPath, mounted := m.workspaceManager.GetMountPath(workflowName, executionID)
	if !mounted {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Workspace %s-%s is not mounted. Use mount_workspace first", workflowName, executionID)}},
			IsError: true,
		}, fmt.Errorf("workspace not mounted")
	}

	// Update access time
	m.workspaceManager.UpdateAccessTime(workflowName, executionID)

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
func (m *MateyMCPServer) readWorkspaceFile(ctx context.Context, workflowName, executionID, filePath string, maxSize int) (*ToolResult, error) {
	if m.workspaceManager == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Workspace manager not configured"}},
			IsError: true,
		}, fmt.Errorf("workspace manager not configured")
	}

	// Check if workspace is mounted
	mountPath, mounted := m.workspaceManager.GetMountPath(workflowName, executionID)
	if !mounted {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Workspace %s-%s is not mounted. Use mount_workspace first", workflowName, executionID)}},
			IsError: true,
		}, fmt.Errorf("workspace not mounted")
	}

	// Update access time
	m.workspaceManager.UpdateAccessTime(workflowName, executionID)

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
func (m *MateyMCPServer) unmountWorkspace(ctx context.Context, workflowName, executionID string) (*ToolResult, error) {
	if m.workspaceManager == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Workspace manager not configured"}},
			IsError: true,
		}, fmt.Errorf("workspace manager not configured")
	}

	err := m.workspaceManager.UnmountWorkspacePVC(workflowName, executionID)
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

// listMountedWorkspaces lists all currently mounted workspaces
func (m *MateyMCPServer) listMountedWorkspaces(ctx context.Context) (*ToolResult, error) {
	if m.workspaceManager == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Workspace manager not configured"}},
			IsError: true,
		}, fmt.Errorf("workspace manager not configured")
	}

	mounts := m.workspaceManager.ListMountedWorkspaces()

	if len(mounts) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "No workspaces currently mounted"}},
		}, nil
	}

	result := fmt.Sprintf("Currently mounted workspaces (%d):\n\n", len(mounts))
	for _, mount := range mounts {
		result += fmt.Sprintf("- %s-%s\n", mount.WorkflowName, mount.ExecutionID)
		result += fmt.Sprintf("  PVC: %s\n", mount.PVCName)
		result += fmt.Sprintf("  Mount Path: %s\n", mount.MountPath)
		result += fmt.Sprintf("  Mounted At: %s\n", mount.MountedAt.Format("2006-01-02 15:04:05"))
		result += fmt.Sprintf("  Last Access: %s\n", mount.LastAccess.Format("2006-01-02 15:04:05"))
		result += fmt.Sprintf("  Access Count: %d\n\n", mount.AccessCount)
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
	}, nil
}

// getWorkspaceStats gets statistics about workspace PVCs and retention policies
func (m *MateyMCPServer) getWorkspaceStats(ctx context.Context) (*ToolResult, error) {
	if m.workspaceManager == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Workspace manager not configured"}},
			IsError: true,
		}, fmt.Errorf("workspace manager not configured")
	}

	mounts := m.workspaceManager.ListMountedWorkspaces()

	// Basic stats
	totalMounts := len(mounts)
	totalAccessCount := int64(0)
	
	for _, mount := range mounts {
		totalAccessCount += mount.AccessCount
	}

	result := fmt.Sprintf("Workspace Statistics:\n\n")
	result += fmt.Sprintf("Total mounted workspaces: %d\n", totalMounts)
	result += fmt.Sprintf("Total access count: %d\n", totalAccessCount)
	
	if totalMounts > 0 {
		avgAccess := float64(totalAccessCount) / float64(totalMounts)
		result += fmt.Sprintf("Average accesses per workspace: %.1f\n", avgAccess)
	}
	
	result += fmt.Sprintf("Retention policy: 2 hours since last access\n")
	result += fmt.Sprintf("Base mount path: /tmp/matey-workspaces\n")

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
	}, nil
}