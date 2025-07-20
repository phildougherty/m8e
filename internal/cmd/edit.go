package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/phildougherty/m8e/internal/edit"
	"github.com/phildougherty/m8e/internal/treesitter"
	"github.com/phildougherty/m8e/internal/visual"
)

type editFlags struct {
	content        string
	diffContent    string
	previewOnly    bool
	autoApprove    bool
	diffMode       string
	backupDir      string
	showLineNumbers bool
	maxFileSize    int64
	interactive    bool
}

// NewEditCommand creates the edit command
func NewEditCommand() *cobra.Command {
	var editOpts editFlags

	editCmd := &cobra.Command{
		Use:   "edit [FILE_PATH]",
		Short: "Advanced file editing with visual diff and approval workflow",
		Long: `Edit files with enterprise-grade features including:
- Visual diff preview with multiple display modes
- Interactive approval workflow for changes
- Context-aware editing with @-mentions
- Tree-sitter code parsing and syntax highlighting
- Backup and rollback capabilities
- Git integration and change tracking

Examples:
  matey edit src/main.go                    # Edit file with interactive editor
  matey edit --content "new content" file.txt   # Replace file content directly
  matey edit --diff-content "SEARCH\nREPLACE" file.go  # Apply SEARCH/REPLACE diff
  matey edit --preview-only src/lib.go     # Show diff preview without applying
  matey edit --auto-approve config.yaml    # Auto-approve changes without interaction`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEditCommand(cmd, args, editOpts)
		},
	}

	editCmd.Flags().StringVar(&editOpts.content, "content", "", "New file content to replace existing content")
	editCmd.Flags().StringVar(&editOpts.diffContent, "diff-content", "", "SEARCH/REPLACE diff content to apply")
	editCmd.Flags().BoolVar(&editOpts.previewOnly, "preview-only", false, "Show diff preview without applying changes")
	editCmd.Flags().BoolVar(&editOpts.autoApprove, "auto-approve", false, "Auto-approve changes without interactive confirmation")
	editCmd.Flags().StringVar(&editOpts.diffMode, "diff-mode", "unified", "Diff display mode: unified, side-by-side, inline")
	editCmd.Flags().StringVar(&editOpts.backupDir, "backup-dir", "", "Directory for backup files (default: /tmp/matey-backups)")
	editCmd.Flags().BoolVar(&editOpts.showLineNumbers, "line-numbers", true, "Show line numbers in diff view")
	editCmd.Flags().Int64Var(&editOpts.maxFileSize, "max-file-size", 1048576, "Maximum file size in bytes (1MB default)")
	editCmd.Flags().BoolVar(&editOpts.interactive, "interactive", false, "Use interactive TUI editor mode")

	return editCmd
}

func runEditCommand(cmd *cobra.Command, args []string, editOpts editFlags) error {
	if len(args) == 0 {
		return fmt.Errorf("file path is required")
	}

	filePath := args[0]
	
	// Convert to absolute path
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to resolve file path: %w", err)
	}

	// Validate inputs
	if editOpts.content == "" && editOpts.diffContent == "" && !editOpts.interactive {
		return fmt.Errorf("one of --content, --diff-content, or --interactive must be specified")
	}

	if editOpts.content != "" && editOpts.diffContent != "" {
		return fmt.Errorf("cannot specify both --content and --diff-content")
	}

	// Set default backup directory
	backupDir := editOpts.backupDir
	if backupDir == "" {
		backupDir = "/tmp/matey-backups"
	}

	// Initialize components
	fileEditor, err := initializeFileEditor(backupDir)
	if err != nil {
		return fmt.Errorf("failed to initialize file editor: %w", err)
	}

	// Check if file exists and get current content
	var originalContent string
	if _, err := os.Stat(absPath); err == nil {
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		originalContent = string(content)
		
		// Check file size limit
		if int64(len(content)) > editOpts.maxFileSize {
			return fmt.Errorf("file size exceeds limit: %d bytes (max: %d)", len(content), editOpts.maxFileSize)
		}
	}

	// Handle interactive mode
	if editOpts.interactive {
		return runInteractiveEdit(absPath, originalContent, editOpts)
	}

	// Determine new content
	var newContent string
	if editOpts.content != "" {
		newContent = editOpts.content
	} else if editOpts.diffContent != "" {
		newContent, err = applyDiffContent(originalContent, editOpts.diffContent, fileEditor)
		if err != nil {
			return fmt.Errorf("failed to apply diff: %w", err)
		}
	}

	// Show diff preview
	if originalContent != newContent {
		err = showDiffPreview(absPath, originalContent, newContent, editOpts)
		if err != nil {
			return fmt.Errorf("failed to show diff preview: %w", err)
		}
	}

	// Apply changes if not preview-only
	if !editOpts.previewOnly {
		if !editOpts.autoApprove {
			approved, err := getUserApproval()
			if err != nil {
				return fmt.Errorf("failed to get user approval: %w", err)
			}
			if !approved {
				fmt.Println("Edit cancelled by user")
				return nil
			}
		}

		err = applyFileEdit(fileEditor, absPath, newContent)
		if err != nil {
			return fmt.Errorf("failed to apply edit: %w", err)
		}

		fmt.Printf("✅ Successfully edited file: %s\n", absPath)
	} else {
		fmt.Println("Preview mode - no changes applied")
	}

	return nil
}

func initializeFileEditor(backupDir string) (*edit.FileEditor, error) {
	config := edit.EditorConfig{
		BackupDir:     backupDir,
		MaxBackups:    10,
		K8sIntegrated: false, // CLI mode doesn't need K8s integration
	}

	return edit.NewFileEditor(config), nil
}

func runInteractiveEdit(filePath, originalContent string, editOpts editFlags) error {
	// Initialize file editor
	fileEditor, err := initializeFileEditor(editOpts.backupDir)
	if err != nil {
		return fmt.Errorf("failed to initialize file editor: %w", err)
	}

	// Initialize parser for interactive mode
	parser, err := treesitter.NewTreeSitterParser(treesitter.ParserConfig{
		CacheSize:     50,
		MaxFileSize:   editOpts.maxFileSize,
		EnableLogging: false,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize parser: %w", err)
	}

	// Detect language from file extension
	language := parser.DetectLanguage(filePath)

	// Configure editor view
	config := visual.EditorViewConfig{
		FilePath:        filePath,
		Language:        language,
		Mode:            visual.EditorModeEdit,
		ShowLineNumbers: editOpts.showLineNumbers,
		TabSize:         4,
		WordWrap:        false,
		SyntaxHighlight: true,
	}

	// Run interactive editor
	result, err := visual.RunEditorView(config, fileEditor, parser)
	if err != nil {
		return fmt.Errorf("interactive edit failed: %w", err)
	}

	// Handle result
	switch result.Action {
	case visual.EditorActionSave:
		fmt.Printf("✅ File saved: %s\n", filePath)
	case visual.EditorActionCancel:
		fmt.Println("Edit cancelled")
	default:
		fmt.Printf("Edit completed with action: %s\n", result.Action)
	}

	return nil
}

func applyDiffContent(originalContent, diffContent string, fileEditor *edit.FileEditor) (string, error) {
	// Parse SEARCH/REPLACE diff format
	diffProcessor := edit.NewDiffProcessor(originalContent)

	err := diffProcessor.ParseDiff(diffContent, true)
	if err != nil {
		return "", fmt.Errorf("failed to parse diff format: %w", err)
	}

	// Apply diff blocks
	result, err := diffProcessor.ApplyDiff()
	if err != nil {
		return "", fmt.Errorf("failed to apply diff: %w", err)
	}

	return result, nil
}

func showDiffPreview(filePath, originalContent, newContent string, editOpts editFlags) error {
	// Show diff preview
	if editOpts.previewOnly || !editOpts.autoApprove {
		preview := visual.ShowDiffPreview(originalContent, newContent, 50)
		fmt.Printf("\n📄 Diff Preview for %s:\n", filePath)
		fmt.Printf("═══════════════════════════════════════════════════════════════════════════════\n")
		fmt.Println(preview)
		fmt.Printf("═══════════════════════════════════════════════════════════════════════════════\n\n")
	}

	return nil
}

func getUserApproval() (bool, error) {
	fmt.Print("Apply these changes? [y/N]: ")
	var response string
	_, err := fmt.Scanln(&response)
	if err != nil {
		return false, err
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes", nil
}

func applyFileEdit(fileEditor *edit.FileEditor, filePath, newContent string) error {
	// Use WriteFile method to apply the edit
	err := fileEditor.WriteFile(filePath, newContent)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}