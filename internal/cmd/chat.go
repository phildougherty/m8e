package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/phildougherty/m8e/internal/ai"
)

// TerminalChat represents a simple terminal-based chat interface
type TerminalChat struct {
	aiManager       *ai.Manager
	ctx             context.Context
	cancel          context.CancelFunc
	currentProvider string
	currentModel    string
	termWidth       int
	termHeight      int
}

// NewTerminalChat creates a new terminal chat instance
func NewTerminalChat() *TerminalChat {
	ctx, cancel := context.WithCancel(context.Background())
	return &TerminalChat{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Run starts the terminal chat interface
func (tc *TerminalChat) Run() error {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		// Silently ignore missing .env file
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

	// Get terminal size
	tc.updateTerminalSize()

	// Show welcome message
	tc.printWelcome()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start input loop
	scanner := bufio.NewScanner(os.Stdin)

	for {
		select {
		case <-sigChan:
			fmt.Println("\nGoodbye!")
			return nil
		default:
			// Show simple prompt
			fmt.Printf("❯ ")

			// Read input
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					fmt.Printf("Error reading input: %v\n", err)
				}
				return nil
			}

			input := strings.TrimSpace(scanner.Text())
			if input == "" {
				continue
			}

			// Handle commands
			if strings.HasPrefix(input, "/") {
				if tc.handleCommand(input) {
					return nil // Exit command
				}
				continue
			}

			// Send to AI
			tc.chatWithAI(input)
		}
	}
}

// updateTerminalSize gets the current terminal dimensions
func (tc *TerminalChat) updateTerminalSize() {
	// Try to get terminal size using stty
	if cmd := exec.Command("stty", "size"); cmd != nil {
		if output, err := cmd.Output(); err == nil {
			parts := strings.Fields(string(output))
			if len(parts) == 2 {
				if height, err := strconv.Atoi(parts[0]); err == nil {
					tc.termHeight = height
				}
				if width, err := strconv.Atoi(parts[1]); err == nil {
					tc.termWidth = width
				}
			}
		}
	}
	
	// Fallback defaults
	if tc.termWidth == 0 {
		tc.termWidth = 80
	}
	if tc.termHeight == 0 {
		tc.termHeight = 24
	}
}

// printWelcome shows the welcome message
func (tc *TerminalChat) printWelcome() {
	fmt.Println("\033[1;36m╭─────────────────────────────────────────────────────────╮\033[0m")
	fmt.Println("\033[1;36m│\033[0m \033[1;37mMatey AI Chat\033[0m - Kubernetes-native MCP orchestration \033[1;36m│\033[0m")
	fmt.Println("\033[1;36m╰─────────────────────────────────────────────────────────╯\033[0m")
	fmt.Println()
	fmt.Println("\033[37mI help you build MCP workflows using natural language.\033[0m")
	fmt.Println()
	fmt.Println("\033[32m• Describe what you want and I'll configure it using existing MCP servers\033[0m")
	fmt.Println("\033[32m• Generate matey.yaml configs and Kubernetes manifests\033[0m") 
	fmt.Println("\033[32m• Set up workflows, toolboxes, and automations\033[0m")
	fmt.Println()
	fmt.Println("\033[90mType '/help' for commands or just describe what you want to build!\033[0m")
	fmt.Println()
}

// printStatusBar shows the bottom status bar like Claude Code
func (tc *TerminalChat) printStatusBar() {
	statusItems := []string{
		fmt.Sprintf("\033[36m%s\033[0m", tc.currentProvider),
		fmt.Sprintf("\033[37m%s\033[0m", tc.currentModel),
		fmt.Sprintf("\033[90m%s\033[0m", time.Now().Format("15:04")),
	}
	
	statusText := strings.Join(statusItems, " \033[90m•\033[0m ")
	padding := tc.termWidth - len(tc.stripAnsi(statusText)) - 2
	if padding < 0 {
		padding = 0
	}
	
	fmt.Printf("\033[90m%s\033[0m %s\n", strings.Repeat("─", padding), statusText)
}

// printPromptWithStatus shows the prompt with status information
func (tc *TerminalChat) printPromptWithStatus() {
	// Move cursor up to overwrite status bar
	fmt.Print("\033[A\033[2K")
	
	// Print new status bar
	tc.printStatusBar()
	
	// Print prompt
	fmt.Print("❯ ")
}

// stripAnsi removes ANSI escape codes for length calculation
func (tc *TerminalChat) stripAnsi(text string) string {
	// Simple regex would be better, but avoiding dependencies
	result := text
	for {
		start := strings.Index(result, "\033[")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "m")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+end+1:]
	}
	return result
}

// handleCommand processes slash commands
func (tc *TerminalChat) handleCommand(command string) bool {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}

	switch parts[0] {
	case "/help":
		tc.showHelp()
	case "/provider":
		if len(parts) > 1 {
			tc.switchProvider(parts[1])
		} else {
			fmt.Println("Usage: /provider <name>")
			fmt.Println("Available: ollama, openai, claude, openrouter")
		}
	case "/model":
		if len(parts) > 1 {
			tc.switchModel(parts[1])
		} else {
			fmt.Println("Usage: /model <name>")
		}
	case "/status":
		tc.showStatus()
	case "/exit", "/quit":
		fmt.Println("Goodbye!")
		return true
	default:
		fmt.Printf("Unknown command: %s\n", parts[0])
		fmt.Println("Type '/help' for available commands")
	}
	fmt.Println()
	return false
}

// showHelp displays help information
func (tc *TerminalChat) showHelp() {
	fmt.Println("\033[1;33mMatey AI Commands\033[0m")
	fmt.Println()
	fmt.Println("\033[1;32mChat:\033[0m")
	fmt.Println("  Just type naturally to chat with AI")
	fmt.Println("  AI can help with MCP configuration, Kubernetes manifests, troubleshooting")
	fmt.Println()
	fmt.Println("\033[1;32mCommands:\033[0m")
	fmt.Println("  \033[36m/help\033[0m      - Show this help")
	fmt.Println("  \033[36m/provider\033[0m  - Switch AI provider (ollama, openai, claude, openrouter)")
	fmt.Println("  \033[36m/model\033[0m     - Switch AI model")
	fmt.Println("  \033[36m/status\033[0m    - Show system status")
	fmt.Println("  \033[36m/exit\033[0m      - Exit the chat")
	fmt.Println()
	fmt.Println("\033[1;32mExamples:\033[0m")
	fmt.Println("  \"Create an MCP server configuration for a web scraper\"")
	fmt.Println("  \"How do I scale my workflow to 3 replicas?\"")
	fmt.Println("  \"Show me a health monitoring workflow\"")
	fmt.Println("  \"/provider ollama\"")
	fmt.Println("  \"/model llama3:70b\"")
	fmt.Println()
}

// switchProvider changes the AI provider
func (tc *TerminalChat) switchProvider(name string) {
	if err := tc.aiManager.SwitchProvider(name); err != nil {
		fmt.Printf("\033[31m✗\033[0m Failed to switch provider: %s\n", err)
		return
	}
	tc.currentProvider = name
	if provider, err := tc.aiManager.GetCurrentProvider(); err == nil {
		tc.currentModel = provider.DefaultModel()
	}
	fmt.Printf("\033[32m✓\033[0m Switched to provider: %s\n", name)
}

// switchModel changes the AI model
func (tc *TerminalChat) switchModel(name string) {
	if err := tc.aiManager.SwitchModel(name); err != nil {
		fmt.Printf("\033[31m✗\033[0m Failed to switch model: %s\n", err)
		return
	}
	tc.currentModel = name
	fmt.Printf("\033[32m✓\033[0m Switched to model: %s\n", name)
}

// showStatus displays system status
func (tc *TerminalChat) showStatus() {
	fmt.Println("\033[1;33mSystem Status\033[0m")
	fmt.Println()

	// Current provider
	if provider, err := tc.aiManager.GetCurrentProvider(); err == nil {
		fmt.Printf("Current Provider: \033[36m%s\033[0m\n", provider.Name())
		fmt.Printf("Current Model: \033[36m%s\033[0m\n", provider.DefaultModel())
		fmt.Println()
	}

	// Provider status
	fmt.Println("\033[1;32mProviders:\033[0m")
	providerStatus := tc.aiManager.GetProviderStatus()
	for name, status := range providerStatus {
		indicator := "\033[31m●\033[0m"
		if status.Available {
			indicator = "\033[32m●\033[0m"
		}
		fmt.Printf("  %s %s\n", indicator, name)
		if status.Error != "" {
			fmt.Printf("    \033[90mError: %s\033[0m\n", status.Error)
		}
	}
	fmt.Println()
}

// chatWithAI sends a message to AI and streams the response  
func (tc *TerminalChat) chatWithAI(message string) {
	// Build messages with system context
	messages := []ai.Message{
		{
			Role:    "system", 
			Content: tc.getSystemContext(),
		},
		{
			Role:    "user",
			Content: message,
		},
	}

	// Get current model
	var currentModel string
	if provider, err := tc.aiManager.GetCurrentProvider(); err == nil {
		currentModel = provider.DefaultModel()
	}

	// Start streaming
	stream, err := tc.aiManager.StreamChatWithFallback(tc.ctx, messages, ai.StreamOptions{
		Model: currentModel,
	})
	if err != nil {
		fmt.Printf("\033[31m✗ Error: %v\033[0m\n\n", err)
		return
	}

	// Print AI label
	fmt.Printf("\n\033[32mAI\033[0m \033[90m%s\033[0m\n", time.Now().Format("15:04:05"))

	// Collect response for potential paging
	var fullResponse strings.Builder
	thinkProcessor := ai.NewThinkProcessor()
	var hasThinking bool

	for response := range stream {
		if response.Error != nil {
			fmt.Printf("\033[31m✗ Error: %v\033[0m\n", response.Error)
			return
		}

		if response.Finished {
			break
		}

		if response.Content != "" || response.ThinkContent != "" {
			// Process any think tags
			regularContent := response.Content
			thinkContent := response.ThinkContent

			if response.Content != "" && (strings.Contains(response.Content, "<think>") || strings.Contains(response.Content, "</think>")) {
				regularContent, thinkContent = ai.ProcessStreamContentWithThinks(thinkProcessor, response.Content)
			}

			// Handle thinking content
			if thinkContent != "" && !hasThinking {
				fmt.Print("\033[90m▼ Thinking...\033[0m\n")
				hasThinking = true
			}

			// Print and collect regular content
			if regularContent != "" {
				if hasThinking {
					fmt.Print("\n")
					hasThinking = false
				}
				formatted := tc.formatCodeBlocks(regularContent)
				fmt.Print(formatted)
				fullResponse.WriteString(formatted)
			}
		}
	}

	fmt.Println()
	
	// Check if response is very long and offer pager
	responseText := fullResponse.String()
	if strings.Count(responseText, "\n") > tc.termHeight-5 {
		fmt.Print("\033[90m📄 Response is long. View in pager? [y/N]: \033[0m")
		
		// Read single character without Enter
		if response := tc.readSingleChar(); response == 'y' || response == 'Y' {
			tc.showInPager(responseText)
		}
	}
	
	fmt.Println()
}

// formatCodeBlocks adds visual formatting to code blocks
func (tc *TerminalChat) formatCodeBlocks(content string) string {
	if !strings.Contains(content, "```") {
		return content
	}

	result := content
	
	// Add headers for different code block types
	result = strings.ReplaceAll(result, "```yaml", "\n\033[33m── matey.yaml ──\033[0m\n```yaml")
	result = strings.ReplaceAll(result, "```json", "\n\033[33m── config.json ──\033[0m\n```json")
	result = strings.ReplaceAll(result, "```bash", "\n\033[33m── commands ──\033[0m\n```bash")
	result = strings.ReplaceAll(result, "```kubernetes", "\n\033[33m── manifest.yaml ──\033[0m\n```kubernetes")
	result = strings.ReplaceAll(result, "```k8s", "\n\033[33m── manifest.yaml ──\033[0m\n```k8s")

	return result
}

// getSystemContext returns the comprehensive system context
func (tc *TerminalChat) getSystemContext() string {
	return "You are Matey, an AI assistant for Kubernetes-native MCP (Model Context Protocol) server orchestration.\n\n" +
		"## What Matey Is\n" +
		"Matey (m8e) is a Kubernetes-native tool for defining and running multi-server MCP applications. It provides orchestration capabilities for MCP servers using Kubernetes resources and custom resource definitions (CRDs).\n\n" +
		"## Core Commands\n" +
		"- matey up/down - Start/stop all services\n" +
		"- matey install - Install Matey CRDs and controllers\n" +
		"- matey proxy - Start HTTP proxy server\n" +
		"- matey ps - List service status\n" +
		"- matey logs - View service logs\n" +
		"- matey k8s memory/task-scheduler - Manage services directly\n\n" +
		"## Custom Resource Definitions (CRDs)\n" +
		"Matey provides comprehensive Kubernetes CRDs:\n" +
		"- MCPServer: Individual MCP server instances\n" +
		"- MCPTaskScheduler: Task scheduling with event triggers\n" +
		"- MCPMemory: Memory service with PostgreSQL\n" +
		"- MCPProxy: HTTP proxy with OAuth\n" +
		"- Workflow: Advanced workflow automation with cron scheduling, dependencies, retries\n" +
		"- MCPToolbox: Collections of MCP servers working together as cohesive AI workflows\n\n" +
		"## Workflow System (Advanced Automation)\n" +
		"### Workflow CRD Features:\n" +
		"- **Cron Scheduling**: Run workflows on schedules (e.g., '0 */6 * * *' for every 6 hours)\n" +
		"- **Multi-Step Orchestration**: Chain multiple MCP tool calls together\n" +
		"- **Dependencies**: Steps can depend on other steps completing successfully\n" +
		"- **Conditional Execution**: Steps run based on conditions and previous step outputs\n" +
		"- **Retry Policies**: Automatic retries with Linear/Exponential/Fixed backoff\n" +
		"- **Concurrency Control**: Allow/Forbid/Replace concurrent executions\n" +
		"- **Templating Support**: Parameters and conditions support templating\n" +
		"- **Error Handling**: Steps can continue on error or fail the entire workflow\n" +
		"- **Execution History**: Track workflow runs with detailed step results\n" +
		"- **Timezone Support**: Schedule workflows in specific timezones\n\n" +
		"## MCPToolbox System (AI Workflow Collections)\n" +
		"### Toolbox Features:\n" +
		"- **Server Collections**: Group related MCP servers into cohesive workflows\n" +
		"- **Templates**: Pre-built configurations (coding-assistant, rag-stack, research-agent)\n" +
		"- **Dependency Management**: Define startup order and health dependencies\n" +
		"- **Team Collaboration**: Share toolboxes with teams and manage permissions\n" +
		"- **OAuth Integration**: Per-toolbox OAuth clients with custom scopes\n" +
		"- **Resource Management**: Toolbox-level resource limits and scaling\n" +
		"- **Security Policies**: Pod security standards and image signing requirements\n" +
		"- **Monitoring**: Built-in observability and alerting\n" +
		"- **Networking**: Service mesh and load balancing support\n" +
		"- **Auto-scaling**: Dynamic scaling based on demand\n\n" +
		"## Task Scheduler with Event Triggers\n" +
		"### Enhanced Features:\n" +
		"- **Kubernetes Event Triggers**: React to Pod failures, Deployment issues, etc.\n" +
		"- **Conditional Dependencies**: Cross-workflow dependencies with timeout handling\n" +
		"- **Auto-scaling**: Dynamic scaling based on queue depth and resource usage\n" +
		"- **Incident Response**: Automated workflows for Pod failures, rollbacks\n" +
		"- **Custom Metrics**: Scale based on external metrics (queue depth, etc.)\n" +
		"- **Cooldown Periods**: Prevent trigger spam with configurable cooldowns\n\n" +
		"## Available MCP Servers (USE THESE - NO CUSTOM CODE)\n" +
		"### Ready-to-Use MCP Servers:\n" +
		"- dexcom: Diabetes monitoring data (dexcom.com API)\n" +
		"- filesystem: File operations on /home/phil, /tmp (read/write files)\n" +
		"- github: GitHub API integration with token\n" +
		"- hn-radio: Hacker News podcast generation with AI/TTS\n" +
		"- meal-log: Food logging and tracking with AI analysis\n" +
		"- memory: Persistent knowledge graph with PostgreSQL\n" +
		"- openrouter-gateway: AI model routing service\n" +
		"- playwright: Web automation, scraping, screenshots\n" +
		"- postgres-mcp: Direct PostgreSQL database operations\n" +
		"- searxng: Search engine integration via SearXNG\n" +
		"- sequential-thinking: AI reasoning and logic tools\n" +
		"- task-scheduler: Workflow automation with K8s events\n" +
		"- timezone: Time zone utilities and conversions\n" +
		"- postgres-memory: PostgreSQL service for memory graph\n\n" +
		"## CRITICAL RULES\n" +
		"- NEVER write custom code or applications\n" +
		"- ONLY use existing MCP servers listed above\n" +
		"- Build solutions by combining existing servers\n" +
		"- Focus on matey.yaml configs and CRD manifests\n" +
		"- Use real MCP servers with proper capabilities\n" +
		"- Leverage Workflows for automation and scheduling\n" +
		"- Use MCPToolboxes for complex multi-server solutions\n" +
		"- Put ALL code/configs in markdown code blocks\n\n" +
		"## Solution Patterns\n" +
		"1. **Simple Automation**: Use Workflows for scheduled tasks\n" +
		"2. **Complex AI Pipelines**: Use MCPToolboxes with dependencies\n" +
		"3. **Event-Driven**: Use TaskScheduler with Kubernetes event triggers\n" +
		"4. **Research Workflows**: Combine searxng + memory + sequential-thinking\n" +
		"5. **Data Processing**: Combine filesystem + memory + task-scheduler\n" +
		"6. **Incident Response**: Use event triggers for Pod failures and auto-recovery\n\n" +
		"## Response Guidelines\n" +
		"- Be concise and direct in responses\n" +
		"- For configs/YAML, use standard markdown code blocks\n" +
		"- Keep chat responses brief and conversational\n" +
		"- Explain which existing MCP servers you're using\n" +
		"- Suggest appropriate CRDs (Workflow, MCPToolbox, etc.) for the use case"
}

// readSingleChar reads a single character without requiring Enter
func (tc *TerminalChat) readSingleChar() rune {
	// Disable input buffering
	exec.Command("stty", "-F", "/dev/tty", "cbreak", "min", "1").Run()
	exec.Command("stty", "-F", "/dev/tty", "-echo").Run()
	defer func() {
		exec.Command("stty", "-F", "/dev/tty", "echo").Run()
		exec.Command("stty", "-F", "/dev/tty", "cooked").Run()
	}()

	var b []byte = make([]byte, 1)
	os.Stdin.Read(b)
	return rune(b[0])
}

// showInPager displays content in a pager like less
func (tc *TerminalChat) showInPager(content string) {
	// Try to use less first, then more, then fallback to cat
	pagers := []string{"less -R", "more", "cat"}
	
	for _, pager := range pagers {
		parts := strings.Fields(pager)
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		
		// Create a pipe to send content to the pager
		stdin, err := cmd.StdinPipe()
		if err != nil {
			continue
		}
		
		// Start the pager
		if err := cmd.Start(); err != nil {
			stdin.Close()
			continue
		}
		
		// Send content to pager
		go func() {
			defer stdin.Close()
			io.WriteString(stdin, content)
		}()
		
		// Wait for pager to finish
		cmd.Wait()
		return
	}
	
	// Fallback: just print the content
	fmt.Print(content)
}

// NewChatCommand creates the terminal chat command
func NewChatCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Launch terminal-native AI chat (like Claude Code)",
		Long:  "Launch a simple terminal-native chat interface with natural scrolling",
		RunE: func(cmd *cobra.Command, args []string) error {
			chat := NewTerminalChat()
			defer chat.cancel()
			return chat.Run()
		},
	}
}