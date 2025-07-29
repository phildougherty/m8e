package cmd

import (
	"testing"
)

func TestNewLogsCommand(t *testing.T) {
	cmd := NewLogsCommand()
	
	if cmd == nil {
		t.Fatal("NewLogsCommand() returned nil")
	}
	
	if cmd.Use != "logs [SERVER...]" {
		t.Errorf("Expected Use to be 'logs [SERVER...]', got %s", cmd.Use)
	}
	
	if cmd.Short != "View logs from MCP servers" {
		t.Errorf("Expected correct Short description, got %s", cmd.Short)
	}
	
	if cmd.RunE == nil {
		t.Error("RunE should not be nil")
	}
	
	if cmd.Long == "" {
		t.Error("Long description should not be empty")
	}
}

func TestLogsCommandFlags(t *testing.T) {
	cmd := NewLogsCommand()
	
	// Check that follow flag exists
	if flag := cmd.Flags().Lookup("follow"); flag == nil {
		t.Error("Should have 'follow' flag")
	}
	
	// Check short flag - verify it's actually a shorthand
	followFlag := cmd.Flags().Lookup("follow")
	if followFlag == nil || followFlag.Shorthand != "f" {
		t.Error("Follow flag should have 'f' shorthand")
	}
}