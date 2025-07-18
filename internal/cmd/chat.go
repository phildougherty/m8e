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
	verboseMode     bool // Toggle for compact/verbose output
	functionResults map[string]string // Store full results for toggle
	approvalMode    ApprovalMode // Controls autonomous behavior level
	maxTurns        int          // Maximum continuation turns to prevent infinite loops
	currentTurns    int          // Current turn count for this conversation
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
		verboseMode:     false, // Start in compact mode
		functionResults: make(map[string]string),
		approvalMode:    DEFAULT, // Start in manual mode for safety
		maxTurns:        10, // Reasonable limit to prevent infinite loops
		currentTurns:    0,  // Reset turn counter
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

	// Use simple input mode for command testing
	tc.runSimpleInput()
}

// runWithRawInput handles raw terminal input for keyboard shortcuts
func (tc *TermChat) runWithRawInput() {
	var inputBuffer strings.Builder
	buffer := make([]byte, 1)
	
	for {
		fmt.Printf("\n\033[1;34m❯ \033[0m")
		inputBuffer.Reset()
		
		for {
			n, err := os.Stdin.Read(buffer)
			if err != nil || n == 0 {
				return
			}
			
			switch buffer[0] {
			case 18: // Ctrl+R
				tc.verboseMode = !tc.verboseMode
				mode := "compact"
				if tc.verboseMode {
					mode = "verbose"
				}
				fmt.Printf("\n\033[90mSwitched to %s mode\033[0m\n", mode)
				continue
			case 25: // Ctrl+Y
				// Toggle between YOLO and DEFAULT mode
				if tc.approvalMode == YOLO {
					tc.approvalMode = DEFAULT
				} else {
					tc.approvalMode = YOLO
				}
				fmt.Printf("\n\033[90mApproval mode: %s - %s\033[0m\n", 
					tc.approvalMode.GetModeIndicatorNoEmoji(), 
					tc.approvalMode.Description())
				continue
			case 9: // Tab (we'll use this for AUTO_EDIT toggle since Shift+Tab is complex)
				// Toggle between AUTO_EDIT and DEFAULT mode
				if tc.approvalMode == AUTO_EDIT {
					tc.approvalMode = DEFAULT
				} else {
					tc.approvalMode = AUTO_EDIT
				}
				fmt.Printf("\n\033[90mApproval mode: %s - %s\033[0m\n", 
					tc.approvalMode.GetModeIndicatorNoEmoji(), 
					tc.approvalMode.Description())
				continue
			case 3: // Ctrl+C
				return
			case 4: // Ctrl+D
				return
			case 13: // Enter
				input := strings.TrimSpace(inputBuffer.String())
				if input != "" {
					tc.processInput(input)
					if input == "/exit" || input == "/quit" {
						return
					}
				}
				goto nextInput
			case 127: // Backspace
				if inputBuffer.Len() > 0 {
					content := inputBuffer.String()
					inputBuffer.Reset()
					inputBuffer.WriteString(content[:len(content)-1])
					fmt.Printf("\b \b")
				}
			default:
				if buffer[0] >= 32 && buffer[0] < 127 { // Printable characters
					inputBuffer.WriteByte(buffer[0])
					fmt.Printf("%c", buffer[0])
				}
			}
		}
		nextInput:
	}
}

// runSimpleInput handles simple line-based input (fallback)
func (tc *TermChat) runSimpleInput() {
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
		
		tc.processInput(input)
		if input == "/exit" || input == "/quit" {
			break
		}
	}
}

// processInput handles user input
func (tc *TermChat) processInput(input string) {
	// Handle commands
	if strings.HasPrefix(input, "/") {
		if tc.handleCommand(input) {
			return // Command processed successfully, including exit commands
		}
		return // Command processed (even if not found)
	}

	// Add user message and print it
	tc.addMessage("user", input)
	tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])

	// Reset turn counter for new user input
	tc.currentTurns = 0

	// Use conversation flow architecture
	tc.executeConversationFlow(input)
}

// executeConversationFlow implements the gemini-cli style conversation flow
func (tc *TermChat) executeConversationFlow(userInput string) {
	// Create a new conversation turn
	turn := NewConversationTurn(tc, userInput)
	
	// Execute the turn with automatic continuation
	ctx, cancel := context.WithTimeout(tc.ctx, 10*time.Minute) // Generous timeout for complex tasks
	defer cancel()
	
	if err := turn.Execute(ctx); err != nil {
		tc.addMessage("system", fmt.Sprintf("Conversation flow error: %v", err))
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
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
					// Use function name as key to ensure consistent accumulation
					// This handles the OpenRouter streaming pattern where function name is only in first chunk
					callId := toolCall.Function.Name
					if callId == "" {
						// For empty function names, use the first non-empty function name we've seen
						for _, existing := range accumulatedFunctionCalls {
							if existing.Function.Name != "" {
								callId = existing.Function.Name
								break
							}
						}
						if callId == "" {
							callId = "unknown"
						}
					}
					
					if existing, exists := accumulatedFunctionCalls[callId]; exists {
						// Accumulate arguments for existing function call
						existing.Function.Arguments += toolCall.Function.Arguments
						// Update function name if it was empty before
						if existing.Function.Name == "" && toolCall.Function.Name != "" {
							existing.Function.Name = toolCall.Function.Name
						}
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
	} else {
		// Check if AI is asking for permission and force continuation
		tc.checkForPermissionSeekingAndContinue(aiMsgIndex)
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

	// Debug output (remove in production)
	// fmt.Printf("\n[DEBUG] Processing command: '%s'\n", parts[0])

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
	case "/approval":
		if len(parts) > 1 {
			if mode, err := ParseApprovalMode(parts[1]); err == nil {
				tc.approvalMode = mode
				tc.addMessage("system", fmt.Sprintf("Approval mode set to: %s - %s", mode.GetModeIndicatorNoEmoji(), mode.Description()))
			} else {
				tc.addMessage("system", fmt.Sprintf("Invalid approval mode: %s\nValid modes: DEFAULT, AUTO_EDIT, YOLO", parts[1]))
			}
		} else {
			tc.addMessage("system", fmt.Sprintf("Current approval mode: %s - %s\nUsage: /approval <mode>\nValid modes: DEFAULT, AUTO_EDIT, YOLO", tc.approvalMode.GetModeIndicatorNoEmoji(), tc.approvalMode.Description()))
		}
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
	case "/yolo":
		tc.approvalMode = YOLO
		tc.addMessage("system", fmt.Sprintf("Approval mode set to: %s - %s", YOLO.GetModeIndicatorNoEmoji(), YOLO.Description()))
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
	case "/auto":
		tc.approvalMode = AUTO_EDIT
		tc.addMessage("system", fmt.Sprintf("Approval mode set to: %s - %s", AUTO_EDIT.GetModeIndicatorNoEmoji(), AUTO_EDIT.Description()))
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
	case "/manual":
		tc.approvalMode = DEFAULT
		tc.addMessage("system", fmt.Sprintf("Approval mode set to: %s - %s", DEFAULT.GetModeIndicatorNoEmoji(), DEFAULT.Description()))
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
	case "/clear":
		tc.chatHistory = tc.chatHistory[:0]
		tc.addMessage("system", "Chat history cleared.")
		// Clear screen
		fmt.Print("\033[2J\033[H")
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
	case "/exit", "/quit":
		fmt.Println("Goodbye!")
		return true
	case "/compact":
		tc.verboseMode = false
		tc.addMessage("system", "Switched to compact mode. Use Ctrl+R to toggle verbose mode.")
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
	case "/verbose":
		tc.verboseMode = true
		tc.addMessage("system", "Switched to verbose mode. Use Ctrl+R to toggle compact mode.")
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
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
	status := fmt.Sprintf("**Current Status:**\n\n"+
		"- **Provider:** %s\n"+
		"- **Model:** %s\n"+
		"- **Messages:** %d\n"+
		"- **Approval Mode:** %s - %s\n"+
		"- **Output Mode:** %s\n\n"+
		"**Keyboard Shortcuts:**\n"+
		"- `Ctrl+Y`: Toggle YOLO/DEFAULT mode\n"+
		"- `Tab`: Toggle AUTO_EDIT/DEFAULT mode\n"+
		"- `Ctrl+R`: Toggle verbose/compact output", 
		tc.currentProvider, tc.currentModel, len(tc.chatHistory),
		tc.approvalMode.GetModeIndicatorNoEmoji(), tc.approvalMode.Description(),
		func() string {
			if tc.verboseMode {
				return "Verbose"
			}
			return "Compact"
		}())
	tc.addMessage("system", status)
	tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
}

// getWelcomeMessage returns a concise quickstart message
func (tc *TermChat) getWelcomeMessage() string {
	return "# Welcome to Matey AI Chat\n\n" +
		"I help you orchestrate **MCP server applications** using Kubernetes.\n\n" +
		"## Quick Start:\n\n" +
		"    Deploy a new microservice to production\n" +
		"    Set up automated backup workflows\n" +
		"    Create a monitoring dashboard for my cluster\n" +
		"    Build a CI/CD pipeline with GitHub Actions\n" +
		"    Analyze my resource usage and optimize costs\n" +
		"    Configure alerting for critical services\n\n" +
		"## Commands:\n\n" +
		"•  `/help`  - Show available commands\n" +
		"•  `/status`  - System status\n" +
		"•  `/yolo`  - Full autonomy mode\n" +
		"•  `/auto`  - Auto-edit mode\n" +
		"•  `/manual`  - Manual confirmation mode\n" +
		"•  `/clear`  - Clear chat\n\n" +
		fmt.Sprintf("**Current Mode:** %s\n\n", tc.approvalMode.GetModeIndicatorNoEmoji()) +
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
		"| `/compact` | Switch to compact function output |\n" +
		"| `/verbose` | Switch to verbose function output |\n" +
		"| `/approval <mode>` | Set approval mode (DEFAULT/AUTO_EDIT/YOLO) |\n" +
		"| `/yolo` | Switch to YOLO mode (full autonomy) |\n" +
		"| `/auto` | Switch to AUTO_EDIT mode |\n" +
		"| `/manual` | Switch to DEFAULT mode (manual) |\n" +
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
		"## Approval Modes:\n\n" +
		"| Mode | Description |\n" +
		"|------|-------------|\n" +
		"| `DEFAULT` | Manual confirmation for all actions |\n" +
		"| `AUTO_EDIT` | Auto-approve file edits and safe operations |\n" +
		"| `YOLO` | Auto-approve everything (full autonomy) |\n\n" +
		"## Keyboard Shortcuts:\n\n" +
		"| Key | Action |\n" +
		"|-----|--------|\n" +
		"| `Ctrl+R` | Toggle verbose/compact mode |\n" +
		"| `Ctrl+Y` | Toggle YOLO/DEFAULT approval mode |\n" +
		"| `Tab` | Toggle AUTO_EDIT/DEFAULT approval mode |\n" +
		"| `Ctrl+C` | Exit chat |\n\n" +
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
		"**Just describe what you want to build and I'll help you configure it!**\n\n" +
		"**Function Output Modes:**\n\n" +
		"- **Compact mode** (default): Shows concise function results\n" +
		"- **Verbose mode**: Shows full function output\n" +
		"- **Toggle**: Use Ctrl+R to switch between modes\n"
}

// getSystemContext returns the system context for the AI
func (tc *TermChat) getSystemContext() string {
	// Get available MCP tools dynamically
	mcpTools := tc.getMCPToolsContext()
	
	// Get function definitions for schema validation
	functionDefs := tc.generateFunctionSchemas()
	
	return "# Matey AI Assistant - Kubernetes MCP Server Orchestration\n\n" +
		"You are an autonomous interactive CLI agent specializing in Kubernetes MCP server orchestration and management. Your primary goal is to help users safely and efficiently, executing actions immediately without asking for permission.\n\n" +
		"## Core Mandates\n\n" +
		"- **AUTONOMOUS CONVERSATION:** This is a natural conversation where tool calls are part of the dialogue flow\n" +
		"- **IMMEDIATE EXECUTION:** Execute function calls as they become necessary in the conversation\n" +
		"- **CONTINUE NATURALLY:** After tool execution, continue the conversation to complete the user's request\n" +
		"- **NO PERMISSION SEEKING:** NEVER ask 'Would you like me to...', 'Should I...', or 'Do you want me to...'\n" +
		"- **CONVERSATIONAL FLOW:** Chain function calls as part of natural conversation progression\n" +
		"- **COMPLETE TASKS:** Work until the user's request is fully accomplished\n" +
		"- **BE DIRECT:** Minimal explanations, maximum action - this is a working conversation\n\n" +
		"### CRITICAL FUNCTION EXECUTION REQUIREMENTS\n" +
		"- **NEVER call functions without required arguments - ALL CALLS WILL FAIL**\n" +
		"- **ALWAYS pass proper JSON objects as function arguments**\n" +
		"- **Check function schemas below for required parameters**\n" +
		"- **create_workflow REQUIRES 'name' and 'steps' parameters - DO NOT call with empty arguments**\n\n" +
		"### Function Call Format - MANDATORY EXAMPLES\n" +
		"Use your model's native function calling system. CRITICAL EXAMPLES:\n" +
		"```\n" +
		"// ✅ CORRECT - create_workflow with REQUIRED arguments\n" +
		"create_workflow({\n" +
		"  \"name\": \"monitoring-workflow\",\n" +
		"  \"description\": \"Monitor system health\",\n" +
		"  \"steps\": [\n" +
		"    {\"name\": \"check-health\", \"tool\": \"health_check\"},\n" +
		"    {\"name\": \"send-alert\", \"tool\": \"send_notification\"}\n" +
		"  ]\n" +
		"})\n\n" +
		"// ✅ CORRECT - Other functions with empty params if no required args\n" +
		"list_workflows({})\n" +
		"matey_ps({})\n\n" +
		"// ❌ ABSOLUTELY WRONG - create_workflow without arguments (GUARANTEED FAILURE)\n" +
		"create_workflow({})\n" +
		"create_workflow()\n" +
		"```\n\n" +
		"**CRITICAL:** If you call create_workflow without 'name' and 'steps', you will get \"name is required\" error.\n\n" +
		"## Available Function Schemas\n" +
		functionDefs +
		"\n\n" +
		"## Dynamic MCP Tools\n" +
		mcpTools +
		"\n\n" +
		"## MATEY PLATFORM CAPABILITIES\n" +
		"You can interact with the Matey platform using the comprehensive MCP tools available:\n\n" +
		"### Service Management\n" +
		"- `matey_ps()` - Check server status\n" +
		"- `matey_up()` - Start services\n" +
		"- `matey_down()` - Stop services\n" +
		"- `matey_logs({\"server\": \"name\"})` - Get logs\n" +
		"- `start_service({\"name\": \"service-name\"})` - Start specific service\n" +
		"- `stop_service({\"name\": \"service-name\"})` - Stop specific service\n" +
		"- `reload_proxy({})` - Reload proxy configuration\n\n" +
		"### Workflow Management\n" +
		"- `list_workflows({})` - List all workflows\n" +
		"- `get_workflow({\"name\": \"workflow-name\"})` - Get workflow details\n" +
		"- `create_workflow({\"name\": \"...\", \"steps\": [...]})` - Create workflows\n" +
		"- `delete_workflow({\"name\": \"workflow-name\"})` - Delete workflow\n" +
		"- `execute_workflow({\"name\": \"workflow-name\"})` - Execute workflow\n" +
		"- `workflow_logs({\"name\": \"workflow-name\"})` - Get workflow logs\n" +
		"- `pause_workflow({\"name\": \"workflow-name\"})` - Pause workflow\n" +
		"- `resume_workflow({\"name\": \"workflow-name\"})` - Resume workflow\n" +
		"- `workflow_templates({})` - Get workflow templates\n\n" +
		"### Memory & Task Management\n" +
		"- `k8s_memory({\"action\": \"start|stop|status\"})` - Memory service management\n" +
		"- `k8s_task_scheduler({\"action\": \"start|stop|status\"})` - Task scheduler management\n" +
		"- `memory_store({\"key\": \"...\", \"value\": \"...\"})` - Store memory\n" +
		"- `memory_retrieve({\"key\": \"...\"})` - Retrieve memory\n" +
		"- `memory_list({})` - List memory entries\n" +
		"- `memory_delete({\"key\": \"...\"})` - Delete memory\n" +
		"- `task_create({\"name\": \"...\", \"type\": \"...\"})` - Create task\n" +
		"- `task_list({})` - List tasks\n" +
		"- `task_status({\"id\": \"...\"})` - Get task status\n" +
		"- `task_cancel({\"id\": \"...\"})` - Cancel task\n\n" +
		"### Toolbox & Configuration\n" +
		"- `toolbox_install({\"name\": \"tool-name\"})` - Install toolbox\n" +
		"- `toolbox_list({})` - List toolboxes\n" +
		"- `toolbox_remove({\"name\": \"tool-name\"})` - Remove toolbox\n" +
		"- `config_get({\"key\": \"config-key\"})` - Get configuration\n" +
		"- `config_set({\"key\": \"...\", \"value\": \"...\"})` - Set configuration\n" +
		"- `config_list({})` - List configurations\n" +
		"- `apply_config({\"config_yaml\": \"...\", \"config_type\": \"...\"})` - Apply K8s configs\n\n" +
		"### MCP Server Management\n" +
		"- `add_mcp_server({\"name\": \"...\", \"url\": \"...\"})` - Add MCP server\n" +
		"- `list_mcp_servers({})` - List MCP servers\n" +
		"- `remove_mcp_server({\"name\": \"server-name\"})` - Remove MCP server\n" +
		"- `test_mcp_server({\"name\": \"server-name\"})` - Test MCP server\n\n" +
		"## Primary Workflows\n\n" +
		"### MCP Server Orchestration Tasks\n" +
		"When requested to perform orchestration tasks like managing services, workflows, or debugging:\n" +
		"1. **Understand:** Analyze the user's request and current system state using available MCP tools\n" +
		"2. **Execute:** Use appropriate MCP functions to accomplish the task immediately\n" +
		"3. **Verify:** Check results and continue with follow-up actions as needed\n" +
		"4. **Complete:** Ensure the full task is accomplished without stopping halfway\n\n" +
		"### Complex Multi-Step Tasks\n" +
		"**Goal:** Autonomously implement and deliver complete solutions using all available MCP tools\n" +
		"1. **Analyze Requirements:** Understand the user's request for workflows, configurations, or system changes\n" +
		"2. **Execute Plan:** Implement using appropriate MCP functions (create_workflow, config management, etc.)\n" +
		"3. **Verify and Continue:** Check results and continue with additional steps until fully complete\n\n" +
		"## Tone and Style (CLI Interaction)\n" +
		"- **Concise & Direct:** Professional, direct tone suitable for CLI environment\n" +
		"- **Minimal Output:** Aim for fewer than 3 lines of text output per response when practical\n" +
		"- **No Chitchat:** Avoid conversational filler, preambles, or postambles\n" +
		"- **Tools vs Text:** Use tools for actions, text only for essential communication\n" +
		"- **Action-Oriented:** Execute immediately, explain only when necessary\n\n" +
		"### Task Examples:\n" +
		"- **\"List my workflows\"** → Use `list_workflows({})`\n" +
		"- **\"Create a workflow\"** → Use `create_workflow()` with proper parameters\n" +
		"- **\"Check server status\"** → Use `matey_ps()`\n" +
		"- **\"Store some data\"** → Use `memory_store()` with key/value\n" +
		"- **\"Install a tool\"** → Use `toolbox_install()` with tool name\n\n" +
		"## Tool Usage Guidelines\n" +
		"- **Function Arguments:** Always provide function arguments as JSON objects\n" +
		"- **Schema Validation:** Check function schemas above for required parameters\n" +
		"- **Error Handling:** If a function fails, try a different approach or check requirements first\n" +
		"- **Avoid Loops:** Don't repeat the same failing function call - adapt your strategy\n" +
		"- **Appropriate Tools:** Select the right MCP tool for each specific request\n" +
		"- **Context Awareness:** Use tools to gather current state and understand requirements\n\n" +
		"## Final Reminder\n" +
		"You are an autonomous agent. Execute functions immediately to accomplish user goals. Work until tasks are 100% complete. Never ask for permission - analyze, decide, and act.\n\n" +
		"Available tools dynamically adapt based on connected MCP servers. Use them efficiently to help users achieve their Kubernetes orchestration goals!"
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
	
	// Note: Approval checking is now handled in the turn-based conversation flow
	
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
	
	if toolCall.Function.Arguments == "" || toolCall.Function.Arguments == "{}" {
		// If no arguments provided, use empty map
		arguments = make(map[string]interface{})
	} else {
		// Try to parse arguments, but be more forgiving of malformed JSON
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &arguments); err != nil {
			// If parsing fails, check if it's a streaming issue where arguments weren't fully assembled
			fmt.Printf("\n⚠️  Failed to parse function arguments: %v\n", err)
			// For now, use empty map and continue - this might be a streaming issue
			arguments = make(map[string]interface{})
		}
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
	
	// In compact mode, show minimal function call info
	if !tc.verboseMode {
		fmt.Printf("\n\033[1;35m[CALL]\033[0m %s.%s", serverName, toolName)
		if len(arguments) > 0 {
			fmt.Printf(" (with args)")
		}
		fmt.Printf(" -> ")
	} else {
		// Verbose mode: show full function call
		tc.printFunctionCall(serverName, toolName, arguments)
	}
	
	// Execute the tool call
	result := tc.mcpClient.ExecuteToolCall(ctx, serverName, toolName, arguments)
	
	// Format and print the result
	tc.printFunctionResult(result)
	
	// Add the result to the AI message
	tc.chatHistory[aiMsgIndex].Content += "\n\n" + result
}

// printFunctionCall prints a nicely formatted function call
func (tc *TermChat) printFunctionCall(serverName, toolName string, arguments map[string]interface{}) {
	if tc.verboseMode {
		// Verbose mode: show full function call box
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
	} else {
		// Compact mode: show simple function call
		argStr := ""
		if len(arguments) > 0 {
			for key, value := range arguments {
				if argStr != "" {
					argStr += ", "
				}
				argStr += fmt.Sprintf("%s: %v", key, value)
			}
			if len(argStr) > 40 {
				argStr = argStr[:37] + "..."
			}
			argStr = fmt.Sprintf("(%s)", argStr)
		}
		fmt.Printf("\n\033[1;35m[CALL]\033[0m %s.%s%s", serverName, toolName, argStr)
	}
}

// generateCompactResult creates a compact single-line summary of function results
func (tc *TermChat) generateCompactResult(serverName, toolName string, result string) string {
	isError := strings.Contains(result, "Error calling") || strings.Contains(result, "Tool error")
	isSuccess := strings.Contains(result, "executed successfully")
	
	if isError {
		return fmt.Sprintf("[ERROR] %s.%s -> %s", serverName, toolName, tc.extractErrorMessage(result))
	}
	
	if isSuccess {
		return fmt.Sprintf("[SUCCESS] %s.%s -> %s", serverName, toolName, tc.extractSuccessMessage(result))
	}
	
	return fmt.Sprintf("[RESULT] %s.%s -> %s", serverName, toolName, tc.extractMainContent(result))
}

// extractErrorMessage extracts the core error message
func (tc *TermChat) extractErrorMessage(result string) string {
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Error:") || strings.Contains(line, "error:") {
			return tc.truncateText(line, 60)
		}
	}
	return tc.truncateText(result, 60)
}

// extractSuccessMessage extracts the key success information
func (tc *TermChat) extractSuccessMessage(result string) string {
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.Contains(line, "executed successfully") {
			return tc.truncateText(line, 60)
		}
	}
	return "completed"
}

// extractMainContent extracts the main content from any result
func (tc *TermChat) extractMainContent(result string) string {
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			return tc.truncateText(line, 60)
		}
	}
	return "no output"
}

// truncateText truncates text to maxLength with ellipsis
func (tc *TermChat) truncateText(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}
	return text[:maxLength-3] + "..."
}

// printFunctionResult prints a nicely formatted function result
func (tc *TermChat) printFunctionResult(result string) {
	// Parse the result to see if it's an error or success
	isError := strings.Contains(result, "Error calling") || strings.Contains(result, "Tool error")
	isSuccess := strings.Contains(result, "executed successfully")
	
	if tc.verboseMode {
		// Verbose mode: show full result
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
	} else {
		// Compact mode: show compact summary inline
		if isError {
			fmt.Printf("\033[1;31m[ERROR]\033[0m %s\n", tc.extractErrorMessage(result))
		} else if isSuccess {
			fmt.Printf("\033[1;32m[SUCCESS]\033[0m %s\n", tc.extractSuccessMessage(result))
		} else {
			fmt.Printf("\033[1;34m[RESULT]\033[0m %s\n", tc.extractMainContent(result))
		}
	}
}

// stripXMLFunctionCalls removes XML function call tags from content for display
func (tc *TermChat) stripXMLFunctionCalls(content string) string {
	// Remove function_calls blocks completely (including all nested content)
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
	
	// Remove any remaining invoke blocks that might be outside function_calls
	for strings.Contains(content, "<invoke") {
		start := strings.Index(content, "<invoke")
		end := strings.Index(content, "</invoke>")
		if start != -1 && end != -1 && end > start {
			content = content[:start] + content[end+len("</invoke>"):]
		} else {
			break
		}
	}
	
	// Remove any remaining parameter tags
	for strings.Contains(content, "<parameter") {
		start := strings.Index(content, "<parameter")
		end := strings.Index(content, "</parameter>")
		if start != -1 && end != -1 && end > start {
			content = content[:start] + content[end+len("</parameter>"):]
		} else {
			break
		}
	}
	
	return content
}

// requestFunctionConfirmation asks the user to confirm a function call
func (tc *TermChat) requestFunctionConfirmation(functionName, arguments string) bool {
	fmt.Printf("\n\033[1;33m⚠️  Function Call Confirmation\033[0m\n")
	fmt.Printf("Function: \033[1;36m%s\033[0m\n", functionName)
	if arguments != "" && arguments != "{}" {
		fmt.Printf("Arguments: \033[90m%s\033[0m\n", arguments)
	}
	fmt.Printf("\nApproval Mode: \033[1;90m%s\033[0m\n", tc.approvalMode.GetModeIndicatorNoEmoji())
	fmt.Printf("\nOptions:\n")
	fmt.Printf("  \033[1;32m[y]\033[0m Proceed once\n")
	fmt.Printf("  \033[1;31m[n]\033[0m Cancel\n")
	if tc.approvalMode != AUTO_EDIT {
		fmt.Printf("  \033[1;34m[a]\033[0m Always proceed (upgrade to %s mode)\n", AUTO_EDIT.String())
	}
	fmt.Printf("  \033[1;35m[Y]\033[0m YOLO mode (auto-approve everything)\n")
	fmt.Printf("\nYour choice [y/n/a/Y]: ")

	var response string
	fmt.Scanln(&response)
	
	response = strings.TrimSpace(response)
	switch response {
	case "y", "Y", "yes", "":
		if response == "Y" {
			// Uppercase Y means YOLO mode
			tc.approvalMode = YOLO
			fmt.Printf("\033[90mUpgraded to %s mode\033[0m\n", YOLO.GetModeIndicatorNoEmoji())
		}
		return true
	case "n", "no":
		return false
	case "a", "always":
		if tc.approvalMode != AUTO_EDIT {
			tc.approvalMode = AUTO_EDIT
			fmt.Printf("\033[90mUpgraded to %s mode\033[0m\n", AUTO_EDIT.GetModeIndicatorNoEmoji())
		}
		return true
	case "yolo", "YOLO":
		tc.approvalMode = YOLO
		fmt.Printf("\033[90mUpgraded to %s mode\033[0m\n", YOLO.GetModeIndicatorNoEmoji())
		return true
	default:
		return false
	}
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
	
	// Note: Approval checking is now handled in the turn-based conversation flow
	
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
	
	// In compact mode, show minimal function call info
	if !tc.verboseMode {
		fmt.Printf("\n\033[1;35m[CALL]\033[0m %s.%s", serverName, toolName)
		if len(arguments) > 0 {
			fmt.Printf(" (with args)")
		}
		fmt.Printf(" -> ")
	} else {
		// Verbose mode: show full function call
		tc.printFunctionCall(serverName, toolName, arguments)
	}
	
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
	
	// If we have function results, continue the conversation automatically
	if len(functionResults) > 0 {
		tc.continueConversationWithFunctionResults(functionCalls, functionResults, aiMsgIndex)
	} else {
		// Even without function calls, check if AI should continue
		tc.checkAndContinueIfNeeded(aiMsgIndex)
	}
}

// checkAndContinueIfNeeded checks if the AI should continue working automatically
func (tc *TermChat) checkAndContinueIfNeeded(aiMsgIndex int) {
	content := strings.ToLower(tc.chatHistory[aiMsgIndex].Content)
	
	// Patterns that indicate the AI should continue working
	continuationIndicators := []string{
		"next, i will",
		"now i'll",
		"next step is to",
		"i'll now",
		"let me continue",
		"continuing with",
		"next, let me",
		"now let me",
		"i should also",
		"additionally, i will",
	}
	
	// Patterns that indicate the task is incomplete
	incompletePatterns := []string{
		"creating",
		"setting up",
		"configuring",
		"installing",
		"deploying",
		"in progress",
		"starting",
		"initializing",
	}
	
	shouldContinue := false
	
	// Check for explicit continuation indicators
	for _, indicator := range continuationIndicators {
		if strings.Contains(content, indicator) {
			shouldContinue = true
			break
		}
	}
	
	// Check for incomplete task patterns
	if !shouldContinue {
		for _, pattern := range incompletePatterns {
			if strings.Contains(content, pattern) {
				shouldContinue = true
				break
			}
		}
	}
	
	// If the response seems to end abruptly without clear completion
	if !shouldContinue {
		// Look for responses that don't end with clear completion markers
		completionMarkers := []string{
			"complete", "finished", "done", "ready", "successfully created",
			"all set", "configured", "deployed successfully",
		}
		
		hasCompletionMarker := false
		for _, marker := range completionMarkers {
			if strings.Contains(content, marker) {
				hasCompletionMarker = true
				break
			}
		}
		
		// If no completion marker and response is longer than basic acknowledgment
		if !hasCompletionMarker && len(content) > 50 {
			shouldContinue = true
		}
	}
	
	if shouldContinue {
		// Check turn limit to prevent infinite loops
		if tc.currentTurns >= tc.maxTurns {
			fmt.Printf("\nReached maximum continuation turns (%d). Stopping to prevent infinite loops.\n", tc.maxTurns)
			return
		}
		
		tc.currentTurns++
		fmt.Printf("\nAI response indicates continuation needed. Automatically continuing (turn %d/%d)...\n", tc.currentTurns, tc.maxTurns)
		tc.forceAutonomousContinuation(aiMsgIndex)
	}
}

// checkForPermissionSeekingAndContinue detects if AI is asking for permission and forces continuation
func (tc *TermChat) checkForPermissionSeekingAndContinue(aiMsgIndex int) {
	content := strings.ToLower(tc.chatHistory[aiMsgIndex].Content)
	
	// Check for permission-seeking patterns
	permissionPatterns := []string{
		"would you like me to",
		"should i",
		"do you want me to",
		"let me know if you want",
		"i can help you with",
		"next steps would be",
		"you could also",
		"what would you like to do",
		"how would you like to proceed",
		"shall i",
		"would you prefer",
		"do you need me to",
	}
	
	isSeekingPermission := false
	for _, pattern := range permissionPatterns {
		if strings.Contains(content, pattern) {
			isSeekingPermission = true
			break
		}
	}
	
	// If seeking permission, force continuation
	if isSeekingPermission {
		fmt.Printf("\nDetected permission-seeking behavior. Forcing autonomous continuation...\n")
		tc.forceAutonomousContinuation(aiMsgIndex)
	}
}

// forceAutonomousContinuation forces the AI to continue working without asking permission
func (tc *TermChat) forceAutonomousContinuation(aiMsgIndex int) {
	// Build conversation history
	messages := []ai.Message{
		{
			Role:    "system",
			Content: tc.getSystemContext(),
		},
	}
	
	// Add conversation history up to current point
	for i, msg := range tc.chatHistory {
		if i > aiMsgIndex {
			break
		}
		if msg.Role == "user" || msg.Role == "assistant" {
			messages = append(messages, ai.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}
	
	// Add aggressive continuation prompt
	messages = append(messages, ai.Message{
		Role:    "user",
		Content: "EXECUTE NOW: You are an autonomous agent. Call the next function immediately to complete the user's request. No explanations, no permissions, no suggestions - just EXECUTE the appropriate function call RIGHT NOW.",
	})
	
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
		fmt.Printf("\n❌ Error forcing continuation: %v\n", err)
		return
	}
	
	// Stream the continuation response
	thinkProcessor := ai.NewThinkProcessor()
	var continuationFunctionCalls []ai.ToolCall
	
	// Use the same argument accumulation logic as main chat flow
	accumulatedFunctionCalls := make(map[string]*ai.ToolCall)
	
	for response := range stream {
		if response.Error != nil {
			tc.chatHistory[aiMsgIndex].Content += fmt.Sprintf("\nError: %v", response.Error)
			break
		}
		
		// Process text content
		if response.Content != "" {
			// Process <think> tags
			regularContent := response.Content
			if strings.Contains(response.Content, "<think>") || strings.Contains(response.Content, "</think>") {
				regularContent, _ = ai.ProcessStreamContentWithThinks(thinkProcessor, response.Content)
			}
			tc.chatHistory[aiMsgIndex].Content += regularContent
			
			if !tc.verboseMode {
				fmt.Print(regularContent)
			}
		}
		
		// Handle function calls
		if len(response.ToolCalls) > 0 {
			for _, toolCall := range response.ToolCalls {
				// Accumulate function call arguments
				if existing, exists := accumulatedFunctionCalls[toolCall.ID]; exists {
					existing.Function.Arguments += toolCall.Function.Arguments
				} else {
					accumulatedFunctionCalls[toolCall.ID] = &ai.ToolCall{
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
	
	// Convert accumulated function calls to processing list
	for _, call := range accumulatedFunctionCalls {
		if call.Function.Name != "" {
			continuationFunctionCalls = append(continuationFunctionCalls, *call)
		}
	}
	
	// Execute accumulated function calls
	for _, toolCall := range continuationFunctionCalls {
		if toolCall.Type == "function" {
			tc.executeNativeToolCall(toolCall, aiMsgIndex)
		}
	}
}

// continueConversationWithFunctionResults continues the AI conversation with function results
func (tc *TermChat) continueConversationWithFunctionResults(functionCalls []ai.ToolCall, functionResults []ai.Message, aiMsgIndex int) {
	// Check turn limit to prevent infinite loops
	if tc.currentTurns >= tc.maxTurns {
		fmt.Printf("\nReached maximum continuation turns (%d). Stopping to prevent infinite loops.\n", tc.maxTurns)
		return
	}
	
	tc.currentTurns++
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
	
	// Add natural continuation prompt - autonomous behavior is default
	continuationPrompt := "Continue our conversation. Based on the function results above, what's the next step to complete the user's request? Execute any necessary function calls to keep progressing toward the goal."
	
	messages = append(messages, ai.Message{
		Role:    "user",
		Content: continuationPrompt,
	})
	
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
	var continuationFunctionCalls []ai.ToolCall
	
	// Use the same argument accumulation logic as main chat flow
	accumulatedFunctionCalls := make(map[string]*ai.ToolCall)
	
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

		// Accumulate tool calls across streaming chunks (same as main flow)
		if len(response.ToolCalls) > 0 {
			for _, toolCall := range response.ToolCalls {
				if toolCall.Type == "function" {
					
					// Use function name as key to ensure consistent accumulation
					callId := toolCall.Function.Name
					if callId == "" {
						// For empty function names, use the first non-empty function name we've seen
						for _, existing := range accumulatedFunctionCalls {
							if existing.Function.Name != "" {
								callId = existing.Function.Name
								break
							}
						}
						if callId == "" {
							callId = "unknown"
						}
					}
					
					if existing, exists := accumulatedFunctionCalls[callId]; exists {
						// Accumulate arguments for existing function call
						existing.Function.Arguments += toolCall.Function.Arguments
						// Update function name if it was empty before
						if existing.Function.Name == "" && toolCall.Function.Name != "" {
							existing.Function.Name = toolCall.Function.Name
						}
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
	}
	
	// Convert accumulated function calls to processing list
	for _, call := range accumulatedFunctionCalls {
		if call.Function.Name != "" {
			continuationFunctionCalls = append(continuationFunctionCalls, *call)
		}
	}
	
	// Execute accumulated function calls
	for _, toolCall := range continuationFunctionCalls {
		if toolCall.Type == "function" {
			tc.executeNativeToolCall(toolCall, aiMsgIndex)
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