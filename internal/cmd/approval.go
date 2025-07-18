package cmd

import (
	"fmt"
	"strings"
)

// ApprovalMode controls how autonomous the agent should be
type ApprovalMode int

const (
	// DEFAULT mode requires manual confirmation for all actions
	DEFAULT ApprovalMode = iota
	// AUTO_EDIT mode auto-approves file edits and safe operations
	AUTO_EDIT
	// YOLO mode auto-approves everything (most autonomous)
	YOLO
)

// String returns the string representation of the approval mode
func (a ApprovalMode) String() string {
	switch a {
	case DEFAULT:
		return "DEFAULT"
	case AUTO_EDIT:
		return "AUTO_EDIT"
	case YOLO:
		return "YOLO"
	default:
		return "UNKNOWN"
	}
}

// Description returns a human-readable description of the approval mode
func (a ApprovalMode) Description() string {
	switch a {
	case DEFAULT:
		return "Manual confirmation for all actions"
	case AUTO_EDIT:
		return "Auto-approve file edits and safe operations"
	case YOLO:
		return "Auto-approve everything (full autonomy)"
	default:
		return "Unknown approval mode"
	}
}

// ParseApprovalMode parses a string into an ApprovalMode
func ParseApprovalMode(s string) (ApprovalMode, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEFAULT", "":
		return DEFAULT, nil
	case "AUTO_EDIT", "AUTOEDIT", "AUTO-EDIT":
		return AUTO_EDIT, nil
	case "YOLO":
		return YOLO, nil
	default:
		return DEFAULT, fmt.Errorf("invalid approval mode: %s", s)
	}
}

// ShouldConfirm returns whether the given operation should require confirmation
func (a ApprovalMode) ShouldConfirm(operation string) bool {
	switch a {
	case YOLO:
		return false // Never confirm, always auto-approve
	case AUTO_EDIT:
		// Auto-approve safe read operations (no confirmation needed)
		safeOperations := []string{
			"list_workflows", "get_workflow", "workflow_logs", "workflow_templates",
			"list_mcp_servers", "matey_ps", "config_get", "config_list", 
			"memory_retrieve", "memory_list", "task_list", "task_status",
			"toolbox_list", "get_cluster_state", "memory_status", "task_scheduler_status",
			"list_toolboxes", "memory_start", "task_scheduler_start", "matey_up",
			"matey_down", "start_service", "stop_service", "reload_proxy",
		}
		
		// Auto-approve common edit operations (the core of AUTO_EDIT mode)
		editOperations := []string{
			"create_workflow", "execute_workflow", "pause_workflow", "resume_workflow",
			"memory_store", "memory_delete", "config_set", "task_create",
			"add_mcp_server", "remove_mcp_server", "toolbox_install", "toolbox_remove",
		}
		
		// Check against safe operations first
		for _, safe := range safeOperations {
			if strings.Contains(operation, safe) {
				return false // Auto-approve
			}
		}
		
		// Check against edit operations
		for _, edit := range editOperations {
			if strings.Contains(operation, edit) {
				return false // Auto-approve
			}
		}
		
		// Only confirm for truly dangerous operations (like delete_workflow)
		dangerousOperations := []string{
			"delete_workflow", "delete_mcp_server", "delete_config",
		}
		
		for _, dangerous := range dangerousOperations {
			if strings.Contains(operation, dangerous) {
				return true // Require confirmation
			}
		}
		
		return false // Default to auto-approve in AUTO_EDIT mode
	case DEFAULT:
		return true // Always confirm
	default:
		return true // Default to safe behavior
	}
}

// GetModeIndicator returns a visual indicator for the current approval mode
func (a ApprovalMode) GetModeIndicator() string {
	switch a {
	case DEFAULT:
		return "🔒 MANUAL"
	case AUTO_EDIT:
		return "🔧 AUTO-EDIT"
	case YOLO:
		return "🚀 YOLO"
	default:
		return "❓ UNKNOWN"
	}
}

// GetModeIndicatorNoEmoji returns a visual indicator without emojis
func (a ApprovalMode) GetModeIndicatorNoEmoji() string {
	switch a {
	case DEFAULT:
		return "[MANUAL]"
	case AUTO_EDIT:
		return "[AUTO-EDIT]"
	case YOLO:
		return "[YOLO]"
	default:
		return "[UNKNOWN]"
	}
}