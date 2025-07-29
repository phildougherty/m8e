package ai

import (
	"context"
	"testing"
)

func TestNewPlaceholderProvider(t *testing.T) {
	provider := NewPlaceholderProvider("test-provider", "test error")
	
	if provider == nil {
		t.Fatal("NewPlaceholderProvider() returned nil")
	}
	
	if provider.Name() != "test-provider" {
		t.Errorf("Expected name 'test-provider', got %s", provider.Name())
	}
}

func TestPlaceholderProvider_Name(t *testing.T) {
	provider := NewPlaceholderProvider("test-name", "error")
	
	if provider.Name() != "test-name" {
		t.Errorf("Expected 'test-name', got %s", provider.Name())
	}
}

func TestPlaceholderProvider_StreamChat(t *testing.T) {
	provider := NewPlaceholderProvider("test", "not available")
	
	_, err := provider.StreamChat(context.Background(), nil, StreamOptions{})
	
	if err == nil {
		t.Error("Expected error from StreamChat")
	}
}

func TestPlaceholderProvider_SupportedModels(t *testing.T) {
	provider := NewPlaceholderProvider("test", "error")
	
	models := provider.SupportedModels()
	
	if len(models) != 0 {
		t.Errorf("Expected empty slice, got %v", models)
	}
}

func TestPlaceholderProvider_ValidateConfig(t *testing.T) {
	provider := NewPlaceholderProvider("test", "validation error")
	
	err := provider.ValidateConfig()
	
	if err == nil {
		t.Error("Expected validation error")
	}
}

func TestPlaceholderProvider_GetModelContextWindow(t *testing.T) {
	provider := NewPlaceholderProvider("test", "error")
	
	contextWindow := provider.GetModelContextWindow("any-model")
	
	if contextWindow != 32768 {
		t.Errorf("Expected 32768, got %d", contextWindow)
	}
}

func TestPlaceholderProvider_DefaultModel(t *testing.T) {
	provider := NewPlaceholderProvider("test", "error")
	
	model := provider.DefaultModel()
	
	if model != "" {
		t.Errorf("Expected empty string, got %s", model)
	}
}

func TestPlaceholderProvider_IsAvailable(t *testing.T) {
	provider := NewPlaceholderProvider("test", "error")
	
	if provider.IsAvailable() {
		t.Error("Expected false from IsAvailable")
	}
}