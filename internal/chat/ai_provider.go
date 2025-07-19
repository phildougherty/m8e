package chat

import (
	"fmt"
	"strings"

	"github.com/phildougherty/m8e/internal/ai"
)

// renderMarkdown renders markdown content using glamour with custom code block headers
func (tc *TermChat) renderMarkdown(content string) string {
	if tc.markdownRenderer == nil {
		return content
	}
	
	// Add custom headers for code blocks before rendering
	enhanced := tc.addCodeBlockHeaders(content)
	
	// Render with glamour
	rendered, err := tc.markdownRenderer.Render(enhanced)
	if err != nil {
		// Fallback to original content if rendering fails
		return content
	}
	
	return rendered
}

// addCodeBlockHeaders adds beautiful headers to code blocks
func (tc *TermChat) addCodeBlockHeaders(content string) string {
	result := content
	
	// Add visual headers for different code block types
	if strings.Contains(result, "```") {
		// YAML files
		result = strings.ReplaceAll(result, "```yaml", "\n\033[1;33m╭─ matey.yaml ─────────────────────────╮\033[0m\n```yaml")
		result = strings.ReplaceAll(result, "```yml", "\n\033[1;33m╭─ config.yml ─────────────────────────╮\033[0m\n```yml")
		
		// JSON files
		result = strings.ReplaceAll(result, "```json", "\n\033[1;32m╭─ config.json ────────────────────────╮\033[0m\n```json")
		
		// Shell/Bash commands
		result = strings.ReplaceAll(result, "```bash", "\n\033[1;36m╭─ commands ───────────────────────────╮\033[0m\n```bash")
		result = strings.ReplaceAll(result, "```sh", "\n\033[1;36m╭─ script.sh ──────────────────────────╮\033[0m\n```sh")
		
		// Kubernetes manifests
		result = strings.ReplaceAll(result, "```kubernetes", "\n\033[1;35m╭─ kubernetes.yaml ────────────────────╮\033[0m\n```kubernetes")
		result = strings.ReplaceAll(result, "```k8s", "\n\033[1;35m╭─ manifest.yaml ──────────────────────╮\033[0m\n```k8s")
		
		// Go code
		result = strings.ReplaceAll(result, "```go", "\n\033[1;34m╭─ main.go ────────────────────────────╮\033[0m\n```go")
		
		// Other languages
		result = strings.ReplaceAll(result, "```python", "\n\033[1;37m╭─ script.py ──────────────────────────╮\033[0m\n```python")
		result = strings.ReplaceAll(result, "```javascript", "\n\033[1;31m╭─ script.js ──────────────────────────╮\033[0m\n```javascript")
		result = strings.ReplaceAll(result, "```typescript", "\n\033[1;94m╭─ script.ts ──────────────────────────╮\033[0m\n```typescript")
	}
	
	return result
}

// chatWithAISilent processes AI response WITH streaming for UI (silent version for UI)
func (tc *TermChat) chatWithAISilent(message string) {
	// This method handles AI communication for the UI
	// It should stream responses back to the UI without terminal output
	
	// Get the optimized system context
	systemContext := tc.GetOptimizedSystemPrompt()
	
	// Prepare messages for AI
	messages := []ai.Message{
		{Role: "system", Content: systemContext},
	}
	
	// Add chat history
	for _, msg := range tc.chatHistory {
		messages = append(messages, ai.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	
	// Get current provider
	provider, err := tc.aiManager.GetCurrentProvider()
	if err != nil {
		// Send error message to UI
		if uiProgram != nil {
			uiProgram.Send(aiResponseMsg{content: "Error: Unable to get AI provider: " + err.Error()})
		}
		return
	}
	
	// Start streaming response
	if uiProgram != nil {
		uiProgram.Send(aiStreamMsg{content: ""}) // Signal start of response
	}
	
	// Make AI request with streaming
	streamCh, err := provider.StreamChat(tc.ctx, messages, ai.StreamOptions{
		Temperature: 0.7,
		MaxTokens:   4000,
		Model:       tc.currentModel,
	})
	
	if err != nil {
		// Send error to UI
		if uiProgram != nil {
			uiProgram.Send(aiResponseMsg{content: "Error: " + err.Error()})
		}
		return
	}
	
	// Collect streaming response and send to UI
	var fullResponse strings.Builder
	for response := range streamCh {
		if response.Error != nil {
			if uiProgram != nil {
				uiProgram.Send(aiResponseMsg{content: "Stream error: " + response.Error.Error()})
			}
			return
		}
		fullResponse.WriteString(response.Content)
		// Send streaming content to UI
		if uiProgram != nil {
			uiProgram.Send(aiStreamMsg{content: response.Content})
		}
	}
	
	// Add AI response to history
	tc.addMessage("assistant", fullResponse.String())
	
	// Send final response to UI
	if uiProgram != nil {
		uiProgram.Send(aiResponseMsg{content: fullResponse.String()})
	}
}

// switchProviderSilent switches AI provider without terminal output
func (tc *TermChat) switchProviderSilent(name string) error {
	if tc.aiManager == nil {
		return fmt.Errorf("AI manager not initialized")
	}
	
	err := tc.aiManager.SwitchProvider(name)
	if err != nil {
		return err
	}
	
	// Update current provider info
	if provider, err := tc.aiManager.GetCurrentProvider(); err == nil {
		tc.currentProvider = provider.Name()
		tc.currentModel = provider.DefaultModel()
	}
	
	return nil
}

// switchModelSilent switches AI model without terminal output
func (tc *TermChat) switchModelSilent(name string) error {
	if tc.aiManager == nil {
		return fmt.Errorf("AI manager not initialized")
	}
	
	// Try to switch model (this depends on the provider implementation)
	// For now, just update the current model reference
	tc.currentModel = name
	
	return nil
}

// getMCPFunctions returns available MCP functions
func (tc *TermChat) getMCPFunctions() []ai.Function {
	if tc.mcpClient == nil {
		return []ai.Function{}
	}
	
	// This is a placeholder - the actual implementation would query the MCP client
	// for available tools and convert them to ai.Function format
	functions := []ai.Function{
		{
			Name:        "deploy_service",
			Description: "Deploy a service to Kubernetes cluster",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Service name",
					},
					"image": map[string]interface{}{
						"type":        "string",
						"description": "Docker image",
					},
					"replicas": map[string]interface{}{
						"type":        "integer",
						"description": "Number of replicas",
					},
				},
				"required": []string{"name", "image"},
			},
		},
		{
			Name:        "scale_service",
			Description: "Scale a service in the Kubernetes cluster",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Service name to scale",
					},
					"replicas": map[string]interface{}{
						"type":        "integer",
						"description": "Target number of replicas",
					},
				},
				"required": []string{"name", "replicas"},
			},
		},
		{
			Name:        "get_service_status",
			Description: "Get the status of services in the cluster",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace (optional)",
					},
				},
			},
		},
		{
			Name:        "get_logs",
			Description: "Get logs from a service or pod",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"service": map[string]interface{}{
						"type":        "string",
						"description": "Service name",
					},
					"lines": map[string]interface{}{
						"type":        "integer",
						"description": "Number of log lines to retrieve",
						"default":     100,
					},
				},
				"required": []string{"service"},
			},
		},
		{
			Name:        "create_backup",
			Description: "Create a backup of cluster resources",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace to backup",
					},
					"resources": map[string]interface{}{
						"type":        "array",
						"description": "Resource types to backup",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
		},
	}
	
	return functions
}