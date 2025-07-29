package cmd

import (
	"testing"
)

func TestNewPsCommand(t *testing.T) {
	cmd := NewPsCommand()
	
	if cmd == nil {
		t.Fatal("NewPsCommand() returned nil")
	}
	
	if cmd.Use != "ps" {
		t.Errorf("Expected Use to be 'ps', got %s", cmd.Use)
	}
	
	if cmd.Short != "Show running MCP servers with detailed process information" {
		t.Errorf("Expected correct Short description, got %s", cmd.Short)
	}
	
	if cmd.RunE == nil {
		t.Error("RunE should not be nil")
	}
	
	if cmd.Long == "" {
		t.Error("Long description should not be empty")
	}
}

func TestPsCommandFlags(t *testing.T) {
	cmd := NewPsCommand()
	
	// Check that flags exist
	if !cmd.Flags().HasFlags() {
		t.Error("Command should have flags")
	}
	
	// Check specific flags
	if flag := cmd.Flags().Lookup("watch"); flag == nil {
		t.Error("Should have 'watch' flag")
	}
	
	if flag := cmd.Flags().Lookup("format"); flag == nil {
		t.Error("Should have 'format' flag")
	}
	
	if flag := cmd.Flags().Lookup("filter"); flag == nil {
		t.Error("Should have 'filter' flag")
	}
}