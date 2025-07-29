package cmd

import (
	"testing"
)

func TestNewValidateCommand(t *testing.T) {
	cmd := NewValidateCommand()
	
	if cmd == nil {
		t.Fatal("NewValidateCommand() returned nil")
	}
	
	if cmd.Use != "validate" {
		t.Errorf("Expected Use to be 'validate', got %s", cmd.Use)
	}
	
	if cmd.Short != "Validate the compose file" {
		t.Errorf("Expected Short to be 'Validate the compose file', got %s", cmd.Short)
	}
	
	if cmd.RunE == nil {
		t.Error("RunE should not be nil")
	}
}