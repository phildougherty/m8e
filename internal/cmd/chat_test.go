package cmd

import (
	"testing"
)

func TestNewChatCommand(t *testing.T) {
	cmd := NewChatCommand()
	
	if cmd == nil {
		t.Fatal("NewChatCommand() returned nil")
	}
	
	if cmd.Use == "" {
		t.Error("Command should have a Use field")
	}
	
	if cmd.Short == "" {
		t.Error("Command should have a Short description")
	}
}