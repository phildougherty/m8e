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

// OpenRouterProvider implements the OpenRouter provider
type OpenRouterProvider struct {
	config     ProviderConfig
	httpClient *http.Client
}

// NewOpenRouterProvider creates a new OpenRouter provider
func NewOpenRouterProvider(config ProviderConfig) (*OpenRouterProvider, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("OpenRouter API key is required")
	}
	
	if config.Endpoint == "" {
		config.Endpoint = "https://openrouter.ai/api/v1"
	}
	
	if config.DefaultModel == "" {
		config.DefaultModel = "moonshotai/kimi-k2"
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
	
	return &OpenRouterProvider{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

// Name returns the provider name
func (p *OpenRouterProvider) Name() string {
	return string(ProviderTypeOpenRouter)
}

// StreamChat streams a chat completion
func (p *OpenRouterProvider) StreamChat(ctx context.Context, messages []Message, options StreamOptions) (<-chan StreamResponse, error) {
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
			// Ensure parameters is a valid schema for OpenAI API
			params := function.Parameters
			if params == nil {
				params = map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{},
				}
			} else {
				// Ensure it has the required fields
				if params["type"] == nil {
					params["type"] = "object"
				}
				if params["properties"] == nil {
					params["properties"] = map[string]interface{}{}
				}
			}
			
			tools[i] = map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        function.Name,
					"description": function.Description,
					"parameters":  params,
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
	req.Header.Set("HTTP-Referer", "https://matey.dev")
	req.Header.Set("X-Title", "Matey Dashboard")
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
				responseChan <- StreamResponse{Error: NewProviderError(p.Name(), "failed to close response body: "+err.Error(), "close_error")}
			}
		}()
		
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errorMsg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
			responseChan <- StreamResponse{
				Error: NewProviderError(p.Name(), errorMsg, "http_error"),
			}
			return
		}
		
		// Use shared SSE processing function
		ProcessOpenAIStreamResponse(resp.Body, p.Name(), thinkProcessor, responseChan)
	}()
	
	return responseChan, nil
}

// SupportedModels returns the list of supported models
func (p *OpenRouterProvider) SupportedModels() []string {
	// Try to get models from OpenRouter API
	models := p.getAvailableModels()
	if len(models) > 0 {
		return models
	}
	
	// Return popular models as fallback
	return []string{
		"anthropic/claude-3.5-sonnet",
		"anthropic/claude-3.5-haiku", 
		"anthropic/claude-3-opus",
		"anthropic/claude-3.5-sonnet-20241022",
		"openai/gpt-4-turbo",
		"openai/gpt-4",
		"openai/gpt-4o",
		"openai/gpt-4o-mini",
		"openai/gpt-3.5-turbo",
		"moonshotai/kimi-k2",
		"google/gemini-2.5-flash",
		"google/gemini-2.5-pro", 
		"meta-llama/llama-3-70b-instruct",
		"meta-llama/llama-3-8b-instruct",
		"mistralai/mistral-7b-instruct",
		"x-ai/grok-4",
		"cohere/command-r-plus",
	}
}

// getAvailableModels gets available models from OpenRouter API
func (p *OpenRouterProvider) getAvailableModels() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", p.config.Endpoint+"/models", nil)
	if err != nil {
		return nil
	}
	
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("Warning: Failed to close response body: %v\n", err)
		}
	}()
	
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil
	}
	
	models := make([]string, len(response.Data))
	for i, model := range response.Data {
		models[i] = model.ID
	}
	
	return models
}

// ValidateConfig validates the provider configuration
func (p *OpenRouterProvider) ValidateConfig() error {
	if p.config.APIKey == "" {
		return fmt.Errorf("OpenRouter API key is required")
	}
	
	if p.config.Endpoint == "" {
		return fmt.Errorf("OpenRouter endpoint is required")
	}
	
	return nil
}

// IsAvailable checks if the provider is available
func (p *OpenRouterProvider) IsAvailable() bool {
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

// GetModelContextWindow returns the context window size for a given model
func (p *OpenRouterProvider) GetModelContextWindow(model string) int {
	// Model context windows for common models (based on 2025 specifications)
	contextWindows := map[string]int{
		// Original models
		"moonshotai/kimi-k2":                       66000,   // 66K tokens
		"minimax/minimax-m1":                       1000000, // 1M tokens (expandable to 4M)
		"anthropic/claude-sonnet-4":               200000,  // 200K tokens
		"openai/gpt-4o":                           128000,  // 128K tokens
		"openai/gpt-4o-mini":                      128000,  // 128K tokens
		"qwen3:32b":                               131072,  // 128K tokens (32K native, 128K with YaRN)
		
		// Mistral models
		"mistralai/devstral-medium":               131072,  // 131K tokens
		"mistralai/devstral-small":                131072,  // 131K tokens
		
		// X.AI models
		"x-ai/grok-4":                             262144,  // 256K tokens
		
		// Google models
		"google/gemini-2.5-flash":                 1075200, // 1.05M tokens
		"google/gemini-2.5-pro":                   1075200, // 1.05M tokens
		
		// Qwen models
		"qwen/qwen3-30b-a3b":                      41943,   // 41K tokens
		"qwen/qwen3-235b-a22b":                    41943,   // 41K tokens
		
		// OpenAI new models
		"openai/o4-mini":                          200000,  // 200K tokens
		"openai/gpt-4.1":                          1075200, // 1.05M tokens
		"openai/gpt-4.1-mini":                     1075200, // 1.05M tokens
		"openai/o3":                               200000,  // 200K tokens
		"openai/o4-mini-high":                     200000,  // 200K tokens
		"openai/o3-mini":                          200000,  // 200K tokens
		
		// Amazon models
		"amazon/nova-pro-v1":                      300000,  // 300K tokens
	}
	
	if window, exists := contextWindows[model]; exists {
		return window
	}
	
	// Default fallback - conservative estimate
	return 32768 // 32K tokens
}

// DefaultModel returns the default model for this provider
func (p *OpenRouterProvider) DefaultModel() string {
	if p.config.DefaultModel != "" {
		return p.config.DefaultModel
	}
	return "moonshotai/kimi-k2"
}