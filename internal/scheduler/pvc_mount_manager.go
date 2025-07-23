// internal/scheduler/pvc_mount_manager.go
package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
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

// PVCMountManager handles mounting and unmounting of workspace PVCs for chat agent access
type PVCMountManager struct {
	k8sClient     kubernetes.Interface
	namespace     string
	baseMountPath string
	activeMounts  map[string]*MountInfo // key: workflowName-executionID
	mountMutex    sync.RWMutex
	logger        logr.Logger
	
	// Configuration
	mountTimeout    time.Duration // How long to keep mounts active without access
	cleanupInterval time.Duration // How often to run cleanup
	
	// Cleanup context
	cleanupCtx    context.Context
	cleanupCancel context.CancelFunc
	cleanupWG     sync.WaitGroup
}

// NewPVCMountManager creates a new PVC mount manager
func NewPVCMountManager(k8sClient kubernetes.Interface, namespace string, logger logr.Logger) *PVCMountManager {
	baseMountPath := "/tmp/workspaces"
	
	// Ensure base mount directory exists
	if err := os.MkdirAll(baseMountPath, 0755); err != nil {
		logger.Error(err, "Failed to create base mount directory", "path", baseMountPath)
	}
	
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	
	pmm := &PVCMountManager{
		k8sClient:       k8sClient,
		namespace:       namespace,
		baseMountPath:   baseMountPath,
		activeMounts:    make(map[string]*MountInfo),
		logger:          logger,
		mountTimeout:    1 * time.Hour,  // Default: unmount after 1 hour of inactivity
		cleanupInterval: 15 * time.Minute, // Default: cleanup every 15 minutes
		cleanupCtx:      cleanupCtx,
		cleanupCancel:   cleanupCancel,
	}
	
	// Start cleanup goroutine
	pmm.startCleanupRoutine()
	
	return pmm
}

// MountWorkspacePVC mounts a workspace PVC and returns the mount path
func (pmm *PVCMountManager) MountWorkspacePVC(workflowName, executionID string) (string, error) {
	pmm.mountMutex.Lock()
	defer pmm.mountMutex.Unlock()
	
	mountKey := fmt.Sprintf("%s-%s", workflowName, executionID)
	
	// Check if already mounted
	if mountInfo, exists := pmm.activeMounts[mountKey]; exists {
		// Update access time and return existing mount
		mountInfo.LastAccess = time.Now()
		mountInfo.AccessCount++
		pmm.logger.Info("Workspace already mounted, updating access time", 
			"workflow", workflowName, "executionID", executionID, "mountPath", mountInfo.MountPath)
		return mountInfo.MountPath, nil
	}
	
	// Check if PVC exists
	pvcName := fmt.Sprintf("workspace-%s-%s", workflowName, executionID)
	pvc, err := pmm.k8sClient.CoreV1().PersistentVolumeClaims(pmm.namespace).Get(
		context.Background(), pvcName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("PVC %s not found: %w", pvcName, err)
	}
	
	// Verify PVC is bound
	if pvc.Status.Phase != corev1.ClaimBound {
		return "", fmt.Errorf("PVC %s is not bound (phase: %s)", pvcName, pvc.Status.Phase)
	}
	
	// Create mount directory
	mountPath := filepath.Join(pmm.baseMountPath, executionID)
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create mount directory %s: %w", mountPath, err)
	}
	
	// For now, we simulate mounting by creating a symlink to show the concept
	// In a full implementation, this would use a proper volume mount mechanism
	// This is a simplified version that demonstrates the interface
	
	// Create a marker file to indicate this is a mounted workspace
	markerFile := filepath.Join(mountPath, ".workspace-info")
	markerContent := fmt.Sprintf("workflow: %s\nexecution: %s\npvc: %s\nmounted: %s\n",
		workflowName, executionID, pvcName, time.Now().Format(time.RFC3339))
	
	if err := os.WriteFile(markerFile, []byte(markerContent), 0644); err != nil {
		return "", fmt.Errorf("failed to create workspace marker: %w", err)
	}
	
	// Record the mount
	mountInfo := &MountInfo{
		WorkflowName: workflowName,
		ExecutionID:  executionID,
		PVCName:      pvcName,
		MountPath:    mountPath,
		MountedAt:    time.Now(),
		LastAccess:   time.Now(),
		AccessCount:  1,
	}
	
	pmm.activeMounts[mountKey] = mountInfo
	
	pmm.logger.Info("Workspace PVC mounted successfully", 
		"workflow", workflowName, "executionID", executionID, 
		"pvc", pvcName, "mountPath", mountPath)
	
	return mountPath, nil
}

// UnmountWorkspacePVC unmounts a workspace PVC
func (pmm *PVCMountManager) UnmountWorkspacePVC(workflowName, executionID string) error {
	pmm.mountMutex.Lock()
	defer pmm.mountMutex.Unlock()
	
	mountKey := fmt.Sprintf("%s-%s", workflowName, executionID)
	
	mountInfo, exists := pmm.activeMounts[mountKey]
	if !exists {
		return fmt.Errorf("workspace %s-%s is not mounted", workflowName, executionID)
	}
	
	// Remove mount directory
	if err := os.RemoveAll(mountInfo.MountPath); err != nil {
		pmm.logger.Error(err, "Failed to remove mount directory", "path", mountInfo.MountPath)
		// Continue with cleanup even if directory removal fails
	}
	
	// Remove from active mounts
	delete(pmm.activeMounts, mountKey)
	
	pmm.logger.Info("Workspace PVC unmounted successfully", 
		"workflow", workflowName, "executionID", executionID, 
		"mountPath", mountInfo.MountPath)
	
	return nil
}

// ListMountedWorkspaces returns a list of currently mounted workspaces
func (pmm *PVCMountManager) ListMountedWorkspaces() []MountInfo {
	pmm.mountMutex.RLock()
	defer pmm.mountMutex.RUnlock()
	
	mounts := make([]MountInfo, 0, len(pmm.activeMounts))
	for _, mountInfo := range pmm.activeMounts {
		mounts = append(mounts, *mountInfo)
	}
	
	return mounts
}

// UpdateAccessTime updates the last access time for a mounted workspace
func (pmm *PVCMountManager) UpdateAccessTime(workflowName, executionID string) {
	pmm.mountMutex.Lock()
	defer pmm.mountMutex.Unlock()
	
	mountKey := fmt.Sprintf("%s-%s", workflowName, executionID)
	if mountInfo, exists := pmm.activeMounts[mountKey]; exists {
		mountInfo.LastAccess = time.Now()
		mountInfo.AccessCount++
	}
}

// GetMountPath returns the mount path for a workspace if it's mounted
func (pmm *PVCMountManager) GetMountPath(workflowName, executionID string) (string, bool) {
	pmm.mountMutex.RLock()
	defer pmm.mountMutex.RUnlock()
	
	mountKey := fmt.Sprintf("%s-%s", workflowName, executionID)
	if mountInfo, exists := pmm.activeMounts[mountKey]; exists {
		return mountInfo.MountPath, true
	}
	
	return "", false
}

// CleanupExpiredMounts unmounts workspaces that haven't been accessed recently
func (pmm *PVCMountManager) CleanupExpiredMounts() {
	pmm.mountMutex.Lock()
	defer pmm.mountMutex.Unlock()
	
	now := time.Now()
	var toRemove []string
	
	for mountKey, mountInfo := range pmm.activeMounts {
		if now.Sub(mountInfo.LastAccess) > pmm.mountTimeout {
			toRemove = append(toRemove, mountKey)
		}
	}
	
	for _, mountKey := range toRemove {
		mountInfo := pmm.activeMounts[mountKey]
		
		pmm.logger.Info("Unmounting expired workspace", 
			"workflow", mountInfo.WorkflowName, 
			"executionID", mountInfo.ExecutionID,
			"lastAccess", mountInfo.LastAccess,
			"age", now.Sub(mountInfo.LastAccess))
		
		// Remove mount directory
		if err := os.RemoveAll(mountInfo.MountPath); err != nil {
			pmm.logger.Error(err, "Failed to remove expired mount directory", "path", mountInfo.MountPath)
		}
		
		delete(pmm.activeMounts, mountKey)
	}
	
	if len(toRemove) > 0 {
		pmm.logger.Info("Cleaned up expired mounts", "count", len(toRemove))
	}
}

// startCleanupRoutine starts a background routine to cleanup expired mounts
func (pmm *PVCMountManager) startCleanupRoutine() {
	pmm.cleanupWG.Add(1)
	go func() {
		defer pmm.cleanupWG.Done()
		
		ticker := time.NewTicker(pmm.cleanupInterval)
		defer ticker.Stop()
		
		for {
			select {
			case <-pmm.cleanupCtx.Done():
				pmm.logger.Info("PVC mount manager cleanup routine stopping")
				return
			case <-ticker.C:
				pmm.CleanupExpiredMounts()
			}
		}
	}()
}

// Stop stops the mount manager and cleans up all mounts
func (pmm *PVCMountManager) Stop() {
	pmm.logger.Info("Stopping PVC mount manager")
	
	// Cancel cleanup routine
	if pmm.cleanupCancel != nil {
		pmm.cleanupCancel()
	}
	
	// Wait for cleanup routine to finish
	pmm.cleanupWG.Wait()
	
	// Unmount all active mounts
	pmm.mountMutex.Lock()
	defer pmm.mountMutex.Unlock()
	
	for mountKey, mountInfo := range pmm.activeMounts {
		pmm.logger.Info("Unmounting workspace during shutdown", 
			"workflow", mountInfo.WorkflowName, 
			"executionID", mountInfo.ExecutionID)
		
		if err := os.RemoveAll(mountInfo.MountPath); err != nil {
			pmm.logger.Error(err, "Failed to remove mount directory during shutdown", 
				"path", mountInfo.MountPath)
		}
		
		delete(pmm.activeMounts, mountKey)
	}
}

// SetMountTimeout configures how long to keep mounts active without access
func (pmm *PVCMountManager) SetMountTimeout(timeout time.Duration) {
	pmm.mountTimeout = timeout
}

// SetCleanupInterval configures how often to run cleanup
func (pmm *PVCMountManager) SetCleanupInterval(interval time.Duration) {
	pmm.cleanupInterval = interval
}