package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	
	// GetModelContextWindow returns the context window size for a given model
	GetModelContextWindow(model string) int
	
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



// ProcessStreamContentWithThinks processes streaming content and separates think tags
func ProcessStreamContentWithThinks(processor *ThinkProcessor, chunk string) (regularContent, thinkContent string) {
	regularContent, thinkContent = processor.ProcessChunk(chunk)
	return regularContent, thinkContent
}

// ProcessOpenAIStreamResponse processes OpenAI-compatible SSE streaming responses
// This shared function handles the common SSE parsing logic for OpenAI and OpenRouter
func ProcessOpenAIStreamResponse(reader io.Reader, providerName string, thinkProcessor *ThinkProcessor, responseChan chan<- StreamResponse) {
	scanner := bufio.NewScanner(reader)
	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		
		// Parse SSE format
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			
			// Check for end of stream
			if data == "[DONE]" {
				responseChan <- StreamResponse{
					Finished: true,
				}
				return
			}
			
			// Parse JSON
			var streamResp struct {
				Choices []struct {
					Delta struct {
						Content   string `json:"content"`
						ToolCalls []struct {
							Index int `json:"index"`
							ID    string `json:"id"`
							Type  string `json:"type"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
			}
			
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				continue // Skip malformed JSON
			}
			
			// Extract content and tool calls
			if len(streamResp.Choices) > 0 {
				choice := streamResp.Choices[0]
				
				// Handle content
				if choice.Delta.Content != "" {
					// Process content to separate think tags from regular content
					regularContent, thinkContent := ProcessStreamContentWithThinks(thinkProcessor, choice.Delta.Content)
					
					// Send response with separated content
					if regularContent != "" || thinkContent != "" {
						responseChan <- StreamResponse{
							Content:      regularContent,
							ThinkContent: thinkContent,
						}
					}
				}
				
				// Handle tool calls
				if len(choice.Delta.ToolCalls) > 0 {
					var toolCalls []ToolCall
					for _, tc := range choice.Delta.ToolCalls {
						toolCalls = append(toolCalls, ToolCall{
							ID:   tc.ID,
							Type: tc.Type,
							Function: FunctionCall{
								Name:      tc.Function.Name,
								Arguments: tc.Function.Arguments,
							},
						})
					}
					
					responseChan <- StreamResponse{
						ToolCalls: toolCalls,
					}
				}
				
				if choice.FinishReason != nil {
					responseChan <- StreamResponse{
						Finished: true,
					}
					return
				}
			}
		}
	}
	
	if err := scanner.Err(); err != nil {
		responseChan <- StreamResponse{
			Error: NewProviderError(providerName, "stream read error: "+err.Error(), "stream_error"),
		}
	}
}