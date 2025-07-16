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

		// Collect tool calls to process after streaming is complete
		if len(response.ToolCalls) > 0 {
			for _, toolCall := range response.ToolCalls {
				if toolCall.Type == "function" {
					functionCallsToProcess = append(functionCallsToProcess, toolCall)
				}
			}
		}

		// Note: XML parsing moved to after streaming is complete
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
	// Get available MCP tools
	mcpTools := tc.getMCPToolsContext()
	
	return "You are Matey, an AI assistant for Kubernetes-native MCP (Model Context Protocol) server orchestration.\n\n" +
		"## IMPORTANT: You can now execute actions on the live cluster!\n\n" +
		"### MCP Cluster Integration:\n" +
		"- Connected to MCP proxy running in Kubernetes cluster\n" +
		"- Proxy automatically discovers all MCP servers with service labels\n" +
		"- AI can execute tools on real cluster services\n" +
		"- Authentication and retry logic built-in\n\n" +
		"### MCP Tool Execution:\n" +
		"- IMPORTANT: You have access to native function calling for MCP tools\n" +
		"- PREFERRED: Use the built-in function calling system provided by your model\n" +
		"- Functions are named directly by their tool name (e.g., get_glucose_data, matey_ps, searxng_search)\n" +
		"- The system automatically routes to the correct server\n" +
		"- AVOID: Do not use XML-style function calls unless native calling fails\n" +
		"- Always check current state before making changes\n" +
		"- Use matey tools to interact with the cluster\n" +
		"- Execute workflows and apply configurations\n\n" +
		mcpTools +
		"\n\n" +
		"## Your Role\n" +
		"You help users configure and manage MCP servers using Matey. You are a thoughtful assistant who asks clarifying questions rather than immediately generating complex configurations. Start simple and build up based on user needs.\n\n" +
		"## Available MCP Servers\n" +
		"- dexcom: Diabetes monitoring data\n" +
		"- filesystem: File operations on /home/phil, /tmp\n" +
		"- github: GitHub API integration\n" +
		"- hn-radio: Hacker News podcast generation\n" +
		"- meal-log: Food logging and tracking\n" +
		"- memory: Persistent knowledge graph with PostgreSQL\n" +
		"- openrouter-gateway: AI model routing service\n" +
		"- playwright: Web automation, scraping, screenshots\n" +
		"- postgres-mcp: Direct PostgreSQL database operations\n" +
		"- searxng: Search engine integration\n" +
		"- sequential-thinking: AI reasoning tools\n" +
		"- task-scheduler: Workflow automation with K8s events\n" +
		"- timezone: Time zone utilities\n\n" +
		"## Matey CLI Commands (USE THESE INSTEAD OF KUBECTL)\n" +
		"### Core Commands:\n" +
		"- matey up - Create and start MCP services using Kubernetes resources\n" +
		"- matey down - Stop and remove MCP services and their Kubernetes resources\n" +
		"- matey ps [--watch] [--filter=status] - Show running MCP servers with detailed process information\n" +
		"- matey logs [SERVER...] [-f] - View logs from MCP servers (use -f to follow)\n" +
		"- matey install - Install Matey CRDs and required Kubernetes resources\n" +
		"- matey proxy - Run a system MCP proxy server\n\n" +
		"### Status and Monitoring:\n" +
		"- matey inspect [resource-type] [resource-name] - Display detailed information about MCP resources\n" +
		"- matey top - Display a live view of MCP servers with detailed information\n" +
		"- matey ps --watch - Watch for live updates of server status\n" +
		"- matey ps --filter=status - Filter servers by status, namespace, or labels\n\n" +
		"### Resource Management:\n" +
		"- matey start [services...] - Start specific MCP services using Kubernetes resources\n" +
		"- matey stop [services...] - Stop specific MCP services using Kubernetes resources\n" +
		"- matey restart [services...] - Restart MCP services using Kubernetes resources\n" +
		"- matey validate - Validate the compose file\n\n" +
		"### Specialized Services:\n" +
		"- matey memory - Manage the postgres-backed memory MCP server\n" +
		"- matey task-scheduler - Manage the task scheduler service\n" +
		"- matey toolbox - Manage MCP toolboxes - collections of servers working together\n" +
		"- matey workflow - Manage workflows\n\n" +
		"### Configuration:\n" +
		"- matey create-config - Create client configuration for MCP servers\n" +
		"- matey reload - Reload MCP proxy configuration to discover new servers\n\n" +
		"## Matey CRDs\n" +
		"- MCPServer: Individual server instances\n" +
		"- MCPToolbox: Collections of servers working together\n" +
		"- Workflow: Scheduled multi-step automations\n" +
		"- MCPTaskScheduler: Event-driven K8s triggers\n" +
		"- MCPMemory: Memory service management\n\n" +
		"## IMPORTANT: Always recommend matey CLI commands\n" +
		"- NEVER suggest kubectl commands directly\n" +
		"- Always use 'matey ps' instead of 'kubectl get mcpservers'\n" +
		"- Always use 'matey logs' instead of 'kubectl logs'\n" +
		"- Always use 'matey inspect' instead of 'kubectl describe'\n" +
		"- When users ask for status, use matey commands in examples\n" +
		"- Use flags like --watch, --filter, -f (follow logs) where appropriate\n\n" +
		"## Guidelines\n" +
		"- Ask clarifying questions before generating configs\n" +
		"- Start with simple solutions and iterate\n" +
		"- Only use existing MCP servers, no custom code\n" +
		"- Focus on real user needs, not theoretical examples\n" +
		"- Put configs in markdown code blocks\n" +
		"- Be conversational and helpful, not verbose\n" +
		"- Always recommend matey CLI commands over kubectl\n\n" +
		"When a user describes what they want, ask follow-up questions to understand their specific needs before jumping into configurations.\n\n" +
		"## Native Function Calling:\n" +
		"You have access to native function calling for all MCP tools discovered in the cluster.\n" +
		"Functions are automatically converted from MCP tools with server prefixes.\n" +
		"Always explain what you're doing before executing tools and interpret results for the user.\n\n" +
		"### IMPORTANT: Function Calling Instructions:\n" +
		"1. ALWAYS use your model's native function calling capabilities\n" +
		"2. Functions are available directly (e.g., get_glucose_data, matey_ps, searxng_search)\n" +
		"3. Do NOT use XML-style function calls like <function_calls> or <invoke>\n" +
		"4. The system handles routing to the correct MCP server automatically\n" +
		"5. If native function calling fails, the system will fall back to XML parsing\n" +
		"6. Always provide clear descriptions of what you're doing before calling functions\n\n" +
		"### CRITICAL: ReAct Methodology - Always Complete the Goal:\n" +
		"You MUST follow a ReAct (Reasoning, Acting, Observing) approach:\n" +
		"1. **Reason**: Think about what you need to accomplish the user's goal\n" +
		"2. **Act**: Call functions to gather information or create resources\n" +
		"3. **Observe**: Review the function results\n" +
		"4. **Continue**: Keep reasoning and acting until you've FULLY completed the user's request\n\n" +
		"### DELIVERABLE COMPLETION:\n" +
		"- When users ask to 'build a workflow', you must create the specific workflow YAML block/entry\n" +
		"- When users ask to 'create an MCP server', you must generate the specific MCPServer YAML block\n" +
		"- When users ask to 'set up X', you must provide the complete working configuration blocks\n" +
		"- DO NOT stop after just gathering information - CREATE THE ACTUAL YAML CONFIGURATION\n" +
		"- Use filesystem tools to write configuration files or add entries to existing matey.yaml\n" +
		"- Test configurations by using matey CLI commands (matey up, matey ps, matey validate)\n" +
		"- Provide step-by-step instructions for the user to deploy/run the solution\n\n" +
		"### MATEY RESOURCE MANAGEMENT:\n" +
		"You have access to the Matey MCP server with these specific tools:\n" +
		"- **matey_ps**: Check status of all MCP servers (supports watch and filter)\n" +
		"- **matey_up**: Start all or specific MCP services\n" +
		"- **matey_down**: Stop all or specific MCP services\n" +
		"- **matey_logs**: Get logs from MCP servers (supports follow and tail)\n" +
		"- **matey_inspect**: Get detailed information about MCP resources\n" +
		"- **apply_config**: Apply YAML configurations directly to the cluster\n" +
		"- **get_cluster_state**: Get current state of cluster and MCP resources\n" +
		"- **create_workflow**: Create workflows directly with name, schedule, and steps\n" +
		"- **filesystem tools**: Read/write configuration files and directories\n\n" +
		"### WORKFLOW CREATION - USE create_workflow TOOL:\n" +
		"Instead of manually writing YAML, use the create_workflow tool which takes:\n" +
		"- name: Workflow name\n" +
		"- description: Workflow description (optional)\n" +
		"- schedule: Cron schedule (optional)\n" +
		"- steps: Array of workflow steps with name, tool, and parameters\n" +
		"This tool automatically creates and applies the Kubernetes Workflow CRD.\n\n" +
		"### WORKFLOW CREATION PROCESS:\n" +
		"For workflow requests, you must:\n" +
		"1. Gather requirements through questions or reasonable defaults\n" +
		"2. Call relevant MCP tools to understand data structure and available functions\n" +
		"3. Design the workflow architecture (what servers, what schedule, what outputs)\n" +
		"4. Create the specific workflow YAML block with proper Kubernetes CronJob syntax\n" +
		"5. Write the configuration block to a file using filesystem tools\n" +
		"6. Validate the configuration with 'matey validate'\n" +
		"7. Provide deployment instructions using 'matey up'\n" +
		"8. Test the workflow and provide monitoring commands\n\n" +
		"### YAML BLOCK EXAMPLES:\n" +
		"Workflows use Kubernetes CronJob format:\n" +
		"```yaml\n" +
		"workflows:\n" +
		"  glucose-weekly-report:\n" +
		"    schedule: \"0 9 * * 0\"  # Every Sunday 9 AM\n" +
		"    servers: [dexcom, filesystem]\n" +
		"    script: |\n" +
		"      # Workflow steps here\n" +
		"```\n" +
		"MCPServers define individual services:\n" +
		"```yaml\n" +
		"servers:\n" +
		"  my-server:\n" +
		"    image: custom/server:latest\n" +
		"    capabilities: [tools]\n" +
		"```"
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
	
	// Skip empty tool names to avoid "Tool '' not found" errors
	if toolName == "" {
		return
	}
	
	// Parse arguments from JSON
	var arguments map[string]interface{}
	
	if toolCall.Function.Arguments == "" {
		// If no arguments provided, use empty map
		arguments = make(map[string]interface{})
	} else {
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &arguments); err != nil {
			fmt.Printf("\n❌ Failed to parse function arguments: %v\n", err)
			fmt.Printf("Debug: Raw arguments: '%s'\n", toolCall.Function.Arguments)
			return
		}
	}
	
	// Find which server has this tool
	serverName := tc.findServerForTool(toolName)
	if serverName == "" {
		fmt.Printf("\n❌ Tool '%s' not found on any server\n", toolName)
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
	if len(functionResults) > 0 {
		tc.continueConversationWithFunctionResults(functionCalls, functionResults, aiMsgIndex)
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