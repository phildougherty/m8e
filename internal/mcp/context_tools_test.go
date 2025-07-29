package mcp

import (
	"testing"
)

func TestNewContextTools(t *testing.T) {
	config := ContextToolConfig{}
	
	tools := NewContextTools(config, nil, nil, nil, nil)
	
	if tools == nil {
		t.Fatal("NewContextTools() returned nil")
	}
	
	// Check default values are set
	if tools.config.MaxTokens != 32768 {
		t.Errorf("Expected default MaxTokens 32768, got %d", tools.config.MaxTokens)
	}
	
	if tools.config.TruncationStrategy != "intelligent" {
		t.Errorf("Expected default TruncationStrategy 'intelligent', got %s", tools.config.TruncationStrategy)
	}
	
	if tools.config.RelevanceThreshold != 0.5 {
		t.Errorf("Expected default RelevanceThreshold 0.5, got %f", tools.config.RelevanceThreshold)
	}
	
	if tools.config.MaxContextItems != 50 {
		t.Errorf("Expected default MaxContextItems 50, got %d", tools.config.MaxContextItems)
	}
}

func TestNewContextTools_WithCustomConfig(t *testing.T) {
	config := ContextToolConfig{
		MaxTokens:          16384,
		TruncationStrategy: "simple",
		RelevanceThreshold: 0.7,
		MaxContextItems:    25,
		EnableMentions:     true,
		AutoTrack:         true,
	}
	
	tools := NewContextTools(config, nil, nil, nil, nil)
	
	if tools == nil {
		t.Fatal("NewContextTools() returned nil")
	}
	
	// Check custom values are preserved
	if tools.config.MaxTokens != 16384 {
		t.Errorf("Expected MaxTokens 16384, got %d", tools.config.MaxTokens)
	}
	
	if tools.config.TruncationStrategy != "simple" {
		t.Errorf("Expected TruncationStrategy 'simple', got %s", tools.config.TruncationStrategy)
	}
	
	if tools.config.RelevanceThreshold != 0.7 {
		t.Errorf("Expected RelevanceThreshold 0.7, got %f", tools.config.RelevanceThreshold)
	}
	
	if tools.config.MaxContextItems != 25 {
		t.Errorf("Expected MaxContextItems 25, got %d", tools.config.MaxContextItems)
	}
	
	if !tools.config.EnableMentions {
		t.Error("Expected EnableMentions to be true")
	}
	
	if !tools.config.AutoTrack {
		t.Error("Expected AutoTrack to be true")
	}
}

func TestGetContextRequest_Struct(t *testing.T) {
	req := GetContextRequest{
		IncludeFiles:    true,
		IncludeEdits:    false,
		IncludeMentions: true,
		IncludeLogs:     false,
		FilterTypes:     []string{"file", "edit"},
		MaxItems:        100,
		SinceTimestamp:  1640995200,
	}
	
	if !req.IncludeFiles {
		t.Error("Expected IncludeFiles to be true")
	}
	
	if req.IncludeEdits {
		t.Error("Expected IncludeEdits to be false")
	}
	
	if !req.IncludeMentions {
		t.Error("Expected IncludeMentions to be true")
	}
	
	if req.IncludeLogs {
		t.Error("Expected IncludeLogs to be false")
	}
	
	if len(req.FilterTypes) != 2 {
		t.Errorf("Expected 2 filter types, got %d", len(req.FilterTypes))
	}
	
	if req.MaxItems != 100 {
		t.Errorf("Expected MaxItems 100, got %d", req.MaxItems)
	}
	
	if req.SinceTimestamp != 1640995200 {
		t.Errorf("Expected SinceTimestamp 1640995200, got %d", req.SinceTimestamp)
	}
}

func TestGetContextResponse_Struct(t *testing.T) {
	resp := GetContextResponse{
		Success:     true,
		TotalTokens: 5000,
		TotalItems:  25,
		Truncated:   false,
		Strategy:    "intelligent",
		Error:       "",
	}
	
	if !resp.Success {
		t.Error("Expected Success to be true")
	}
	
	if resp.TotalTokens != 5000 {
		t.Errorf("Expected TotalTokens 5000, got %d", resp.TotalTokens)
	}
	
	if resp.TotalItems != 25 {
		t.Errorf("Expected TotalItems 25, got %d", resp.TotalItems)
	}
	
	if resp.Truncated {
		t.Error("Expected Truncated to be false")
	}
	
	if resp.Strategy != "intelligent" {
		t.Errorf("Expected Strategy 'intelligent', got %s", resp.Strategy)
	}
	
	if resp.Error != "" {
		t.Errorf("Expected empty Error, got %s", resp.Error)
	}
}

func TestAddContextRequest_Struct(t *testing.T) {
	metadata := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}
	
	req := AddContextRequest{
		Type:     "file",
		FilePath: "/path/to/file.go",
		Content:  "package main",
		Metadata: metadata,
		Priority: 0.8,
	}
	
	if req.Type != "file" {
		t.Errorf("Expected Type 'file', got %s", req.Type)
	}
	
	if req.FilePath != "/path/to/file.go" {
		t.Errorf("Expected FilePath '/path/to/file.go', got %s", req.FilePath)
	}
	
	if req.Content != "package main" {
		t.Errorf("Expected Content 'package main', got %s", req.Content)
	}
	
	if req.Priority != 0.8 {
		t.Errorf("Expected Priority 0.8, got %f", req.Priority)
	}
	
	if len(req.Metadata) != 2 {
		t.Errorf("Expected 2 metadata items, got %d", len(req.Metadata))
	}
}

func TestAddContextResponse_Struct(t *testing.T) {
	resp := AddContextResponse{
		Success: true,
	}
	
	if !resp.Success {
		t.Error("Expected Success to be true")
	}
}