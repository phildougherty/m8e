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

// OllamaProvider implements the Ollama provider
type OllamaProvider struct {
	config     ProviderConfig
	httpClient *http.Client
}


// NewOllamaProvider creates a new Ollama provider
func NewOllamaProvider(config ProviderConfig) (*OllamaProvider, error) {
	if config.Endpoint == "" {
		config.Endpoint = "http://localhost:11434"
	}
	
	if config.DefaultModel == "" {
		config.DefaultModel = "llama3"
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
	
	return &OllamaProvider{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

// Name returns the provider name
func (p *OllamaProvider) Name() string {
	return string(ProviderTypeOllama)
}

// StreamChat streams a chat completion
func (p *OllamaProvider) StreamChat(ctx context.Context, messages []Message, options StreamOptions) (<-chan StreamResponse, error) {
	responseChan := make(chan StreamResponse, 10)
	// Create response channel
	
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
	
	// Build request in OpenAI-compatible format
	requestBody := map[string]interface{}{
		"model":       options.Model,
		"messages":    messages,
		"max_tokens":  options.MaxTokens,
		"temperature": options.Temperature,
		"stream":      true,
	}
	
	// Add tools if functions are provided (for future Ollama function calling support)
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
	req, err := http.NewRequestWithContext(ctx, "POST", p.config.Endpoint+"/v1/chat/completions", bytes.NewBuffer(reqBytes))
	if err != nil {
		close(responseChan)
		return responseChan, NewProviderError(p.Name(), "failed to create request", "request_error")
	}
	
	req.Header.Set("Content-Type", "application/json")
	
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
		
		// Read streaming response (OpenAI-compatible SSE format)
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
							response := StreamResponse{
								Content:      regularContent,
								ThinkContent: thinkContent,
							}
							responseChan <- response
						}
					}
					
					// Handle tool calls (for future Ollama function calling support)
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
func (p *OllamaProvider) SupportedModels() []string {
	// Try to get models from Ollama API
	models := p.getAvailableModels()
	if len(models) > 0 {
		return models
	}
	
	// Return common models as fallback
	return []string{
		"llama3",
		"llama3:70b",
		"codellama",
		"codellama:13b",
		"mistral",
		"mistral:7b",
		"phi3",
		"gemma",
		"qwen",
	}
}

// getAvailableModels gets available models from Ollama API
func (p *OllamaProvider) getAvailableModels() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", p.config.Endpoint+"/api/tags", nil)
	if err != nil {
		return nil
	}
	
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	
	var response struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil
	}
	
	models := make([]string, len(response.Models))
	for i, model := range response.Models {
		models[i] = model.Name
	}
	
	return models
}

// ValidateConfig validates the provider configuration
func (p *OllamaProvider) ValidateConfig() error {
	if p.config.Endpoint == "" {
		return fmt.Errorf("Ollama endpoint is required")
	}
	
	return nil
}

// IsAvailable checks if the provider is available
func (p *OllamaProvider) IsAvailable() bool {
	if err := p.ValidateConfig(); err != nil {
		return false
	}
	
	// Quick health check
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", p.config.Endpoint+"/api/tags", nil)
	if err != nil {
		return false
	}
	
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	return resp.StatusCode == http.StatusOK
}

// DefaultModel returns the default model for this provider
func (p *OllamaProvider) DefaultModel() string {
	return p.config.DefaultModel
}