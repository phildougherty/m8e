package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestLogLevel_String(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{DEBUG, "DEBUG"},
		{INFO, "INFO"},
		{WARNING, "WARNING"},
		{ERROR, "ERROR"},
		{FATAL, "FATAL"},
		{LogLevel(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := tt.level.String(); got != tt.expected {
			t.Errorf("LogLevel.String() = %v, want %v", got, tt.expected)
		}
	}
}

func TestNewLogger(t *testing.T) {
	tests := []struct {
		level    string
		expected LogLevel
	}{
		{"DEBUG", DEBUG},
		{"debug", DEBUG},
		{"INFO", INFO},
		{"info", INFO},
		{"WARNING", WARNING},
		{"warning", WARNING},
		{"ERROR", ERROR},
		{"error", ERROR},
		{"FATAL", FATAL},
		{"fatal", FATAL},
		{"invalid", INFO}, // defaults to INFO
		{"", INFO},        // defaults to INFO
	}

	for _, tt := range tests {
		logger := NewLogger(tt.level)
		if logger.level != tt.expected {
			t.Errorf("NewLogger(%q) level = %v, want %v", tt.level, logger.level, tt.expected)
		}
	}
}

func TestLogger_SetOutput(t *testing.T) {
	logger := NewLogger("INFO")
	buf := &bytes.Buffer{}
	
	logger.SetOutput(buf)
	logger.Info("test message")
	
	if buf.Len() == 0 {
		t.Error("Expected output to be written to buffer")
	}
	
	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("Expected output to contain 'test message', got %q", output)
	}
}

func TestLogger_SetJSONFormat(t *testing.T) {
	logger := NewLogger("INFO")
	buf := &bytes.Buffer{}
	logger.SetOutput(buf)
	
	// Test text format (default)
	logger.Info("test message")
	textOutput := buf.String()
	if strings.Contains(textOutput, `"message"`) {
		t.Error("Text format should not contain JSON-style message field")
	}
	
	// Test JSON format
	buf.Reset()
	logger.SetJSONFormat(true)
	logger.Info("test message")
	jsonOutput := buf.String()
	if !strings.Contains(jsonOutput, `"message":"test message"`) {
		t.Errorf("JSON format should contain JSON-style message field, got %q", jsonOutput)
	}
}

func TestLogger_ShouldLog(t *testing.T) {
	logger := NewLogger("WARNING")
	
	tests := []struct {
		level    LogLevel
		expected bool
	}{
		{DEBUG, false},
		{INFO, false},
		{WARNING, true},
		{ERROR, true},
		{FATAL, true},
	}
	
	for _, tt := range tests {
		if got := logger.shouldLog(tt.level); got != tt.expected {
			t.Errorf("shouldLog(%v) = %v, want %v", tt.level, got, tt.expected)
		}
	}
}

func TestLogger_LogMethods(t *testing.T) {
	logger := NewLogger("DEBUG")
	buf := &bytes.Buffer{}
	logger.SetOutput(buf)
	
	tests := []struct {
		method   func(string, ...interface{})
		level    string
		message  string
	}{
		{logger.Debug, "DEBUG", "debug message"},
		{logger.Info, "INFO", "info message"},
		{logger.Warning, "WARNING", "warning message"},
		{logger.Error, "ERROR", "error message"},
	}
	
	for _, tt := range tests {
		buf.Reset()
		tt.method(tt.message)
		
		output := buf.String()
		if !strings.Contains(output, tt.level) {
			t.Errorf("Expected output to contain level %q, got %q", tt.level, output)
		}
		if !strings.Contains(output, tt.message) {
			t.Errorf("Expected output to contain message %q, got %q", tt.message, output)
		}
	}
}

func TestLogger_LogWithFormat(t *testing.T) {
	logger := NewLogger("INFO")
	buf := &bytes.Buffer{}
	logger.SetOutput(buf)
	
	logger.Info("Hello %s, you have %d messages", "Alice", 5)
	
	output := buf.String()
	expected := "Hello Alice, you have 5 messages"
	if !strings.Contains(output, expected) {
		t.Errorf("Expected formatted message %q, got %q", expected, output)
	}
}

func TestLogger_WithFields(t *testing.T) {
	logger := NewLogger("INFO")
	buf := &bytes.Buffer{}
	logger.SetOutput(buf)
	
	fields := map[string]interface{}{
		"user":    "alice",
		"action":  "login",
		"success": true,
	}
	
	fieldLogger := logger.WithFields(fields)
	if fieldLogger == nil {
		t.Fatal("WithFields should return a FieldLogger")
	}
	
	// Test text format with fields
	fieldLogger.Info("User action completed")
	output := buf.String()
	
	if !strings.Contains(output, "User action completed") {
		t.Error("Output should contain the log message")
	}
	if !strings.Contains(output, "user=alice") {
		t.Error("Output should contain user field")
	}
	if !strings.Contains(output, "action=login") {
		t.Error("Output should contain action field")
	}
}

func TestFieldLogger_JSONFormat(t *testing.T) {
	logger := NewLogger("INFO")
	logger.SetJSONFormat(true)
	buf := &bytes.Buffer{}
	logger.SetOutput(buf)
	
	fields := map[string]interface{}{
		"user": "alice",
		"count": 42,
	}
	
	fieldLogger := logger.WithFields(fields)
	fieldLogger.Info("Test message")
	
	output := buf.String()
	if !strings.Contains(output, `"user":"alice"`) {
		t.Error("JSON output should contain user field")
	}
	if !strings.Contains(output, `"count":"42"`) {
		t.Error("JSON output should contain count field")
	}
	if !strings.Contains(output, `"message":"Test message"`) {
		t.Error("JSON output should contain message field")
	}
}

func TestFieldLogger_LogLevels(t *testing.T) {
	logger := NewLogger("DEBUG")
	buf := &bytes.Buffer{}
	logger.SetOutput(buf)
	
	fields := map[string]interface{}{"test": "value"}
	fieldLogger := logger.WithFields(fields)
	
	tests := []struct {
		method func(string, ...interface{})
		level  string
	}{
		{fieldLogger.Debug, "DEBUG"},
		{fieldLogger.Info, "INFO"},
		{fieldLogger.Warning, "WARNING"},
		{fieldLogger.Error, "ERROR"},
	}
	
	for _, tt := range tests {
		buf.Reset()
		tt.method("test message")
		
		output := buf.String()
		if !strings.Contains(output, tt.level) {
			t.Errorf("Expected output to contain level %q, got %q", tt.level, output)
		}
		if !strings.Contains(output, "test=value") {
			t.Error("Expected output to contain field")
		}
	}
}

func TestLogrSink_Enabled(t *testing.T) {
	logger := NewLogger("INFO")
	logrLogger := logger.GetLogr()
	
	// Test that info level (0) is enabled
	if !logrLogger.Enabled() {
		t.Error("INFO level should be enabled")
	}
	
	// Create a DEBUG level logger
	debugLogger := NewLogger("DEBUG")
	debugLogrLogger := debugLogger.GetLogr()
	
	if !debugLogrLogger.Enabled() {
		t.Error("DEBUG level should be enabled")
	}
}

func TestLogrSink_Info(t *testing.T) {
	logger := NewLogger("DEBUG")
	buf := &bytes.Buffer{}
	logger.SetOutput(buf)
	
	logrLogger := logger.GetLogr()
	logrLogger.Info("test info message")
	
	output := buf.String()
	if !strings.Contains(output, "test info message") {
		t.Errorf("Expected logr info output to contain message, got %q", output)
	}
	if !strings.Contains(output, "INFO") {
		t.Error("Expected logr info to use INFO level")
	}
}

func TestLogrSink_Error(t *testing.T) {
	logger := NewLogger("DEBUG")
	buf := &bytes.Buffer{}
	logger.SetOutput(buf)
	
	logrLogger := logger.GetLogr()
	logrLogger.Error(nil, "test error message")
	
	output := buf.String()
	if !strings.Contains(output, "test error message") {
		t.Errorf("Expected logr error output to contain message, got %q", output)
	}
	if !strings.Contains(output, "ERROR") {
		t.Error("Expected logr error to use ERROR level")
	}
}

func TestLogrSink_WithName(t *testing.T) {
	logger := NewLogger("INFO")
	logrLogger := logger.GetLogr()
	
	namedLogger := logrLogger.WithName("test-component")
	if namedLogger.GetSink() == nil {
		t.Error("WithName should return a logger")
	}
	
	// Test chaining names
	doubleNamedLogger := namedLogger.WithName("sub-component")
	if doubleNamedLogger.GetSink() == nil {
		t.Error("Chained WithName should return a logger")
	}
}

func TestLogrSink_WithValues(t *testing.T) {
	logger := NewLogger("INFO")
	logrLogger := logger.GetLogr()
	
	valuedLogger := logrLogger.WithValues("key1", "value1", "key2", 42)
	if valuedLogger.GetSink() == nil {
		t.Error("WithValues should return a logger")
	}
	
	// Test chaining values
	doubleValuedLogger := valuedLogger.WithValues("key3", "value3")
	if doubleValuedLogger.GetSink() == nil {
		t.Error("Chained WithValues should return a logger")
	}
}