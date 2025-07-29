package mcp

import (
	"os"
	"testing"
)

func TestNewFilesystemTools(t *testing.T) {
	// Test with custom working directory
	customDir := "/test/custom/dir"
	tools := NewFilesystemTools(customDir)
	
	if tools == nil {
		t.Fatal("NewFilesystemTools() returned nil")
	}
	
	if tools.workingDirectory != customDir {
		t.Errorf("Expected workingDirectory %s, got %s", customDir, tools.workingDirectory)
	}
}

func TestNewFilesystemTools_EmptyWorkingDirectory(t *testing.T) {
	// Test with empty working directory (should use current working directory)
	tools := NewFilesystemTools("")
	
	if tools == nil {
		t.Fatal("NewFilesystemTools() returned nil")
	}
	
	// Should have some working directory set (current directory)
	if tools.workingDirectory == "" {
		t.Error("Expected workingDirectory to be set when empty string provided")
	}
	
	// Verify it's a valid directory path
	currentDir, _ := os.Getwd()
	if tools.workingDirectory != currentDir {
		t.Errorf("Expected workingDirectory to be current directory %s, got %s", currentDir, tools.workingDirectory)
	}
}

func TestBasicReadFileRequest_Struct(t *testing.T) {
	req := BasicReadFileRequest{
		AbsolutePath: "/path/to/file.txt",
	}
	
	if req.AbsolutePath != "/path/to/file.txt" {
		t.Errorf("Expected AbsolutePath '/path/to/file.txt', got %s", req.AbsolutePath)
	}
}