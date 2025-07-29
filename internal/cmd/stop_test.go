package cmd

import (
	"testing"
)

func TestNewStopCommand(t *testing.T) {
	cmd := NewStopCommand()
	
	if cmd == nil {
		t.Fatal("NewStopCommand() returned nil")
	}
	
	if cmd.Use != "stop [SERVICE...]" {
		t.Errorf("Expected Use to be 'stop [SERVICE...]', got %s", cmd.Use)
	}
	
	if cmd.RunE == nil {
		t.Error("RunE should not be nil")
	}
	
	// Stop command does manual validation in RunE
}