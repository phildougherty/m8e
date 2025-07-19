package chat

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/joho/godotenv"
	"golang.org/x/term"

	"github.com/phildougherty/m8e/internal/ai"
	"github.com/phildougherty/m8e/internal/mcp"
)

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
			fmt.Printf("Connected to MCP proxy at: %s\n", endpoint)
			return endpoint
		}
		if resp != nil {
			resp.Body.Close()
		}
		// Debug: show the error
		if err != nil {
			fmt.Printf("  Error: %v\n", err)
		} else if resp != nil {
			fmt.Printf("  HTTP Status: %d\n", resp.StatusCode)
		}
	}
	
	// If no endpoint works, return the first one and let the user know
	fallback := endpoints[0]
	fmt.Printf("Warning: No MCP proxy detected, using fallback: %s\n", fallback)
	fmt.Printf("   To deploy MCP proxy to cluster, run: matey proxy\n")
	return fallback
}

// NewTermChat creates a new terminal chat instance
func NewTermChat() *TermChat {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Load environment variables FIRST, before creating MCP client
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Debug: Failed to load .env file: %v\n", err)
		// Try to load from current directory explicitly
		if err2 := godotenv.Load(".env"); err2 != nil {
			fmt.Printf("Debug: Failed to load .env from current dir: %v\n", err2)
		}
	} else {
		fmt.Printf("Debug: Successfully loaded .env file\n")
	}
	
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
	// (Now that .env is loaded, MCP_API_KEY will be available)
	proxyURL := detectClusterMCPProxy()
	mcpClient := mcp.NewMCPClient(proxyURL)
	fmt.Printf("Initialized MCP client with proxy URL: %s\n", proxyURL)
	
	// Debug: Check if MCP API key is loaded
	if os.Getenv("MCP_API_KEY") != "" {
		fmt.Printf("Debug: MCP API key loaded successfully\n")
	} else {
		fmt.Printf("Debug: WARNING - MCP API key not found\n")
	}
	
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
	// Check if we have OpenRouter API key (env already loaded in NewTermChat)
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		fmt.Println("Warning: OpenRouter API key not found - responses may not work")
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

	// Use enhanced UI with Bubbletea
	if tc.shouldUseEnhancedUI() {
		return tc.runEnhancedUI()
	} else {
		// Fallback to simple terminal output
		tc.runSimple()
		return nil
	}
}

// shouldUseEnhancedUI determines if we should use the enhanced Bubbletea UI
func (tc *TermChat) shouldUseEnhancedUI() bool {
	// Check if we're in a terminal that supports the enhanced UI
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}
	
	// Check terminal capabilities
	termType := os.Getenv("TERM")
	if termType == "" || strings.Contains(termType, "dumb") {
		return false
	}
	
	// Default to enhanced UI
	return true
}

// runEnhancedUI starts the enhanced Bubbletea-based UI
func (tc *TermChat) runEnhancedUI() error {
	ui := NewChatUI(tc)
	return ui.Run()
}

// addMessage adds a message to the chat history
func (tc *TermChat) addMessage(role, content string) {
	tc.chatHistory = append(tc.chatHistory, TermChatMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
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
	
	// Use simple input mode for command testing
	tc.runSimpleInput()
}

// runSimpleInput handles simple line-based input
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

// getWelcomeMessage returns an enhanced welcome message
func (tc *TermChat) getWelcomeMessage() string {
	return fmt.Sprintf(`Welcome to **Matey AI Chat** - your expert assistant for Kubernetes-native MCP server orchestration!

🚀 **I specialize in:**
• **Infrastructure Automation** - Deploy, scale, and manage cloud-native services
• **MCP Protocol** - Orchestrate MCP servers with full protocol support
• **Kubernetes Integration** - Native CRDs, controllers, and service discovery
• **AI-Powered Operations** - Intelligent automation with multiple AI providers

📋 **Quick Commands:**
• /auto - Enable auto-edit mode (smart automation)
• /yolo - Enable maximum autonomy mode
• /status - Check system status and running services
• /help - Show all available commands

💡 **Try asking me:**
• "Deploy a new microservice to production"
• "Set up monitoring for my cluster"
• "Create a backup workflow"
• "Show me the health of all services"

**Current Mode:** %s | **Provider:** %s | **Model:** %s

Ready to orchestrate your infrastructure! What would you like to build today?`, 
		tc.approvalMode.GetModeIndicatorNoEmoji(), tc.currentProvider, tc.currentModel)
}