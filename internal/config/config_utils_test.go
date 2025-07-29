package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/constants"
)

func TestTimeoutConfig_GetConnectTimeout(t *testing.T) {
	tests := []struct {
		name     string
		config   TimeoutConfig
		expected time.Duration
	}{
		{
			name:     "valid timeout",
			config:   TimeoutConfig{Connect: "30s"},
			expected: 30 * time.Second,
		},
		{
			name:     "invalid timeout",
			config:   TimeoutConfig{Connect: "invalid"},
			expected: constants.DefaultConnectTimeout,
		},
		{
			name:     "empty timeout",
			config:   TimeoutConfig{Connect: ""},
			expected: constants.DefaultConnectTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetConnectTimeout()
			if result != tt.expected {
				t.Errorf("GetConnectTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTimeoutConfig_GetReadTimeout(t *testing.T) {
	tests := []struct {
		name     string
		config   TimeoutConfig
		expected time.Duration
	}{
		{
			name:     "valid timeout",
			config:   TimeoutConfig{Read: "45s"},
			expected: 45 * time.Second,
		},
		{
			name:     "invalid timeout",
			config:   TimeoutConfig{Read: "invalid"},
			expected: constants.DefaultReadTimeout,
		},
		{
			name:     "empty timeout",
			config:   TimeoutConfig{Read: ""},
			expected: constants.DefaultReadTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetReadTimeout()
			if result != tt.expected {
				t.Errorf("GetReadTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTimeoutConfig_GetWriteTimeout(t *testing.T) {
	tests := []struct {
		name     string
		config   TimeoutConfig
		expected time.Duration
	}{
		{
			name:     "valid timeout",
			config:   TimeoutConfig{Write: "1m"},
			expected: 1 * time.Minute,
		},
		{
			name:     "invalid timeout",
			config:   TimeoutConfig{Write: "invalid"},
			expected: constants.DefaultReadTimeout,
		},
		{
			name:     "empty timeout",
			config:   TimeoutConfig{Write: ""},
			expected: constants.DefaultReadTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetWriteTimeout()
			if result != tt.expected {
				t.Errorf("GetWriteTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTimeoutConfig_GetIdleTimeout(t *testing.T) {
	tests := []struct {
		name     string
		config   TimeoutConfig
		expected time.Duration
	}{
		{
			name:     "valid timeout",
			config:   TimeoutConfig{Idle: "2m"},
			expected: 2 * time.Minute,
		},
		{
			name:     "invalid timeout",
			config:   TimeoutConfig{Idle: "invalid"},
			expected: constants.DefaultProtoTimeout,
		},
		{
			name:     "empty timeout",
			config:   TimeoutConfig{Idle: ""},
			expected: constants.DefaultProtoTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetIdleTimeout()
			if result != tt.expected {
				t.Errorf("GetIdleTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTimeoutConfig_GetHealthCheckTimeout(t *testing.T) {
	tests := []struct {
		name     string
		config   TimeoutConfig
		expected time.Duration
	}{
		{
			name:     "valid timeout",
			config:   TimeoutConfig{HealthCheck: "10s"},
			expected: 10 * time.Second,
		},
		{
			name:     "invalid timeout",
			config:   TimeoutConfig{HealthCheck: "invalid"},
			expected: constants.DefaultHealthTimeout,
		},
		{
			name:     "empty timeout",
			config:   TimeoutConfig{HealthCheck: ""},
			expected: constants.DefaultHealthTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetHealthCheckTimeout()
			if result != tt.expected {
				t.Errorf("GetHealthCheckTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTimeoutConfig_GetShutdownTimeout(t *testing.T) {
	tests := []struct {
		name     string
		config   TimeoutConfig
		expected time.Duration
	}{
		{
			name:     "valid timeout",
			config:   TimeoutConfig{Shutdown: "15s"},
			expected: 15 * time.Second,
		},
		{
			name:     "invalid timeout",
			config:   TimeoutConfig{Shutdown: "invalid"},
			expected: constants.DefaultReadTimeout,
		},
		{
			name:     "empty timeout",
			config:   TimeoutConfig{Shutdown: ""},
			expected: constants.DefaultReadTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetShutdownTimeout()
			if result != tt.expected {
				t.Errorf("GetShutdownTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTimeoutConfig_GetLifecycleHookTimeout(t *testing.T) {
	tests := []struct {
		name     string
		config   TimeoutConfig
		expected time.Duration
	}{
		{
			name:     "valid timeout",
			config:   TimeoutConfig{LifecycleHook: "20s"},
			expected: 20 * time.Second,
		},
		{
			name:     "invalid timeout",
			config:   TimeoutConfig{LifecycleHook: "invalid"},
			expected: constants.DefaultReadTimeout,
		},
		{
			name:     "empty timeout",
			config:   TimeoutConfig{LifecycleHook: ""},
			expected: constants.DefaultReadTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetLifecycleHookTimeout()
			if result != tt.expected {
				t.Errorf("GetLifecycleHookTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetProjectName(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected string
	}{
		{
			name:     "absolute path",
			filePath: "/home/user/myproject/config.yaml",
			expected: "myproject",
		},
		{
			name:     "relative path with directory",
			filePath: "testdata/project/config.yaml",
			expected: "project",
		},
		{
			name:     "current directory",
			filePath: "./config.yaml",
			expected: filepath.Base(getCurrentWorkingDir()),
		},
		{
			name:     "just filename",
			filePath: "config.yaml",
			expected: filepath.Base(getCurrentWorkingDir()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetProjectName(tt.filePath)
			if result != tt.expected {
				t.Errorf("GetProjectName(%q) = %q, want %q", tt.filePath, result, tt.expected)
			}
		})
	}
}

func TestIsCapabilityEnabled(t *testing.T) {
	tests := []struct {
		name       string
		server     ServerConfig
		capability string
		expected   bool
	}{
		{
			name: "capability exists",
			server: ServerConfig{
				Capabilities: []string{"tools", "resources", "prompts"},
			},
			capability: "tools",
			expected:   true,
		},
		{
			name: "capability does not exist",
			server: ServerConfig{
				Capabilities: []string{"tools", "resources"},
			},
			capability: "prompts",
			expected:   false,
		},
		{
			name: "empty capabilities",
			server: ServerConfig{
				Capabilities: []string{},
			},
			capability: "tools",
			expected:   false,
		},
		{
			name: "nil capabilities",
			server: ServerConfig{
				Capabilities: nil,
			},
			capability: "tools",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCapabilityEnabled(tt.server, tt.capability)
			if result != tt.expected {
				t.Errorf("IsCapabilityEnabled() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMergeEnv(t *testing.T) {
	tests := []struct {
		name      string
		serverEnv map[string]string
		extraEnv  map[string]string
		expected  map[string]string
	}{
		{
			name: "merge environments",
			serverEnv: map[string]string{
				"ENV1": "value1",
				"ENV2": "value2",
			},
			extraEnv: map[string]string{
				"ENV3": "value3",
				"ENV4": "value4",
			},
			expected: map[string]string{
				"ENV1": "value1",
				"ENV2": "value2",
				"ENV3": "value3",
				"ENV4": "value4",
			},
		},
		{
			name: "override environment variables",
			serverEnv: map[string]string{
				"ENV1": "old_value",
				"ENV2": "value2",
			},
			extraEnv: map[string]string{
				"ENV1": "new_value",
				"ENV3": "value3",
			},
			expected: map[string]string{
				"ENV1": "new_value",
				"ENV2": "value2",
				"ENV3": "value3",
			},
		},
		{
			name:      "empty server env",
			serverEnv: map[string]string{},
			extraEnv: map[string]string{
				"ENV1": "value1",
			},
			expected: map[string]string{
				"ENV1": "value1",
			},
		},
		{
			name: "empty extra env",
			serverEnv: map[string]string{
				"ENV1": "value1",
			},
			extraEnv: map[string]string{},
			expected: map[string]string{
				"ENV1": "value1",
			},
		},
		{
			name:      "both empty",
			serverEnv: map[string]string{},
			extraEnv:  map[string]string{},
			expected:  map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeEnv(tt.serverEnv, tt.extraEnv)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("MergeEnv() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestConvertToEnvList(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected []string
	}{
		{
			name: "multiple environment variables",
			env: map[string]string{
				"ENV1": "value1",
				"ENV2": "value2",
				"ENV3": "value3",
			},
			expected: []string{"ENV1=value1", "ENV2=value2", "ENV3=value3"},
		},
		{
			name: "single environment variable",
			env: map[string]string{
				"PATH": "/usr/bin",
			},
			expected: []string{"PATH=/usr/bin"},
		},
		{
			name:     "empty environment",
			env:      map[string]string{},
			expected: []string{},
		},
		{
			name: "environment with special characters",
			env: map[string]string{
				"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
				"API_TOKEN":    "abc123-xyz789",
			},
			expected: []string{"DATABASE_URL=postgres://user:pass@localhost:5432/db", "API_TOKEN=abc123-xyz789"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertToEnvList(tt.env)
			
			// Since map iteration order is not guaranteed, we need to check if all expected elements are present
			if len(result) != len(tt.expected) {
				t.Errorf("ConvertToEnvList() length = %v, want %v", len(result), len(tt.expected))
				return
			}
			
			// Convert to maps for easier comparison
			resultMap := make(map[string]bool)
			expectedMap := make(map[string]bool)
			
			for _, item := range result {
				resultMap[item] = true
			}
			for _, item := range tt.expected {
				expectedMap[item] = true
			}
			
			if !reflect.DeepEqual(resultMap, expectedMap) {
				t.Errorf("ConvertToEnvList() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestComposeConfig_GetRegistryImage(t *testing.T) {
	tests := []struct {
		name      string
		config    ComposeConfig
		imageName string
		expected  string
	}{
		{
			name: "no registry configured",
			config: ComposeConfig{
				Registry: RegistryConfig{URL: ""},
			},
			imageName: "myapp:latest",
			expected:  "myapp:latest",
		},
		{
			name: "add registry prefix",
			config: ComposeConfig{
				Registry: RegistryConfig{URL: "localhost:5000"},
			},
			imageName: "myapp:latest",
			expected:  "localhost:5000/myapp:latest",
		},
		{
			name: "image already has registry prefix",
			config: ComposeConfig{
				Registry: RegistryConfig{URL: "localhost:5000"},
			},
			imageName: "localhost:5000/myapp:latest",
			expected:  "localhost:5000/myapp:latest",
		},
		{
			name: "image has different registry without port",
			config: ComposeConfig{
				Registry: RegistryConfig{URL: "localhost:5000"},
			},
			imageName: "docker.io/library/nginx:latest",
			expected:  "localhost:5000/docker.io/library/nginx:latest",
		},
		{
			name: "image has different registry with port",
			config: ComposeConfig{
				Registry: RegistryConfig{URL: "localhost:5000"},
			},
			imageName: "registry.example.com:443/library/nginx:latest",
			expected:  "registry.example.com:443/library/nginx:latest",
		},
		{
			name: "image with namespace but no registry",
			config: ComposeConfig{
				Registry: RegistryConfig{URL: "localhost:5000"},
			},
			imageName: "library/nginx:latest",
			expected:  "localhost:5000/library/nginx:latest",
		},
		{
			name: "registry with path",
			config: ComposeConfig{
				Registry: RegistryConfig{URL: "registry.example.com"},
			},
			imageName: "myapp:v1.0",
			expected:  "registry.example.com/myapp:v1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetRegistryImage(tt.imageName)
			if result != tt.expected {
				t.Errorf("GetRegistryImage(%q) = %q, want %q", tt.imageName, result, tt.expected)
			}
		})
	}
}

func TestSaveConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    *ComposeConfig
		expectErr bool
	}{
		{
			name: "valid config",
			config: &ComposeConfig{
				Version: "1",
				Servers: map[string]ServerConfig{
					"test": {
						Protocol: "stdio",
						Command:  "echo hello",
					},
				},
			},
			expectErr: false,
		},
		{
			name: "empty config",
			config: &ComposeConfig{
				Version: "1",
				Servers: map[string]ServerConfig{},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpFile, err := os.CreateTemp("", "config_save_test_*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("Failed to close temp file: %v", err)
			}
			defer func() {
				if err := os.Remove(tmpFile.Name()); err != nil {
					t.Logf("Failed to remove temp file: %v", err)
				}
			}()

			// Test SaveConfig
			err = SaveConfig(tmpFile.Name(), tt.config)
			if tt.expectErr && err == nil {
				t.Errorf("SaveConfig() expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("SaveConfig() expected no error but got: %v", err)
			}

			// If no error, verify file was created and has content
			if !tt.expectErr {
				if _, err := os.Stat(tmpFile.Name()); os.IsNotExist(err) {
					t.Errorf("SaveConfig() did not create file")
				}
			}
		})
	}
}

func TestToYAML(t *testing.T) {
	tests := []struct {
		name      string
		config    *ComposeConfig
		expectErr bool
	}{
		{
			name: "valid config",
			config: &ComposeConfig{
				Version: "1",
				Servers: map[string]ServerConfig{
					"test": {
						Protocol: "stdio",
						Command:  "echo hello",
					},
				},
			},
			expectErr: false,
		},
		{
			name: "config with complex structures",
			config: &ComposeConfig{
				Version: "1",
				OAuth: &OAuthConfig{
					Enabled: true,
					Issuer:  "https://oauth.example.com",
				},
				Servers: map[string]ServerConfig{},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ToYAML(tt.config)
			if tt.expectErr && err == nil {
				t.Errorf("ToYAML() expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("ToYAML() expected no error but got: %v", err)
			}
			if !tt.expectErr && result == "" {
				t.Errorf("ToYAML() returned empty string")
			}
			if !tt.expectErr && result != "" {
				// Basic check that it's valid YAML-like content
				if !contains(result, "version:") {
					t.Errorf("ToYAML() result doesn't contain expected content")
				}
			}
		})
	}
}

// Helper function to get current working directory for testing
func getCurrentWorkingDir() string {
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "unknown"
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsInMiddle(s, substr)))
}

func containsInMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}