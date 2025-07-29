package cmd

import (
	"testing"
)

func TestNewUpCommand(t *testing.T) {
	cmd := NewUpCommand()
	
	if cmd == nil {
		t.Fatal("NewUpCommand() returned nil")
	}
	
	if cmd.Use != "up [SERVICE...]" {
		t.Errorf("Expected Use to be 'up [SERVICE...]', got %s", cmd.Use)
	}
	
	if cmd.Short != "Create and start MCP services using Kubernetes resources" {
		t.Errorf("Expected correct Short description, got %s", cmd.Short)
	}
	
	if cmd.RunE == nil {
		t.Error("RunE should not be nil")
	}
	
	if cmd.Long == "" {
		t.Error("Long description should not be empty")
	}
}