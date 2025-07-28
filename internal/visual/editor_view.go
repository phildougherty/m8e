package visual

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/phildougherty/m8e/internal/edit"
	"github.com/phildougherty/m8e/internal/treesitter"
)

// EditorMode represents different editor modes
type EditorMode string

const (
	EditorModeEdit     EditorMode = "edit"
	EditorModePreview  EditorMode = "preview"
	EditorModeSplit    EditorMode = "split"
	EditorModeDiff     EditorMode = "diff"
)

// EditorAction represents user actions in the editor
type EditorAction string

const (
	EditorActionSave    EditorAction = "save"
	EditorActionCancel  EditorAction = "cancel"
	EditorActionPreview EditorAction = "preview"
	EditorActionFormat  EditorAction = "format"
	EditorActionUndo    EditorAction = "undo"
	EditorActionRedo    EditorAction = "redo"
	EditorActionFind    EditorAction = "find"
	EditorActionReplace EditorAction = "replace"
	EditorActionQuit    EditorAction = "quit"
)

// EditorViewConfig configures the editor behavior
type EditorViewConfig struct {
	FilePath        string                 `json:"file_path"`
	Language        treesitter.Language    `json:"language"`
	Mode            EditorMode             `json:"mode"`
	ShowLineNumbers bool                   `json:"show_line_numbers"`
	TabSize         int                    `json:"tab_size"`
	WordWrap        bool                   `json:"word_wrap"`
	AutoSave        bool                   `json:"auto_save"`
	AutoFormat      bool                   `json:"auto_format"`
	SyntaxHighlight bool                   `json:"syntax_highlight"`
	LivePreview     bool                   `json:"live_preview"`
	VimMode         bool                   `json:"vim_mode"`
	Theme           string                 `json:"theme"`
	MaxFileSize     int64                  `json:"max_file_size"`
}

// EditorView represents an interactive code editor component
type EditorView struct {
	config         EditorViewConfig
	mode           EditorMode
	originalContent string
	currentContent  string
	filePath       string
	language       treesitter.Language
	textarea       textarea.Model
	preview        viewport.Model
	diffView       *DiffView
	keyMap         EditorKeyMap
	action         EditorAction
	saved          bool
	modified       bool
	width          int
	height         int
	styles         EditorStyles
	fileEditor     *edit.FileEditor
	parser         *treesitter.TreeSitterParser
	undoStack      []string
	redoStack      []string
	maxUndoSize    int
	error          string
	status         string
	cursorLine     int
	cursorCol      int
}

// EditorKeyMap defines keyboard shortcuts for the editor
type EditorKeyMap struct {
	Save        key.Binding
	Cancel      key.Binding
	Preview     key.Binding
	Format      key.Binding
	Undo        key.Binding
	Redo        key.Binding
	Find        key.Binding
	Replace     key.Binding
	SwitchMode  key.Binding
	ToggleWrap  key.Binding
	LineNumbers key.Binding
	Quit        key.Binding
}

// EditorStyles defines styling for editor components
type EditorStyles struct {
	Base        lipgloss.Style
	Header      lipgloss.Style
	Footer      lipgloss.Style
	Editor      lipgloss.Style
	Preview     lipgloss.Style
	LineNumber  lipgloss.Style
	Cursor      lipgloss.Style
	Selection   lipgloss.Style
	Status      lipgloss.Style
	Error       lipgloss.Style
	Success     lipgloss.Style
	Modified    lipgloss.Style
	Border      lipgloss.Style
}

// NewEditorView creates a new editor instance
func NewEditorView(config EditorViewConfig, fileEditor *edit.FileEditor, parser *treesitter.TreeSitterParser) *EditorView {
	if config.TabSize == 0 {
		config.TabSize = 4
	}
	if config.MaxFileSize == 0 {
		config.MaxFileSize = 1024 * 1024 // 1MB
	}

	keyMap := EditorKeyMap{
		Save:        key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
		Cancel:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Preview:     key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "preview")),
		Format:      key.NewBinding(key.WithKeys("ctrl+shift+f"), key.WithHelp("ctrl+shift+f", "format")),
		Undo:        key.NewBinding(key.WithKeys("ctrl+z"), key.WithHelp("ctrl+z", "undo")),
		Redo:        key.NewBinding(key.WithKeys("ctrl+y", "ctrl+shift+z"), key.WithHelp("ctrl+y", "redo")),
		Find:        key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "find")),
		Replace:     key.NewBinding(key.WithKeys("ctrl+h"), key.WithHelp("ctrl+h", "replace")),
		SwitchMode:  key.NewBinding(key.WithKeys("ctrl+m"), key.WithHelp("ctrl+m", "mode")),
		ToggleWrap:  key.NewBinding(key.WithKeys("ctrl+w"), key.WithHelp("ctrl+w", "wrap")),
		LineNumbers: key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "line numbers")),
		Quit:        key.NewBinding(key.WithKeys("ctrl+q"), key.WithHelp("ctrl+q", "quit")),
	}

	styles := createEditorStyles(config.Theme)

	// Initialize textarea
	ta := textarea.New()
	ta.Placeholder = "Start typing..."
	ta.ShowLineNumbers = config.ShowLineNumbers
	ta.CharLimit = 0 // No limit
	
	// Configure textarea based on settings
	if config.VimMode {
		ta.KeyMap = textarea.KeyMap{} // Custom vim keymap would go here
	}

	// Initialize preview viewport
	preview := viewport.New(0, 0)
	preview.Style = styles.Preview

	ev := &EditorView{
		config:      config,
		mode:        config.Mode,
		filePath:    config.FilePath,
		language:    config.Language,
		textarea:    ta,
		preview:     preview,
		keyMap:      keyMap,
		styles:      styles,
		fileEditor:  fileEditor,
		parser:      parser,
		undoStack:   make([]string, 0),
		redoStack:   make([]string, 0),
		maxUndoSize: 100,
	}

	return ev
}

// LoadFile loads content from a file
func (ev *EditorView) LoadFile(filePath string) error {
	if ev.fileEditor == nil {
		return fmt.Errorf("file editor not available")
	}

	content, err := ev.fileEditor.ReadFile(filePath)
	if err != nil {
		ev.error = err.Error()
		return err
	}

	// Check file size
	if int64(len(content)) > ev.config.MaxFileSize {
		return fmt.Errorf("file too large: %d bytes (max: %d)", len(content), ev.config.MaxFileSize)
	}

	ev.originalContent = content
	ev.currentContent = content
	ev.filePath = filePath
	ev.modified = false

	// Detect language if not set
	if ev.language == treesitter.LanguageUnknown && ev.parser != nil {
		ev.language = ev.parser.DetectLanguage(filePath)
		ev.config.Language = ev.language
	}

	// Set textarea content
	ev.textarea.SetValue(content)

	// Initialize undo stack
	ev.undoStack = []string{content}
	ev.redoStack = []string{}

	// Update preview if enabled
	if ev.config.LivePreview {
		ev.updatePreview()
	}

	ev.status = fmt.Sprintf("Loaded %s (%d lines)", filePath, strings.Count(content, "\n")+1)
	
	return nil
}

// SaveFile saves the current content to file
func (ev *EditorView) SaveFile() error {
	if ev.fileEditor == nil {
		return fmt.Errorf("file editor not available")
	}

	content := ev.textarea.Value()

	// Apply formatting if enabled
	if ev.config.AutoFormat {
		content = ev.formatContent(content)
		ev.textarea.SetValue(content)
	}

	// Validate content if we have validation
	if err := ev.validateContent(content); err != nil {
		ev.error = err.Error()
		return err
	}

	// Save to file
	if err := ev.fileEditor.WriteFile(ev.filePath, content); err != nil {
		ev.error = err.Error()
		return err
	}

	ev.originalContent = content
	ev.currentContent = content
	ev.modified = false
	ev.saved = true

	ev.status = fmt.Sprintf("Saved %s", ev.filePath)
	ev.error = ""

	return nil
}

// SetContent sets the editor content directly
func (ev *EditorView) SetContent(content string) {
	ev.currentContent = content
	ev.textarea.SetValue(content)
	ev.modified = true

	// Add to undo stack
	ev.addToUndoStack(content)

	// Update preview if enabled
	if ev.config.LivePreview {
		ev.updatePreview()
	}
}

// GetContent returns the current editor content
func (ev *EditorView) GetContent() string {
	return ev.textarea.Value()
}

// Resize updates the editor dimensions
// Init initializes the editor view model
func (ev *EditorView) Init() tea.Cmd {
	return textarea.Blink
}

func (ev *EditorView) Resize(width, height int) {
	ev.width = width
	ev.height = height

	// Calculate layout dimensions
	headerHeight := 2
	footerHeight := 2
	contentHeight := height - headerHeight - footerHeight

	switch ev.mode {
	case EditorModeSplit:
		// Split view: editor on left, preview on right
		editorWidth := width / 2
		previewWidth := width - editorWidth - 1 // -1 for border
		
		ev.textarea.SetWidth(editorWidth)
		ev.textarea.SetHeight(contentHeight)
		ev.preview.Width = previewWidth
		ev.preview.Height = contentHeight
		
	case EditorModePreview:
		// Full preview
		ev.preview.Width = width
		ev.preview.Height = contentHeight
		
	default: // EditorModeEdit, EditorModeDiff
		// Full editor
		ev.textarea.SetWidth(width)
		ev.textarea.SetHeight(contentHeight)
	}
}

// Update handles tea.Msg updates
func (ev *EditorView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, ev.keyMap.Save):
			if err := ev.SaveFile(); err != nil {
				ev.error = err.Error()
			} else {
				ev.action = EditorActionSave
			}
			return ev, nil
			
		case key.Matches(msg, ev.keyMap.Cancel):
			if ev.modified {
				// TODO: Show confirmation dialog
				ev.action = EditorActionCancel
				return ev, tea.Quit
			}
			ev.action = EditorActionCancel
			return ev, tea.Quit
			
		case key.Matches(msg, ev.keyMap.Preview):
			ev.mode = EditorModePreview
			ev.updatePreview()
			ev.Resize(ev.width, ev.height) // Recalculate layout
			
		case key.Matches(msg, ev.keyMap.Format):
			content := ev.formatContent(ev.textarea.Value())
			ev.SetContent(content)
			ev.action = EditorActionFormat
			
		case key.Matches(msg, ev.keyMap.Undo):
			ev.undo()
			ev.action = EditorActionUndo
			
		case key.Matches(msg, ev.keyMap.Redo):
			ev.redo()
			ev.action = EditorActionRedo
			
		case key.Matches(msg, ev.keyMap.SwitchMode):
			ev.switchMode()
			ev.Resize(ev.width, ev.height) // Recalculate layout
			
		case key.Matches(msg, ev.keyMap.ToggleWrap):
			ev.config.WordWrap = !ev.config.WordWrap
			// Apply word wrap setting to textarea
			
		case key.Matches(msg, ev.keyMap.LineNumbers):
			ev.config.ShowLineNumbers = !ev.config.ShowLineNumbers
			ev.textarea.ShowLineNumbers = ev.config.ShowLineNumbers
			
		case key.Matches(msg, ev.keyMap.Quit):
			ev.action = EditorActionQuit
			return ev, tea.Quit
			
		default:
			// Pass to textarea for normal editing
			if ev.mode == EditorModeEdit || ev.mode == EditorModeSplit {
				ev.textarea, cmd = ev.textarea.Update(msg)
				cmds = append(cmds, cmd)
				
				// Check if content changed
				newContent := ev.textarea.Value()
				if newContent != ev.currentContent {
					ev.currentContent = newContent
					ev.modified = (newContent != ev.originalContent)
					
					// Update live preview
					if ev.config.LivePreview && (ev.mode == EditorModeSplit || ev.mode == EditorModePreview) {
						ev.updatePreview()
					}
					
					// Auto-save if enabled
					if ev.config.AutoSave && ev.modified {
						go func() {
							time.Sleep(2 * time.Second) // Debounce
							if ev.textarea.Value() == newContent { // Content hasn't changed
								ev.SaveFile()
							}
						}()
					}
				}
				
				// Update cursor position
				ev.updateCursorPosition()
			} else {
				// Pass to preview viewport for navigation
				ev.preview, cmd = ev.preview.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
		
	case tea.WindowSizeMsg:
		ev.Resize(msg.Width, msg.Height)
	}

	return ev, tea.Batch(cmds...)
}

// View renders the editor view
func (ev *EditorView) View() string {
	var b strings.Builder

	// Header
	b.WriteString(ev.renderHeader())
	b.WriteString("\n")

	// Main content
	switch ev.mode {
	case EditorModeEdit:
		b.WriteString(ev.textarea.View())
	case EditorModePreview:
		b.WriteString(ev.preview.View())
	case EditorModeSplit:
		b.WriteString(ev.renderSplitView())
	case EditorModeDiff:
		if ev.diffView != nil {
			b.WriteString(ev.diffView.View())
		} else {
			b.WriteString(ev.textarea.View())
		}
	default:
		b.WriteString(ev.textarea.View())
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(ev.renderFooter())

	return ev.styles.Base.Render(b.String())
}

// GetAction returns the last user action
func (ev *EditorView) GetAction() EditorAction {
	return ev.action
}

// IsModified returns whether the content has been modified
func (ev *EditorView) IsModified() bool {
	return ev.modified
}

// IsSaved returns whether the content has been saved
func (ev *EditorView) IsSaved() bool {
	return ev.saved
}

// Private methods

func (ev *EditorView) updatePreview() {
	content := ev.textarea.Value()
	
	// Generate preview based on language
	preview := ev.generatePreview(content)
	ev.preview.SetContent(preview)
}

func (ev *EditorView) generatePreview(content string) string {
	var preview strings.Builder
	
	preview.WriteString(fmt.Sprintf("Preview of %s\n", ev.filePath))
	preview.WriteString(strings.Repeat("=", 50))
	preview.WriteString("\n\n")
	
	// Language-specific preview
	switch ev.language {
	case treesitter.LanguageGo:
		preview.WriteString(ev.generateGoPreview(content))
	case treesitter.LanguagePython:
		preview.WriteString(ev.generatePythonPreview(content))
	case treesitter.LanguageJavaScript:
		preview.WriteString(ev.generateJavaScriptPreview(content))
	default:
		// Generic preview with line numbers
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i >= 50 { // Limit preview
				preview.WriteString("... (truncated)")
				break
			}
			preview.WriteString(fmt.Sprintf("%3d: %s\n", i+1, line))
		}
	}
	
	return preview.String()
}

func (ev *EditorView) generateGoPreview(content string) string {
	// TODO: Use tree-sitter to parse and show structure
	var preview strings.Builder
	preview.WriteString("Go Code Structure:\n\n")
	
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") ||
		   strings.HasPrefix(trimmed, "import ") ||
		   strings.HasPrefix(trimmed, "func ") ||
		   strings.HasPrefix(trimmed, "type ") ||
		   strings.HasPrefix(trimmed, "var ") ||
		   strings.HasPrefix(trimmed, "const ") {
			preview.WriteString(fmt.Sprintf("L%d: %s\n", i+1, trimmed))
		}
	}
	
	return preview.String()
}

func (ev *EditorView) generatePythonPreview(content string) string {
	var preview strings.Builder
	preview.WriteString("Python Code Structure:\n\n")
	
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "import ") ||
		   strings.HasPrefix(trimmed, "from ") ||
		   strings.HasPrefix(trimmed, "def ") ||
		   strings.HasPrefix(trimmed, "class ") {
			preview.WriteString(fmt.Sprintf("L%d: %s\n", i+1, trimmed))
		}
	}
	
	return preview.String()
}

func (ev *EditorView) generateJavaScriptPreview(content string) string {
	var preview strings.Builder
	preview.WriteString("JavaScript Code Structure:\n\n")
	
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "import ") ||
		   strings.HasPrefix(trimmed, "export ") ||
		   strings.HasPrefix(trimmed, "function ") ||
		   strings.HasPrefix(trimmed, "class ") ||
		   strings.HasPrefix(trimmed, "const ") ||
		   strings.HasPrefix(trimmed, "let ") ||
		   strings.HasPrefix(trimmed, "var ") {
			preview.WriteString(fmt.Sprintf("L%d: %s\n", i+1, trimmed))
		}
	}
	
	return preview.String()
}

func (ev *EditorView) formatContent(content string) string {
	// Basic formatting - in practice, this would integrate with language-specific formatters
	switch ev.language {
	case treesitter.LanguageGo:
		return ev.formatGoCode(content)
	case treesitter.LanguagePython:
		return ev.formatPythonCode(content)
	case treesitter.LanguageJavaScript:
		return ev.formatJavaScriptCode(content)
	default:
		return content
	}
}

func (ev *EditorView) formatGoCode(content string) string {
	// TODO: Integration with gofmt
	return content
}

func (ev *EditorView) formatPythonCode(content string) string {
	// TODO: Integration with black or autopep8
	return content
}

func (ev *EditorView) formatJavaScriptCode(content string) string {
	// TODO: Integration with prettier
	return content
}

func (ev *EditorView) validateContent(content string) error {
	// Basic validation - could be enhanced with language-specific linting
	if ev.language == treesitter.LanguageGo {
		if !strings.Contains(content, "package ") {
			return fmt.Errorf("Go file must contain a package declaration")
		}
	}
	
	return nil
}

func (ev *EditorView) addToUndoStack(content string) {
	ev.undoStack = append(ev.undoStack, content)
	
	// Limit undo stack size
	if len(ev.undoStack) > ev.maxUndoSize {
		ev.undoStack = ev.undoStack[1:]
	}
	
	// Clear redo stack when new content is added
	ev.redoStack = []string{}
}

func (ev *EditorView) undo() {
	if len(ev.undoStack) <= 1 {
		return // Nothing to undo
	}
	
	// Move current state to redo stack
	current := ev.undoStack[len(ev.undoStack)-1]
	ev.redoStack = append(ev.redoStack, current)
	
	// Remove from undo stack
	ev.undoStack = ev.undoStack[:len(ev.undoStack)-1]
	
	// Apply previous state
	previous := ev.undoStack[len(ev.undoStack)-1]
	ev.textarea.SetValue(previous)
	ev.currentContent = previous
	ev.modified = (previous != ev.originalContent)
	
	if ev.config.LivePreview {
		ev.updatePreview()
	}
}

func (ev *EditorView) redo() {
	if len(ev.redoStack) == 0 {
		return // Nothing to redo
	}
	
	// Get state from redo stack
	next := ev.redoStack[len(ev.redoStack)-1]
	ev.redoStack = ev.redoStack[:len(ev.redoStack)-1]
	
	// Add current state back to undo stack
	ev.undoStack = append(ev.undoStack, next)
	
	// Apply next state
	ev.textarea.SetValue(next)
	ev.currentContent = next
	ev.modified = (next != ev.originalContent)
	
	if ev.config.LivePreview {
		ev.updatePreview()
	}
}

func (ev *EditorView) switchMode() {
	switch ev.mode {
	case EditorModeEdit:
		ev.mode = EditorModePreview
		ev.updatePreview()
	case EditorModePreview:
		ev.mode = EditorModeSplit
		ev.updatePreview()
	case EditorModeSplit:
		ev.mode = EditorModeEdit
	case EditorModeDiff:
		ev.mode = EditorModeEdit
	default:
		ev.mode = EditorModeEdit
	}
	
	ev.config.Mode = ev.mode
}

func (ev *EditorView) updateCursorPosition() {
	// Get cursor position from textarea
	// This is a simplified implementation
	content := ev.textarea.Value()
	cursor := len(content) // Simplified - get actual cursor position
	
	// Calculate line and column
	lines := strings.Split(content[:cursor], "\n")
	ev.cursorLine = len(lines)
	if len(lines) > 0 {
		ev.cursorCol = len(lines[len(lines)-1]) + 1
	} else {
		ev.cursorCol = 1
	}
}

func (ev *EditorView) renderHeader() string {
	var header strings.Builder
	
	// File path and modified indicator
	pathDisplay := ev.filePath
	if ev.modified {
		pathDisplay += " *"
		pathDisplay = ev.styles.Modified.Render(pathDisplay)
	} else {
		pathDisplay = ev.styles.Header.Render(pathDisplay)
	}
	header.WriteString("📝 ")
	header.WriteString(pathDisplay)
	
	// Language indicator
	if ev.language != treesitter.LanguageUnknown {
		langStr := fmt.Sprintf("[%s]", strings.ToUpper(string(ev.language)))
		header.WriteString(" ")
		header.WriteString(ev.styles.Header.Render(langStr))
	}
	
	// Mode indicator
	modeStr := fmt.Sprintf("[%s]", strings.ToUpper(string(ev.mode)))
	header.WriteString(" ")
	header.WriteString(ev.styles.Header.Render(modeStr))
	
	// Cursor position
	if ev.mode == EditorModeEdit || ev.mode == EditorModeSplit {
		posStr := fmt.Sprintf("L%d:C%d", ev.cursorLine, ev.cursorCol)
		header.WriteString(" ")
		header.WriteString(ev.styles.Header.Render(posStr))
	}
	
	return header.String()
}

func (ev *EditorView) renderFooter() string {
	var footer strings.Builder
	
	// Status message
	if ev.error != "" {
		footer.WriteString(ev.styles.Error.Render("Error: " + ev.error))
	} else if ev.status != "" {
		footer.WriteString(ev.styles.Status.Render(ev.status))
	}
	
	footer.WriteString(" • ")
	
	// Key bindings
	bindings := []string{
		ev.keyMap.Save.Help().Key + " save",
		ev.keyMap.Preview.Help().Key + " preview",
		ev.keyMap.SwitchMode.Help().Key + " mode",
		ev.keyMap.Undo.Help().Key + " undo",
		ev.keyMap.Quit.Help().Key + " quit",
	}
	
	footer.WriteString(ev.styles.Footer.Render(strings.Join(bindings, " • ")))
	
	return footer.String()
}

func (ev *EditorView) renderSplitView() string {
	editorView := ev.textarea.View()
	previewView := ev.preview.View()
	
	// Split horizontally
	editorLines := strings.Split(editorView, "\n")
	previewLines := strings.Split(previewView, "\n")
	
	maxLines := len(editorLines)
	if len(previewLines) > maxLines {
		maxLines = len(previewLines)
	}
	
	var result strings.Builder
	editorWidth := ev.width / 2
	
	for i := 0; i < maxLines; i++ {
		var editorLine, previewLine string
		
		if i < len(editorLines) {
			editorLine = editorLines[i]
		}
		if i < len(previewLines) {
			previewLine = previewLines[i]
		}
		
		// Truncate editor line to fit
		if len(editorLine) > editorWidth {
			editorLine = editorLine[:editorWidth-1]
		}
		
		line := fmt.Sprintf("%-*s │ %s", editorWidth, editorLine, previewLine)
		result.WriteString(line)
		if i < maxLines-1 {
			result.WriteString("\n")
		}
	}
	
	return result.String()
}

func createEditorStyles(theme string) EditorStyles {
	// Define color schemes
	var (
		primaryColor   = lipgloss.Color("#3b82f6")   // Blue
		successColor   = lipgloss.Color("#10b981")   // Green
		errorColor     = lipgloss.Color("#ef4444")   // Red
		_ = lipgloss.Color("#f59e0b")   // Yellow (warning color, unused for now)
		mutedColor     = lipgloss.Color("#6b7280")   // Gray
		modifiedColor  = lipgloss.Color("#8b5cf6")   // Purple
	)
	
	// Adjust colors for dark theme
	if theme == "dark" {
		primaryColor = lipgloss.Color("#60a5fa")
		successColor = lipgloss.Color("#34d399")
		errorColor = lipgloss.Color("#f87171")
		modifiedColor = lipgloss.Color("#a78bfa")
	}
	
	return EditorStyles{
		Base: lipgloss.NewStyle().
			Padding(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#374151")),
			
		Header: lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true),
			
		Footer: lipgloss.NewStyle().
			Foreground(mutedColor).
			Italic(true),
			
		Editor: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#d1d5db")).
			Padding(1),
			
		Preview: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#d1d5db")).
			Padding(1),
			
		LineNumber: lipgloss.NewStyle().
			Foreground(mutedColor).
			Width(4).
			Align(lipgloss.Right),
			
		Cursor: lipgloss.NewStyle().
			Background(primaryColor).
			Foreground(lipgloss.Color("#ffffff")),
			
		Selection: lipgloss.NewStyle().
			Background(lipgloss.Color("#dbeafe")).
			Foreground(lipgloss.Color("#1e40af")),
			
		Status: lipgloss.NewStyle().
			Foreground(mutedColor),
			
		Error: lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true),
			
		Success: lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true),
			
		Modified: lipgloss.NewStyle().
			Foreground(modifiedColor).
			Bold(true),
			
		Border: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#d1d5db")),
	}
}

// EditorViewResult represents the result of an editor session
type EditorViewResult struct {
	Action   EditorAction  `json:"action"`
	Content  string        `json:"content,omitempty"`
	FilePath string        `json:"file_path"`
	Saved    bool          `json:"saved"`
	Modified bool          `json:"modified"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

// RunEditorView runs an interactive editor session
func RunEditorView(config EditorViewConfig, fileEditor *edit.FileEditor, parser *treesitter.TreeSitterParser) (*EditorViewResult, error) {
	startTime := time.Now()
	
	editorView := NewEditorView(config, fileEditor, parser)
	
	// Load file if specified
	if config.FilePath != "" {
		if err := editorView.LoadFile(config.FilePath); err != nil {
			return &EditorViewResult{
				Action:   EditorActionCancel,
				FilePath: config.FilePath,
				Duration: time.Since(startTime),
				Error:    err.Error(),
			}, err
		}
	}
	
	program := tea.NewProgram(editorView, tea.WithAltScreen())
	
	if _, err := program.Run(); err != nil {
		return &EditorViewResult{
			Action:   EditorActionCancel,
			Content:  editorView.GetContent(),
			FilePath: editorView.filePath,
			Saved:    editorView.IsSaved(),
			Modified: editorView.IsModified(),
			Duration: time.Since(startTime),
			Error:    err.Error(),
		}, err
	}
	
	return &EditorViewResult{
		Action:   editorView.GetAction(),
		Content:  editorView.GetContent(),
		FilePath: editorView.filePath,
		Saved:    editorView.IsSaved(),
		Modified: editorView.IsModified(),
		Duration: time.Since(startTime),
	}, nil
}