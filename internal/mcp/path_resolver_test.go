package mcp

import (
	"os"
	"testing"
)

func TestNewPathResolver(t *testing.T) {
	// Save original env vars
	originalMateyHostPwd := os.Getenv("MATEY_HOST_PWD")
	originalHome := os.Getenv("HOME")
	originalUser := os.Getenv("USER")
	
	// Restore env vars after test
	defer func() {
		os.Setenv("MATEY_HOST_PWD", originalMateyHostPwd)
		os.Setenv("HOME", originalHome)
		os.Setenv("USER", originalUser)
	}()
	
	// Test with MATEY_HOST_PWD set
	os.Setenv("MATEY_HOST_PWD", "/test/host/dir")
	os.Setenv("HOME", "/test/home")
	
	resolver := NewPathResolver()
	
	if resolver == nil {
		t.Fatal("NewPathResolver() returned nil")
	}
	
	if resolver.HostWorkingDir != "/test/host/dir" {
		t.Errorf("Expected HostWorkingDir '/test/host/dir', got %s", resolver.HostWorkingDir)
	}
	
	if len(resolver.mappings) == 0 {
		t.Error("Expected default mappings to be created")
	}
}

func TestNewPathResolver_FallbackHome(t *testing.T) {
	// Save original env vars
	originalHome := os.Getenv("HOME")
	originalUser := os.Getenv("USER")
	
	// Restore env vars after test
	defer func() {
		os.Setenv("HOME", originalHome)
		os.Setenv("USER", originalUser)
	}()
	
	// Test with no HOME but with USER
	os.Unsetenv("HOME")
	os.Setenv("USER", "testuser")
	
	resolver := NewPathResolver()
	
	// Check that mappings use fallback home directory
	found := false
	for _, mapping := range resolver.mappings {
		if mapping.HostPath == "/home/testuser" {
			found = true
			break
		}
	}
	
	if !found {
		t.Error("Expected fallback home directory '/home/testuser' in mappings")
	}
}

func TestNewPathResolver_UltimateFallback(t *testing.T) {
	// Save original env vars
	originalHome := os.Getenv("HOME")
	originalUser := os.Getenv("USER")
	
	// Restore env vars after test
	defer func() {
		os.Setenv("HOME", originalHome)
		os.Setenv("USER", originalUser)
	}()
	
	// Test with no HOME and no USER
	os.Unsetenv("HOME")
	os.Unsetenv("USER")
	
	resolver := NewPathResolver()
	
	// Check that mappings use ultimate fallback
	found := false
	for _, mapping := range resolver.mappings {
		if mapping.HostPath == "/tmp" {
			found = true
			break
		}
	}
	
	if !found {
		t.Error("Expected ultimate fallback '/tmp' in mappings")
	}
}

func TestResolveHostToContainer(t *testing.T) {
	resolver := &PathResolver{
		HostWorkingDir: "/test/host/wd",
		mappings: []PathMapping{
			{
				HostPath:      "/test/home",
				ContainerPath: "/workspace",
				ServerName:    "filesystem",
			},
		},
	}
	
	tests := []struct {
		name       string
		hostPath   string
		serverName string
		expected   string
	}{
		{
			name:       "absolute path with mapping",
			hostPath:   "/test/home/project/file.txt",
			serverName: "filesystem",
			expected:   "/workspace/project/file.txt",
		},
		{
			name:       "relative path",
			hostPath:   "relative/file.txt",
			serverName: "filesystem",
			expected:   "/workspace/test/host/wd/relative/file.txt",
		},
		{
			name:       "already container path",
			hostPath:   "/workspace/file.txt",
			serverName: "filesystem",
			expected:   "/workspace/file.txt",
		},
		{
			name:       "projects container path",
			hostPath:   "/projects/file.txt",
			serverName: "filesystem",
			expected:   "/projects/file.txt",
		},
		{
			name:       "no mapping found",
			hostPath:   "/other/path/file.txt",
			serverName: "filesystem",
			expected:   "/other/path/file.txt",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.ResolveHostToContainer(tt.hostPath, tt.serverName)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestResolveContainerToHost(t *testing.T) {
	resolver := &PathResolver{
		mappings: []PathMapping{
			{
				HostPath:      "/test/home",
				ContainerPath: "/workspace",
				ServerName:    "filesystem",
			},
			{
				HostPath:      "/test/home",
				ContainerPath: "/projects",
				ServerName:    "playwright",
			},
		},
	}
	
	tests := []struct {
		name          string
		containerPath string
		expected      string
	}{
		{
			name:          "workspace mapping",
			containerPath: "/workspace/project/file.txt",
			expected:      "/test/home/project/file.txt",
		},
		{
			name:          "projects mapping",
			containerPath: "/projects/browser/test.js",
			expected:      "/test/home/browser/test.js",
		},
		{
			name:          "no mapping found",
			containerPath: "/other/path/file.txt",
			expected:      "/other/path/file.txt",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.ResolveContainerToHost(tt.containerPath)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestGetCurrentWorkingDir(t *testing.T) {
	resolver := &PathResolver{
		HostWorkingDir: "/test/working/dir",
	}
	
	result := resolver.GetCurrentWorkingDir()
	if result != "/test/working/dir" {
		t.Errorf("Expected '/test/working/dir', got %s", result)
	}
}

func TestResolveWorkingDir(t *testing.T) {
	resolver := &PathResolver{
		HostWorkingDir: "/test/host/wd",
	}
	
	tests := []struct {
		name       string
		workingDir string
		expected   string
	}{
		{
			name:       "empty working dir",
			workingDir: "",
			expected:   "/test/host/wd",
		},
		{
			name:       "absolute path",
			workingDir: "/absolute/path",
			expected:   "/absolute/path",
		},
		{
			name:       "relative path",
			workingDir: "relative/path",
			expected:   "/test/host/wd/relative/path",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.ResolveWorkingDir(tt.workingDir)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestGetContextInfo(t *testing.T) {
	resolver := &PathResolver{
		HostWorkingDir:      "/test/host/wd",
		ContainerWorkingDir: "/test/container/wd",
		mappings: []PathMapping{
			{
				HostPath:      "/test/home",
				ContainerPath: "/workspace",
				ServerName:    "filesystem",
			},
		},
	}
	
	contextInfo := resolver.GetContextInfo()
	
	if contextInfo == nil {
		t.Fatal("GetContextInfo() returned nil")
	}
	
	if contextInfo["current_working_directory"] != "/test/host/wd" {
		t.Errorf("Expected current_working_directory '/test/host/wd', got %v", contextInfo["current_working_directory"])
	}
	
	if contextInfo["container_working_directory"] != "/test/container/wd" {
		t.Errorf("Expected container_working_directory '/test/container/wd', got %v", contextInfo["container_working_directory"])
	}
	
	// Check that path_mappings exists
	if _, ok := contextInfo["path_mappings"]; !ok {
		t.Error("Expected path_mappings in context info")
	}
	
	// Check that guidance exists
	if _, ok := contextInfo["guidance"]; !ok {
		t.Error("Expected guidance in context info")
	}
	
	// Check that important_notes exists
	if _, ok := contextInfo["important_notes"]; !ok {
		t.Error("Expected important_notes in context info")
	}
}

func TestAddMapping(t *testing.T) {
	resolver := &PathResolver{
		mappings: []PathMapping{},
	}
	
	originalCount := len(resolver.mappings)
	
	resolver.AddMapping("/test/host", "/test/container", "test-server")
	
	if len(resolver.mappings) != originalCount+1 {
		t.Errorf("Expected %d mappings, got %d", originalCount+1, len(resolver.mappings))
	}
	
	lastMapping := resolver.mappings[len(resolver.mappings)-1]
	if lastMapping.HostPath != "/test/host" {
		t.Errorf("Expected HostPath '/test/host', got %s", lastMapping.HostPath)
	}
	
	if lastMapping.ContainerPath != "/test/container" {
		t.Errorf("Expected ContainerPath '/test/container', got %s", lastMapping.ContainerPath)
	}
	
	if lastMapping.ServerName != "test-server" {
		t.Errorf("Expected ServerName 'test-server', got %s", lastMapping.ServerName)
	}
}