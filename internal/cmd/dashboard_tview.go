package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/joho/godotenv"
	"github.com/rivo/tview"
	"github.com/spf13/cobra"

	"github.com/phildougherty/m8e/internal/ai"
)

// TViewDashboard represents the main TUI application
type TViewDashboard struct {
	app          *tview.Application
	pages        *tview.Pages
	mainLayout   *tview.Flex
	chatView     *tview.TextView
	inputField   *tview.InputField
	statusView   *tview.TextView
	
	// State
	aiManager    *ai.Manager
	ctx          context.Context
	cancel       context.CancelFunc
	
	// Chat state
	chatHistory  []TViewChatMessage
	streaming    bool
	
	// Streaming optimization
	lastUpdateTime time.Time
	updatePending  bool
	
	// Code block streaming state (for inline formatting)
	inCodeBlock     bool
	codeBlockLang   string
	codeBlockLines  []string
	
	// UI state
	currentProvider string
	currentModel    string
}

// TViewChatMessage represents a chat message for tview
type TViewChatMessage struct {
	Role      string
	Content   string
	Think     string
	Timestamp time.Time
	Streaming bool
}

// NewTViewDashboard creates a new tview-based dashboard
func NewTViewDashboard() *TViewDashboard {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &TViewDashboard{
		app:         tview.NewApplication(),
		ctx:         ctx,
		cancel:      cancel,
		chatHistory: make([]TViewChatMessage, 0),
	}
}

// Run starts the TUI application
func (d *TViewDashboard) Run() error {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "No .env file found: %v\n", err)
	}
	
	// Initialize AI manager with all providers
	aiConfig := ai.Config{
		DefaultProvider: "openrouter",
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
	
	d.aiManager = ai.NewManager(aiConfig)
	
	// Get current provider info
	if provider, err := d.aiManager.GetCurrentProvider(); err == nil {
		d.currentProvider = provider.Name()
		d.currentModel = provider.DefaultModel()
	}
	
	// Setup UI
	d.setupUI()
	
	// Start the application
	return d.app.Run()
}

// setupUI creates the main UI layout
func (d *TViewDashboard) setupUI() {
	// Create main components
	d.createChatView()
	d.createInputField()
	d.createStatusView()
	
	// Create layouts
	d.createMainLayout()
	
	// Setup pages
	d.pages = tview.NewPages()
	d.pages.AddPage("main", d.mainLayout, true, true)
	
	// Set root and focus
	d.app.SetRoot(d.pages, true)
	d.app.SetFocus(d.inputField)
	
	// Enable mouse support globally
	d.app.EnableMouse(true)
	
	// Add global key handling for scrolling that works regardless of focus
	d.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Handle global scrolling keys that should work regardless of focus
		switch event.Key() {
		case tcell.KeyPgUp:
			row, col := d.chatView.GetScrollOffset()
			d.chatView.ScrollTo(row-10, col)
			d.app.Draw()
			return nil
		case tcell.KeyPgDn:
			row, col := d.chatView.GetScrollOffset()
			d.chatView.ScrollTo(row+10, col)
			d.app.Draw()
			return nil
		case tcell.KeyHome:
			if event.Modifiers()&tcell.ModCtrl != 0 {
				d.chatView.ScrollToBeginning()
				d.app.Draw()
				return nil
			}
		case tcell.KeyEnd:
			if event.Modifiers()&tcell.ModCtrl != 0 {
				d.chatView.ScrollToEnd()
				d.app.Draw()
				return nil
			}
		case tcell.KeyUp:
			if event.Modifiers()&tcell.ModCtrl != 0 {
				row, col := d.chatView.GetScrollOffset()
				d.chatView.ScrollTo(row-1, col)
				d.app.Draw()
				return nil
			}
		case tcell.KeyDown:
			if event.Modifiers()&tcell.ModCtrl != 0 {
				row, col := d.chatView.GetScrollOffset()
				d.chatView.ScrollTo(row+1, col)
				d.app.Draw()
				return nil
			}
		}
		return event
	})
	
	// Add initial welcome message
	d.addWelcomeMessage()
}

// createChatView creates the main chat display
func (d *TViewDashboard) createChatView() {
	d.chatView = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetScrollable(true).
		SetWrap(true).
		SetWordWrap(true).
		SetChangedFunc(func() {
			d.app.Draw()
		})
		
	d.chatView.
		SetBorder(false).
		SetBorderPadding(1, 1, 2, 2)
		
	// Add comprehensive scrolling support with keyboard and mouse
	d.chatView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			row, col := d.chatView.GetScrollOffset()
			d.chatView.ScrollTo(row-1, col)
			d.app.Draw()
			return nil
		case tcell.KeyDown:
			row, col := d.chatView.GetScrollOffset()
			d.chatView.ScrollTo(row+1, col)
			d.app.Draw()
			return nil
		case tcell.KeyPgUp:
			row, col := d.chatView.GetScrollOffset()
			d.chatView.ScrollTo(row-10, col)
			d.app.Draw()
			return nil
		case tcell.KeyPgDn:
			row, col := d.chatView.GetScrollOffset()
			d.chatView.ScrollTo(row+10, col)
			d.app.Draw()
			return nil
		case tcell.KeyHome:
			d.chatView.ScrollToBeginning()
			d.app.Draw()
			return nil
		case tcell.KeyEnd:
			d.chatView.ScrollToEnd()
			d.app.Draw()
			return nil
		case tcell.KeyCtrlU: // Scroll up half screen
			row, col := d.chatView.GetScrollOffset()
			d.chatView.ScrollTo(row-5, col)
			d.app.Draw()
			return nil
		case tcell.KeyCtrlD: // Scroll down half screen
			row, col := d.chatView.GetScrollOffset()
			d.chatView.ScrollTo(row+5, col)
			d.app.Draw()
			return nil
		}
		return event
	})
	
	// Enhanced mouse support for scrolling
	d.chatView.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		switch action {
		case tview.MouseScrollUp:
			row, col := d.chatView.GetScrollOffset()
			d.chatView.ScrollTo(row-3, col)
			d.app.Draw()
			return action, nil
		case tview.MouseScrollDown:
			row, col := d.chatView.GetScrollOffset()
			d.chatView.ScrollTo(row+3, col)
			d.app.Draw()
			return action, nil
		case tview.MouseLeftClick:
			// Allow clicking to focus the chat view for scrolling
			d.app.SetFocus(d.chatView)
			return action, event
		}
		return action, event
	})
}

// createInputField creates the input field
func (d *TViewDashboard) createInputField() {
	d.inputField = tview.NewInputField().
		SetLabel("❯ ").
		SetFieldWidth(0).
		SetPlaceholder("Describe your MCP workflow, servers, or tasks...")
		
	d.inputField.
		SetBorder(false).
		SetBorderPadding(0, 0, 1, 1)
		
	// Handle input
	d.inputField.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			text := d.inputField.GetText()
			if strings.TrimSpace(text) != "" {
				d.sendMessage(text)
				d.inputField.SetText("")
			}
		}
	})
}



// createStatusView creates the status display
func (d *TViewDashboard) createStatusView() {
	d.statusView = tview.NewTextView().
		SetDynamicColors(true)
		
	d.statusView.
		SetBorder(true).
		SetTitle(" Status ").
		SetBorderColor(tcell.ColorOrange).
		SetTitleColor(tcell.ColorOrange)
		
	d.updateStatusView()
}

// createMainLayout creates the main layout
func (d *TViewDashboard) createMainLayout() {
	// Simple full-screen layout like Claude Code
	d.mainLayout = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(d.chatView, 0, 1, false).
		AddItem(d.inputField, 3, 0, true).
		AddItem(d.statusView, 3, 0, false)
}

// addWelcomeMessage adds the initial welcome message
func (d *TViewDashboard) addWelcomeMessage() {
	msg := TViewChatMessage{
		Role:      "system",
		Content:   "Welcome to Matey! I help you build MCP workflows using natural language.\n\nDescribe what you want and I'll configure it using your existing MCP servers:\n• Dexcom, GitHub, filesystem, playwright, memory, task-scheduler, etc.\n• Generate matey.yaml configs and Kubernetes manifests\n• Set up scheduled workflows and automations\n\nJust tell me what you want to build!",
		Timestamp: time.Now(),
	}
	
	d.chatHistory = append(d.chatHistory, msg)
	d.updateChatView()
}

// sendMessage sends a message to the AI
func (d *TViewDashboard) sendMessage(text string) {
	// Add user message
	userMsg := TViewChatMessage{
		Role:      "user",
		Content:   text,
		Timestamp: time.Now(),
	}
	d.chatHistory = append(d.chatHistory, userMsg)
	
	// Add AI placeholder
	aiMsg := TViewChatMessage{
		Role:      "assistant",
		Content:   "",
		Think:     "",
		Timestamp: time.Now(),
		Streaming: true,
	}
	d.chatHistory = append(d.chatHistory, aiMsg)
	d.streaming = true
	
	d.updateChatView()
	d.updateStatusView()
	
	// Handle commands
	if strings.HasPrefix(text, "/") {
		d.handleCommand(text)
		return
	}
	
	// Reset code block state for new message
	d.inCodeBlock = false
	d.codeBlockLines = []string{}
	
	// Send to AI
	go d.streamFromAI(text)
}

// streamFromAI handles streaming from AI
func (d *TViewDashboard) streamFromAI(message string) {
	defer func() {
		d.streaming = false
		if len(d.chatHistory) > 0 {
			d.chatHistory[len(d.chatHistory)-1].Streaming = false
		}
		d.app.QueueUpdateDraw(func() {
			d.updateChatView()
			d.updateStatusView()
		})
	}()
	
	// Build messages for AI with comprehensive system context
	messages := []ai.Message{
		{
			Role:    "system",
			Content: d.getSystemContext(),
		},
	}
	
	// Add recent chat history
	for _, msg := range d.chatHistory[maxInt(0, len(d.chatHistory)-10):] {
		if msg.Role == "user" || msg.Role == "assistant" {
			content := msg.Content
			if msg.Think != "" {
				content = msg.Think + "\n\n" + content
			}
			messages = append(messages, ai.Message{
				Role:    msg.Role,
				Content: content,
			})
		}
	}
	
	// Start streaming with current model
	stream, err := d.aiManager.StreamChatWithFallback(d.ctx, messages, ai.StreamOptions{
		Model: d.currentModel,
	})
	if err != nil {
		d.app.QueueUpdateDraw(func() {
			if len(d.chatHistory) > 0 {
				d.chatHistory[len(d.chatHistory)-1].Content = fmt.Sprintf("❌ Error: %v", err)
				d.chatHistory[len(d.chatHistory)-1].Streaming = false
			}
			d.updateChatView()
		})
		return
	}
	
	// Process stream with batched updates
	thinkProcessor := ai.NewThinkProcessor()
	updateBuffer := make(chan bool, 1)
	
	// Start update throttler goroutine
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond) // Update max 10 times per second
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				select {
				case <-updateBuffer:
					d.app.QueueUpdateDraw(func() {
						d.updateChatView()
					})
				default:
					// No update needed
				}
			case <-d.ctx.Done():
				return
			}
		}
	}()
	
	for response := range stream {
		if response.Error != nil {
			d.app.QueueUpdateDraw(func() {
				if len(d.chatHistory) > 0 {
					d.chatHistory[len(d.chatHistory)-1].Content = fmt.Sprintf("❌ Error: %v", response.Error)
				}
				d.updateChatView()
			})
			return
		}
		
		if response.Finished {
			break
		}
		
		if response.Content != "" || response.ThinkContent != "" {
			// Process with think processor if needed
			regularContent := response.Content
			thinkContent := response.ThinkContent
			
			if response.Content != "" && (strings.Contains(response.Content, "<think>") || strings.Contains(response.Content, "</think>")) {
				regularContent, thinkContent = ai.ProcessStreamContentWithThinks(thinkProcessor, response.Content)
			}
			
			// Update content immediately but batch UI updates
			if len(d.chatHistory) > 0 {
				lastMsg := &d.chatHistory[len(d.chatHistory)-1]
				if regularContent != "" {
					// Process content for inline code block formatting
					processedContent := d.formatInlineCodeBlocks(regularContent)
					lastMsg.Content += processedContent
				}
				if thinkContent != "" {
					lastMsg.Think += thinkContent
				}
			}
			
			// Signal for batched UI update
			select {
			case updateBuffer <- true:
			default:
				// Update already pending
			}
		}
	}
	
	// Final update when stream is done
	d.app.QueueUpdateDraw(func() {
		d.updateChatView()
	})
}

// handleCommand handles slash commands
func (d *TViewDashboard) handleCommand(command string) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return
	}
	
	var response string
	
	switch parts[0] {
	case "/help":
		response = d.getHelpText()
	case "/provider":
		if len(parts) > 1 {
			response = d.switchProvider(parts[1])
		} else {
			response = "Usage: /provider <name>\nAvailable: ollama, openai, claude, openrouter"
		}
	case "/model":
		if len(parts) > 1 {
			response = d.switchModel(parts[1])
		} else {
			response = "Usage: /model <name>"
		}
	case "/status":
		response = d.getSystemStatus()
	case "/clear":
		d.chatHistory = d.chatHistory[:0]
		// Reset code block state
		d.inCodeBlock = false
		d.codeBlockLines = []string{}
		d.addWelcomeMessage()
		d.updateChatView()
		return
	default:
		response = fmt.Sprintf("Unknown command: %s\nType '/help' for available commands", parts[0])
	}
	
	// Add system response
	if response != "" {
		sysMsg := TViewChatMessage{
			Role:      "system",
			Content:   response,
			Timestamp: time.Now(),
		}
		d.chatHistory[len(d.chatHistory)-1] = sysMsg // Replace the streaming placeholder
		d.streaming = false
		d.updateChatView()
		d.updateStatusView()
	}
}

// updateChatView updates the chat display
func (d *TViewDashboard) updateChatView() {
	d.chatView.Clear()
	
	for _, msg := range d.chatHistory {
		timestamp := msg.Timestamp.Format("15:04:05")
		
		switch msg.Role {
		case "user":
			fmt.Fprintf(d.chatView, "[blue]You[white] [gray]%s[white]\n%s\n\n", timestamp, msg.Content)
		case "assistant":
			label := "AI"
			if msg.Streaming {
				label = "AI [yellow]●[white]"
			}
			
			fmt.Fprintf(d.chatView, "[green]%s[white] [gray]%s[white]\n", label, timestamp)
			
			// Show thinking content in collapsed format if present
			if msg.Think != "" {
				thinkPreview := strings.ReplaceAll(msg.Think, "\n", " ")
				if len(thinkPreview) > 60 {
					thinkPreview = thinkPreview[:60] + "..."
				}
				fmt.Fprintf(d.chatView, "[cyan]▼ Thinking:[white] [gray]%s[white]\n\n", thinkPreview)
			}
			
			if msg.Content != "" {
				fmt.Fprintf(d.chatView, "%s", msg.Content)
			}
			
			if msg.Streaming && msg.Content == "" {
				fmt.Fprintf(d.chatView, "[yellow]Thinking...[white]")
			}
			
			fmt.Fprintf(d.chatView, "\n\n")
			
		case "system":
			fmt.Fprintf(d.chatView, "[orange]System[white] [gray]%s[white]\n%s\n\n", timestamp, msg.Content)
		}
	}
	
	d.chatView.ScrollToEnd()
}

// updateThinkView updates the think content display (now handled in chat view)
func (d *TViewDashboard) updateThinkView() {
	// Think content is now displayed inline in the chat view
	// This function is kept for compatibility but does nothing
}

// updateStatusView updates the status display
func (d *TViewDashboard) updateStatusView() {
	var status strings.Builder
	
	// Provider info
	status.WriteString(fmt.Sprintf("[yellow]Provider:[white] %s\n", d.currentProvider))
	status.WriteString(fmt.Sprintf("[yellow]Model:[white] %s\n", d.currentModel))
	
	// Streaming status
	if d.streaming {
		status.WriteString("[green]Status:[white] [yellow]Streaming[white]\n")
	} else {
		status.WriteString("[green]Status:[white] [green]Ready[white]\n")
	}
	
	// Provider status
	providerStatus := d.aiManager.GetProviderStatus()
	status.WriteString("\n[yellow]Providers:[white]\n")
	for name, pStatus := range providerStatus {
		indicator := "[red]●[white]"
		if pStatus.Available {
			indicator = "[green]●[white]"
		}
		status.WriteString(fmt.Sprintf("%s %s\n", indicator, name))
	}
	
	d.statusView.SetText(status.String())
}

// Helper functions
func (d *TViewDashboard) switchProvider(name string) string {
	if err := d.aiManager.SwitchProvider(name); err != nil {
		return fmt.Sprintf("❌ Failed to switch provider: %s", err)
	}
	
	if provider, err := d.aiManager.GetCurrentProvider(); err == nil {
		d.currentProvider = provider.Name()
		d.currentModel = provider.DefaultModel()
	}
	
	d.updateStatusView()
	return fmt.Sprintf("Switched to provider: %s", name)
}

func (d *TViewDashboard) switchModel(name string) string {
	if err := d.aiManager.SwitchModel(name); err != nil {
		return fmt.Sprintf("❌ Failed to switch model: %s", err)
	}
	
	d.currentModel = name
	d.updateStatusView()
	return fmt.Sprintf("Switched to model: %s", name)
}

func (d *TViewDashboard) getHelpText() string {
	return `[yellow]Matey AI Commands[white]

[green]Chat Commands:[white]
• Just type naturally to chat with AI
• AI can help with MCP configuration, Kubernetes manifests, troubleshooting

[green]System Commands:[white]
• [cyan]/help[white] - Show this help
• [cyan]/provider <name>[white] - Switch AI provider (ollama, openai, claude, openrouter)
• [cyan]/model <name>[white] - Switch AI model
• [cyan]/status[white] - Show system status
• [cyan]/clear[white] - Clear chat history

[green]Examples:[white]
• "Create an MCP server configuration for a web scraper"
• "How do I scale my workflow to 3 replicas?"
• "Show me a health monitoring workflow"
• "/provider ollama"
• "/model llama3:70b"

[green]Navigation:[white]
• [yellow]Page Up/Down[white] - Scroll chat history
• [yellow]Ctrl+Home/End[white] - Go to beginning/end
• [yellow]Ctrl+Up/Down[white] - Scroll line by line
• [yellow]Mouse/Trackpad[white] - Scroll naturally

[green]Keyboard:[white]
• [yellow]Enter[white] - Send message
• [yellow]Ctrl+C[white] - Exit`
}

func (d *TViewDashboard) getSystemStatus() string {
	var status strings.Builder
	
	status.WriteString("[yellow]System Status[white]\n\n")
	
	// AI Providers
	status.WriteString("[green]AI Providers:[white]\n")
	providerStatus := d.aiManager.GetProviderStatus()
	for name, pStatus := range providerStatus {
		statusStr := "[red]Unavailable[white]"
		if pStatus.Available {
			statusStr = "[green]Available[white]"
		}
		status.WriteString(fmt.Sprintf("• %s: %s\n", name, statusStr))
		if pStatus.Error != "" {
			status.WriteString(fmt.Sprintf("  Error: %s\n", pStatus.Error))
		}
	}
	
	// Current config
	status.WriteString(fmt.Sprintf("\n[green]Current:[white]\n"))
	status.WriteString(fmt.Sprintf("• Provider: %s\n", d.currentProvider))
	status.WriteString(fmt.Sprintf("• Model: %s\n", d.currentModel))
	
	return status.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// formatInlineCodeBlocks formats code blocks inline with simple highlighting
func (d *TViewDashboard) formatInlineCodeBlocks(content string) string {
	// Simple approach: add visual headers before opening code blocks only
	if strings.Contains(content, "```") {
		result := content
		
		// Add headers before opening code block markers (preserve original markdown)
		result = strings.ReplaceAll(result, "```yaml", "[yellow]── matey.yaml ──[white]\n```yaml")
		result = strings.ReplaceAll(result, "```json", "[yellow]── config.json ──[white]\n```json") 
		result = strings.ReplaceAll(result, "```bash", "[yellow]── commands ──[white]\n```bash")
		result = strings.ReplaceAll(result, "```kubernetes", "[yellow]── manifest.yaml ──[white]\n```kubernetes")
		result = strings.ReplaceAll(result, "```k8s", "[yellow]── manifest.yaml ──[white]\n```k8s")
		
		return result
	}
	
	return content
}

// getCodeBlockFilename generates a filename based on language
func (d *TViewDashboard) getCodeBlockFilename() string {
	switch d.codeBlockLang {
	case "yaml", "yml":
		return "matey.yaml"
	case "json":
		return "config.json"
	case "bash", "sh":
		return "install.sh"
	case "kubernetes", "k8s":
		return "deployment.yaml"
	default:
		return fmt.Sprintf("%s-config", d.codeBlockLang)
	}
}


// getSystemContext returns comprehensive system context for the AI
func (d *TViewDashboard) getSystemContext() string {
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
		"## Configuration Format (matey.yaml)\n" +
		"Each server in the servers section has:\n" +
		"- build: Docker build context and dockerfile\n" +
		"- command/args: Execution parameters\n" +
		"- env: Environment variables\n" +
		"- http_port: HTTP port for the server\n" +
		"- protocol: http, sse, or stdio\n" +
		"- capabilities: [tools, resources, prompts]\n" +
		"- security: Security policies and constraints\n" +
		"- authentication: OAuth/API key requirements\n" +
		"- volumes: Volume mounts\n" +
		"- deploy.resources: CPU/memory limits\n\n" +
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
		"### Workflow Example Structure:\n" +
		"```yaml\n" +
		"apiVersion: mcp.matey.ai/v1\n" +
		"kind: Workflow\n" +
		"spec:\n" +
		"  schedule: '0 9 * * 1-5'  # Monday-Friday at 9 AM\n" +
		"  timezone: 'America/New_York'\n" +
		"  enabled: true\n" +
		"  steps:\n" +
		"  - name: fetch-data\n" +
		"    tool: 'github/search_repositories'\n" +
		"    parameters:\n" +
		"      query: 'language:go stars:>100'\n" +
		"  - name: analyze-data\n" +
		"    tool: 'memory/store_knowledge'\n" +
		"    dependsOn: ['fetch-data']\n" +
		"    condition: '{{ .fetch-data.output.total_count > 0 }}'\n" +
		"    parameters:\n" +
		"      data: '{{ .fetch-data.output }}'\n" +
		"```\n\n" +
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
		"### Toolbox Example:\n" +
		"```yaml\n" +
		"apiVersion: mcp.matey.ai/v1\n" +
		"kind: MCPToolbox\n" +
		"spec:\n" +
		"  template: 'research-agent'\n" +
		"  description: 'AI research workflow with web search and knowledge storage'\n" +
		"  servers:\n" +
		"    searcher:\n" +
		"      serverTemplate: 'searxng'\n" +
		"      toolboxRole: 'data-collector'\n" +
		"      priority: 1\n" +
		"    memory:\n" +
		"      serverTemplate: 'memory'\n" +
		"      toolboxRole: 'knowledge-store'\n" +
		"      priority: 2\n" +
		"  dependencies:\n" +
		"  - server: memory\n" +
		"    dependsOn: [searcher]\n" +
		"    waitTimeout: '30s'\n" +
		"```\n\n" +
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
		"### MCP Server Capabilities:\n" +
		"- tools: Provides executable functions\n" +
		"- resources: Provides data and content\n" +
		"- prompts: Provides AI prompt templates\n\n" +
		"## Protocols Supported\n" +
		"- HTTP: Standard REST API (most servers)\n" +
		"- SSE: Real-time streaming (postgres-mcp)\n" +
		"- STDIO: Process-based (github, sequential-thinking, searxng)\n\n" +
		"## Security Features\n" +
		"- OAuth2 authentication and authorization\n" +
		"- RBAC with scopes (mcp:tools, mcp:resources, mcp:prompts, mcp:*)\n" +
		"- API key authentication\n" +
		"- Container security policies\n" +
		"- Pod security standards (restricted, baseline, privileged)\n" +
		"- Image signature verification\n\n" +
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
		"- For configs/YAML, use standard markdown code blocks:\n" +
		"  ```yaml\n" +
		"  [your configuration here]\n" +
		"  ```\n" +
		"- Use ```json for JSON configs, ```bash for commands\n" +
		"- Keep chat responses brief and conversational\n" +
		"- Explain which existing MCP servers you're using\n" +
		"- Suggest appropriate CRDs (Workflow, MCPToolbox, etc.) for the use case\n" +
		"- Code blocks automatically appear with visual separation"
}

// NewTViewDashboardCommand creates the tview dashboard command
func NewTViewDashboardCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch AI chat (like Claude Code)",
		Long:  "Launch an AI chat interface that works exactly like Claude Code with natural terminal scrolling",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check if terminal mode is requested
			terminalMode, _ := cmd.Flags().GetBool("terminal")
			if terminalMode {
				// Run pure terminal mode like Claude Code
				return runClaudeCodeMode()
			}
			
			// Default to pure terminal mode since TUI doesn't scroll properly
			return runClaudeCodeMode()
		},
	}
	
	// Add flag for terminal-native mode
	cmd.Flags().BoolP("terminal", "t", true, "Use terminal-native mode (like Claude Code) - DEFAULT")
	
	return cmd
}

// runClaudeCodeMode runs exactly like Claude Code - pure terminal output
func runClaudeCodeMode() error {
	// Just run the simple chat from termchat.go
	chat := NewTermChat()
	defer chat.cancel()
	return chat.Run()
}