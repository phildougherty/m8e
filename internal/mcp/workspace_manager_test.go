package mcp

import (
	"testing"
	"time"

	"github.com/go-logr/logr"
)

func TestNewWorkspaceManager(t *testing.T) {
	logger := logr.Discard()
	namespace := "test-namespace"
	
	manager := NewWorkspaceManager(nil, namespace, logger)
	
	if manager == nil {
		t.Fatal("NewWorkspaceManager() returned nil")
	}
	
	if manager.namespace != namespace {
		t.Errorf("Expected namespace %s, got %s", namespace, manager.namespace)
	}
	
	if manager.baseMountPath != "/tmp/matey-workspaces" {
		t.Errorf("Expected baseMountPath '/tmp/matey-workspaces', got %s", manager.baseMountPath)
	}
	
	if manager.activeMounts == nil {
		t.Error("Expected activeMounts to be initialized")
	}
	
	if len(manager.activeMounts) != 0 {
		t.Errorf("Expected empty activeMounts, got %d items", len(manager.activeMounts))
	}
}

func TestMountInfo_Struct(t *testing.T) {
	now := time.Now()
	
	mountInfo := MountInfo{
		WorkflowName: "test-workflow",
		ExecutionID:  "exec-123",
		PVCName:      "test-pvc",
		MountPath:    "/tmp/test-mount",
		MountedAt:    now,
		LastAccess:   now,
		AccessCount:  5,
	}
	
	if mountInfo.WorkflowName != "test-workflow" {
		t.Errorf("Expected WorkflowName 'test-workflow', got %s", mountInfo.WorkflowName)
	}
	
	if mountInfo.ExecutionID != "exec-123" {
		t.Errorf("Expected ExecutionID 'exec-123', got %s", mountInfo.ExecutionID)
	}
	
	if mountInfo.PVCName != "test-pvc" {
		t.Errorf("Expected PVCName 'test-pvc', got %s", mountInfo.PVCName)
	}
	
	if mountInfo.MountPath != "/tmp/test-mount" {
		t.Errorf("Expected MountPath '/tmp/test-mount', got %s", mountInfo.MountPath)
	}
	
	if mountInfo.AccessCount != 5 {
		t.Errorf("Expected AccessCount 5, got %d", mountInfo.AccessCount)
	}
	
	// Check that time fields are set correctly
	if mountInfo.MountedAt != now {
		t.Error("MountedAt time mismatch")
	}
	
	if mountInfo.LastAccess != now {
		t.Error("LastAccess time mismatch")
	}
}