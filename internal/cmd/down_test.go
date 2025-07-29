package cmd

import (
	"testing"
)

func TestNewDownCommand(t *testing.T) {
	cmd := NewDownCommand()
	
	if cmd == nil {
		t.Fatal("NewDownCommand() returned nil")
	}
	
	if cmd.Use != "down [SERVICE...]" {
		t.Errorf("Expected Use to be 'down [SERVICE...]', got %s", cmd.Use)
	}
	
	if cmd.Short != "Stop and remove MCP services and their Kubernetes resources" {
		t.Errorf("Expected correct Short description, got %s", cmd.Short)
	}
	
	if cmd.RunE == nil {
		t.Error("RunE should not be nil")
	}
	
	if cmd.Long == "" {
		t.Error("Long description should not be empty")
	}
}