// internal/mcp/workspace_manager.go
package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// MountInfo represents information about a mounted workspace
type MountInfo struct {
	WorkflowName string    `json:"workflowName"`
	ExecutionID  string    `json:"executionID"`
	PVCName      string    `json:"pvcName"`
	MountPath    string    `json:"mountPath"`
	MountedAt    time.Time `json:"mountedAt"`
	LastAccess   time.Time `json:"lastAccess"`
	AccessCount  int64     `json:"accessCount"`
}

// WorkspaceManager handles mounting and unmounting of workspace PVCs for chat agent access
type WorkspaceManager struct {
	k8sClient     kubernetes.Interface
	namespace     string
	baseMountPath string
	logger        logr.Logger
	
	// Thread-safe mount tracking
	activeMounts map[string]*MountInfo
	mountMutex   sync.RWMutex
}

// NewWorkspaceManager creates a new workspace manager
func NewWorkspaceManager(k8sClient kubernetes.Interface, namespace string, logger logr.Logger) *WorkspaceManager {
	baseMountPath := "/tmp/matey-workspaces"
	
	// Ensure base mount directory exists
	if err := os.MkdirAll(baseMountPath, 0755); err != nil {
		logger.Error(err, "Failed to create base mount directory", "path", baseMountPath)
	}
	
	return &WorkspaceManager{
		k8sClient:     k8sClient,
		namespace:     namespace,
		baseMountPath: baseMountPath,
		logger:        logger,
		activeMounts:  make(map[string]*MountInfo),
	}
}

// MountWorkspacePVC mounts a workspace PVC and returns the mount path
func (wm *WorkspaceManager) MountWorkspacePVC(workflowName, executionID string) (string, error) {
	wm.mountMutex.Lock()
	defer wm.mountMutex.Unlock()
	
	mountKey := fmt.Sprintf("%s-%s", workflowName, executionID)
	
	// Check if already mounted
	if mountInfo, exists := wm.activeMounts[mountKey]; exists {
		wm.logger.Info("Workspace already mounted, returning existing mount path",
			"workflow", workflowName, "executionID", executionID, "mountPath", mountInfo.MountPath)
		return mountInfo.MountPath, nil
	}
	
	// Check if PVC exists
	pvcName := fmt.Sprintf("workspace-%s-%s", workflowName, executionID)
	pvc, err := wm.k8sClient.CoreV1().PersistentVolumeClaims(wm.namespace).Get(
		context.Background(), pvcName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("workspace PVC %s not found: %w", pvcName, err)
	}
	
	// Create mount directory
	mountPath := filepath.Join(wm.baseMountPath, mountKey)
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create mount directory: %w", err)
	}
	
	// Record the mount (simplified implementation - in production this would use actual PVC mounting)
	mountInfo := &MountInfo{
		WorkflowName: workflowName,
		ExecutionID:  executionID,
		PVCName:      pvcName,
		MountPath:    mountPath,
		MountedAt:    time.Now(),
		LastAccess:   time.Now(),
		AccessCount:  1,
	}
	
	wm.activeMounts[mountKey] = mountInfo
	
	wm.logger.Info("Mounted workspace PVC", 
		"workflow", workflowName, "executionID", executionID, "mountPath", mountPath,
		"pvc", pvcName, "pvcStatus", pvc.Status.Phase)
	
	// Create a marker file to indicate this is a mounted workspace
	markerFile := filepath.Join(mountPath, ".workspace-info")
	markerContent := fmt.Sprintf("workflow: %s\nexecution: %s\npvc: %s\nmounted: %s\n",
		workflowName, executionID, pvcName, time.Now().Format(time.RFC3339))
	
	if err := os.WriteFile(markerFile, []byte(markerContent), 0644); err != nil {
		wm.logger.Error(err, "Failed to create workspace marker file", "file", markerFile)
	}
	
	return mountPath, nil
}

// UnmountWorkspacePVC unmounts a workspace PVC
func (wm *WorkspaceManager) UnmountWorkspacePVC(workflowName, executionID string) error {
	wm.mountMutex.Lock()
	defer wm.mountMutex.Unlock()
	
	mountKey := fmt.Sprintf("%s-%s", workflowName, executionID)
	
	mountInfo, exists := wm.activeMounts[mountKey]
	if !exists {
		return fmt.Errorf("workspace %s-%s is not mounted", workflowName, executionID)
	}
	
	// Remove mount directory
	if err := os.RemoveAll(mountInfo.MountPath); err != nil {
		wm.logger.Error(err, "Failed to remove mount directory", "path", mountInfo.MountPath)
		// Continue with unmount even if directory removal fails
	}
	
	// Remove from active mounts
	delete(wm.activeMounts, mountKey)
	
	wm.logger.Info("Unmounted workspace PVC", 
		"workflow", workflowName, "executionID", executionID, "mountPath", mountInfo.MountPath)
	
	return nil
}

// ListMountedWorkspaces returns a list of currently mounted workspaces
func (wm *WorkspaceManager) ListMountedWorkspaces() []MountInfo {
	wm.mountMutex.RLock()
	defer wm.mountMutex.RUnlock()
	
	mounts := make([]MountInfo, 0, len(wm.activeMounts))
	for _, mountInfo := range wm.activeMounts {
		mounts = append(mounts, *mountInfo)
	}
	
	return mounts
}

// UpdateAccessTime updates the last access time for a mounted workspace
func (wm *WorkspaceManager) UpdateAccessTime(workflowName, executionID string) {
	wm.mountMutex.Lock()
	defer wm.mountMutex.Unlock()
	
	mountKey := fmt.Sprintf("%s-%s", workflowName, executionID)
	if mountInfo, exists := wm.activeMounts[mountKey]; exists {
		mountInfo.LastAccess = time.Now()
		mountInfo.AccessCount++
	}
}

// GetMountPath returns the mount path for a workspace if it's mounted
func (wm *WorkspaceManager) GetMountPath(workflowName, executionID string) (string, bool) {
	wm.mountMutex.RLock()
	defer wm.mountMutex.RUnlock()
	
	mountKey := fmt.Sprintf("%s-%s", workflowName, executionID)
	if mountInfo, exists := wm.activeMounts[mountKey]; exists {
		return mountInfo.MountPath, true
	}
	
	return "", false
}

// CleanupExpiredMounts unmounts workspaces that haven't been accessed recently
func (wm *WorkspaceManager) CleanupExpiredMounts() {
	wm.mountMutex.Lock()
	defer wm.mountMutex.Unlock()
	
	expireTime := time.Now().Add(-2 * time.Hour) // 2 hour expiration
	var toRemove []string
	
	for mountKey, mountInfo := range wm.activeMounts {
		if mountInfo.LastAccess.Before(expireTime) {
			toRemove = append(toRemove, mountKey)
		}
	}
	
	for _, mountKey := range toRemove {
		mountInfo := wm.activeMounts[mountKey]
		
		wm.logger.Info("Unmounting expired workspace", 
			"workflow", mountInfo.WorkflowName, 
			"executionID", mountInfo.ExecutionID,
			"lastAccess", mountInfo.LastAccess,
			"accessCount", mountInfo.AccessCount)
		
		// Remove mount directory
		if err := os.RemoveAll(mountInfo.MountPath); err != nil {
			wm.logger.Error(err, "Failed to remove expired mount directory", "path", mountInfo.MountPath)
		}
		
		delete(wm.activeMounts, mountKey)
	}
}

// Shutdown unmounts all workspaces during server shutdown
func (wm *WorkspaceManager) Shutdown() {
	wm.mountMutex.Lock()
	defer wm.mountMutex.Unlock()
	
	for mountKey, mountInfo := range wm.activeMounts {
		wm.logger.Info("Unmounting workspace during shutdown", 
			"workflow", mountInfo.WorkflowName, 
			"executionID", mountInfo.ExecutionID)
		
		if err := os.RemoveAll(mountInfo.MountPath); err != nil {
			wm.logger.Error(err, "Failed to remove mount directory during shutdown", "path", mountInfo.MountPath)
		}
		
		delete(wm.activeMounts, mountKey)
	}
}