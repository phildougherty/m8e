package mcp

import (
	"os"
	"testing"
	"time"
)

func TestNewMCPClient(t *testing.T) {
	// Save original API key
	originalAPIKey := os.Getenv("MCP_API_KEY")
	defer os.Setenv("MCP_API_KEY", originalAPIKey)
	
	// Set test API key
	os.Setenv("MCP_API_KEY", "test-api-key")
	
	proxyURL := "http://localhost:8080"
	client := NewMCPClient(proxyURL)
	
	if client == nil {
		t.Fatal("NewMCPClient() returned nil")
	}
	
	if client.proxyURL != proxyURL {
		t.Errorf("Expected proxyURL %s, got %s", proxyURL, client.proxyURL)
	}
	
	if client.httpClient == nil {
		t.Error("Expected httpClient to be initialized")
	}
	
	if client.httpClient.Timeout != 20*time.Minute {
		t.Errorf("Expected timeout 20 minutes, got %v", client.httpClient.Timeout)
	}
	
	if client.apiKey != "test-api-key" {
		t.Errorf("Expected apiKey 'test-api-key', got %s", client.apiKey)
	}
	
	if client.retryCount != 0 {
		t.Errorf("Expected retryCount 0, got %d", client.retryCount)
	}
}

func TestNewMCPClient_NoAPIKey(t *testing.T) {
	// Save original API key
	originalAPIKey := os.Getenv("MCP_API_KEY")
	defer os.Setenv("MCP_API_KEY", originalAPIKey)
	
	// Unset API key
	os.Unsetenv("MCP_API_KEY")
	
	proxyURL := "http://localhost:8080"
	client := NewMCPClient(proxyURL)
	
	if client == nil {
		t.Fatal("NewMCPClient() returned nil")
	}
	
	if client.apiKey != "" {
		t.Errorf("Expected empty apiKey, got %s", client.apiKey)
	}
}