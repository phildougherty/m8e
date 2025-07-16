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
		defer resp.Body.Close()
		
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			responseChan <- StreamResponse{
				Error: NewProviderError(p.Name(), fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)), "http_error"),
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
	defer resp.Body.Close()
	
	return resp.StatusCode == http.StatusOK
}

// DefaultModel returns the default model for this provider
func (p *OpenAIProvider) DefaultModel() string {
	return p.config.DefaultModel
}