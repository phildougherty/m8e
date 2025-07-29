// internal/scheduler/workspace_cleanup.go
package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// WorkspaceCleanupManager handles cleanup of retained workspace PVCs based on retention policies
type WorkspaceCleanupManager struct {
	k8sClient       kubernetes.Interface
	namespace       string
	logger          logr.Logger
	
	// Configuration
	cleanupInterval time.Duration // How often to run cleanup checks
	
	// Cleanup context
	cleanupCtx    context.Context
	cleanupCancel context.CancelFunc
	cleanupWG     sync.WaitGroup
}

// WorkspaceCleanupStats represents statistics about workspace cleanup operations
type WorkspaceCleanupStats struct {
	TotalWorkspaces     int32     `json:"totalWorkspaces"`
	ExpiredWorkspaces   int32     `json:"expiredWorkspaces"`
	DeletedCount        int32     `json:"deletedCount"`
	ErrorCount          int32     `json:"errorCount"`
	LastCleanupTime     time.Time `json:"lastCleanupTime"`
	NextCleanupTime     time.Time `json:"nextCleanupTime"`
}

// NewWorkspaceCleanupManager creates a new workspace cleanup manager
func NewWorkspaceCleanupManager(k8sClient kubernetes.Interface, namespace string, logger logr.Logger) *WorkspaceCleanupManager {
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	
	wcm := &WorkspaceCleanupManager{
		k8sClient:       k8sClient,
		namespace:       namespace,
		logger:          logger,
		cleanupInterval: 6 * time.Hour, // Default: cleanup every 6 hours
		cleanupCtx:      cleanupCtx,
		cleanupCancel:   cleanupCancel,
	}
	
	// Start cleanup routine
	wcm.startCleanupRoutine()
	
	return wcm
}

// CleanupExpiredWorkspaces removes workspace PVCs that have exceeded their retention period
func (wcm *WorkspaceCleanupManager) CleanupExpiredWorkspaces() (*WorkspaceCleanupStats, error) {
	ctx := context.Background()
	stats := &WorkspaceCleanupStats{
		LastCleanupTime: time.Now(),
		NextCleanupTime: time.Now().Add(wcm.cleanupInterval),
	}
	
	// List all workspace PVCs
	pvcList, err := wcm.k8sClient.CoreV1().PersistentVolumeClaims(wcm.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "mcp.matey.ai/workspace-type=workflow",
	})
	if err != nil {
		return stats, fmt.Errorf("failed to list workspace PVCs: %w", err)
	}
	
	stats.TotalWorkspaces = int32(len(pvcList.Items))
	wcm.logger.Info("Starting workspace cleanup", "totalWorkspaces", stats.TotalWorkspaces)
	
	now := time.Now()
	
	for _, pvc := range pvcList.Items {
		// Skip PVCs that are marked for auto-deletion (they'll be cleaned up by the job manager)
		if autoDelete, exists := pvc.Annotations["mcp.matey.ai/auto-delete"]; exists && autoDelete == "true" {
			continue
		}
		
		// Get retention period from annotations
		retentionDaysStr, exists := pvc.Annotations["mcp.matey.ai/workspace-retention-days"]
		if !exists {
			retentionDaysStr = "7" // Default to 7 days
		}
		
		retentionDays, err := strconv.Atoi(retentionDaysStr)
		if err != nil {
			wcm.logger.Error(err, "Invalid retention days annotation", "pvc", pvc.Name, "retentionDays", retentionDaysStr)
			stats.ErrorCount++
			continue
		}
		
		// Check if PVC is expired
		createdAt := pvc.CreationTimestamp.Time
		retentionPeriod := time.Duration(retentionDays) * 24 * time.Hour
		expiryTime := createdAt.Add(retentionPeriod)
		
		if now.After(expiryTime) {
			stats.ExpiredWorkspaces++
			
			workflowName := pvc.Labels["mcp.matey.ai/workflow-name"]
			executionID := pvc.Labels["mcp.matey.ai/execution-id"]
			
			wcm.logger.Info("Workspace PVC expired, deleting", 
				"pvc", pvc.Name,
				"workflow", workflowName,
				"executionID", executionID,
				"createdAt", createdAt,
				"retentionDays", retentionDays,
				"age", now.Sub(createdAt))
			
			// Delete the PVC
			err := wcm.k8sClient.CoreV1().PersistentVolumeClaims(wcm.namespace).Delete(
				ctx, pvc.Name, metav1.DeleteOptions{})
			if err != nil {
				wcm.logger.Error(err, "Failed to delete expired workspace PVC", "pvc", pvc.Name)
				stats.ErrorCount++
				continue
			}
			
			stats.DeletedCount++
			wcm.logger.Info("Successfully deleted expired workspace PVC", "pvc", pvc.Name)
		}
	}
	
	wcm.logger.Info("Workspace cleanup completed", 
		"totalWorkspaces", stats.TotalWorkspaces,
		"expiredWorkspaces", stats.ExpiredWorkspaces,
		"deletedCount", stats.DeletedCount,
		"errorCount", stats.ErrorCount)
	
	return stats, nil
}

// GetWorkspaceStats returns information about workspace PVCs and their retention status
func (wcm *WorkspaceCleanupManager) GetWorkspaceStats() (map[string]interface{}, error) {
	ctx := context.Background()
	
	// List all workspace PVCs
	pvcList, err := wcm.k8sClient.CoreV1().PersistentVolumeClaims(wcm.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "mcp.matey.ai/workspace-type=workflow",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list workspace PVCs: %w", err)
	}
	
	now := time.Now()
	stats := map[string]interface{}{
		"totalWorkspaces": len(pvcList.Items),
		"workspaces":      make([]map[string]interface{}, 0),
	}
	
	expiringCount := 0
	retainedCount := 0
	autoDeleteCount := 0
	
	workspaces := make([]map[string]interface{}, 0, len(pvcList.Items))
	
	for _, pvc := range pvcList.Items {
		workflowName := pvc.Labels["mcp.matey.ai/workflow-name"]
		executionID := pvc.Labels["mcp.matey.ai/execution-id"]
		
		autoDelete := pvc.Annotations["mcp.matey.ai/auto-delete"] == "true"
		if autoDelete {
			autoDeleteCount++
		}
		
		retentionDaysStr := pvc.Annotations["mcp.matey.ai/workspace-retention-days"]
		if retentionDaysStr == "" {
			retentionDaysStr = "7"
		}
		
		retentionDays, _ := strconv.Atoi(retentionDaysStr)
		createdAt := pvc.CreationTimestamp.Time
		age := now.Sub(createdAt)
		retentionPeriod := time.Duration(retentionDays) * 24 * time.Hour
		timeUntilExpiry := retentionPeriod - age
		
		var status string
		if autoDelete {
			status = "auto-delete"
		} else if timeUntilExpiry <= 0 {
			status = "expired"
		} else if timeUntilExpiry <= 24*time.Hour {
			status = "expiring-soon"
			expiringCount++
		} else {
			status = "retained"
			retainedCount++
		}
		
		workspace := map[string]interface{}{
			"pvcName":          pvc.Name,
			"workflowName":     workflowName,
			"executionID":      executionID,
			"status":           status,
			"createdAt":        createdAt.Format(time.RFC3339),
			"age":              age.String(),
			"retentionDays":    retentionDays,
			"timeUntilExpiry":  timeUntilExpiry.String(),
			"size":             pvc.Spec.Resources.Requests.Storage().String(),
			"phase":            string(pvc.Status.Phase),
			"autoDelete":       autoDelete,
		}
		
		workspaces = append(workspaces, workspace)
	}
	
	stats["workspaces"] = workspaces
	stats["retainedCount"] = retainedCount
	stats["expiringCount"] = expiringCount
	stats["autoDeleteCount"] = autoDeleteCount
	
	return stats, nil
}

// CleanupWorkspaceByName manually deletes a specific workspace PVC
func (wcm *WorkspaceCleanupManager) CleanupWorkspaceByName(workflowName, executionID string) error {
	ctx := context.Background()
	pvcName := fmt.Sprintf("workspace-%s-%s", workflowName, executionID)
	
	// Verify the PVC exists and is a workspace PVC
	pvc, err := wcm.k8sClient.CoreV1().PersistentVolumeClaims(wcm.namespace).Get(
		ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("workspace PVC %s not found: %w", pvcName, err)
	}
	
	// Verify it's a workspace PVC
	if pvc.Labels["mcp.matey.ai/workspace-type"] != "workflow" {
		return fmt.Errorf("PVC %s is not a workspace PVC", pvcName)
	}
	
	// Delete the PVC
	err = wcm.k8sClient.CoreV1().PersistentVolumeClaims(wcm.namespace).Delete(
		ctx, pvcName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete workspace PVC %s: %w", pvcName, err)
	}
	
	wcm.logger.Info("Manually deleted workspace PVC", 
		"pvc", pvcName, "workflow", workflowName, "executionID", executionID)
	
	return nil
}

// startCleanupRoutine starts a background routine to cleanup expired workspaces
func (wcm *WorkspaceCleanupManager) startCleanupRoutine() {
	wcm.cleanupWG.Add(1)
	go func() {
		defer wcm.cleanupWG.Done()
		
		// Run initial cleanup after a short delay
		initialDelay := 5 * time.Minute
		wcm.logger.Info("Workspace cleanup routine starting", 
			"initialDelay", initialDelay, 
			"cleanupInterval", wcm.cleanupInterval)
		
		select {
		case <-wcm.cleanupCtx.Done():
			return
		case <-time.After(initialDelay):
			// Run initial cleanup
			stats, err := wcm.CleanupExpiredWorkspaces()
			if err != nil {
				wcm.logger.Error(err, "Initial workspace cleanup failed")
			} else {
				wcm.logger.Info("Initial workspace cleanup completed", "stats", stats)
			}
		}
		
		ticker := time.NewTicker(wcm.cleanupInterval)
		defer ticker.Stop()
		
		for {
			select {
			case <-wcm.cleanupCtx.Done():
				wcm.logger.Info("Workspace cleanup routine stopping")
				return
			case <-ticker.C:
				stats, err := wcm.CleanupExpiredWorkspaces()
				if err != nil {
					wcm.logger.Error(err, "Scheduled workspace cleanup failed")
				} else if stats.DeletedCount > 0 || stats.ExpiredWorkspaces > 0 {
					wcm.logger.Info("Scheduled workspace cleanup completed", "stats", stats)
				}
			}
		}
	}()
}

// Stop stops the cleanup manager
func (wcm *WorkspaceCleanupManager) Stop() {
	wcm.logger.Info("Stopping workspace cleanup manager")
	
	// Cancel cleanup routine
	if wcm.cleanupCancel != nil {
		wcm.cleanupCancel()
	}
	
	// Wait for cleanup routine to finish
	wcm.cleanupWG.Wait()
}

// SetCleanupInterval configures how often to run cleanup
func (wcm *WorkspaceCleanupManager) SetCleanupInterval(interval time.Duration) {
	wcm.cleanupInterval = interval
}