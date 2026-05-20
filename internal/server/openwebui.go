// internal/server/openwebui.go
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/phildougherty/m8e/internal/logging"
)

// openWebUIAdapter detects MCP tools/call responses destined for OpenWebUI and
// flattens them into plain text. It is a focused collaborator of the router
// rather than another responsibility bolted onto the proxy handler.
type openWebUIAdapter struct {
	logger *logging.Logger
}

// newOpenWebUIAdapter creates an adapter using the given logger.
func newOpenWebUIAdapter(logger *logging.Logger) *openWebUIAdapter {
	return &openWebUIAdapter{logger: logger}
}

// shouldProcess determines if a response should be processed for OpenWebUI compatibility
func (a *openWebUIAdapter) shouldProcess(r *http.Request, responseBody []byte) bool {
	// Check if the client expects JSON-RPC format
	// If User-Agent indicates it's a standard MCP client or if Accept header specifies JSON,
	// don't process for OpenWebUI
	userAgent := r.Header.Get("User-Agent")
	accept := r.Header.Get("Accept")

	// Check if this is a standard MCP client that expects JSON-RPC
	if strings.Contains(accept, "application/json") ||
		strings.Contains(userAgent, "MCP") ||
		strings.Contains(userAgent, "claude") ||
		strings.Contains(userAgent, "curl") {
		a.logger.Info("Client expects JSON-RPC format - NOT processing for OpenWebUI")
		return false
	}

	// Check if response looks like MCP JSON-RPC with tools/call result
	var responseData map[string]interface{}
	if json.Unmarshal(responseBody, &responseData) == nil {
		if _, hasResult := responseData["result"]; hasResult {
			if _, hasJsonRPC := responseData["jsonrpc"]; hasJsonRPC {
				// Check if result contains content array (typical of tools/call responses)
				if result, ok := responseData["result"].(map[string]interface{}); ok {
					if _, hasContent := result["content"]; hasContent {
						a.logger.Info("Detected MCP tools/call response - processing for OpenWebUI")
						return true
					}
				}
			}
		}
	}

	return false
}

// processResponse processes MCP response for OpenWebUI compatibility
func (a *openWebUIAdapter) processResponse(responseBody []byte) []byte {
	a.logger.Info("Processing MCP response for OpenWebUI: %s", string(responseBody))

	var response map[string]interface{}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		a.logger.Warning("Failed to parse MCP response: %v", err)
		return nil
	}

	// Extract and format the successful result for OpenWebUI - return clean text
	if result, exists := response["result"]; exists {
		a.logger.Info("Found result in MCP response")
		if resultMap, ok := result.(map[string]interface{}); ok {
			a.logger.Info("Result is a map")
			if content, exists := resultMap["content"]; exists {
				a.logger.Info("Found content in result: %+v", content)
				// Process the content for OpenWebUI - extract text from MCP content array
				cleanResult := a.processContent(content)
				a.logger.Info("processContent returned: %+v (type: %T)", cleanResult, cleanResult)

				// For OpenWebUI, we want just the text content, not JSON
				if cleanText, ok := cleanResult.(string); ok {
					a.logger.Info("Successfully converted to string: %s", cleanText)
					return []byte(cleanText)
				} else {
					a.logger.Warning("cleanResult is not a string, type: %T", cleanResult)
				}
			} else {
				a.logger.Warning("No content found in result")
			}
		} else {
			a.logger.Warning("Result is not a map, type: %T", result)
		}
	} else {
		a.logger.Warning("No result found in response")
	}

	return nil
}

// processContent processes MCP content for OpenWebUI compatibility
func (a *openWebUIAdapter) processContent(content interface{}) interface{} {
	a.logger.Info("processContent called with: %+v (type: %T)", content, content)

	if contentArray, ok := content.([]interface{}); ok {
		a.logger.Info("Content is an array with %d items", len(contentArray))
		var textParts []string
		for i, item := range contentArray {
			a.logger.Info("Processing item %d: %+v (type: %T)", i, item, item)
			if itemMap, ok := item.(map[string]interface{}); ok {
				if itemType, ok := itemMap["type"].(string); ok {
					a.logger.Info("Item type: %s", itemType)
					switch itemType {
					case "text":
						if text, ok := itemMap["text"].(string); ok {
							a.logger.Info("Found text: %s", text)
							textParts = append(textParts, text)
						}
					case "image":
						if data, ok := itemMap["data"].(string); ok {
							if mimeType, ok := itemMap["mimeType"].(string); ok {
								imageURL := fmt.Sprintf("data:%s;base64,%s", mimeType, data)
								a.logger.Info("Found image: %s", imageURL)
								textParts = append(textParts, imageURL)
							}
						}
						// For other types, we skip them for OpenWebUI simplicity
					}
				}
			}
		}

		// Join all text parts with newlines for OpenWebUI
		if len(textParts) > 0 {
			result := strings.Join(textParts, "\n")
			a.logger.Info("Returning joined text: %s", result)
			return result
		}
		a.logger.Info("No text parts found, returning original content")
	} else {
		a.logger.Warning("Content is not an array, type: %T", content)
	}

	return content
}
