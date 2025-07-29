package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFindComposeFile(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Warning: Failed to restore original directory: %v", err)
		}
	}()
	
	// Change to temp directory
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}
	
	// Test absolute path
	absPath := filepath.Join(tmpDir, "test-compose.yml") 
	file, err := os.Create(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Failed to close file: %v", err)
	}
	
	result, err := FindComposeFile(absPath)
	if err != nil {
		t.Errorf("FindComposeFile with absolute path failed: %v", err)
	}
	if result != absPath {
		t.Errorf("Expected %s, got %s", absPath, result)
	}
	
	// Test file in current directory
	currentFile := "compose.yml"
	file, err = os.Create(currentFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Failed to close file: %v", err)
	}
	
	result, err = FindComposeFile(currentFile)
	if err != nil {
		t.Errorf("FindComposeFile in current directory failed: %v", err)
	}
	if result != currentFile {
		t.Errorf("Expected %s, got %s", currentFile, result)
	}
	
	// Test file in .mcp directory
	mcpDir := ".mcp"
	err = os.Mkdir(mcpDir, 0755)
	if err != nil {
		t.Fatal(err)
	}
	
	mcpFile := "mcp-compose.yml"
	mcpPath := filepath.Join(mcpDir, mcpFile)
	file, err = os.Create(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Failed to close file: %v", err)
	}
	
	result, err = FindComposeFile(mcpFile)
	if err != nil {
		t.Errorf("FindComposeFile in .mcp directory failed: %v", err)
	}
	if result != mcpPath {
		t.Errorf("Expected %s, got %s", mcpPath, result)
	}
	
	// Test non-existent file
	_, err = FindComposeFile("non-existent.yml")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' in error message, got: %v", err)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{100 * time.Millisecond, "less than a second"},
		{1500 * time.Millisecond, "1.5 seconds"},
		{30 * time.Second, "30.0 seconds"},
		{90 * time.Second, "1.5 minutes"},
		{3600 * time.Second, "1.0 hours"},
		{7200 * time.Second, "2.0 hours"},
		{25 * time.Hour, "1 days"},
		{48 * time.Hour, "2 days"},
		{72 * time.Hour, "3 days"},
	}
	
	for _, tt := range tests {
		result := FormatDuration(tt.duration)
		if result != tt.expected {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.duration, result, tt.expected)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		size     int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KiB"},
		{1536, "1.50 KiB"},
		{1024 * 1024, "1.00 MiB"},
		{1024 * 1024 * 1024, "1.00 GiB"},
		{1024 * 1024 * 1024 * 1024, "1.00 TiB"},
		{1024 * 1024 * 1024 * 1024 * 1024, "1.00 PiB"},
		{1536 * 1024, "1.50 MiB"},
		{2.5 * 1024 * 1024 * 1024, "2.50 GiB"},
	}
	
	for _, tt := range tests {
		result := FormatSize(tt.size)
		if result != tt.expected {
			t.Errorf("FormatSize(%d) = %q, want %q", tt.size, result, tt.expected)
		}
	}
}

func TestParseEnvFile(t *testing.T) {
	// Create a temporary env file
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	
	content := `# This is a comment
KEY1=value1
KEY2=value2
KEY3="quoted value"
KEY4='single quoted'
EMPTY_KEY=
# Another comment

SPACED_KEY = spaced value  
EQUALS_IN_VALUE=key=value=test
`
	
	err := os.WriteFile(envFile, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
	
	result, err := ParseEnvFile(envFile)
	if err != nil {
		t.Fatalf("ParseEnvFile failed: %v", err)
	}
	
	expected := map[string]string{
		"KEY1":             "value1",
		"KEY2":             "value2", 
		"KEY3":             "quoted value",
		"KEY4":             "single quoted",
		"EMPTY_KEY":        "",
		"SPACED_KEY":       "spaced value",
		"EQUALS_IN_VALUE":  "key=value=test",
	}
	
	if len(result) != len(expected) {
		t.Errorf("Expected %d env vars, got %d", len(expected), len(result))
	}
	
	for key, expectedValue := range expected {
		if actualValue, exists := result[key]; !exists {
			t.Errorf("Expected key %q to exist", key)
		} else if actualValue != expectedValue {
			t.Errorf("Expected %q=%q, got %q=%q", key, expectedValue, key, actualValue)
		}
	} 
}

func TestParseEnvFile_NonExistentFile(t *testing.T) {
	_, err := ParseEnvFile("/non/existent/file.env")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestParseEnvFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, "empty.env")
	
	err := os.WriteFile(envFile, []byte(""), 0644)
	if err != nil {
		t.Fatal(err)
	}
	
	result, err := ParseEnvFile(envFile)
	if err != nil {
		t.Fatalf("ParseEnvFile failed: %v", err)
	}
	
	if len(result) != 0 {
		t.Errorf("Expected empty map, got %d entries", len(result))
	}
}

func TestParseEnvFile_OnlyComments(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, "comments.env")
	
	content := `# Comment 1
# Comment 2

# Comment 3
`
	
	err := os.WriteFile(envFile, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
	
	result, err := ParseEnvFile(envFile)
	if err != nil {
		t.Fatalf("ParseEnvFile failed: %v", err)
	}
	
	if len(result) != 0 {
		t.Errorf("Expected empty map for comments-only file, got %d entries", len(result))
	}
}

func TestParseEnvFile_MalformedLines(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, "malformed.env")
	
	content := `VALID_KEY=valid_value
INVALID_LINE_NO_EQUALS
ANOTHER_VALID=another_value
=VALUE_NO_KEY
`
	
	err := os.WriteFile(envFile, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
	
	result, err := ParseEnvFile(envFile)
	if err != nil {
		t.Fatalf("ParseEnvFile failed: %v", err)
	}
	
	// Should only contain valid entries
	expected := map[string]string{
		"VALID_KEY":     "valid_value",
		"ANOTHER_VALID": "another_value",
		"":              "VALUE_NO_KEY", // Empty key with value
	}
	
	if len(result) != len(expected) {
		t.Errorf("Expected %d env vars, got %d", len(expected), len(result))
	}
	
	for key, expectedValue := range expected {
		if actualValue, exists := result[key]; !exists {
			t.Errorf("Expected key %q to exist", key)
		} else if actualValue != expectedValue {
			t.Errorf("Expected %q=%q, got %q=%q", key, expectedValue, key, actualValue)
		}
	}
}