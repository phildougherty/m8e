package cmd

import (
	"testing"
)

func TestNewStartCommand(t *testing.T) {
	cmd := NewStartCommand()
	
	if cmd == nil {
		t.Fatal("NewStartCommand() returned nil")
	}
	
	if cmd.Use != "start [SERVICE...]" {
		t.Errorf("Expected Use to be 'start [SERVICE...]', got %s", cmd.Use)
	}
	
	if cmd.Short != "Start specific MCP services using Kubernetes resources" {
		t.Errorf("Expected correct Short description, got %s", cmd.Short)
	}
	
	if cmd.RunE == nil {
		t.Error("RunE should not be nil")
	}
	
	if cmd.Long == "" {
		t.Error("Long description should not be empty")
	}
	
	if cmd.Args == nil {
		t.Error("Expected Args validation to be set")
	}
}