package ai

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Message represents a chat message
type Message struct {
	Role      string     `json:"role"`      // "user", "assistant", "system"
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolCallId string    `json:"tool_call_id,omitempty"`
}

// ToolCall represents a function call made by the AI
type ToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`     // "function"
	Function FunctionCall           `json:"function"`
}

// FunctionCall represents a function call
type FunctionCall struct {
	Name      string                 `json:"name"`
	Arguments string                 `json:"arguments"` // JSON string of arguments
}

// Function represents a function definition
type Function struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// StreamResponse represents a streaming response chunk
type StreamResponse struct {
	Content      string     `json:"content"`      // Regular content to display
	ThinkContent string     `json:"think_content"` // Reasoning content in collapsible section
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Finished     bool       `json:"finished"`
	Error        error      `json:"error,omitempty"`
}

// ThinkProcessor tracks state for processing think tags in streams
type ThinkProcessor struct {
	insideThinkTag  bool
	thinkBuffer     string
	regularBuffer   string
}

// StreamOptions contains options for streaming chat
type StreamOptions struct {
	MaxTokens   int        `json:"max_tokens,omitempty"`
	Temperature float64    `json:"temperature,omitempty"`
	Model       string     `json:"model,omitempty"`
	Functions   []Function `json:"functions,omitempty"`
}

// Provider represents an AI provider interface
type Provider interface {
	// Name returns the provider name
	Name() string
	
	// StreamChat streams a chat completion
	StreamChat(ctx context.Context, messages []Message, options StreamOptions) (<-chan StreamResponse, error)
	
	// SupportedModels returns the list of supported models
	SupportedModels() []string
	
	// ValidateConfig validates the provider configuration
	ValidateConfig() error
	
	// IsAvailable checks if the provider is available
	IsAvailable() bool
	
	// DefaultModel returns the default model for this provider
	DefaultModel() string
}

// Config represents AI provider configuration
type Config struct {
	DefaultProvider string            `yaml:"default_provider"`
	Providers       map[string]ProviderConfig `yaml:"providers"`
	FallbackProviders []string        `yaml:"fallback_providers"`
}

// ProviderConfig represents configuration for a specific provider
type ProviderConfig struct {
	APIKey       string        `yaml:"api_key"`
	Endpoint     string        `yaml:"endpoint"`
	DefaultModel string        `yaml:"default_model"`
	MaxTokens    int           `yaml:"max_tokens"`
	Temperature  float64       `yaml:"temperature"`
	Timeout      time.Duration `yaml:"timeout"`
}

// ProviderType represents the type of AI provider
type ProviderType string

const (
	ProviderTypeOpenAI     ProviderType = "openai"
	ProviderTypeClaude     ProviderType = "claude"
	ProviderTypeOllama     ProviderType = "ollama"
	ProviderTypeOpenRouter ProviderType = "openrouter"
)

// ProviderStatus represents the status of a provider
type ProviderStatus struct {
	Name      string    `json:"name"`
	Available bool      `json:"available"`
	LastCheck time.Time `json:"last_check"`
	Error     string    `json:"error,omitempty"`
	Models    []string  `json:"models"`
}

// ProviderError represents an error from a provider
type ProviderError struct {
	Provider string
	Message  string
	Code     string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider %s: %s", e.Provider, e.Message)
}

// NewProviderError creates a new provider error
func NewProviderError(provider, message, code string) *ProviderError {
	return &ProviderError{
		Provider: provider,
		Message:  message,
		Code:     code,
	}
}

// NewThinkProcessor creates a new think processor
func NewThinkProcessor() *ThinkProcessor {
	return &ThinkProcessor{
		insideThinkTag:  false,
		thinkBuffer:     "",
		regularBuffer:   "",
	}
}

// ProcessChunk processes a content chunk and separates think content from regular content
func (tp *ThinkProcessor) ProcessChunk(chunk string) (regularContent, thinkContent string) {
	// Simple approach: if we see <think> or </think> in this chunk, it's think content
	// Otherwise, if we're not inside a think block, it's regular content
	
	if strings.Contains(chunk, "<think>") {
		tp.insideThinkTag = true
		// Extract any content before <think> as regular content
		if beforeThink := strings.Split(chunk, "<think>")[0]; beforeThink != "" {
			regularContent = beforeThink
		}
		// The rest is think content (excluding the tag itself)
		return regularContent, ""
	}
	
	if strings.Contains(chunk, "</think>") {
		tp.insideThinkTag = false
		// Extract any content after </think> as regular content
		parts := strings.Split(chunk, "</think>")
		if len(parts) > 1 && parts[1] != "" {
			regularContent = parts[1]
		}
		// The part before </think> is think content (excluding the tag itself)
		return regularContent, ""
	}
	
	// No think tags in this chunk
	if tp.insideThinkTag {
		// We're inside a think block, so this is think content
		return "", chunk
	} else {
		// We're outside think blocks, so this is regular content
		return chunk, ""
	}
}

// processResult holds the result of processing content
type processResult struct {
	regularContent    string
	thinkContent     string
	remainingBuffer  string
}

// processBuffer processes the current buffer content
func (tp *ThinkProcessor) processBuffer() processResult {
	content := tp.regularBuffer
	var regularParts []string
	var thinkParts []string
	var remaining string
	
	i := 0
	for i < len(content) {
		if !tp.insideThinkTag {
			// Look for opening think tag
			if thinkStart := strings.Index(content[i:], "<think>"); thinkStart != -1 {
				// Add content before think tag to regular content
				if thinkStart > 0 {
					regularParts = append(regularParts, content[i:i+thinkStart])
				}
				tp.insideThinkTag = true
				i += thinkStart + 7 // length of "<think>"
			} else {
				// No think tag found, add remaining content to regular
				regularParts = append(regularParts, content[i:])
				break
			}
		} else {
			// Look for closing think tag
			if thinkEnd := strings.Index(content[i:], "</think>"); thinkEnd != -1 {
				// Add content inside think tag
				if thinkEnd > 0 {
					thinkParts = append(thinkParts, content[i:i+thinkEnd])
				}
				tp.insideThinkTag = false
				i += thinkEnd + 8 // length of "</think>"
			} else {
				// No closing tag found, save content as think and mark as remaining
				thinkParts = append(thinkParts, content[i:])
				remaining = content[i:]
				break
			}
		}
	}
	
	// If we're still inside a think tag, save the remaining content
	if tp.insideThinkTag && remaining == "" {
		remaining = ""
	}
	
	return processResult{
		regularContent:   strings.Join(regularParts, ""),
		thinkContent:    strings.Join(thinkParts, ""),
		remainingBuffer: remaining,
	}
}

// ProcessStreamContentWithThinks processes streaming content and separates think tags
func ProcessStreamContentWithThinks(processor *ThinkProcessor, chunk string) (regularContent, thinkContent string) {
	regularContent, thinkContent = processor.ProcessChunk(chunk)
	return regularContent, thinkContent
}