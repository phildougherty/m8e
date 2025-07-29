package mcp

import (
	"testing"
)

func TestGetBoolArg(t *testing.T) {
	tests := []struct {
		name         string
		args         map[string]interface{}
		key          string
		defaultValue bool
		expected     bool
	}{
		{
			name:         "existing true bool",
			args:         map[string]interface{}{"test": true},
			key:          "test",
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "existing false bool",
			args:         map[string]interface{}{"test": false},
			key:          "test",
			defaultValue: true,
			expected:     false,
		},
		{
			name:         "missing key uses default",
			args:         map[string]interface{}{},
			key:          "test",
			defaultValue: true,
			expected:     true,
		},
		{
			name:         "wrong type uses default",
			args:         map[string]interface{}{"test": "not a bool"},
			key:          "test",
			defaultValue: true,
			expected:     true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getBoolArg(tt.args, tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetIntArg(t *testing.T) {
	tests := []struct {
		name         string
		args         map[string]interface{}
		key          string
		defaultValue int
		expected     int
	}{
		{
			name:         "existing int",
			args:         map[string]interface{}{"test": 42},
			key:          "test",
			defaultValue: 0,
			expected:     42,
		},
		{
			name:         "existing float64",
			args:         map[string]interface{}{"test": 42.0},
			key:          "test",
			defaultValue: 0,
			expected:     42,
		},
		{
			name:         "missing key uses default",
			args:         map[string]interface{}{},
			key:          "test",
			defaultValue: 100,
			expected:     100,
		},
		{
			name:         "wrong type uses default",
			args:         map[string]interface{}{"test": "not a number"},
			key:          "test",
			defaultValue: 100,
			expected:     100,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIntArg(tt.args, tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetStringArg(t *testing.T) {
	tests := []struct {
		name         string
		args         map[string]interface{}
		key          string
		defaultValue string
		expected     string
	}{
		{
			name:         "existing string",
			args:         map[string]interface{}{"test": "hello"},
			key:          "test",
			defaultValue: "default",
			expected:     "hello",
		},
		{
			name:         "missing key uses default",
			args:         map[string]interface{}{},
			key:          "test",
			defaultValue: "default",
			expected:     "default",
		},
		{
			name:         "wrong type uses default",
			args:         map[string]interface{}{"test": 123},
			key:          "test",
			defaultValue: "default",
			expected:     "default",
		},
		{
			name:         "empty string",
			args:         map[string]interface{}{"test": ""},
			key:          "test",
			defaultValue: "default",
			expected:     "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStringArg(tt.args, tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestValidateCommand(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		shouldError bool
	}{
		{
			name:        "safe command",
			command:     "ls -la",
			shouldError: false,
		},
		{
			name:        "safe git command",
			command:     "git status",
			shouldError: false,
		},
		{
			name:        "dangerous rm command",
			command:     "rm -rf /",
			shouldError: true,
		},
		{
			name:        "fork bomb",
			command:     ":(){ :|:& };:",
			shouldError: true,
		},
		{
			name:        "safe echo command",
			command:     "echo hello world",
			shouldError: false,
		},
		{
			name:        "empty command",
			command:     "",
			shouldError: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCommand(tt.command)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error for command %s, but got none", tt.command)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error for command %s, but got: %v", tt.command, err)
			}
		})
	}
}