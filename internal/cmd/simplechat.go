package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/phildougherty/m8e/internal/ai"
)

// SimpleChat represents the simplest possible chat interface
type SimpleChat struct {
	aiManager       *ai.Manager
	ctx             context.Context
	cancel          context.CancelFunc
	currentProvider string
	currentModel    string
}

// NewSimpleChat creates a new simple chat instance
func NewSimpleChat() *SimpleChat {
	ctx, cancel := context.WithCancel(context.Background())
	return &SimpleChat{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Run starts the simple chat interface
func (sc *SimpleChat) Run() error {
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

	sc.aiManager = ai.NewManager(aiConfig)

	// Get current provider info
	if provider, err := sc.aiManager.GetCurrentProvider(); err == nil {
		sc.currentProvider = provider.Name()
		sc.currentModel = provider.DefaultModel()
	}

	// Welcome
	fmt.Println("=== Matey AI Chat ===")
	fmt.Printf("Provider: %s | Model: %s\n", sc.currentProvider, sc.currentModel)
	fmt.Println("Type '/help' for commands, '/exit' to quit, or just chat!")
	fmt.Println()

	// Simple chat loop
	scanner := bufio.NewScanner(os.Stdin)
	
	for {
		fmt.Print("> ")
		
		if !scanner.Scan() {
			break
		}
		
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		
		// Handle commands
		if strings.HasPrefix(input, "/") {
			if sc.handleCommand(input) {
				break // Exit
			}
			continue
		}

		// Chat with AI
		sc.chatWithAI(input)
	}
	
	return nil
}

// handleCommand processes slash commands
func (sc *SimpleChat) handleCommand(command string) bool {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}

	switch parts[0] {
	case "/help":
		fmt.Println("Commands:")
		fmt.Println("  /help      - Show this help")
		fmt.Println("  /provider  - Switch AI provider (ollama, openai, claude, openrouter)")
		fmt.Println("  /model     - Switch AI model")
		fmt.Println("  /status    - Show system status")
		fmt.Println("  /exit      - Exit chat")
		fmt.Println()
		fmt.Println("Just type naturally to chat with AI about MCP workflows!")
	case "/provider":
		if len(parts) > 1 {
			sc.switchProvider(parts[1])
		} else {
			fmt.Println("Usage: /provider <name>")
			fmt.Println("Available: ollama, openai, claude, openrouter")
		}
	case "/model":
		if len(parts) > 1 {
			sc.switchModel(parts[1])
		} else {
			fmt.Println("Usage: /model <name>")
		}
	case "/status":
		sc.showStatus()
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

// switchProvider changes the AI provider
func (sc *SimpleChat) switchProvider(name string) {
	if err := sc.aiManager.SwitchProvider(name); err != nil {
		fmt.Printf("Failed to switch provider: %s\n", err)
		return
	}
	sc.currentProvider = name
	if provider, err := sc.aiManager.GetCurrentProvider(); err == nil {
		sc.currentModel = provider.DefaultModel()
	}
	fmt.Printf("Switched to provider: %s\n", name)
}

// switchModel changes the AI model
func (sc *SimpleChat) switchModel(name string) {
	if err := sc.aiManager.SwitchModel(name); err != nil {
		fmt.Printf("Failed to switch model: %s\n", err)
		return
	}
	sc.currentModel = name
	fmt.Printf("Switched to model: %s\n", name)
}

// showStatus displays system status
func (sc *SimpleChat) showStatus() {
	fmt.Printf("Provider: %s\n", sc.currentProvider)
	fmt.Printf("Model: %s\n", sc.currentModel)
	fmt.Println()
	
	providerStatus := sc.aiManager.GetProviderStatus()
	fmt.Println("Provider Status:")
	for name, status := range providerStatus {
		indicator := "✗"
		if status.Available {
			indicator = "✓"
		}
		fmt.Printf("  %s %s\n", indicator, name)
		if status.Error != "" {
			fmt.Printf("    Error: %s\n", status.Error)
		}
	}
}

// chatWithAI sends a message to AI and streams the response
func (sc *SimpleChat) chatWithAI(message string) {
	// Build messages with system context
	messages := []ai.Message{
		{
			Role:    "system",
			Content: sc.getSystemContext(),
		},
		{
			Role:    "user",
			Content: message,
		},
	}

	// Start streaming
	stream, err := sc.aiManager.StreamChatWithFallback(sc.ctx, messages, ai.StreamOptions{
		Model: sc.currentModel,
	})
	if err != nil {
		fmt.Printf("Error: %v\n\n", err)
		return
	}

	fmt.Printf("\nAI (%s):\n", time.Now().Format("15:04:05"))

	// Stream response
	thinkProcessor := ai.NewThinkProcessor()
	
	for response := range stream {
		if response.Error != nil {
			fmt.Printf("Error: %v\n", response.Error)
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

			// Print content
			if regularContent != "" {
				fmt.Print(regularContent)
			}
		}
	}
	
	fmt.Println("\n") // Add newlines after response
}

// getSystemContext returns the system context
func (sc *SimpleChat) getSystemContext() string {
	return `You are Matey, an AI assistant for Kubernetes-native MCP (Model Context Protocol) server orchestration.

Matey is a tool for defining and running multi-server MCP applications using Kubernetes resources.

Available MCP Servers (USE THESE - NO CUSTOM CODE):
- dexcom: Diabetes monitoring data
- filesystem: File operations on /home/phil, /tmp  
- github: GitHub API integration
- memory: Persistent knowledge graph with PostgreSQL
- playwright: Web automation, scraping, screenshots
- searxng: Search engine integration
- task-scheduler: Workflow automation with K8s events
- timezone: Time zone utilities

CRITICAL RULES:
- NEVER write custom code or applications
- ONLY use existing MCP servers listed above
- Build solutions by combining existing servers
- Focus on matey.yaml configs and CRD manifests
- Put ALL code/configs in markdown code blocks

Be concise and direct in responses.`
}

// NewSimpleChatCommand creates the simple chat command
func NewSimpleChatCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "simplechat",
		Short: "Launch simple SSH-friendly chat",
		Long:  "Launch a basic chat interface that works over SSH without fancy terminal features",
		RunE: func(cmd *cobra.Command, args []string) error {
			chat := NewSimpleChat()
			defer chat.cancel()
			return chat.Run()
		},
	}
}