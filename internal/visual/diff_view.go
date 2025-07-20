package visual

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/phildougherty/m8e/internal/edit"
)

// DiffMode represents different diff display modes
type DiffMode string

const (
	DiffModeUnified    DiffMode = "unified"
	DiffModeSideBySide DiffMode = "side-by-side"
	DiffModeInline     DiffMode = "inline"
)

// DiffViewAction represents user actions in the diff view
type DiffViewAction string

const (
	ActionApprove DiffViewAction = "approve"
	ActionReject  DiffViewAction = "reject"
	ActionEdit    DiffViewAction = "edit"
	ActionSave    DiffViewAction = "save"
	ActionCancel  DiffViewAction = "cancel"
)

// DiffViewConfig configures the diff view behavior
type DiffViewConfig struct {
	Mode            DiffMode `json:"mode"`
	ShowLineNumbers bool     `json:"show_line_numbers"`
	TabSize         int      `json:"tab_size"`
	ContextLines    int      `json:"context_lines"`
	EnableSyntax    bool     `json:"enable_syntax"`
	AutoApprove     bool     `json:"auto_approve"`
	Theme           string   `json:"theme"`
}

// DiffView represents a visual diff display component
type DiffView struct {
	config       DiffViewConfig
	original     string
	modified     string
	filePath     string
	diff         []diffmatchpatch.Diff
	viewport     viewport.Model
	keyMap       DiffKeyMap
	action       DiffViewAction
	approved     bool
	width        int
	height       int
	styles       DiffStyles
}

// DiffKeyMap defines keyboard shortcuts for diff view
type DiffKeyMap struct {
	Approve key.Binding
	Reject  key.Binding
	Edit    key.Binding
	Save    key.Binding
	Cancel  key.Binding
	Up      key.Binding
	Down    key.Binding
	PageUp  key.Binding
	PageDown key.Binding
	Home    key.Binding
	End     key.Binding
	Help    key.Binding
	Quit    key.Binding
}

// DiffStyles defines styling for diff view components
type DiffStyles struct {
	Base       lipgloss.Style
	Header     lipgloss.Style
	Footer     lipgloss.Style
	LineNumber lipgloss.Style
	Added      lipgloss.Style
	Removed    lipgloss.Style
	Modified   lipgloss.Style
	Context    lipgloss.Style
	Highlight  lipgloss.Style
	Error      lipgloss.Style
	Success    lipgloss.Style
}

// NewDiffView creates a new diff view instance
func NewDiffView(config DiffViewConfig) *DiffView {
	if config.TabSize == 0 {
		config.TabSize = 4
	}
	if config.ContextLines == 0 {
		config.ContextLines = 3
	}

	keyMap := DiffKeyMap{
		Approve:  key.NewBinding(key.WithKeys("a", "y"), key.WithHelp("a/y", "approve")),
		Reject:   key.NewBinding(key.WithKeys("r", "n"), key.WithHelp("r/n", "reject")),
		Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Save:     key.NewBinding(key.WithKeys("s", "ctrl+s"), key.WithHelp("s", "save")),
		Cancel:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("pgup/b", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "f"), key.WithHelp("pgdn/f", "page down")),
		Home:     key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("home/g", "top")),
		End:      key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("end/G", "bottom")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}

	styles := createDiffStyles(config.Theme)

	vp := viewport.New(80, 24)
	vp.Style = styles.Base

	return &DiffView{
		config:   config,
		viewport: vp,
		keyMap:   keyMap,
		styles:   styles,
	}
}

// SetContent sets the original and modified content for diff
func (dv *DiffView) SetContent(original, modified, filePath string) {
	dv.original = original
	dv.modified = modified
	dv.filePath = filePath

	// Generate diff
	dmp := diffmatchpatch.New()
	dv.diff = dmp.DiffMain(original, modified, false)
	dv.diff = dmp.DiffCleanupSemantic(dv.diff)

	// Update viewport content
	dv.updateViewportContent()
}

// SetDiff sets the diff content directly from edit operations
func (dv *DiffView) SetDiff(diffBlocks []edit.DiffFormat, filePath string) {
	dv.filePath = filePath

	// Convert DiffFormat to display format
	var content strings.Builder
	
	for i, block := range diffBlocks {
		if i > 0 {
			content.WriteString("\n" + strings.Repeat("-", 60) + "\n")
		}
		
		content.WriteString(fmt.Sprintf("--- SEARCH (Block %d) ---\n", i+1))
		content.WriteString(block.SearchContent)
		content.WriteString("\n=======\n")
		content.WriteString(fmt.Sprintf("+++ REPLACE (Block %d) +++\n", i+1))
		content.WriteString(block.ReplaceContent)
		content.WriteString("\n")
	}

	dv.viewport.SetContent(dv.renderContent(content.String()))
}

// Init initializes the diff view model
func (dv *DiffView) Init() tea.Cmd {
	return nil
}

// Resize updates the diff view dimensions
func (dv *DiffView) Resize(width, height int) {
	dv.width = width
	dv.height = height
	
	// Reserve space for header and footer
	contentHeight := height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}
	
	dv.viewport.Width = width
	dv.viewport.Height = contentHeight
	
	// Update content after resize
	dv.updateViewportContent()
}

// Update handles tea.Msg updates
func (dv *DiffView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, dv.keyMap.Approve):
			dv.action = ActionApprove
			dv.approved = true
			return dv, tea.Quit
			
		case key.Matches(msg, dv.keyMap.Reject):
			dv.action = ActionReject
			dv.approved = false
			return dv, tea.Quit
			
		case key.Matches(msg, dv.keyMap.Edit):
			dv.action = ActionEdit
			return dv, tea.Quit
			
		case key.Matches(msg, dv.keyMap.Save):
			dv.action = ActionSave
			return dv, tea.Quit
			
		case key.Matches(msg, dv.keyMap.Cancel):
			dv.action = ActionCancel
			return dv, tea.Quit
			
		case key.Matches(msg, dv.keyMap.Quit):
			return dv, tea.Quit
			
		default:
			// Pass other keys to viewport for navigation
			dv.viewport, cmd = dv.viewport.Update(msg)
		}
		
	case tea.WindowSizeMsg:
		dv.Resize(msg.Width, msg.Height)
	}

	return dv, cmd
}

// View renders the diff view
func (dv *DiffView) View() string {
	var b strings.Builder

	// Header
	header := dv.renderHeader()
	b.WriteString(header)
	b.WriteString("\n")

	// Main content
	b.WriteString(dv.viewport.View())

	// Footer
	footer := dv.renderFooter()
	b.WriteString("\n")
	b.WriteString(footer)

	return dv.styles.Base.Render(b.String())
}

// GetAction returns the last user action
func (dv *DiffView) GetAction() DiffViewAction {
	return dv.action
}

// IsApproved returns whether the diff was approved
func (dv *DiffView) IsApproved() bool {
	return dv.approved
}

// Private methods

func (dv *DiffView) updateViewportContent() {
	switch dv.config.Mode {
	case DiffModeSideBySide:
		content := dv.renderSideBySide()
		dv.viewport.SetContent(content)
	case DiffModeInline:
		content := dv.renderInline()
		dv.viewport.SetContent(content)
	default: // Unified
		content := dv.renderUnified()
		dv.viewport.SetContent(content)
	}
}

func (dv *DiffView) renderHeader() string {
	var header strings.Builder
	
	// File path
	header.WriteString(dv.styles.Header.Render(fmt.Sprintf("📄 %s", dv.filePath)))
	
	// Mode indicator
	modeStr := fmt.Sprintf("[%s]", strings.ToUpper(string(dv.config.Mode)))
	header.WriteString(" ")
	header.WriteString(dv.styles.Header.Render(modeStr))
	
	// Stats
	if len(dv.diff) > 0 {
		adds, dels := dv.countChanges()
		stats := fmt.Sprintf("(+%d -%d)", adds, dels)
		header.WriteString(" ")
		header.WriteString(dv.styles.Header.Render(stats))
	}
	
	return header.String()
}

func (dv *DiffView) renderFooter() string {
	var footer strings.Builder
	
	// Key bindings
	bindings := []string{
		dv.keyMap.Approve.Help().Key + " approve",
		dv.keyMap.Reject.Help().Key + " reject",
		dv.keyMap.Edit.Help().Key + " edit",
		dv.keyMap.Help.Help().Key + " help",
		dv.keyMap.Quit.Help().Key + " quit",
	}
	
	footer.WriteString(dv.styles.Footer.Render(strings.Join(bindings, " • ")))
	
	return footer.String()
}

func (dv *DiffView) renderUnified() string {
	if len(dv.diff) == 0 {
		return dv.styles.Context.Render("No changes detected")
	}

	var content strings.Builder
	lineNum := 1
	
	for _, d := range dv.diff {
		lines := strings.Split(d.Text, "\n")
		
		for i, line := range lines {
			if i == len(lines)-1 && line == "" {
				continue // Skip empty last line
			}
			
			var lineContent string
			if dv.config.ShowLineNumbers {
				lineNumStr := dv.styles.LineNumber.Render(fmt.Sprintf("%4d ", lineNum))
				lineContent = lineNumStr
			}
			
			switch d.Type {
			case diffmatchpatch.DiffInsert:
				lineContent += dv.styles.Added.Render("+ " + line)
			case diffmatchpatch.DiffDelete:
				lineContent += dv.styles.Removed.Render("- " + line)
			default: // DiffEqual
				lineContent += dv.styles.Context.Render("  " + line)
				lineNum++
			}
			
			content.WriteString(lineContent)
			content.WriteString("\n")
		}
		
		if d.Type == diffmatchpatch.DiffInsert {
			lineNum += len(lines) - 1
		}
	}
	
	return content.String()
}

func (dv *DiffView) renderSideBySide() string {
	if len(dv.diff) == 0 {
		return dv.styles.Context.Render("No changes detected")
	}

	originalLines := strings.Split(dv.original, "\n")
	modifiedLines := strings.Split(dv.modified, "\n")
	
	maxLines := len(originalLines)
	if len(modifiedLines) > maxLines {
		maxLines = len(modifiedLines)
	}
	
	var content strings.Builder
	width := dv.width / 2 - 5 // Split width, account for margins
	
	// Header for side-by-side
	content.WriteString(dv.styles.Header.Render(fmt.Sprintf("%-*s │ %s", width, "Original", "Modified")))
	content.WriteString("\n")
	content.WriteString(strings.Repeat("─", dv.width))
	content.WriteString("\n")
	
	for i := 0; i < maxLines; i++ {
		var originalLine, modifiedLine string
		
		if i < len(originalLines) {
			originalLine = originalLines[i]
		}
		if i < len(modifiedLines) {
			modifiedLine = modifiedLines[i]
		}
		
		// Truncate long lines
		if len(originalLine) > width {
			originalLine = originalLine[:width-3] + "..."
		}
		if len(modifiedLine) > width {
			modifiedLine = modifiedLine[:width-3] + "..."
		}
		
		// Style based on changes
		if originalLine != modifiedLine {
			if originalLine != "" {
				originalLine = dv.styles.Removed.Render(originalLine)
			}
			if modifiedLine != "" {
				modifiedLine = dv.styles.Added.Render(modifiedLine)
			}
		} else {
			originalLine = dv.styles.Context.Render(originalLine)
			modifiedLine = dv.styles.Context.Render(modifiedLine)
		}
		
		line := fmt.Sprintf("%-*s │ %s", width, originalLine, modifiedLine)
		content.WriteString(line)
		content.WriteString("\n")
	}
	
	return content.String()
}

func (dv *DiffView) renderInline() string {
	if len(dv.diff) == 0 {
		return dv.styles.Context.Render("No changes detected")
	}

	var content strings.Builder
	
	for _, d := range dv.diff {
		switch d.Type {
		case diffmatchpatch.DiffInsert:
			content.WriteString(dv.styles.Added.Render(d.Text))
		case diffmatchpatch.DiffDelete:
			content.WriteString(dv.styles.Removed.Render(d.Text))
		default: // DiffEqual
			content.WriteString(dv.styles.Context.Render(d.Text))
		}
	}
	
	return content.String()
}

func (dv *DiffView) renderContent(text string) string {
	// Apply syntax highlighting if enabled
	if dv.config.EnableSyntax {
		// TODO: Integrate with existing syntax highlighting
		// For now, just apply basic styling
		return dv.styles.Context.Render(text)
	}
	
	return text
}

func (dv *DiffView) countChanges() (adds, deletes int) {
	for _, d := range dv.diff {
		lines := strings.Split(d.Text, "\n")
		lineCount := len(lines)
		if lines[len(lines)-1] == "" {
			lineCount-- // Don't count empty last line
		}
		
		switch d.Type {
		case diffmatchpatch.DiffInsert:
			adds += lineCount
		case diffmatchpatch.DiffDelete:
			deletes += lineCount
		}
	}
	return
}

func createDiffStyles(theme string) DiffStyles {
	// Define color schemes
	var (
		addedColor   = lipgloss.Color("#22c55e")   // Green
		removedColor = lipgloss.Color("#ef4444")   // Red
		contextColor = lipgloss.Color("#6b7280")   // Gray
		headerColor  = lipgloss.Color("#3b82f6")   // Blue
		lineNumColor = lipgloss.Color("#9ca3af")   // Light gray
	)
	
	// Adjust colors for different themes
	if theme == "dark" {
		addedColor = lipgloss.Color("#10b981")
		removedColor = lipgloss.Color("#f87171")
		contextColor = lipgloss.Color("#d1d5db")
		headerColor = lipgloss.Color("#60a5fa")
	}
	
	return DiffStyles{
		Base: lipgloss.NewStyle().
			Padding(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#374151")),
			
		Header: lipgloss.NewStyle().
			Foreground(headerColor).
			Bold(true),
			
		Footer: lipgloss.NewStyle().
			Foreground(contextColor).
			Italic(true),
			
		LineNumber: lipgloss.NewStyle().
			Foreground(lineNumColor).
			Width(6).
			Align(lipgloss.Right),
			
		Added: lipgloss.NewStyle().
			Foreground(addedColor),
			
		Removed: lipgloss.NewStyle().
			Foreground(removedColor),
			
		Modified: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f59e0b")). // Yellow
			Bold(true),
			
		Context: lipgloss.NewStyle().
			Foreground(contextColor),
			
		Highlight: lipgloss.NewStyle().
			Background(lipgloss.Color("#1f2937")).
			Foreground(lipgloss.Color("#f9fafb")),
			
		Error: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#dc2626")).
			Bold(true),
			
		Success: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#059669")).
			Bold(true),
	}
}

// DiffViewResult represents the result of a diff view session
type DiffViewResult struct {
	Action   DiffViewAction `json:"action"`
	Approved bool           `json:"approved"`
	FilePath string         `json:"file_path"`
	Duration time.Duration  `json:"duration"`
	Error    string         `json:"error,omitempty"`
}

// RunDiffView runs an interactive diff view session
func RunDiffView(original, modified, filePath string, config DiffViewConfig) (*DiffViewResult, error) {
	startTime := time.Now()
	
	diffView := NewDiffView(config)
	diffView.SetContent(original, modified, filePath)
	
	program := tea.NewProgram(diffView, tea.WithAltScreen())
	
	if _, err := program.Run(); err != nil {
		return &DiffViewResult{
			Action:   ActionCancel,
			Approved: false,
			FilePath: filePath,
			Duration: time.Since(startTime),
			Error:    err.Error(),
		}, err
	}
	
	return &DiffViewResult{
		Action:   diffView.GetAction(),
		Approved: diffView.IsApproved(),
		FilePath: filePath,
		Duration: time.Since(startTime),
	}, nil
}

// ShowDiffPreview shows a quick diff preview without interaction
func ShowDiffPreview(original, modified string, maxLines int) string {
	config := DiffViewConfig{
		Mode:            DiffModeUnified,
		ShowLineNumbers: true,
		ContextLines:    2,
		EnableSyntax:    false,
		Theme:           "light",
	}
	
	diffView := NewDiffView(config)
	diffView.SetContent(original, modified, "preview")
	diffView.Resize(80, maxLines)
	
	// Get just the content without interactive elements
	switch config.Mode {
	case DiffModeUnified:
		return diffView.renderUnified()
	case DiffModeSideBySide:
		return diffView.renderSideBySide()
	default:
		return diffView.renderInline()
	}
}

// CompareDiffs compares two diff results and highlights differences
func CompareDiffs(diff1, diff2 []diffmatchpatch.Diff) string {
	// This would be used to compare different diff algorithms or versions
	// For now, return a simple comparison
	return fmt.Sprintf("Diff 1: %d changes, Diff 2: %d changes", len(diff1), len(diff2))
}