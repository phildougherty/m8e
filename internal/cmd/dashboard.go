package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	
	"github.com/phildougherty/m8e/internal/ai"
	"github.com/phildougherty/m8e/internal/compose"
	"github.com/phildougherty/m8e/internal/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Tab represents different dashboard tabs
type Tab string

const (
	TabChat        Tab = "chat"
	TabMonitoring  Tab = "monitoring"
	TabConfig      Tab = "config"
	TabWorkflows   Tab = "workflows"
)

// ConfigState represents the state of the configuration tab
type ConfigState int

const (
	ConfigStateList ConfigState = iota
	ConfigStateEdit
	ConfigStateNew
)

// WorkflowState represents the state of the workflows tab
type WorkflowState int

const (
	WorkflowStateList WorkflowState = iota
	WorkflowStateEdit
	WorkflowStateMonitor
	WorkflowStateDesign
)

// ConfigResource represents a Kubernetes configuration resource
type ConfigResource struct {
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	Modified  time.Time `json:"modified"`
	Valid     bool      `json:"valid"`
	Errors    []string  `json:"errors,omitempty"`
}

// WorkflowInfo represents workflow information for the TUI
type WorkflowInfo struct {
	Name        string    `json:"name"`
	Namespace   string    `json:"namespace"`
	Schedule    string    `json:"schedule"`
	Enabled     bool      `json:"enabled"`
	Phase       string    `json:"phase"`
	LastRun     *time.Time `json:"lastRun,omitempty"`
	NextRun     *time.Time `json:"nextRun,omitempty"`
	StepCount   int       `json:"stepCount"`
}

// DashboardStyles contains all styling for the dashboard
var DashboardStyles = struct {
	// Layout styles
	Container      lipgloss.Style
	ChatPane       lipgloss.Style
	ArtifactsPane  lipgloss.Style
	InputPane      lipgloss.Style
	
	// Content styles
	UserMessage    lipgloss.Style
	AIMessage      lipgloss.Style
	SystemMessage  lipgloss.Style
	ErrorMessage   lipgloss.Style
	Muted          lipgloss.Style
	
	// UI element styles
	TabActive      lipgloss.Style
	TabInactive    lipgloss.Style
	ProviderBadge  lipgloss.Style
	StatusBadge    lipgloss.Style
	
	// Artifacts styles
	CodeBlock      lipgloss.Style
	ConfigBlock    lipgloss.Style
	DiagramBlock   lipgloss.Style
	
	// Configuration styles
	ConfigList     lipgloss.Style
	ConfigSelected lipgloss.Style
	ConfigEditor   lipgloss.Style
	ConfigValid    lipgloss.Style
	ConfigInvalid  lipgloss.Style
	
	// Workflow styles
	WorkflowCard   lipgloss.Style
	WorkflowActive lipgloss.Style
	WorkflowPaused lipgloss.Style
}{
	Container: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("238")),
	
	ChatPane: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("238")).
		Padding(1),
	
	ArtifactsPane: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("238")).
		Padding(1),
	
	InputPane: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("39")).
		Padding(0, 1),
	
	UserMessage: lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Bold(true).
		Margin(0, 0, 1, 0),
	
	AIMessage: lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Margin(0, 0, 1, 0),
	
	SystemMessage: lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Italic(true).
		Margin(0, 0, 1, 0),
	
	ErrorMessage: lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Bold(true).
		Margin(0, 0, 1, 0),
	
	Muted: lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Italic(true).
		Margin(0, 0, 1, 0),
	
	TabActive: lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Background(lipgloss.Color("238")).
		Bold(true).
		Padding(0, 1),
	
	TabInactive: lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(0, 1),
	
	ProviderBadge: lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Background(lipgloss.Color("236")).
		Bold(true).
		Padding(0, 1),
	
	StatusBadge: lipgloss.NewStyle().
		Foreground(lipgloss.Color("46")).
		Bold(true),
	
	CodeBlock: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Background(lipgloss.Color("235")).
		Padding(1).
		Margin(1, 0),
	
	ConfigBlock: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("33")).
		Background(lipgloss.Color("234")).
		Padding(1).
		Margin(1, 0),
	
	DiagramBlock: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("205")).
		Background(lipgloss.Color("234")).
		Padding(1).
		Margin(1, 0),
	
	ConfigList: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1),
	
	ConfigSelected: lipgloss.NewStyle().
		Background(lipgloss.Color("240")).
		Foreground(lipgloss.Color("15")).
		Bold(true),
	
	ConfigEditor: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("33")).
		Background(lipgloss.Color("234")).
		Padding(1),
	
	ConfigValid: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("46")).
		Foreground(lipgloss.Color("46")),
	
	ConfigInvalid: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("196")).
		Foreground(lipgloss.Color("196")),
	
	WorkflowCard: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1).
		Margin(0, 1, 1, 0),
	
	WorkflowActive: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("46")).
		Padding(1).
		Margin(0, 1, 1, 0),
	
	WorkflowPaused: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("226")).
		Padding(1).
		Margin(0, 1, 1, 0),
}

// DashboardModel represents the state of the dashboard TUI
type DashboardModel struct {
	// Core components
	composer      *compose.K8sComposer
	k8sClient     kubernetes.Interface
	aiManager     *ai.Manager
	
	// UI state
	width         int
	height        int
	currentTab    Tab
	
	// Chat state
	chatHistory   []ChatMessage
	inputText     string
	streaming     bool
	
	// Artifacts state
	currentArtifact string
	artifactType    string
	
	// Configuration state
	configState     ConfigState
	selectedConfig  string
	configContent   string
	configErrors    []string
	
	// Workflow state  
	workflowState   WorkflowState
	workflows       []WorkflowInfo
	selectedWorkflow string
	
	// Status
	lastUpdate    time.Time
	err           error
	
	// Context
	ctx           context.Context
	cancel        context.CancelFunc
}

// ChatMessage represents a message in the chat
type ChatMessage struct {
	Role         string    `json:"role"`
	Content      string    `json:"content"`
	ThinkContent string    `json:"think_content"` // Reasoning content for collapsible sections
	Timestamp    time.Time `json:"timestamp"`
	Streaming    bool      `json:"streaming"`
	ThinkExpanded bool     `json:"think_expanded"` // Whether think section is expanded
}

// NewDashboardCommand creates a new dashboard command
func NewDashboardCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Launch the AI-powered matey dashboard",
		Long: `Launch the AI-powered matey dashboard with the following features:

• Chat with AI to configure and manage your MCP servers
• Real-time visual feedback with artifact generation
• Multi-provider AI support (OpenAI, Claude, Ollama, OpenRouter)
• Live monitoring and configuration management
• Interactive workflow and server management

Controls:
• Tab: Switch between chat, monitoring, configuration, and workflows
• Ctrl+C: Exit dashboard
• Enter: Send message or execute command
• ↑/↓: Navigate chat history
• F1: Help and command reference

Provider Commands:
• /provider <name>: Switch AI provider
• /model <name>: Switch AI model
• /list servers: List all MCP servers
• /status: Show system status
• /help: Show all commands`,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")
			provider, _ := cmd.Flags().GetString("provider")
			model, _ := cmd.Flags().GetString("model")
			
			return runDashboard(file, provider, model)
		},
	}

	cmd.Flags().StringP("provider", "p", "", "AI provider to use (openai, claude, ollama, openrouter)")
	cmd.Flags().StringP("model", "m", "", "AI model to use")
	cmd.Flags().Duration("refresh", 2*time.Second, "Refresh interval for monitoring")

	return cmd
}

// runDashboard starts the dashboard TUI
func runDashboard(configFile, provider, model string) error {
	// Load .env file if it exists (for API keys)
	if err := godotenv.Load(); err != nil {
		// Don't fail if .env doesn't exist, just log
		log.Printf("No .env file found or error loading it: %v", err)
	}

	// Create composer
	composer, err := compose.NewK8sComposer(configFile, "default")
	if err != nil {
		return fmt.Errorf("failed to create composer: %w", err)
	}

	// Create Kubernetes client
	k8sClient, err := createK8sClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create AI manager
	aiConfig := loadAIConfigFromFile(configFile)
	aiManager := ai.NewManager(aiConfig)

	// Override provider/model if specified
	if provider != "" {
		if err := aiManager.SwitchProvider(provider); err != nil {
			return fmt.Errorf("failed to switch to provider %s: %w", provider, err)
		}
	}

	// Create context
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize dashboard model
	dashboardModel := DashboardModel{
		composer:    composer,
		k8sClient:   k8sClient,
		aiManager:   aiManager,
		currentTab:  TabChat,
		chatHistory: []ChatMessage{
			{
				Role:      "system",
				Content:   "Welcome to Matey Dashboard! I can help you configure and manage your MCP servers. Type '/help' for commands or just ask me anything.",
				Timestamp: time.Now(),
			},
		},
		ctx:        ctx,
		cancel:     cancel,
		lastUpdate: time.Now(),
	}

	// Start the TUI
	program := tea.NewProgram(&dashboardModel, tea.WithAltScreen())
	_, err = program.Run()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to start dashboard: %w", err)
	}

	return nil
}

// createK8sClient creates a Kubernetes client
func createK8sClient() (kubernetes.Interface, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return clientset, nil
}

// loadAIConfigFromFile loads AI configuration from matey.yaml file
func loadAIConfigFromFile(configFile string) ai.Config {
	// Load matey config
	composeConfig, err := config.LoadConfig(configFile)
	if err != nil {
		// Fall back to defaults if config can't be loaded
		return getDefaultAIConfig()
	}

	// Convert config AI settings to ai.Config
	aiConfig := ai.Config{
		DefaultProvider:   composeConfig.AI.DefaultProvider,
		FallbackProviders: composeConfig.AI.FallbackProviders,
		Providers:         make(map[string]ai.ProviderConfig),
	}

	// Convert provider configurations
	for name, providerConfig := range composeConfig.AI.Providers {
		timeout := 30 * time.Second
		if providerConfig.Timeout != "" {
			if d, err := time.ParseDuration(providerConfig.Timeout); err == nil {
				timeout = d
			}
		}

		aiConfig.Providers[name] = ai.ProviderConfig{
			APIKey:       providerConfig.APIKey,
			Endpoint:     providerConfig.Endpoint,
			DefaultModel: providerConfig.DefaultModel,
			MaxTokens:    providerConfig.MaxTokens,
			Temperature:  providerConfig.Temperature,
			Timeout:      timeout,
		}
	}

	// Apply defaults and environment variables if not set in config
	return mergeWithDefaults(aiConfig)
}

// getDefaultAIConfig returns default AI configuration
func getDefaultAIConfig() ai.Config {
	return ai.Config{
		DefaultProvider: "ollama",
		Providers: map[string]ai.ProviderConfig{
			"openai": {
				APIKey:       os.Getenv("OPENAI_API_KEY"),
				DefaultModel: "gpt-4",
				MaxTokens:    4096,
				Temperature:  0.7,
				Timeout:      30 * time.Second,
			},
			"claude": {
				APIKey:       os.Getenv("ANTHROPIC_API_KEY"),
				DefaultModel: "claude-3.5-sonnet-20241022",
				MaxTokens:    8192,
				Temperature:  0.7,
				Timeout:      30 * time.Second,
			},
			"ollama": {
				Endpoint:     getEnvOrDefault("OLLAMA_ENDPOINT", "http://localhost:11434"),
				DefaultModel: "llama3",
				MaxTokens:    4096,
				Temperature:  0.7,
				Timeout:      60 * time.Second,
			},
			"openrouter": {
				APIKey:       os.Getenv("OPENROUTER_API_KEY"),
				DefaultModel: "anthropic/claude-3.5-sonnet",
				MaxTokens:    4096,
				Temperature:  0.7,
				Timeout:      30 * time.Second,
			},
		},
		FallbackProviders: []string{"claude", "openai"},
	}
}

// mergeWithDefaults merges config with defaults and environment variables
func mergeWithDefaults(configAI ai.Config) ai.Config {
	defaults := getDefaultAIConfig()
	
	// Use defaults if not set in config
	if configAI.DefaultProvider == "" {
		configAI.DefaultProvider = defaults.DefaultProvider
	}
	
	if len(configAI.FallbackProviders) == 0 {
		configAI.FallbackProviders = defaults.FallbackProviders
	}

	// Merge providers - config takes precedence, but fill in missing values from defaults
	for name, defaultProvider := range defaults.Providers {
		if configProvider, exists := configAI.Providers[name]; exists {
			// Merge missing fields from defaults
			if configProvider.APIKey == "" {
				configProvider.APIKey = defaultProvider.APIKey
			}
			if configProvider.Endpoint == "" {
				configProvider.Endpoint = defaultProvider.Endpoint
			}
			if configProvider.DefaultModel == "" {
				configProvider.DefaultModel = defaultProvider.DefaultModel
			}
			if configProvider.MaxTokens == 0 {
				configProvider.MaxTokens = defaultProvider.MaxTokens
			}
			if configProvider.Temperature == 0 {
				configProvider.Temperature = defaultProvider.Temperature
			}
			if configProvider.Timeout == 0 {
				configProvider.Timeout = defaultProvider.Timeout
			}
			configAI.Providers[name] = configProvider
		} else {
			// Provider not in config, use default
			configAI.Providers[name] = defaultProvider
		}
	}

	return configAI
}

// getEnvOrDefault gets environment variable or returns default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getAnimatedDots returns animated dots for loading indicator
func (m *DashboardModel) getAnimatedDots() string {
	// Create animation based on current time
	now := time.Now()
	cycle := now.UnixMilli() / 300 // Change every 300ms for faster animation
	
	switch cycle % 4 {
	case 0:
		return "."
	case 1:
		return ".."
	case 2:
		return "..."
	case 3:
		return ""
	default:
		return "."
	}
}