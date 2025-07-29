package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIProvider implements the OpenAI provider
type OpenAIProvider struct {
	config     ProviderConfig
	httpClient *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(config ProviderConfig) (*OpenAIProvider, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required")
	}
	
	if config.Endpoint == "" {
		config.Endpoint = "https://api.openai.com/v1"
	}
	
	if config.DefaultModel == "" {
		config.DefaultModel = "gpt-4"
	}
	
	if config.MaxTokens == 0 {
		config.MaxTokens = 4096
	}
	
	if config.Temperature == 0 {
		config.Temperature = 0.7
	}
	
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Minute // Increased for streaming responses
	}
	
	return &OpenAIProvider{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

// Name returns the provider name
func (p *OpenAIProvider) Name() string {
	return string(ProviderTypeOpenAI)
}

// StreamChat streams a chat completion
func (p *OpenAIProvider) StreamChat(ctx context.Context, messages []Message, options StreamOptions) (<-chan StreamResponse, error) {
	responseChan := make(chan StreamResponse, 10)
	
	// Set defaults from config
	if options.Model == "" {
		options.Model = p.config.DefaultModel
	}
	if options.MaxTokens == 0 {
		options.MaxTokens = p.config.MaxTokens
	}
	if options.Temperature == 0 {
		options.Temperature = p.config.Temperature
	}
	
	// Build request
	requestBody := map[string]interface{}{
		"model":       options.Model,
		"messages":    messages,
		"max_tokens":  options.MaxTokens,
		"temperature": options.Temperature,
		"stream":      true,
	}
	
	// Add tools if functions are provided
	if len(options.Functions) > 0 {
		tools := make([]map[string]interface{}, len(options.Functions))
		for i, function := range options.Functions {
			tools[i] = map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        function.Name,
					"description": function.Description,
					"parameters":  function.Parameters,
				},
			}
		}
		requestBody["tools"] = tools
		requestBody["tool_choice"] = "auto"
	}
	
	reqBytes, err := json.Marshal(requestBody)
	if err != nil {
		close(responseChan)
		return responseChan, NewProviderError(p.Name(), "failed to marshal request", "marshal_error")
	}
	
	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", p.config.Endpoint+"/chat/completions", bytes.NewBuffer(reqBytes))
	if err != nil {
		close(responseChan)
		return responseChan, NewProviderError(p.Name(), "failed to create request", "request_error")
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	
	// Start streaming in a goroutine
	go func() {
		defer close(responseChan)
		
		// Create think processor for this stream
		thinkProcessor := NewThinkProcessor()
		
		resp, err := p.httpClient.Do(req)
		if err != nil {
			responseChan <- StreamResponse{
				Error: NewProviderError(p.Name(), "request failed: "+err.Error(), "request_failed"),
			}
			return
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				fmt.Printf("Warning: Failed to close response body: %v\n", err)
			}
		}()
		
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			responseChan <- StreamResponse{
				Error: NewProviderError(p.Name(), fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)), "http_error"),
			}
			return
		}
		
		// Use shared SSE processing function
		ProcessOpenAIStreamResponse(resp.Body, p.Name(), thinkProcessor, responseChan)
	}()
	
	return responseChan, nil
}

// SupportedModels returns the list of supported models
func (p *OpenAIProvider) SupportedModels() []string {
	return []string{
		"gpt-4",
		"gpt-4-turbo",
		"gpt-4-turbo-preview",
		"gpt-3.5-turbo",
		"gpt-3.5-turbo-16k",
	}
}

// ValidateConfig validates the provider configuration
func (p *OpenAIProvider) ValidateConfig() error {
	if p.config.APIKey == "" {
		return fmt.Errorf("OpenAI API key is required")
	}
	
	if p.config.Endpoint == "" {
		return fmt.Errorf("OpenAI endpoint is required")
	}
	
	return nil
}

// IsAvailable checks if the provider is available
// GetModelContextWindow returns the context window size for a given model
func (p *OpenAIProvider) GetModelContextWindow(model string) int {
	contextWindows := map[string]int{
		"gpt-4":                  128000,  // 128K tokens
		"gpt-4-turbo":           128000,  // 128K tokens  
		"gpt-4-turbo-preview":   128000,  // 128K tokens
		"gpt-3.5-turbo":         16384,   // 16K tokens
		"gpt-3.5-turbo-16k":     16384,   // 16K tokens
	}
	
	if window, exists := contextWindows[model]; exists {
		return window
	}
	return 32768 // Default fallback
}

// DefaultModel returns the default model for this provider
func (p *OpenAIProvider) DefaultModel() string {
	if p.config.DefaultModel != "" {
		return p.config.DefaultModel
	}
	return "gpt-4"
}

func (p *OpenAIProvider) IsAvailable() bool {
	if err := p.ValidateConfig(); err != nil {
		return false
	}
	
	// Quick health check
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", p.config.Endpoint+"/models", nil)
	if err != nil {
		return false
	}
	
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("Warning: Failed to close response body: %v\n", err)
		}
	}()
	
	return resp.StatusCode == http.StatusOK
}

