package chat

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNewChatCommand(t *testing.T) {
	cmd := NewChatCommand()

	// Test basic command properties
	if cmd == nil {
		t.Fatal("Expected NewChatCommand to return non-nil command")
	}

	if cmd.Use != "chat" {
		t.Errorf("Expected Use to be 'chat', got '%s'", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("Expected Short description to be non-empty")
	}

	if cmd.Long == "" {
		t.Error("Expected Long description to be non-empty")
	}

	if cmd.RunE == nil {
		t.Error("Expected RunE function to be set")
	}

	// Test that the command descriptions contain expected keywords
	expectedShortKeywords := []string{
		"interactive",
		"AI",
		"chat",
		"MCP",
	}

	for _, keyword := range expectedShortKeywords {
		if !strings.Contains(cmd.Short, keyword) {
			t.Errorf("Expected Short description to contain '%s'", keyword)
		}
	}

	// Test that the long description contains expected sections
	expectedLongSections := []string{
		"The chat interface provides",
		"The AI assistant specializes in",
		"Keyboard shortcuts",
		"Slash commands",
		"orchestration",
		"Kubernetes",
		"MANUAL",
		"AUTO",
		"YOLO",
		"/auto",
		"/yolo", 
		"/status",
		"/help",
		"Ctrl+C",
		"Ctrl+L",
		"ESC",
	}

	for _, section := range expectedLongSections {
		if !strings.Contains(cmd.Long, section) {
			t.Errorf("Expected Long description to contain '%s'", section)
		}
	}

	// Test that it's a valid cobra command structure
	if cmd.Parent() != nil {
		t.Error("Expected command to have no parent initially")
	}

	if cmd.HasSubCommands() {
		t.Error("Expected chat command to have no subcommands")
	}
}

func TestChatCommand_Structure(t *testing.T) {
	cmd := NewChatCommand()

	// Test command hierarchy
	if cmd.HasParent() {
		t.Error("Expected chat command to not have a parent when created")
	}

	// Test flags (should have none initially)
	if cmd.Flags().NFlag() != 0 {
		t.Errorf("Expected no flags, got %d", cmd.Flags().NFlag())
	}

	// Test persistent flags (should have none initially)
	if cmd.PersistentFlags().NFlag() != 0 {
		t.Errorf("Expected no persistent flags, got %d", cmd.PersistentFlags().NFlag())
	}

	// Test that it can be added as a subcommand
	rootCmd := &cobra.Command{
		Use: "matey",
	}
	rootCmd.AddCommand(cmd)

	if !cmd.HasParent() {
		t.Error("Expected command to have parent after being added")
	}

	if cmd.Parent() != rootCmd {
		t.Error("Expected parent to be the root command")
	}
}

func TestChatCommand_RunE(t *testing.T) {
	cmd := NewChatCommand()

	// Test that RunE function exists and doesn't panic when called
	// We can't easily test the full execution without mocking the entire TermChat system
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("RunE function panicked: %v", r)
		}
	}()

	// Test that the function signature is correct
	if cmd.RunE == nil {
		t.Fatal("RunE should not be nil")
	}

	// We can't easily test the actual execution because it starts an interactive session
	// and would require extensive mocking of the TermChat system, terminal, and AI providers
	// Instead, we verify the function is callable and has the right signature
	
	// The function should accept cmd and args parameters
	// This is implicitly tested by the cobra framework when we assign it
}

func TestChatCommand_Help(t *testing.T) {
	cmd := NewChatCommand()

	// Test help output contains expected elements
	helpOutput := cmd.Long

	// Check for feature descriptions
	featureKeywords := []string{
		"orchestration",
		"infrastructure", 
		"Kubernetes",
		"approval modes",
		"function calling",
		"Enhanced UI",
	}

	for _, keyword := range featureKeywords {
		if !strings.Contains(helpOutput, keyword) {
			t.Errorf("Expected help output to describe '%s' feature", keyword)
		}
	}

	// Check for specialization areas
	specializationKeywords := []string{
		"microservices",
		"monitoring",
		"CI/CD",
		"pipelines",
		"automation",
	}

	for _, keyword := range specializationKeywords {
		if !strings.Contains(helpOutput, keyword) {
			t.Errorf("Expected help output to mention '%s' specialization", keyword)
		}
	}

	// Check for keyboard shortcuts documentation
	shortcutKeywords := []string{
		"ESC",
		"Ctrl+C",
		"Ctrl+L",
		"Ctrl+Y",
		"Tab",
		"Ctrl+R",
		"Ctrl+V",
		"Alt+V",
		"Ctrl+T",
	}

	for _, shortcut := range shortcutKeywords {
		if !strings.Contains(helpOutput, shortcut) {
			t.Errorf("Expected help output to document '%s' keyboard shortcut", shortcut)
		}
	}

	// Check for slash commands documentation
	slashCommands := []string{
		"/auto",
		"/yolo", 
		"/status",
		"/help",
	}

	for _, command := range slashCommands {
		if !strings.Contains(helpOutput, command) {
			t.Errorf("Expected help output to document '%s' slash command", command)
		}
	}
}

func TestChatCommand_Integration(t *testing.T) {
	// Test that the command integrates properly with cobra
	rootCmd := &cobra.Command{
		Use: "matey",
	}

	chatCmd := NewChatCommand()
	rootCmd.AddCommand(chatCmd)

	// Test command can be found by name
	foundCmd, _, err := rootCmd.Find([]string{"chat"})
	if err != nil {
		t.Errorf("Error finding chat command: %v", err)
	}

	if foundCmd != chatCmd {
		t.Error("Found command is not the same as the chat command")
	}

	// Test command appears in help
	rootHelp := rootCmd.UsageString()
	if !strings.Contains(rootHelp, "chat") {
		t.Error("Expected root command help to contain 'chat'")
	}
}

func TestChatCommand_Validation(t *testing.T) {
	cmd := NewChatCommand()

	// Test command name validation
	if cmd.Use == "" {
		t.Error("Command Use should not be empty")
	}

	// Test that Use doesn't contain spaces (cobra requirement)
	if strings.Contains(cmd.Use, " ") {
		t.Error("Command Use should not contain spaces")
	}

	// Test that Short is appropriate length (not too long)
	if len(cmd.Short) > 100 {
		t.Error("Short description should be concise (under 100 characters)")
	}

	// Test that Short doesn't end with period (cobra convention)
	if strings.HasSuffix(cmd.Short, ".") {
		t.Error("Short description should not end with period")
	}

	// Test that Long description is substantial
	if len(cmd.Long) < 200 {
		t.Error("Long description should be comprehensive (over 200 characters)")
	}
}