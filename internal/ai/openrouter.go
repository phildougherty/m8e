package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
		config.DefaultModel = "anthropic/claude-3.5-sonnet"
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
		defer resp.Body.Close()
		
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errorMsg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
			// API error occurred
			responseChan <- StreamResponse{
				Error: NewProviderError(p.Name(), errorMsg, "http_error"),
			}
			return
		}
		
		// Read streaming response
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			
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
				Error: NewProviderError(p.Name(), "stream read error: "+err.Error(), "stream_error"),
			}
		}
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
		"openai/gpt-4-turbo",
		"openai/gpt-4",
		"openai/gpt-3.5-turbo",
		"meta-llama/llama-3-70b-instruct",
		"meta-llama/llama-3-8b-instruct",
		"mistralai/mistral-7b-instruct",
		"google/gemini-pro",
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
	defer resp.Body.Close()
	
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
	defer resp.Body.Close()
	
	return resp.StatusCode == http.StatusOK
}

// DefaultModel returns the default model for this provider
func (p *OpenRouterProvider) DefaultModel() string {
	return p.config.DefaultModel
}