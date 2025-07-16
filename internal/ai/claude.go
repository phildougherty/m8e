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

// ClaudeProvider implements the Claude provider
type ClaudeProvider struct {
	config     ProviderConfig
	httpClient *http.Client
}

// NewClaudeProvider creates a new Claude provider
func NewClaudeProvider(config ProviderConfig) (*ClaudeProvider, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("Claude API key is required")
	}
	
	if config.Endpoint == "" {
		config.Endpoint = "https://api.anthropic.com/v1"
	}
	
	if config.DefaultModel == "" {
		config.DefaultModel = "claude-3.5-sonnet-20241022"
	}
	
	if config.MaxTokens == 0 {
		config.MaxTokens = 8192
	}
	
	if config.Temperature == 0 {
		config.Temperature = 0.7
	}
	
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Minute // Increased for streaming responses
	}
	
	return &ClaudeProvider{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

// Name returns the provider name
func (p *ClaudeProvider) Name() string {
	return string(ProviderTypeClaude)
}

// StreamChat streams a chat completion
func (p *ClaudeProvider) StreamChat(ctx context.Context, messages []Message, options StreamOptions) (<-chan StreamResponse, error) {
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
	
	// Convert messages to Claude format
	claudeMessages := make([]map[string]interface{}, 0, len(messages))
	var systemMessage string
	
	for _, msg := range messages {
		if msg.Role == "system" {
			systemMessage = msg.Content
			continue
		}
		claudeMessages = append(claudeMessages, map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}
	
	// Build request
	requestBody := map[string]interface{}{
		"model":      options.Model,
		"max_tokens": options.MaxTokens,
		"messages":   claudeMessages,
		"stream":     true,
	}
	
	if systemMessage != "" {
		requestBody["system"] = systemMessage
	}
	
	// Add tools if functions are provided (Claude format)
	if len(options.Functions) > 0 {
		tools := make([]map[string]interface{}, len(options.Functions))
		for i, function := range options.Functions {
			tools[i] = map[string]interface{}{
				"name":        function.Name,
				"description": function.Description,
				"input_schema": function.Parameters,
			}
		}
		requestBody["tools"] = tools
	}
	
	if options.Temperature > 0 {
		requestBody["temperature"] = options.Temperature
	}
	
	reqBytes, err := json.Marshal(requestBody)
	if err != nil {
		close(responseChan)
		return responseChan, NewProviderError(p.Name(), "failed to marshal request", "marshal_error")
	}
	
	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", p.config.Endpoint+"/messages", bytes.NewBuffer(reqBytes))
	if err != nil {
		close(responseChan)
		return responseChan, NewProviderError(p.Name(), "failed to create request", "request_error")
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Accept", "text/event-stream")
	
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
				
				// Parse JSON
				var streamResp struct {
					Type  string `json:"type"`
					Delta struct {
						Type          string `json:"type"`
						Text          string `json:"text"`
						PartialJSON   string `json:"partial_json"`
					} `json:"delta"`
					ContentBlock struct {
						Type string `json:"type"`
						Name string `json:"name"`
						ID   string `json:"id"`
						Input map[string]interface{} `json:"input"`
					} `json:"content_block"`
				}
				
				if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
					continue // Skip malformed JSON
				}
				
				// Handle different event types
				switch streamResp.Type {
				case "content_block_start":
					// Handle tool use blocks
					if streamResp.ContentBlock.Type == "tool_use" {
						// Tool call started
						toolCall := ToolCall{
							ID:   streamResp.ContentBlock.ID,
							Type: "function",
							Function: FunctionCall{
								Name:      streamResp.ContentBlock.Name,
								Arguments: "", // Will be filled as we get input_json_delta events
							},
						}
						
						responseChan <- StreamResponse{
							ToolCalls: []ToolCall{toolCall},
						}
					}
				case "content_block_delta":
					if streamResp.Delta.Type == "text_delta" {
						// Process content to separate think tags from regular content
						regularContent, thinkContent := ProcessStreamContentWithThinks(thinkProcessor, streamResp.Delta.Text)
						
						// Send response with separated content
						if regularContent != "" || thinkContent != "" {
							responseChan <- StreamResponse{
								Content:      regularContent,
								ThinkContent: thinkContent,
							}
						}
					} else if streamResp.Delta.Type == "input_json_delta" {
						// Tool call arguments are being streamed
						// For now, we'll handle this by accumulating in a buffer
						// A more sophisticated implementation would track tool calls by ID
						if streamResp.Delta.PartialJSON != "" {
							// This is partial JSON for tool arguments
							// In a production system, you'd want to buffer this by tool call ID
						}
					}
				case "message_stop":
					responseChan <- StreamResponse{
						Finished: true,
					}
					return
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
func (p *ClaudeProvider) SupportedModels() []string {
	return []string{
		"claude-3.5-sonnet-20241022",
		"claude-3.5-haiku-20241022",
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
	}
}

// ValidateConfig validates the provider configuration
func (p *ClaudeProvider) ValidateConfig() error {
	if p.config.APIKey == "" {
		return fmt.Errorf("Claude API key is required")
	}
	
	if p.config.Endpoint == "" {
		return fmt.Errorf("Claude endpoint is required")
	}
	
	return nil
}

// IsAvailable checks if the provider is available
func (p *ClaudeProvider) IsAvailable() bool {
	if err := p.ValidateConfig(); err != nil {
		return false
	}
	
	// Quick health check - try to make a minimal request
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	testRequest := map[string]interface{}{
		"model":      p.config.DefaultModel,
		"max_tokens": 1,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": "Hi",
			},
		},
	}
	
	reqBytes, err := json.Marshal(testRequest)
	if err != nil {
		return false
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", p.config.Endpoint+"/messages", bytes.NewBuffer(reqBytes))
	if err != nil {
		return false
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	return resp.StatusCode == http.StatusOK
}

// DefaultModel returns the default model for this provider
func (p *ClaudeProvider) DefaultModel() string {
	return p.config.DefaultModel
}