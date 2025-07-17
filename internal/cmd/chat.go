package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/phildougherty/m8e/internal/ai"
	"github.com/phildougherty/m8e/internal/mcp"
)

// TermChat represents a split-screen terminal chat interface
type TermChat struct {
	aiManager       *ai.Manager
	mcpClient       *mcp.MCPClient
	ctx             context.Context
	cancel          context.CancelFunc
	currentProvider string
	currentModel    string
	chatHistory     []TermChatMessage
	termWidth       int
	termHeight      int
	markdownRenderer *glamour.TermRenderer
}

// TermChatMessage represents a chat message
type TermChatMessage struct {
	Role      string
	Content   string    // Markdown content (for AI context)
	Timestamp time.Time
}

// detectClusterMCPProxy detects the MCP proxy endpoint in the cluster
func detectClusterMCPProxy() string {
	// Try multiple connection methods in order of preference
	endpoints := []string{
		// 1. Try ingress endpoint (production) - HTTPS
		"https://mcp.robotrad.io",
		// 2. Try NodePort service (development)
		"http://localhost:30876", // Common NodePort for development
		// 3. Try port-forward (kubectl port-forward)
		"http://localhost:9876",
		// 4. Try in-cluster service (if running in cluster)
		"http://matey-proxy.default.svc.cluster.local:9876",
		// 5. Fallback to localhost (development)
		"http://localhost:8080",
	}

	client := &http.Client{Timeout: 2 * time.Second}
	
	for _, endpoint := range endpoints {
		fmt.Printf("Trying MCP proxy endpoint: %s\n", endpoint)
		
		// Test health endpoint
		resp, err := client.Get(endpoint + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			fmt.Printf("✓ Connected to MCP proxy at: %s\n", endpoint)
			return endpoint
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	
	// If no endpoint works, return the first one and let the user know
	fallback := endpoints[0]
	fmt.Printf("⚠️  No MCP proxy detected, using fallback: %s\n", fallback)
	fmt.Printf("   To deploy MCP proxy to cluster, run: matey proxy\n")
	return fallback
}

// NewTermChat creates a new terminal chat instance
func NewTermChat() *TermChat {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Get terminal width
	width := 80 // default fallback
	if termWidth, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		width = termWidth
	}
	
	// Initialize markdown renderer with terminal width
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	
	// Initialize MCP client with dynamic cluster endpoint detection
	proxyURL := detectClusterMCPProxy()
	mcpClient := mcp.NewMCPClient(proxyURL)
	
	return &TermChat{
		ctx:              ctx,
		cancel:           cancel,
		chatHistory:      make([]TermChatMessage, 0),
		markdownRenderer: renderer,
		mcpClient:       mcpClient,
	}
}

// Run starts the terminal chat interface
func (tc *TermChat) Run() error {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		// Silently ignore missing .env file
	}
	
	// Check if we have OpenRouter API key
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		fmt.Println("⚠️  OpenRouter API key not found - responses may not work")
	}

	// Initialize AI manager
	aiConfig := ai.Config{
		DefaultProvider:   "openrouter",
		FallbackProviders: []string{"ollama", "openai", "claude"},
		Providers: map[string]ai.ProviderConfig{
			"ollama": {
				Endpoint:     "http://localhost:11434",
				DefaultModel: "llama3",
			},
			"openai": {
				APIKey:       os.Getenv("OPENAI_API_KEY"),
				Endpoint:     "https://api.openai.com/v1",
				DefaultModel: "gpt-4",
			},
			"claude": {
				APIKey:       os.Getenv("ANTHROPIC_API_KEY"),
				Endpoint:     "https://api.anthropic.com",
				DefaultModel: "claude-3-sonnet-20240229",
			},
			"openrouter": {
				APIKey:       os.Getenv("OPENROUTER_API_KEY"),
				Endpoint:     "https://openrouter.ai/api/v1",
				DefaultModel: "anthropic/claude-sonnet-4",
			},
		},
	}

	tc.aiManager = ai.NewManager(aiConfig)

	// Get current provider info
	if provider, err := tc.aiManager.GetCurrentProvider(); err == nil {
		tc.currentProvider = provider.Name()
		tc.currentModel = provider.DefaultModel()
	}

	// Add welcome message
	tc.addMessage("assistant", tc.getWelcomeMessage())

	// For now, use simple terminal output until we implement termbox
	tc.runSimple()
	return nil
}

// runSimple runs exactly like Claude Code - pure terminal output with natural scrolling
func (tc *TermChat) runSimple() {
	// Get terminal width for header
	width := 80 // default fallback
	if termWidth, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		width = termWidth
	}
	
	// Create header that spans full terminal width
	title := " Matey AI Chat - MCP orchestration assistant "
	padding := (width - len(title) - 2) / 2
	if padding < 0 {
		padding = 0
	}
	
	headerTop := "╭" + strings.Repeat("─", width-2) + "╮"
	headerMiddle := "│" + strings.Repeat(" ", padding) + "\033[1;37m" + title + "\033[0m\033[1;36m" + strings.Repeat(" ", width-len(title)-padding-2) + "│"
	headerBottom := "╰" + strings.Repeat("─", width-2) + "╯"
	
	fmt.Printf("\033[1;36m%s\033[0m\n", headerTop)
	fmt.Printf("\033[1;36m%s\033[0m\n", headerMiddle)
	fmt.Printf("\033[1;36m%s\033[0m\n", headerBottom)
	
	// Provider info with better formatting
	fmt.Printf("\033[90mProvider: \033[36m%s\033[0m \033[90m• Model: \033[37m%s\033[0m \033[90m• Time: \033[37m%s\033[0m\n", 
		tc.currentProvider, tc.currentModel, time.Now().Format("15:04:05"))
	fmt.Println()

	// Print initial messages
	for _, msg := range tc.chatHistory {
		tc.printMessage(msg)
	}
	
	// No need for input box - just clean prompt

	// Use bufio scanner for proper line reading
	scanner := bufio.NewScanner(os.Stdin)

	// Main chat loop - exactly like Claude Code
	for {
			// Simple clean prompt
		fmt.Printf("\n\033[1;34m❯ \033[0m")
		
		if !scanner.Scan() {
			break
		}
		
		input := strings.TrimSpace(scanner.Text())
		
		if input == "" {
			continue
		}
		
		// Handle commands
		if strings.HasPrefix(input, "/") {
			if tc.handleCommand(input) {
				break // Exit command
			}
			continue
		}

		// Add user message and print it
		tc.addMessage("user", input)
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])

		// Chat with AI
		tc.chatWithAI(input)
	}
}

// addMessage adds a message to chat history
func (tc *TermChat) addMessage(role, content string) {
	msg := TermChatMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}
	tc.chatHistory = append(tc.chatHistory, msg)
}

// printMessage prints a single message with proper markdown rendering
func (tc *TermChat) printMessage(msg TermChatMessage) {
	timestamp := msg.Timestamp.Format("15:04:05")
	
	switch msg.Role {
	case "user":
		fmt.Printf("\n\033[1;34m❯ You\033[0m \033[90m%s\033[0m\n%s\n", timestamp, msg.Content)
	case "assistant":
		// For AI messages, always render markdown and hide raw markdown from user
		fmt.Printf("\n\033[1;32m✓ AI\033[0m \033[90m%s\033[0m\n%s\n", timestamp, tc.renderMarkdown(msg.Content))
	case "system":
		// For system messages, always render markdown  
		fmt.Printf("\n\033[1;33m● System\033[0m \033[90m%s\033[0m\n%s\n", timestamp, tc.renderMarkdown(msg.Content))
	}
}

// chatWithAI sends a message to AI and streams the response
func (tc *TermChat) chatWithAI(message string) {
	// Build messages with system context and full conversation history
	messages := []ai.Message{
		{
			Role:    "system",
			Content: tc.getSystemContext(),
		},
	}
	
	// Add conversation history (excluding system messages like /help responses)
	for _, msg := range tc.chatHistory {
		if msg.Role == "user" || msg.Role == "assistant" {
			messages = append(messages, ai.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}
	
	// Add the current message
	messages = append(messages, ai.Message{
		Role:    "user",
		Content: message,
	})

	// Create context with longer timeout for streaming
	streamCtx, cancel := context.WithTimeout(tc.ctx, 5*time.Minute)
	defer cancel()
	
	// Get MCP functions for AI
	mcpFunctions := tc.getMCPFunctions()
	
	// Start streaming
	stream, err := tc.aiManager.StreamChatWithFallback(streamCtx, messages, ai.StreamOptions{
		Model:     tc.currentModel,
		Functions: mcpFunctions,
	})
	if err != nil {
		tc.addMessage("system", fmt.Sprintf("Error: %v", err))
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
		return
	}

	// Create AI message for streaming
	tc.addMessage("assistant", "")
	aiMsgIndex := len(tc.chatHistory) - 1
	
	fmt.Printf("\n\033[1;32m✓ AI\033[0m \033[90m%s\033[0m\n", time.Now().Format("15:04:05"))

	// Stream response
	thinkProcessor := ai.NewThinkProcessor()
	var functionCallsToProcess []ai.ToolCall
	
	// Accumulate function call arguments across streaming chunks
	accumulatedFunctionCalls := make(map[string]*ai.ToolCall)
	
	for response := range stream {
		if response.Error != nil {
			tc.chatHistory[aiMsgIndex].Content = fmt.Sprintf("Error: %v", response.Error)
			break
		}

		if response.Finished {
			break
		}

		if response.Content != "" || response.ThinkContent != "" {
			// Process any think tags
			regularContent := response.Content

			if response.Content != "" && (strings.Contains(response.Content, "<think>") || strings.Contains(response.Content, "</think>")) {
				regularContent, _ = ai.ProcessStreamContentWithThinks(thinkProcessor, response.Content)
			}

			// Print and accumulate content
			if regularContent != "" {
				// For streaming, we want to print raw content and render markdown only at the end
				fmt.Print(regularContent)
				tc.chatHistory[aiMsgIndex].Content += regularContent
			}
		}

		// Accumulate tool calls across streaming chunks
		if len(response.ToolCalls) > 0 {
			for _, toolCall := range response.ToolCalls {
				if toolCall.Type == "function" {
					// Use tool call ID to accumulate arguments
					callId := toolCall.ID
					if callId == "" {
						// Fallback to function name if no ID
						callId = toolCall.Function.Name
					}
					
					if existing, exists := accumulatedFunctionCalls[callId]; exists {
						// Accumulate arguments for existing function call
						existing.Function.Arguments += toolCall.Function.Arguments
					} else {
						// New function call
						accumulatedFunctionCalls[callId] = &ai.ToolCall{
							ID:   toolCall.ID,
							Type: toolCall.Type,
							Function: ai.FunctionCall{
								Name:      toolCall.Function.Name,
								Arguments: toolCall.Function.Arguments,
							},
						}
					}
				}
			}
		}

		// Note: XML parsing moved to after streaming is complete
	}
	
	// Convert accumulated function calls to the processing list
	for _, call := range accumulatedFunctionCalls {
		if call.Function.Name != "" {
			functionCallsToProcess = append(functionCallsToProcess, *call)
		}
	}
	
	// Process any function calls that were collected during streaming
	if len(functionCallsToProcess) > 0 {
		tc.processFunctionCallsWithContinuation(functionCallsToProcess, aiMsgIndex)
	}
	
	// After streaming is complete, parse and execute any XML function calls
	tc.parseAndExecuteXMLFunctionCalls(tc.chatHistory[aiMsgIndex].Content, aiMsgIndex)
	
	// After streaming is complete, re-render the complete message with markdown formatting
	fmt.Print("\r\033[2K") // Clear the line
	fmt.Printf("\033[1;32m✓ AI\033[0m \033[90m%s\033[0m\n", time.Now().Format("15:04:05"))
	
	// Strip XML function calls from final display
	cleanContent := tc.stripXMLFunctionCalls(tc.chatHistory[aiMsgIndex].Content)
	tc.chatHistory[aiMsgIndex].Content = cleanContent
	
	fmt.Print(tc.renderMarkdown(cleanContent))
	fmt.Println()
	
	// No input box to re-print
}

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

// No longer needed - removed input box for simplicity

// handleCommand processes slash commands
func (tc *TermChat) handleCommand(command string) bool {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}

	switch parts[0] {
	case "/help":
		tc.addMessage("system", tc.getHelpText())
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
	case "/provider":
		if len(parts) > 1 {
			tc.switchProvider(parts[1])
		} else {
			tc.addMessage("system", "Usage: /provider <name>\nAvailable: ollama, openai, claude, openrouter")
			tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
		}
	case "/providers":
		tc.listProviders()
	case "/model":
		if len(parts) > 1 {
			tc.switchModel(parts[1])
		} else {
			tc.addMessage("system", "Usage: /model <name>")
			tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
		}
	case "/models":
		tc.listModels()
	case "/status":
		tc.showStatus()
	case "/clear":
		tc.chatHistory = tc.chatHistory[:0]
		tc.addMessage("system", "Chat history cleared.")
		// Clear screen
		fmt.Print("\033[2J\033[H")
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
	case "/exit", "/quit":
		fmt.Println("Goodbye!")
		return true
	default:
		tc.addMessage("system", fmt.Sprintf("Unknown command: %s\nType '/help' for available commands", parts[0]))
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
	}
	return false
}

// switchProvider changes the AI provider
func (tc *TermChat) switchProvider(name string) {
	if err := tc.aiManager.SwitchProvider(name); err != nil {
		tc.addMessage("system", fmt.Sprintf("Failed to switch provider: %s", err))
	} else {
		tc.currentProvider = name
		if provider, err := tc.aiManager.GetCurrentProvider(); err == nil {
			tc.currentModel = provider.DefaultModel()
		}
		tc.addMessage("system", fmt.Sprintf("Switched to provider: %s", name))
	}
	tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
}

// switchModel changes the AI model
func (tc *TermChat) switchModel(name string) {
	if err := tc.aiManager.SwitchModel(name); err != nil {
		tc.addMessage("system", fmt.Sprintf("Failed to switch model: %s", err))
	} else {
		tc.currentModel = name
		tc.addMessage("system", fmt.Sprintf("Switched to model: %s", name))
	}
	tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
}

// listProviders shows available AI providers
func (tc *TermChat) listProviders() {
	providers := "## Available AI Providers:\n\n" +
		"- **openrouter** - Claude Sonnet 4 and premium models\n" +
		"- **ollama** - Local models (llama3, qwen3, etc.)\n" +
		"- **claude** - Direct Anthropic API\n" +
		"- **openai** - GPT-4 and OpenAI models\n\n" +
		"**Current provider:** " + tc.currentProvider + "\n\n" +
		"Use `/provider <name>` to switch providers."
	tc.addMessage("system", providers)
	tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
}

// listModels shows available models for current provider
func (tc *TermChat) listModels() {
	models := "## Available Models:\n\n"
	
	switch tc.currentProvider {
	case "openrouter":
		models += "- **anthropic/claude-sonnet-4** - Latest Claude Sonnet\n" +
			"- **anthropic/claude-3.5-sonnet** - Claude 3.5 Sonnet\n" +
			"- **openai/gpt-4** - GPT-4\n" +
			"- **meta-llama/llama-3.1-70b-instruct** - Llama 3.1 70B\n"
	case "ollama":
		models += "- **llama3** - Llama 3 8B\n" +
			"- **qwen3** - Qwen 3\n" +
			"- **mistral** - Mistral 7B\n" +
			"- **codellama** - Code Llama\n"
	case "claude":
		models += "- **claude-3-sonnet-20240229** - Claude 3 Sonnet\n" +
			"- **claude-3-haiku-20240307** - Claude 3 Haiku\n"
	case "openai":
		models += "- **gpt-4** - GPT-4\n" +
			"- **gpt-4-turbo** - GPT-4 Turbo\n" +
			"- **gpt-3.5-turbo** - GPT-3.5 Turbo\n"
	default:
		models += "Unknown provider. Switch to a known provider first.\n"
	}
	
	models += "\n**Current model:** " + tc.currentModel + "\n\n" +
		"Use `/model <name>` to switch models."
	tc.addMessage("system", models)
	tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
}

// showStatus displays system status
func (tc *TermChat) showStatus() {
	status := fmt.Sprintf("Provider: %s\nModel: %s\nMessages: %d", 
		tc.currentProvider, tc.currentModel, len(tc.chatHistory))
	tc.addMessage("system", status)
	tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
}

// getWelcomeMessage returns a concise quickstart message
func (tc *TermChat) getWelcomeMessage() string {
	return "# Welcome to Matey AI Chat\n\n" +
		"I help you orchestrate **MCP server applications** using Kubernetes.\n\n" +
		"## Quick Start:\n\n" +
		"    Show me running servers\n" +
		"    Get my latest glucose readings\n" +
		"    Search for \"kubernetes tutorial\"\n" +
		"    Create a workflow for daily reports\n" +
		"    Help me set up GitHub integration\n\n" +
		"## Commands:\n\n" +
		"•  `/help`  - Show available commands\n" +
		"•  `/status`  - System status\n" +
		"•  `/clear`  - Clear chat\n\n" +
		"Just tell me what you want to build or ask about your servers!"
}

// getHelpText returns comprehensive help information
func (tc *TermChat) getHelpText() string {
	return "# Matey AI Chat - Commands\n\n" +
		"## Chat Commands:\n\n" +
		"| Command | Description |\n" +
		"|---------|-------------|\n" +
		"| `/help` | Show this command list |\n" +
		"| `/status` | Show system and connection status |\n" +
		"| `/clear` | Clear chat history |\n" +
		"| `/exit` or `/quit` | Exit chat |\n\n" +
		"## AI Provider Commands:\n\n" +
		"| Command | Description |\n" +
		"|---------|-------------|\n" +
		"| `/provider <name>` | Switch AI provider |\n" +
		"| `/providers` | List available providers |\n" +
		"| `/model <name>` | Switch AI model |\n" +
		"| `/models` | List available models |\n\n" +
		"## Available Providers:\n\n" +
		"- **openrouter** - Claude Sonnet 4 and premium models\n" +
		"- **ollama** - Local models (llama3, qwen3, etc.)\n" +
		"- **claude** - Direct Anthropic API\n" +
		"- **openai** - GPT-4 and OpenAI models\n\n" +
		"## Example Usage:\n\n" +
		"```\n" +
		"/provider ollama\n" +
		"/model llama3\n" +
		"/status\n" +
		"```\n\n" +
		"## Natural Language - Just Ask:\n\n" +
		"```\n" +
		"Show me running servers\n" +
		"Get my latest glucose readings\n" +
		"Search for \"kubernetes tutorial\"\n" +
		"Create a workflow for daily reports\n" +
		"Help me set up GitHub integration\n" +
		"```\n\n" +
		"## Essential Matey CLI Commands:\n\n" +
		"```bash\n" +
		"matey ps                 # Check server status\n" +
		"matey up                 # Start all servers\n" +
		"matey logs [server] -f   # Follow logs\n" +
		"matey top                # Live server view\n" +
		"```\n\n" +
		"---\n\n" +
		"**Just describe what you want to build and I'll help you configure it!**"
}

// getSystemContext returns the system context for the AI
func (tc *TermChat) getSystemContext() string {
	// Get available MCP tools dynamically
	mcpTools := tc.getMCPToolsContext()
	
	// Get function definitions for schema validation
	functionDefs := tc.generateFunctionSchemas()
	
	return "# Matey AI Assistant - Kubernetes MCP Server Orchestration\n\n" +
		"You are an autonomous AI assistant for managing MCP (Model Context Protocol) servers in Kubernetes.\n\n" +
		"## 🎯 CRITICAL: FUNCTION CALLING REQUIREMENTS\n\n" +
		"### MANDATORY: Always Provide Function Arguments\n" +
		"- **NEVER call functions without required arguments**\n" +
		"- **ALWAYS pass proper JSON objects as function arguments**\n" +
		"- **Check function schemas below for required parameters**\n" +
		"- **If you call a function without arguments, it will FAIL**\n\n" +
		"### Function Call Format\n" +
		"Use your model's native function calling system. Examples:\n" +
		"```\n" +
		"// ✅ CORRECT - With required arguments\n" +
		"create_workflow({\n" +
		"  \"name\": \"glucose-tracker\",\n" +
		"  \"steps\": [{\"name\": \"check-glucose\", \"tool\": \"get_current_glucose\"}]\n" +
		"})\n\n" +
		"// ❌ WRONG - No arguments (will fail)\n" +
		"create_workflow()\n" +
		"```\n\n" +
		"## 🔧 Available Function Schemas\n" +
		functionDefs +
		"\n\n" +
		"## 🌐 Dynamic MCP Tools\n" +
		mcpTools +
		"\n\n" +
		"## 🚀 AGENTIC BEHAVIOR\n" +
		"You are autonomous - work until the user's goal is 100% complete:\n" +
		"1. **Understand** the user's goal\n" +
		"2. **Execute** function calls with proper arguments\n" +
		"3. **Create** actual working configurations (not examples)\n" +
		"4. **Deploy** using matey tools\n" +
		"5. **Verify** everything works\n" +
		"6. **Continue** until fully complete\n\n" +
		"### Workflow Creation Process\n" +
		"When user asks to create a workflow:\n" +
		"1. Call data gathering functions first (e.g., `get_current_glucose()`)\n" +
		"2. **Immediately** call `create_workflow()` with proper arguments:\n" +
		"   - Required: `name` (string), `steps` (array)\n" +
		"   - Optional: `description`, `schedule`\n" +
		"3. Call `matey_up()` to deploy\n" +
		"4. Call `matey_ps()` to verify\n\n" +
		"## 📋 Key Matey Commands\n" +
		"- `matey_ps()` - Check server status\n" +
		"- `matey_up()` - Start services\n" +
		"- `matey_down()` - Stop services\n" +
		"- `matey_logs({\"server\": \"name\"})` - Get logs\n" +
		"- `create_workflow({\"name\": \"...\", \"steps\": [...]})` - Create workflows\n" +
		"- `apply_config({\"config_yaml\": \"...\", \"config_type\": \"...\"})` - Apply K8s configs\n\n" +
		"## ⚠️ CRITICAL REMINDERS\n" +
		"- **ALWAYS provide function arguments as JSON objects**\n" +
		"- **Check the function schemas above for required parameters**\n" +
		"- **Never call functions without their required arguments**\n" +
		"- **Work autonomously until the task is 100% complete**\n" +
		"- **Create and deploy actual configurations, not examples**\n\n" +
		"When ready, execute functions with proper arguments to help the user achieve their goals!"
}

// generateFunctionSchemas generates detailed function schemas for the AI
func (tc *TermChat) generateFunctionSchemas() string {
	if tc.mcpClient == nil {
		return "### Function Schemas: Not connected to cluster\n"
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Get available servers
	servers, err := tc.mcpClient.ListServers(ctx)
	if err != nil {
		return fmt.Sprintf("### Function Schemas: Error listing servers: %v\n", err)
	}
	
	if len(servers) == 0 {
		return "### Function Schemas: No servers found\n"
	}
	
	var schemas strings.Builder
	schemas.WriteString("### Function Schemas with Required Parameters:\n\n")
	
	// Convert each server's tools to detailed schemas
	for _, server := range servers {
		serverCtx, serverCancel := context.WithTimeout(ctx, 5*time.Second)
		tools, err := tc.mcpClient.GetServerTools(serverCtx, server.Name)
		serverCancel()
		
		if err != nil {
			continue
		}
		
		for _, tool := range tools {
			schemas.WriteString(fmt.Sprintf("#### `%s` (from %s server)\n", tool.Name, server.Name))
			schemas.WriteString(fmt.Sprintf("**Description:** %s\n\n", tool.Description))
			
			if tool.InputSchema != nil {
				if inputSchema, ok := tool.InputSchema.(map[string]interface{}); ok {
					// Extract required parameters
					if required, hasRequired := inputSchema["required"].([]interface{}); hasRequired && len(required) > 0 {
						schemas.WriteString("**Required Parameters:**\n")
						for _, req := range required {
							if reqStr, ok := req.(string); ok {
								schemas.WriteString(fmt.Sprintf("- `%s`\n", reqStr))
							}
						}
						schemas.WriteString("\n")
					}
					
					// Extract properties
					if props, hasProps := inputSchema["properties"].(map[string]interface{}); hasProps {
						schemas.WriteString("**Parameters:**\n")
						for propName, propDef := range props {
							if propMap, ok := propDef.(map[string]interface{}); ok {
								propType := "unknown"
								propDesc := ""
								
								if t, hasType := propMap["type"].(string); hasType {
									propType = t
								}
								if d, hasDesc := propMap["description"].(string); hasDesc {
									propDesc = d
								}
								
								schemas.WriteString(fmt.Sprintf("- `%s` (%s): %s\n", propName, propType, propDesc))
							}
						}
						schemas.WriteString("\n")
					}
					
					// Add example call
					schemas.WriteString("**Example Call:**\n")
					schemas.WriteString("```javascript\n")
					
					// Generate example based on schema
					exampleArgs := make(map[string]interface{})
					if props, hasProps := inputSchema["properties"].(map[string]interface{}); hasProps {
						if required, hasRequired := inputSchema["required"].([]interface{}); hasRequired {
							for _, req := range required {
								if reqStr, ok := req.(string); ok {
									if propDef, hasProp := props[reqStr].(map[string]interface{}); hasProp {
										if propType, hasType := propDef["type"].(string); hasType {
											switch propType {
											case "string":
												exampleArgs[reqStr] = "example-value"
											case "array":
												exampleArgs[reqStr] = []interface{}{"item1", "item2"}
											case "object":
												exampleArgs[reqStr] = map[string]interface{}{"key": "value"}
											case "boolean":
												exampleArgs[reqStr] = true
											case "integer":
												exampleArgs[reqStr] = 1
											default:
												exampleArgs[reqStr] = "value"
											}
										}
									}
								}
							}
						}
					}
					
					// Special handling for create_workflow
					if tool.Name == "create_workflow" {
						exampleArgs = map[string]interface{}{
							"name": "example-workflow",
							"description": "Example workflow description",
							"steps": []interface{}{
								map[string]interface{}{
									"name": "step1",
									"tool": "get_current_glucose",
								},
							},
						}
					}
					
					if len(exampleArgs) > 0 {
						exampleJSON, _ := json.MarshalIndent(exampleArgs, "", "  ")
						schemas.WriteString(fmt.Sprintf("%s(%s)\n", tool.Name, string(exampleJSON)))
					} else {
						schemas.WriteString(fmt.Sprintf("%s()\n", tool.Name))
					}
					
					schemas.WriteString("```\n\n")
				}
			} else {
				schemas.WriteString("**Parameters:** None required\n")
				schemas.WriteString("**Example Call:**\n")
				schemas.WriteString("```javascript\n")
				schemas.WriteString(fmt.Sprintf("%s()\n", tool.Name))
				schemas.WriteString("```\n\n")
			}
		}
	}
	
	return schemas.String()
}

// getMCPToolsContext gets the available MCP tools and formats them for the AI
func (tc *TermChat) getMCPToolsContext() string {
	if tc.mcpClient == nil {
		return "### MCP Tools: Not connected to cluster\n"
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Get server list first for debugging
	servers, err := tc.mcpClient.ListServers(ctx)
	if err != nil {
		return fmt.Sprintf("### MCP Tools: Error listing servers: %v\n", err)
	}
	
	if len(servers) == 0 {
		return "### MCP Tools: No servers found\n" +
			"This usually means:\n" +
			"1. No MCP servers are running - run 'matey up' to start them\n" +
			"2. MCP proxy is not running - run 'matey proxy' to start it\n" +
			"3. Servers haven't registered with the proxy yet\n"
	}
	
	tools := tc.mcpClient.GetAvailableTools(ctx)
	return tools
}

// getMCPFunctions converts MCP tools to AI function definitions
func (tc *TermChat) getMCPFunctions() []ai.Function {
	if tc.mcpClient == nil {
		return []ai.Function{}
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	
	// Get available servers
	servers, err := tc.mcpClient.ListServers(ctx)
	if err != nil {
		return []ai.Function{}
	}
	
	if len(servers) == 0 {
		return []ai.Function{}
	}
	
	var functions []ai.Function
	
	// Convert each server's tools to function definitions
	for _, server := range servers {
		// Create a reasonable timeout for individual server requests
		serverCtx, serverCancel := context.WithTimeout(ctx, 10*time.Second)
		tools, err := tc.mcpClient.GetServerTools(serverCtx, server.Name)
		serverCancel()
		
		if err != nil {
			continue // Skip failed servers and continue with others
		}
		
		for _, tool := range tools {
			// Use the tool name directly
			parameters := make(map[string]interface{})
			if tool.InputSchema != nil {
				if inputSchema, ok := tool.InputSchema.(map[string]interface{}); ok {
					parameters = inputSchema
				} else {
					parameters = map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{},
					}
				}
			} else {
				parameters = map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{},
				}
			}
			
			function := ai.Function{
				Name:        tool.Name,
				Description: fmt.Sprintf("%s (from %s server)", tool.Description, server.Name),
				Parameters:  parameters,
			}
			
			
			functions = append(functions, function)
		}
	}
	
	return functions
}

// executeToolCall executes an MCP tool call and appends the result to the chat
func (tc *TermChat) executeToolCall(serverName, toolName string, arguments map[string]interface{}, aiMsgIndex int) {
	if tc.mcpClient == nil {
		return
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	// Execute the tool call
	result := tc.mcpClient.ExecuteToolCall(ctx, serverName, toolName, arguments)
	
	// Add the result to the AI message
	tc.chatHistory[aiMsgIndex].Content += "\n\n" + result
	
	// Re-render the complete message
	fmt.Print("\n\n" + tc.renderMarkdown(result))
}

// executeNativeToolCall executes a native function call from the AI
func (tc *TermChat) executeNativeToolCall(toolCall ai.ToolCall, aiMsgIndex int) {
	if tc.mcpClient == nil {
		return
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	toolName := toolCall.Function.Name
	var explicitServerName string
	
	// Skip empty tool names to avoid "Tool '' not found" errors
	if toolName == "" {
		return
	}
	
	// Check if tool name includes server prefix (e.g., "matey.create_workflow")
	if strings.Contains(toolName, ".") {
		parts := strings.SplitN(toolName, ".", 2)
		if len(parts) == 2 {
			explicitServerName = parts[0]
			toolName = parts[1]
		}
	}
	
	// Parse arguments from JSON
	var arguments map[string]interface{}
	
	// Debug: Always show what arguments we received
	fmt.Printf("\nDEBUG: Raw arguments for '%s': '%s'\n", toolName, toolCall.Function.Arguments)
	
	if toolCall.Function.Arguments == "" {
		// If no arguments provided, use empty map
		arguments = make(map[string]interface{})
		fmt.Printf("DEBUG: No arguments provided, using empty map\n")
	} else {
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &arguments); err != nil {
			fmt.Printf("\n❌ Failed to parse function arguments: %v\n", err)
			fmt.Printf("Debug: Raw arguments: '%s'\n", toolCall.Function.Arguments)
			return
		}
		fmt.Printf("DEBUG: Parsed arguments: %+v\n", arguments)
	}
	
	// Find which server has this tool
	var serverName string
	if explicitServerName != "" {
		// Use explicitly specified server
		serverName = explicitServerName
	} else {
		// Search for tool across all servers
		serverName = tc.findServerForTool(toolName)
		if serverName == "" {
			fmt.Printf("\n❌ Tool '%s' not found on any server\n", toolName)
			return
		}
	}
	
	// Debug: Print arguments being passed
	fmt.Printf("\nDEBUG: Tool '%s' arguments: %+v\n", toolName, arguments)
	
	// Format function call display nicely
	tc.printFunctionCall(serverName, toolName, arguments)
	
	// Execute the tool call
	result := tc.mcpClient.ExecuteToolCall(ctx, serverName, toolName, arguments)
	
	// Format and print the result
	tc.printFunctionResult(result)
	
	// Add the result to the AI message
	tc.chatHistory[aiMsgIndex].Content += "\n\n" + result
}

// printFunctionCall prints a nicely formatted function call
func (tc *TermChat) printFunctionCall(serverName, toolName string, arguments map[string]interface{}) {
	// Create a concise, elegant function call display
	fmt.Printf("\n\033[1;35m╭─ Function Call ─────────────────────────╮\033[0m\n")
	fmt.Printf("\033[1;35m│\033[0m \033[1;37m%s\033[0m\033[1;90m.\033[0m\033[1;36m%s\033[0m", serverName, toolName)
	
	// Add arguments if they exist
	if len(arguments) > 0 {
		argStr := ""
		for key, value := range arguments {
			if argStr != "" {
				argStr += ", "
			}
			argStr += fmt.Sprintf("%s: %v", key, value)
		}
		if len(argStr) > 30 {
			argStr = argStr[:27] + "..."
		}
		fmt.Printf("\033[1;90m(%s)\033[0m", argStr)
	}
	
	fmt.Printf("\n\033[1;35m╰─────────────────────────────────────────╯\033[0m\n")
}

// printFunctionResult prints a nicely formatted function result
func (tc *TermChat) printFunctionResult(result string) {
	// Parse the result to see if it's an error or success
	isError := strings.Contains(result, "Error calling") || strings.Contains(result, "Tool error")
	isSuccess := strings.Contains(result, "executed successfully")
	
	if isError {
		fmt.Printf("\n\033[1;31m╭─ Function Result (Error) ───────────────╮\033[0m\n")
		fmt.Printf("\033[1;31m│\033[0m %s\n", tc.renderMarkdown(result))
		fmt.Printf("\033[1;31m╰─────────────────────────────────────────╯\033[0m\n")
	} else if isSuccess {
		fmt.Printf("\n\033[1;32m╭─ Function Result (Success) ─────────────╮\033[0m\n")
		fmt.Printf("\033[1;32m│\033[0m %s\n", tc.renderMarkdown(result))
		fmt.Printf("\033[1;32m╰─────────────────────────────────────────╯\033[0m\n")
	} else {
		fmt.Printf("\n\033[1;34m╭─ Function Result ───────────────────────╮\033[0m\n")
		fmt.Printf("\033[1;34m│\033[0m %s\n", tc.renderMarkdown(result))
		fmt.Printf("\033[1;34m╰─────────────────────────────────────────╯\033[0m\n")
	}
}

// stripXMLFunctionCalls removes XML function call tags from content for display
func (tc *TermChat) stripXMLFunctionCalls(content string) string {
	// Remove function_calls blocks
	for strings.Contains(content, "<function_calls>") {
		start := strings.Index(content, "<function_calls>")
		end := strings.Index(content, "</function_calls>")
		if start != -1 && end != -1 && end > start {
			content = content[:start] + content[end+len("</function_calls>"):]
		} else {
			break
		}
	}
	
	// Remove function_result blocks
	for strings.Contains(content, "<function_result>") {
		start := strings.Index(content, "<function_result>")
		end := strings.Index(content, "</function_result>")
		if start != -1 && end != -1 && end > start {
			content = content[:start] + content[end+len("</function_result>"):]
		} else {
			break
		}
	}
	
	return content
}

// parseAndExecuteXMLFunctionCalls parses XML-style function calls from content
func (tc *TermChat) parseAndExecuteXMLFunctionCalls(content string, aiMsgIndex int) {
	// Look for <function_calls> blocks
	if strings.Contains(content, "<function_calls>") && strings.Contains(content, "</function_calls>") {
		// Extract function calls content
		start := strings.Index(content, "<function_calls>")
		end := strings.Index(content, "</function_calls>")
		if start != -1 && end != -1 && end > start {
			funcCallsContent := content[start+len("<function_calls>"):end]
			
			// Parse individual invoke calls
			tc.parseInvokeCalls(funcCallsContent, aiMsgIndex)
		}
	}
}

// parseInvokeCalls parses individual invoke calls from function_calls content
func (tc *TermChat) parseInvokeCalls(content string, aiMsgIndex int) {
	// Look for <invoke name="toolName"> blocks
	lines := strings.Split(content, "\n")
	var currentTool string
	var currentArgs map[string]interface{}
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		// Parse invoke tag
		if strings.HasPrefix(line, "<invoke name=\"") {
			// Extract tool name
			start := strings.Index(line, "\"") + 1
			end := strings.Index(line[start:], "\"")
			if end != -1 {
				currentTool = line[start:start+end]
				currentArgs = make(map[string]interface{})
			}
		}
		
		// Parse parameter tags
		if strings.HasPrefix(line, "<parameter name=\"") && currentTool != "" {
			// Extract parameter name
			nameStart := strings.Index(line, "\"") + 1
			nameEnd := strings.Index(line[nameStart:], "\"")
			if nameEnd != -1 {
				paramName := line[nameStart:nameStart+nameEnd]
				
				// Extract parameter value
				valueStart := strings.Index(line, ">") + 1
				valueEnd := strings.Index(line, "</parameter>")
				if valueStart != -1 && valueEnd != -1 && valueEnd > valueStart {
					paramValue := line[valueStart:valueEnd]
					currentArgs[paramName] = paramValue
				}
			}
		}
		
		// Execute when we reach the end of an invoke block
		if strings.HasPrefix(line, "</invoke>") && currentTool != "" {
			go tc.executeXMLFunctionCall(currentTool, currentArgs, aiMsgIndex)
			currentTool = ""
			currentArgs = nil
		}
	}
}

// executeXMLFunctionCall executes a function call parsed from XML
func (tc *TermChat) executeXMLFunctionCall(toolName string, arguments map[string]interface{}, aiMsgIndex int) {
	if tc.mcpClient == nil {
		fmt.Printf("\nMCP client not available\n")
		return
	}
	
	// Skip empty tool names to avoid "Tool '' not found" errors
	if toolName == "" {
		return
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	// Find which server has this tool
	serverName := tc.findServerForTool(toolName)
	if serverName == "" {
		fmt.Printf("\n\033[1;31m╭─ Function Call Error ──────────────────╮\033[0m\n")
		fmt.Printf("\033[1;31m│\033[0m Tool '%s' not found on any server\n", toolName)
		fmt.Printf("\033[1;31m│\033[0m Run 'matey ps' to see available servers\n")
		fmt.Printf("\033[1;31m╰─────────────────────────────────────────╯\033[0m\n")
		return
	}
	
	// Format function call display nicely
	tc.printFunctionCall(serverName, toolName, arguments)
	
	// Execute the tool call
	result := tc.mcpClient.ExecuteToolCall(ctx, serverName, toolName, arguments)
	
	// Format and print the result
	tc.printFunctionResult(result)
	
	// Add the result to the AI message
	tc.chatHistory[aiMsgIndex].Content += "\n\n" + result
}

// findServerForTool finds which server has a specific tool
func (tc *TermChat) findServerForTool(toolName string) string {
	if tc.mcpClient == nil {
		return ""
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Get available servers
	servers, err := tc.mcpClient.ListServers(ctx)
	if err != nil {
		return ""
	}
	
	// Check each server for the tool
	for _, server := range servers {
		tools, err := tc.mcpClient.GetServerTools(ctx, server.Name)
		if err != nil {
			continue
		}
		
		for _, tool := range tools {
			if tool.Name == toolName {
				return server.Name
			}
		}
	}
	
	return ""
}

// processFunctionCallsWithContinuation executes function calls and continues the conversation
func (tc *TermChat) processFunctionCallsWithContinuation(functionCalls []ai.ToolCall, aiMsgIndex int) {
	if tc.mcpClient == nil {
		return
	}
	
	// Execute all function calls and collect results
	var functionResults []ai.Message
	
	for _, toolCall := range functionCalls {
		if toolCall.Function.Name == "" {
			continue
		}
		
		// Execute the function call and display it
		tc.executeNativeToolCall(toolCall, aiMsgIndex)
		
		// Get the actual result for the AI
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		serverName := tc.findServerForTool(toolCall.Function.Name)
		if serverName == "" {
			cancel()
			continue
		}
		
		// Parse arguments
		var arguments map[string]interface{}
		if toolCall.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &arguments); err != nil {
				cancel()
				continue
			}
		} else {
			arguments = make(map[string]interface{})
		}
		
		// Get structured result for the AI
		result, err := tc.mcpClient.CallTool(ctx, serverName, toolCall.Function.Name, arguments)
		cancel()
		
		if err != nil {
			// Add error result
			functionResults = append(functionResults, ai.Message{
				Role:       "tool",
				Content:    fmt.Sprintf("Error: %v", err),
				ToolCallId: toolCall.ID,
			})
		} else {
			// Add successful result
			var resultContent string
			if result.IsError {
				resultContent = fmt.Sprintf("Error: %s", result.Content[0].Text)
			} else {
				// Convert content to string
				var contentParts []string
				for _, content := range result.Content {
					contentParts = append(contentParts, content.Text)
				}
				resultContent = strings.Join(contentParts, "\n")
			}
			
			functionResults = append(functionResults, ai.Message{
				Role:       "tool",
				Content:    resultContent,
				ToolCallId: toolCall.ID,
			})
		}
	}
	
	// If we have function results, continue the conversation
	fmt.Printf("\n🔍 DEBUG: functionResults length: %d\n", len(functionResults))
	if len(functionResults) > 0 {
		fmt.Printf("🔄 TRIGGERING CONTINUATION with %d results\n", len(functionResults))
		tc.continueConversationWithFunctionResults(functionCalls, functionResults, aiMsgIndex)
	} else {
		fmt.Printf("❌ NO CONTINUATION - no function results\n")
	}
}

// continueConversationWithFunctionResults continues the AI conversation with function results
func (tc *TermChat) continueConversationWithFunctionResults(functionCalls []ai.ToolCall, functionResults []ai.Message, aiMsgIndex int) {
	// Build the conversation history including function results
	messages := []ai.Message{
		{
			Role:    "system",
			Content: tc.getSystemContext(),
		},
	}
	
	// Add conversation history up to the current AI message
	for i, msg := range tc.chatHistory {
		if i >= aiMsgIndex {
			break // Don't include the current AI message that's being built
		}
		if msg.Role == "user" || msg.Role == "assistant" {
			messages = append(messages, ai.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}
	
	// Add the current AI message with the function calls
	currentAIMessage := ai.Message{
		Role:      "assistant",
		Content:   tc.chatHistory[aiMsgIndex].Content,
		ToolCalls: functionCalls, // Use the original function calls
	}
	messages = append(messages, currentAIMessage)
	
	// Add function results
	for _, funcResult := range functionResults {
		messages = append(messages, funcResult)
	}
	
	// Add explicit continuation instruction if this looks like a workflow creation task
	lastUserMsg := ""
	if aiMsgIndex > 0 {
		for i := aiMsgIndex - 1; i >= 0; i-- {
			if tc.chatHistory[i].Role == "user" {
				lastUserMsg = strings.ToLower(tc.chatHistory[i].Content)
				break
			}
		}
	}
	
	// Add continuation prompt based on task type
	if strings.Contains(lastUserMsg, "workflow") || strings.Contains(lastUserMsg, "create") || strings.Contains(lastUserMsg, "build") {
		fmt.Printf("\n🤖 ADDING CONTINUATION PROMPT for task: %s\n", lastUserMsg)
		messages = append(messages, ai.Message{
			Role:    "user", 
			Content: "CONTINUE NOW: You MUST call create_workflow with proper JSON parameters. REQUIRED: name (string) and steps (array). Use this EXACT format:\n\ncreate_workflow({\n  \"name\": \"glucose-tracker\",\n  \"description\": \"Track glucose levels\",\n  \"steps\": [\n    {\"name\": \"check-glucose\", \"tool\": \"get_current_glucose\"}\n  ]\n})\n\nCall it RIGHT NOW with these parameters. Do not provide explanations first.",
		})
	} else {
		fmt.Printf("\n⚠️  No continuation prompt added for: %s\n", lastUserMsg)
	}
	
	// Continue the conversation
	streamCtx, cancel := context.WithTimeout(tc.ctx, 5*time.Minute)
	defer cancel()
	
	// Get MCP functions for AI
	mcpFunctions := tc.getMCPFunctions()
	
	// Start streaming the continuation
	stream, err := tc.aiManager.StreamChatWithFallback(streamCtx, messages, ai.StreamOptions{
		Model:     tc.currentModel,
		Functions: mcpFunctions,
	})
	if err != nil {
		fmt.Printf("\n❌ Error continuing conversation: %v\n", err)
		return
	}
	
	// Stream the continuation response
	thinkProcessor := ai.NewThinkProcessor()
	
	for response := range stream {
		if response.Error != nil {
			tc.chatHistory[aiMsgIndex].Content += fmt.Sprintf("\nError: %v", response.Error)
			break
		}

		if response.Finished {
			break
		}

		if response.Content != "" || response.ThinkContent != "" {
			// Process any think tags
			regularContent := response.Content

			if response.Content != "" && (strings.Contains(response.Content, "<think>") || strings.Contains(response.Content, "</think>")) {
				regularContent, _ = ai.ProcessStreamContentWithThinks(thinkProcessor, response.Content)
			}

			// Print and accumulate content
			if regularContent != "" {
				fmt.Print(regularContent)
				tc.chatHistory[aiMsgIndex].Content += regularContent
			}
		}

		// Handle additional function calls in the continuation
		if len(response.ToolCalls) > 0 {
			// For simplicity, execute them immediately (could be recursive)
			for _, toolCall := range response.ToolCalls {
				if toolCall.Type == "function" {
					tc.executeNativeToolCall(toolCall, aiMsgIndex)
				}
			}
		}
	}
}

// GetSystemContext exposes the system context for testing
func (tc *TermChat) GetSystemContext() string {
	return tc.getSystemContext()
}

// NewChatCommand creates the terminal chat command
func NewChatCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Launch natural scrolling terminal chat",
		Long:  "Launch a terminal chat interface with natural scrolling like traditional CLI tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			chat := NewTermChat()
			defer chat.cancel()
			return chat.Run()
		},
	}
}