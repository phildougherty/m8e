package app

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/phildougherty/m8e/internal/ai"
	appcontext "github.com/phildougherty/m8e/internal/context"
	"github.com/phildougherty/m8e/internal/mcp"
)

// App represents the main application state
type App struct {
	AI            *ai.Manager
	MCP           *mcp.MCPClient
	Context       *appcontext.ContextManager
	FileDiscovery *appcontext.FileDiscovery
	WorkingDir    string
}

// New creates a new App instance with all necessary components
func New() (*App, error) {
	// Load environment variables
	_ = godotenv.Load()

	// Get working directory
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	// Initialize AI manager
	aiConfig := ai.Config{
		DefaultProvider:   "openrouter",
		FallbackProviders: []string{"ollama", "openai", "claude"},
		Providers: map[string]ai.ProviderConfig{
			"openrouter": {
				APIKey:       os.Getenv("OPENROUTER_API_KEY"),
				Endpoint:     "https://openrouter.ai/api/v1",
				DefaultModel: "moonshotai/kimi-k2",
			},
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
		},
	}
	aiManager := ai.NewManager(aiConfig)

	// Initialize MCP client. The API key is read by NewMCPClient from the
	// MCP_API_KEY environment variable, which godotenv.Load() above has
	// already populated from any .env file. We must not overwrite it here:
	// a previous hardcoded "myapikey" silently replaced the operator's real
	// credential and broke every authenticated proxy call.
	proxyURL := detectClusterMCPProxy()
	if os.Getenv("MCP_API_KEY") == "" {
		fmt.Fprintln(os.Stderr, "warning: MCP_API_KEY is not set; calls to the MCP proxy will be unauthenticated")
	}
	mcpClient := mcp.NewMCPClient(proxyURL)

	// Initialize context manager
	currentProvider, _ := aiManager.GetCurrentProvider()
	contextConfig := appcontext.ContextConfig{
		MaxTokens:          32768,
		TruncationStrategy: "intelligent",
		RetentionDays:      7,
	}
	contextManager := appcontext.NewContextManager(contextConfig, currentProvider)

	// Initialize file discovery
	fileDiscovery, err := appcontext.NewFileDiscovery(cwd)
	if err != nil {
		return nil, err
	}

	return &App{
		AI:            aiManager,
		MCP:           mcpClient,
		Context:       contextManager,
		FileDiscovery: fileDiscovery,
		WorkingDir:    cwd,
	}, nil
}

// detectClusterMCPProxy detects the MCP proxy endpoint in the cluster
func detectClusterMCPProxy() string {
	// Try multiple connection methods in order of preference, prioritizing localhost
	endpoints := []string{
		"http://localhost:9876",                           // Default localhost port
		"http://localhost:30876",                          // NodePort service
		"http://localhost:8080",                           // Alternative port
		"http://matey-proxy.matey.svc.cluster.local:9876", // Internal cluster URL
	}

	// For now, return the first endpoint
	// In practice, you'd test connectivity like in your original code
	return endpoints[0]
}
