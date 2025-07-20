package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/phildougherty/m8e/internal/ai"
	appcontext "github.com/phildougherty/m8e/internal/context"
)

type contextFlags struct {
	maxTokens     int
	outputFile    string
	outputFormat  string
	includeStats  bool
	aiProvider    string
	retentionDays int
	namespace     string
	persistK8s    bool
}

// NewContextCommand creates the context management command with full enterprise features
func NewContextCommand() *cobra.Command {
	contextCmd := &cobra.Command{
		Use:   "context",
		Short: "Enterprise context management for AI interactions",
		Long: `Advanced context management with enterprise features:
- Intelligent file aggregation and truncation
- Context size management with token limits
- @-mention processing and file expansion
- AI provider integration for token estimation
- Kubernetes persistence and multi-environment support
- Context window analytics and optimization`,
	}

	// Add subcommands for comprehensive context management
	contextCmd.AddCommand(NewContextAddCommand())
	contextCmd.AddCommand(NewContextListCommand())
	contextCmd.AddCommand(NewContextGetCommand())
	contextCmd.AddCommand(NewContextRemoveCommand())
	contextCmd.AddCommand(NewContextClearCommand())
	contextCmd.AddCommand(NewContextWindowCommand())
	contextCmd.AddCommand(NewContextStatsCommand())
	contextCmd.AddCommand(NewContextMentionsCommand())

	return contextCmd
}

func NewContextAddCommand() *cobra.Command {
	var contextOpts contextFlags

	addCmd := &cobra.Command{
		Use:   "add [FILES...]",
		Short: "Add files to context with intelligent processing",
		Long: `Add files to the context management system with features:
- Automatic @-mention expansion
- Content type detection and optimization
- Intelligent truncation when approaching token limits
- Metadata extraction and indexing

Examples:
  matey context add src/*.go                    # Add Go source files
  matey context add --max-tokens 4000 @main.go # Add with token limit
  matey context add --output context.txt files # Save context to file`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextAddCommand(args, contextOpts)
		},
	}

	// Configuration flags
	addCmd.Flags().IntVar(&contextOpts.maxTokens, "max-tokens", 32768, "Maximum context tokens")
	addCmd.Flags().StringVar(&contextOpts.outputFile, "output", "", "Output file (default: managed internally)")
	addCmd.Flags().StringVar(&contextOpts.outputFormat, "format", "text", "Output format: text, json")
	addCmd.Flags().BoolVar(&contextOpts.includeStats, "stats", true, "Include context statistics")
	addCmd.Flags().StringVar(&contextOpts.aiProvider, "ai-provider", "", "AI provider for token estimation: openai, claude, ollama")
	addCmd.Flags().IntVar(&contextOpts.retentionDays, "retention", 7, "Context retention in days")
	addCmd.Flags().StringVar(&contextOpts.namespace, "namespace", "matey-system", "Kubernetes namespace for persistence")
	addCmd.Flags().BoolVar(&contextOpts.persistK8s, "persist", false, "Persist context to Kubernetes")

	return addCmd
}

func NewContextListCommand() *cobra.Command {
	var outputFormat string

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all context items",
		Long:  `List all context items with metadata and statistics.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextListCommand(outputFormat)
		},
	}

	listCmd.Flags().StringVar(&outputFormat, "format", "table", "Output format: table, json")

	return listCmd
}

func NewContextGetCommand() *cobra.Command {
	var outputFormat string

	getCmd := &cobra.Command{
		Use:   "get [ID|FILE_PATH]",
		Short: "Get specific context item",
		Long:  `Get a specific context item by ID or file path.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextGetCommand(args[0], outputFormat)
		},
	}

	getCmd.Flags().StringVar(&outputFormat, "format", "text", "Output format: text, json")

	return getCmd
}

func NewContextRemoveCommand() *cobra.Command {
	removeCmd := &cobra.Command{
		Use:   "remove [ID|FILE_PATH]",
		Short: "Remove context item",
		Long:  `Remove a context item by ID or file path.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextRemoveCommand(args[0])
		},
	}

	return removeCmd
}

func NewContextClearCommand() *cobra.Command {
	var force bool

	clearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear all context items",
		Long:  `Clear all context items from the context manager.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextClearCommand(force)
		},
	}

	clearCmd.Flags().BoolVar(&force, "force", false, "Force clear without confirmation")

	return clearCmd
}

func NewContextWindowCommand() *cobra.Command {
	var outputFormat string

	windowCmd := &cobra.Command{
		Use:   "window",
		Short: "Show current context window",
		Long:  `Display the current context window with token usage and truncation info.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextWindowCommand(outputFormat)
		},
	}

	windowCmd.Flags().StringVar(&outputFormat, "format", "text", "Output format: text, json")

	return windowCmd
}

func NewContextStatsCommand() *cobra.Command {
	var outputFormat string

	statsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show context statistics",
		Long:  `Display detailed context management statistics and analytics.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextStatsCommand(outputFormat)
		},
	}

	statsCmd.Flags().StringVar(&outputFormat, "format", "text", "Output format: text, json")

	return statsCmd
}

func NewContextMentionsCommand() *cobra.Command {
	var pattern string
	var expand bool

	mentionsCmd := &cobra.Command{
		Use:   "mentions [TEXT]",
		Short: "Process @-mentions in text",
		Long: `Process @-mentions in text and expand file references.
		
Examples:
  matey context mentions "Check @main.go and @utils/*.go"
  matey context mentions --expand --pattern "*.go" "@src"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextMentionsCommand(args[0], pattern, expand)
		},
	}

	mentionsCmd.Flags().StringVar(&pattern, "pattern", "", "File pattern for expansion")
	mentionsCmd.Flags().BoolVar(&expand, "expand", false, "Expand file contents in output")

	return mentionsCmd
}

// Command implementations

func runContextAddCommand(files []string, opts contextFlags) error {
	// Initialize AI provider if specified
	var aiProvider ai.Provider
	if opts.aiProvider != "" {
		var err error
		aiProvider, err = createAIProvider(opts.aiProvider)
		if err != nil {
			fmt.Printf("Warning: Failed to initialize AI provider %s: %v\n", opts.aiProvider, err)
			fmt.Printf("Continuing with default token estimation...\n")
		}
	}

	// Initialize context manager
	config := appcontext.ContextConfig{
		MaxTokens:          opts.maxTokens,
		TruncationStrategy: appcontext.TruncateIntelligent,
		RetentionDays:      opts.retentionDays,
		PersistToK8s:       opts.persistK8s,
		Namespace:          opts.namespace,
	}

	contextManager := appcontext.NewContextManager(config, aiProvider)

	// Initialize mention processor and file discovery
	workingDir, _ := os.Getwd()
	fileDiscovery, err := appcontext.NewFileDiscovery(workingDir)
	if err != nil {
		return fmt.Errorf("failed to initialize file discovery: %w", err)
	}

	mentionProcessor := appcontext.NewMentionProcessor(workingDir, fileDiscovery, contextManager)

	var processedFiles []string

	// Process each file or mention
	for _, file := range files {
		// Handle @-mention syntax
		if strings.HasPrefix(file, "@") {
			// Parse the mention and process it
			mentions, err := mentionProcessor.ParseMentions(file)
			if err != nil {
				fmt.Printf("Warning: Failed to parse mention %s: %v\n", file, err)
				continue
			}

			for _, mention := range mentions {
				processedMention, err := mentionProcessor.ProcessMention(mention)
				if err != nil {
					fmt.Printf("Warning: Failed to process mention %s: %v\n", mention.Raw, err)
					continue
				}
				
				// Add the processed content to context
				err = contextManager.AddContext(appcontext.ContextTypeMention, processedMention.Raw, processedMention.Content, nil)
				if err != nil {
					fmt.Printf("Warning: Failed to add mention to context %s: %v\n", processedMention.Raw, err)
					continue
				}
				processedFiles = append(processedFiles, processedMention.Raw)
			}
		} else {
			// Expand glob patterns
			matches, err := filepath.Glob(file)
			if err != nil {
				fmt.Printf("Warning: Invalid pattern %s: %v\n", file, err)
				continue
			}

			if len(matches) == 0 {
				// Try adding as literal file
				err = addFileToContext(contextManager, file)
				if err != nil {
					fmt.Printf("Warning: Failed to add file %s: %v\n", file, err)
					continue
				}
				processedFiles = append(processedFiles, file)
			} else {
				for _, match := range matches {
					err = addFileToContext(contextManager, match)
					if err != nil {
						fmt.Printf("Warning: Failed to add file %s: %v\n", match, err)
						continue
					}
					processedFiles = append(processedFiles, match)
				}
			}
		}
	}

	// Get current context window and stats
	window := contextManager.GetCurrentWindow()
	stats := contextManager.GetStats()

	// Output results
	if opts.outputFile != "" {
		return outputContextToFile(window, opts.outputFile, opts.outputFormat)
	}

	// Display summary
	fmt.Printf("✅ Successfully processed %d files\n", len(processedFiles))
	fmt.Printf("📊 Context Summary:\n")
	fmt.Printf("  Total items: %d\n", len(window.Items))
	fmt.Printf("  Total tokens: %d / %d\n", window.TotalTokens, window.MaxTokens)
	fmt.Printf("  Truncated: %t\n", window.Truncated)

	if opts.includeStats {
		displayStats(stats, "text")
	}

	return nil
}

func runContextListCommand(outputFormat string) error {
	contextManager := createDefaultContextManager()
	window := contextManager.GetCurrentWindow()

	switch outputFormat {
	case "json":
		return outputJSON(window.Items)
	default:
		return displayContextItems(window.Items)
	}
}

func runContextGetCommand(identifier, outputFormat string) error {
	contextManager := createDefaultContextManager()

	// Try to get by ID first
	item, err := contextManager.GetContext(identifier)
	if err != nil {
		// Try to get by file path
		items, err := contextManager.GetContextByFile(identifier)
		if err != nil || len(items) == 0 {
			return fmt.Errorf("context item not found: %s", identifier)
		}
		item = items[0] // Take first match
	}

	switch outputFormat {
	case "json":
		return outputJSON(item)
	default:
		return displayContextItem(item)
	}
}

func runContextRemoveCommand(identifier string) error {
	contextManager := createDefaultContextManager()

	// Try to remove by ID first
	err := contextManager.RemoveContext(identifier)
	if err != nil {
		// Try to remove by file path
		err = contextManager.RemoveContextByFile(identifier)
		if err != nil {
			return fmt.Errorf("failed to remove context item: %w", err)
		}
	}

	fmt.Printf("✅ Removed context item: %s\n", identifier)
	return nil
}

func runContextClearCommand(force bool) error {
	if !force {
		fmt.Print("Are you sure you want to clear all context items? [y/N]: ")
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" {
			fmt.Println("Operation cancelled")
			return nil
		}
	}

	contextManager := createDefaultContextManager()
	
	// Clear all items (implement by getting all and removing them)
	window := contextManager.GetCurrentWindow()
	for _, item := range window.Items {
		contextManager.RemoveContext(item.ID)
	}

	fmt.Printf("✅ Cleared all context items\n")
	return nil
}

func runContextWindowCommand(outputFormat string) error {
	contextManager := createDefaultContextManager()
	window := contextManager.GetCurrentWindow()

	switch outputFormat {
	case "json":
		return outputJSON(window)
	default:
		return displayContextWindow(window)
	}
}

func runContextStatsCommand(outputFormat string) error {
	contextManager := createDefaultContextManager()
	stats := contextManager.GetStats()

	return displayStats(stats, outputFormat)
}

func runContextMentionsCommand(text, pattern string, expand bool) error {
	workingDir, _ := os.Getwd()
	fileDiscovery, err := appcontext.NewFileDiscovery(workingDir)
	if err != nil {
		return fmt.Errorf("failed to initialize file discovery: %w", err)
	}

	contextManager := createDefaultContextManager()
	mentionProcessor := appcontext.NewMentionProcessor(workingDir, fileDiscovery, contextManager)

	// Parse mentions from text
	mentions, err := mentionProcessor.ParseMentions(text)
	if err != nil {
		return fmt.Errorf("failed to parse mentions: %w", err)
	}
	
	fmt.Printf("🔍 Found %d mentions in text:\n\n", len(mentions))
	
	for _, mention := range mentions {
		fmt.Printf("📌 %s (type: %s)\n", mention.Raw, mention.Type)
		
		if expand {
			processedMention, err := mentionProcessor.ProcessMention(mention)
			if err != nil {
				fmt.Printf("  ❌ Failed to process: %v\n", err)
				continue
			}
			
			fmt.Printf("  📄 Content (%d chars):\n", len(processedMention.Content))
			// Show first 200 characters
			content := processedMention.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			fmt.Printf("  %s\n", content)
		}
		fmt.Println()
	}

	return nil
}

// Helper functions

func createDefaultContextManager() *appcontext.ContextManager {
	config := appcontext.ContextConfig{
		MaxTokens:          32768,
		TruncationStrategy: appcontext.TruncateIntelligent,
		RetentionDays:      7,
		PersistToK8s:       false,
		Namespace:          "matey-system",
	}

	return appcontext.NewContextManager(config, nil)
}

func createAIProvider(providerName string) (ai.Provider, error) {
	// Create a basic AI provider configuration
	// In a real implementation, this would read from config files or environment variables
	switch strings.ToLower(providerName) {
	case "openai":
		return ai.NewOpenAIProvider(ai.ProviderConfig{
			APIKey:       os.Getenv("OPENAI_API_KEY"),
			Endpoint:     "https://api.openai.com/v1",
			DefaultModel: "gpt-4",
			MaxTokens:    4096,
			Temperature:  0.7,
		})
	case "claude":
		return ai.NewClaudeProvider(ai.ProviderConfig{
			APIKey:       os.Getenv("ANTHROPIC_API_KEY"),
			Endpoint:     "https://api.anthropic.com",
			DefaultModel: "claude-3-sonnet-20240229",
			MaxTokens:    4096,
			Temperature:  0.7,
		})
	default:
		return nil, fmt.Errorf("unsupported AI provider: %s", providerName)
	}
}

func addFileToContext(contextManager *appcontext.ContextManager, filePath string) error {
	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Add to context manager
	return contextManager.AddContext(appcontext.ContextTypeFile, filePath, string(content), nil)
}

func outputContextToFile(window *appcontext.ContextWindow, filename, format string) error {
	var content []byte
	var err error

	switch format {
	case "json":
		content, err = json.MarshalIndent(window, "", "  ")
	default:
		// Text format - concatenate all items
		var builder strings.Builder
		builder.WriteString("# Context Window\n\n")
		for _, item := range window.Items {
			builder.WriteString(fmt.Sprintf("## %s\n\n", item.FilePath))
			builder.WriteString("```\n")
			builder.WriteString(item.Content)
			builder.WriteString("\n```\n\n")
		}
		content = []byte(builder.String())
	}

	if err != nil {
		return err
	}

	return os.WriteFile(filename, content, 0644)
}

func displayContextItems(items []appcontext.ContextItem) error {
	if len(items) == 0 {
		fmt.Println("No context items found")
		return nil
	}

	fmt.Printf("📋 Context Items (%d total):\n\n", len(items))
	
	for _, item := range items {
		fmt.Printf("🔗 %s\n", item.ID)
		fmt.Printf("  📄 File: %s\n", item.FilePath)
		fmt.Printf("  📝 Type: %s\n", item.Type)
		fmt.Printf("  🔢 Tokens: %d\n", item.TokenCount)
		fmt.Printf("  📅 Added: %s\n", item.Timestamp.Format("2006-01-02 15:04:05"))
		fmt.Printf("  👁️  Accessed: %d times (last: %s)\n", item.AccessCount, item.LastAccess.Format("2006-01-02 15:04:05"))
		fmt.Println()
	}

	return nil
}

func displayContextItem(item *appcontext.ContextItem) error {
	fmt.Printf("📄 Context Item: %s\n", item.ID)
	fmt.Printf("═══════════════════════════════════════════════════════════════\n")
	fmt.Printf("File: %s\n", item.FilePath)
	fmt.Printf("Type: %s\n", item.Type)
	fmt.Printf("Tokens: %d\n", item.TokenCount)
	fmt.Printf("Priority: %.2f\n", item.Priority)
	fmt.Printf("Hash: %s\n", item.Hash)
	fmt.Printf("Added: %s\n", item.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Printf("Last accessed: %s (%d times)\n", item.LastAccess.Format("2006-01-02 15:04:05"), item.AccessCount)
	
	if len(item.Metadata) > 0 {
		fmt.Printf("\nMetadata:\n")
		for k, v := range item.Metadata {
			fmt.Printf("  %s: %v\n", k, v)
		}
	}
	
	fmt.Printf("\nContent (%d chars):\n", len(item.Content))
	fmt.Printf("─────────────────────────────────────────────────────────────────\n")
	
	// Show first 500 characters
	content := item.Content
	if len(content) > 500 {
		content = content[:500] + "..."
	}
	fmt.Printf("%s\n", content)

	return nil
}

func displayContextWindow(window *appcontext.ContextWindow) error {
	fmt.Printf("🪟 Context Window\n")
	fmt.Printf("═══════════════════════════════════════════════════════════════\n")
	fmt.Printf("Items: %d\n", len(window.Items))
	fmt.Printf("Tokens: %d / %d (%.1f%% used)\n", 
		window.TotalTokens, window.MaxTokens, 
		float64(window.TotalTokens)/float64(window.MaxTokens)*100)
	fmt.Printf("Truncated: %t\n", window.Truncated)
	fmt.Printf("Strategy: %s\n", window.Strategy)

	if len(window.Items) > 0 {
		fmt.Printf("\nItems:\n")
		for i, item := range window.Items {
			fmt.Printf("  %d. %s (%d tokens)\n", i+1, item.FilePath, item.TokenCount)
		}
	}

	return nil
}

func displayStats(stats map[string]interface{}, format string) error {
	switch format {
	case "json":
		return outputJSON(stats)
	default:
		fmt.Printf("📊 Context Statistics\n")
		fmt.Printf("═══════════════════════════════════════════════════════════════\n")
		for key, value := range stats {
			fmt.Printf("%-20s: %v\n", key, value)
		}
		return nil
	}
}

func outputJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}