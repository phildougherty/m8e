package visual

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	appcontext "github.com/phildougherty/m8e/internal/context"
	"github.com/phildougherty/m8e/internal/treesitter"
)

// FileViewMode represents different file browser modes
type FileViewMode string

const (
	FileViewModeTree   FileViewMode = "tree"
	FileViewModeList   FileViewMode = "list"
	FileViewModeSearch FileViewMode = "search"
	FileViewModePreview FileViewMode = "preview"
)

// FileViewAction represents user actions in the file view
type FileViewAction string

const (
	FileActionSelect   FileViewAction = "select"
	FileActionEdit     FileViewAction = "edit"
	FileActionPreview  FileViewAction = "preview"
	FileActionSearch   FileViewAction = "search"
	FileActionRefresh  FileViewAction = "refresh"
	FileActionBack     FileViewAction = "back"
	FileActionQuit     FileViewAction = "quit"
)

// FileItem represents a file or directory in the browser
type FileItem struct {
	Name         string                   `json:"name"`
	Path         string                   `json:"path"`
	RelativePath string                   `json:"relative_path"`
	IsDir        bool                     `json:"is_dir"`
	Size         int64                    `json:"size"`
	ModTime      time.Time                `json:"mod_time"`
	GitStatus    string                   `json:"git_status,omitempty"`
	Language     treesitter.Language      `json:"language,omitempty"`
	Preview      string                   `json:"preview,omitempty"`
	Metadata     map[string]interface{}   `json:"metadata,omitempty"`
}

// FileViewConfig configures the file browser behavior
type FileViewConfig struct {
	RootPath         string            `json:"root_path"`
	Mode             FileViewMode      `json:"mode"`
	ShowHidden       bool              `json:"show_hidden"`
	ShowGitStatus    bool              `json:"show_git_status"`
	EnablePreview    bool              `json:"enable_preview"`
	MaxPreviewLines  int               `json:"max_preview_lines"`
	FuzzySearch      bool              `json:"fuzzy_search"`
	PreviewSyntax    bool              `json:"preview_syntax"`
	TreeIndent       int               `json:"tree_indent"`
	Extensions       []string          `json:"extensions,omitempty"`
	ExcludePatterns  []string          `json:"exclude_patterns,omitempty"`
}

// FileView represents an interactive file browser component
type FileView struct {
	config        FileViewConfig
	mode          FileViewMode
	items         []FileItem
	filteredItems []FileItem
	selectedIndex int
	currentPath   string
	searchInput   textinput.Model
	list          list.Model
	preview       viewport.Model
	keyMap        FileViewKeyMap
	action        FileViewAction
	selectedFile  *FileItem
	width         int
	height        int
	styles        FileViewStyles
	discovery     *appcontext.FileDiscovery
	parser        *treesitter.TreeSitterParser
	searching     bool
	loading       bool
	error         string
}

// FileViewKeyMap defines keyboard shortcuts for file view
type FileViewKeyMap struct {
	Select     key.Binding
	Edit       key.Binding
	Preview    key.Binding
	Search     key.Binding
	Refresh    key.Binding
	Back       key.Binding
	Up         key.Binding
	Down       key.Binding
	Left       key.Binding
	Right      key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	Home       key.Binding
	End        key.Binding
	Enter      key.Binding
	Escape     key.Binding
	Tab        key.Binding
	Help       key.Binding
	Quit       key.Binding
}

// FileViewStyles defines styling for file view components
type FileViewStyles struct {
	Base        lipgloss.Style
	Header      lipgloss.Style
	Footer      lipgloss.Style
	List        lipgloss.Style
	Preview     lipgloss.Style
	Search      lipgloss.Style
	Directory   lipgloss.Style
	File        lipgloss.Style
	Selected    lipgloss.Style
	GitAdded    lipgloss.Style
	GitModified lipgloss.Style
	GitDeleted  lipgloss.Style
	Error       lipgloss.Style
	Loading     lipgloss.Style
	Border      lipgloss.Style
}

// FileListItem implements list.Item for the file list
type FileListItem struct {
	item   FileItem
	styles FileViewStyles
}

func (i FileListItem) FilterValue() string {
	return i.item.Name
}

func (i FileListItem) Title() string {
	title := i.item.Name
	if i.item.IsDir {
		title = i.styles.Directory.Render("📁 " + title + "/")
	} else {
		icon := getFileIcon(i.item.Path)
		title = i.styles.File.Render(icon + " " + title)
	}
	
	if i.item.GitStatus != "" {
		status := getGitStatusIcon(i.item.GitStatus)
		title += " " + status
	}
	
	return title
}

func (i FileListItem) Description() string {
	if i.item.IsDir {
		return i.styles.Directory.Render("Directory")
	}
	
	size := formatFileSize(i.item.Size)
	modTime := i.item.ModTime.Format("Jan 2 15:04")
	
	desc := fmt.Sprintf("%s • %s", size, modTime)
	if i.item.Language != treesitter.LanguageUnknown {
		desc += fmt.Sprintf(" • %s", i.item.Language)
	}
	
	return i.styles.File.Render(desc)
}

// NewFileView creates a new file browser instance
func NewFileView(config FileViewConfig, discovery *appcontext.FileDiscovery, parser *treesitter.TreeSitterParser) *FileView {
	if config.RootPath == "" {
		config.RootPath = "."
	}
	if config.Mode == "" {
		config.Mode = FileViewModeTree
	}
	if config.MaxPreviewLines == 0 {
		config.MaxPreviewLines = 20
	}
	if config.TreeIndent == 0 {
		config.TreeIndent = 2
	}

	keyMap := FileViewKeyMap{
		Select:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Preview:  key.NewBinding(key.WithKeys("p", " "), key.WithHelp("p/space", "preview")),
		Search:   key.NewBinding(key.WithKeys("/", "ctrl+f"), key.WithHelp("/", "search")),
		Refresh:  key.NewBinding(key.WithKeys("r", "F5"), key.WithHelp("r", "refresh")),
		Back:     key.NewBinding(key.WithKeys("backspace", "h"), key.WithHelp("backspace", "back")),
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:     key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "left")),
		Right:    key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "f"), key.WithHelp("pgdn", "page down")),
		Home:     key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("home", "top")),
		End:      key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("end", "bottom")),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Escape:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Tab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "mode")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}

	styles := createFileViewStyles()

	// Initialize search input
	searchInput := textinput.New()
	searchInput.Placeholder = "Type to search files..."
	searchInput.CharLimit = 100
	searchInput.Width = 50

	// Initialize list
	fileList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	fileList.Title = "Files"
	fileList.SetShowStatusBar(true)
	fileList.SetFilteringEnabled(config.FuzzySearch)
	fileList.Styles.Title = styles.Header

	// Initialize preview viewport
	preview := viewport.New(0, 0)
	preview.Style = styles.Preview

	fv := &FileView{
		config:      config,
		mode:        config.Mode,
		currentPath: config.RootPath,
		searchInput: searchInput,
		list:        fileList,
		preview:     preview,
		keyMap:      keyMap,
		styles:      styles,
		discovery:   discovery,
		parser:      parser,
	}

	return fv
}

// LoadDirectory loads files from the specified directory
func (fv *FileView) LoadDirectory(path string) error {
	fv.loading = true
	fv.error = ""
	
	// Use file discovery to get files
	options := appcontext.SearchOptions{
		Root:             path,
		MaxResults:       1000,
		IncludeHidden:    fv.config.ShowHidden,
		RespectGitignore: true,
		IncludeGitStatus: fv.config.ShowGitStatus,
		Extensions:       fv.config.Extensions,
		Exclude:          fv.config.ExcludePatterns,
	}
	
	results, err := fv.discovery.Search(context.Background(), options)
	if err != nil {
		fv.loading = false
		fv.error = err.Error()
		return err
	}
	
	// Convert search results to file items
	var items []FileItem
	for _, result := range results {
		item := FileItem{
			Name:         filepath.Base(result.Path),
			Path:         result.Path,
			RelativePath: result.RelativePath,
			IsDir:        result.Type == "directory",
			Size:         result.Size,
			ModTime:      result.ModTime,
			GitStatus:    result.GitStatus,
			Metadata:     make(map[string]interface{}),
		}
		
		// Detect language for files
		if !item.IsDir && fv.parser != nil {
			item.Language = fv.parser.DetectLanguage(item.Path)
		}
		
		// Add preview for small files
		if fv.config.EnablePreview && !item.IsDir && item.Size < 10240 { // 10KB limit
			item.Preview = fv.generatePreview(item.Path)
		}
		
		items = append(items, item)
	}
	
	// Sort items (directories first, then by name)
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	
	fv.items = items
	fv.filteredItems = items
	fv.currentPath = path
	fv.loading = false
	
	// Update list items
	fv.updateListItems()
	
	return nil
}

// Search performs fuzzy search on file names
func (fv *FileView) Search(query string) {
	if query == "" {
		fv.filteredItems = fv.items
	} else {
		var filtered []FileItem
		query = strings.ToLower(query)
		
		for _, item := range fv.items {
			name := strings.ToLower(item.Name)
			path := strings.ToLower(item.RelativePath)
			
			// Simple fuzzy matching - contains all characters in order
			if fv.config.FuzzySearch {
				if fuzzyMatch(name, query) || fuzzyMatch(path, query) {
					filtered = append(filtered, item)
				}
			} else {
				if strings.Contains(name, query) || strings.Contains(path, query) {
					filtered = append(filtered, item)
				}
			}
		}
		
		fv.filteredItems = filtered
	}
	
	fv.updateListItems()
}

// GetSelectedFile returns the currently selected file
func (fv *FileView) GetSelectedFile() *FileItem {
	if fv.selectedIndex >= 0 && fv.selectedIndex < len(fv.filteredItems) {
		return &fv.filteredItems[fv.selectedIndex]
	}
	return nil
}

// Init initializes the file view model
func (fv *FileView) Init() tea.Cmd {
	return nil
}

// Resize updates the file view dimensions
func (fv *FileView) Resize(width, height int) {
	fv.width = width
	fv.height = height
	
	// Calculate layout dimensions
	headerHeight := 2
	footerHeight := 2
	contentHeight := height - headerHeight - footerHeight
	
	if fv.config.EnablePreview {
		// Split view: list on left, preview on right
		listWidth := width * 2 / 3
		previewWidth := width - listWidth - 1 // -1 for border
		
		fv.list.SetSize(listWidth, contentHeight)
		fv.preview.Width = previewWidth
		fv.preview.Height = contentHeight
	} else {
		// Full width for list
		fv.list.SetSize(width, contentHeight)
	}
	
	fv.searchInput.Width = width - 10 // Leave space for "Search: "
}

// Update handles tea.Msg updates
func (fv *FileView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if fv.searching {
			return fv.handleSearchInput(msg)
		}
		
		switch {
		case key.Matches(msg, fv.keyMap.Search):
			fv.searching = true
			fv.searchInput.Focus()
			return fv, textinput.Blink
			
		case key.Matches(msg, fv.keyMap.Select), key.Matches(msg, fv.keyMap.Enter):
			selectedItem := fv.GetSelectedFile()
			if selectedItem != nil {
				if selectedItem.IsDir {
					// Navigate into directory
					if err := fv.LoadDirectory(selectedItem.Path); err != nil {
						fv.error = err.Error()
					}
				} else {
					// Select file
					fv.action = FileActionSelect
					fv.selectedFile = selectedItem
					return fv, tea.Quit
				}
			}
			
		case key.Matches(msg, fv.keyMap.Edit):
			selectedItem := fv.GetSelectedFile()
			if selectedItem != nil && !selectedItem.IsDir {
				fv.action = FileActionEdit
				fv.selectedFile = selectedItem
				return fv, tea.Quit
			}
			
		case key.Matches(msg, fv.keyMap.Preview):
			selectedItem := fv.GetSelectedFile()
			if selectedItem != nil && !selectedItem.IsDir {
				fv.action = FileActionPreview
				fv.selectedFile = selectedItem
				if fv.config.EnablePreview {
					fv.updatePreview(selectedItem)
				}
			}
			
		case key.Matches(msg, fv.keyMap.Refresh):
			fv.action = FileActionRefresh
			if err := fv.LoadDirectory(fv.currentPath); err != nil {
				fv.error = err.Error()
			}
			
		case key.Matches(msg, fv.keyMap.Back):
			parent := filepath.Dir(fv.currentPath)
			if parent != fv.currentPath { // Avoid infinite loop at root
				if err := fv.LoadDirectory(parent); err != nil {
					fv.error = err.Error()
				}
			}
			
		case key.Matches(msg, fv.keyMap.Tab):
			// Cycle through modes
			switch fv.mode {
			case FileViewModeTree:
				fv.mode = FileViewModeList
			case FileViewModeList:
				fv.mode = FileViewModePreview
			case FileViewModePreview:
				fv.mode = FileViewModeTree
			}
			fv.config.Mode = fv.mode
			
		case key.Matches(msg, fv.keyMap.Quit):
			fv.action = FileActionQuit
			return fv, tea.Quit
			
		default:
			// Pass other keys to list for navigation
			fv.list, cmd = fv.list.Update(msg)
			cmds = append(cmds, cmd)
			
			// Update selected index
			fv.selectedIndex = fv.list.Index()
			
			// Update preview if enabled
			if fv.config.EnablePreview {
				selectedItem := fv.GetSelectedFile()
				if selectedItem != nil && !selectedItem.IsDir {
					fv.updatePreview(selectedItem)
				}
			}
		}
		
	case tea.WindowSizeMsg:
		fv.Resize(msg.Width, msg.Height)
		
	default:
		fv.list, cmd = fv.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return fv, tea.Batch(cmds...)
}

// View renders the file view
func (fv *FileView) View() string {
	var b strings.Builder
	
	// Header
	b.WriteString(fv.renderHeader())
	b.WriteString("\n")
	
	// Main content
	if fv.searching {
		b.WriteString(fv.renderSearch())
	} else if fv.loading {
		b.WriteString(fv.renderLoading())
	} else if fv.error != "" {
		b.WriteString(fv.renderError())
	} else {
		b.WriteString(fv.renderContent())
	}
	
	// Footer
	b.WriteString("\n")
	b.WriteString(fv.renderFooter())
	
	return fv.styles.Base.Render(b.String())
}

// GetAction returns the last user action
func (fv *FileView) GetAction() FileViewAction {
	return fv.action
}

// Private methods

func (fv *FileView) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	
	switch msg.Type {
	case tea.KeyEscape:
		fv.searching = false
		fv.searchInput.Blur()
		fv.searchInput.SetValue("")
		fv.Search("") // Reset search
		return fv, nil
		
	case tea.KeyEnter:
		fv.searching = false
		fv.searchInput.Blur()
		query := fv.searchInput.Value()
		fv.Search(query)
		return fv, nil
		
	default:
		fv.searchInput, cmd = fv.searchInput.Update(msg)
		// Update search results in real-time
		query := fv.searchInput.Value()
		fv.Search(query)
		return fv, cmd
	}
}

func (fv *FileView) updateListItems() {
	var items []list.Item
	
	for _, item := range fv.filteredItems {
		listItem := FileListItem{
			item:   item,
			styles: fv.styles,
		}
		items = append(items, listItem)
	}
	
	fv.list.SetItems(items)
	
	// Reset selection if out of bounds
	if fv.selectedIndex >= len(items) {
		fv.selectedIndex = 0
		fv.list.Select(0)
	}
}

func (fv *FileView) updatePreview(item *FileItem) {
	if item.Preview != "" {
		fv.preview.SetContent(item.Preview)
	} else {
		preview := fv.generatePreview(item.Path)
		fv.preview.SetContent(preview)
	}
}

func (fv *FileView) generatePreview(filePath string) string {
	// TODO: Integrate with file editor for reading
	// For now, return placeholder
	ext := filepath.Ext(filePath)
	content := fmt.Sprintf("Preview of %s\n\n", filepath.Base(filePath))
	content += fmt.Sprintf("File type: %s\n", ext)
	content += fmt.Sprintf("Path: %s\n\n", filePath)
	content += "Content preview would be shown here...\n"
	content += "(Integration with internal/edit needed)"
	
	return content
}

func (fv *FileView) renderHeader() string {
	var header strings.Builder
	
	// Current path
	pathDisplay := fv.currentPath
	if len(pathDisplay) > fv.width-20 {
		pathDisplay = "..." + pathDisplay[len(pathDisplay)-(fv.width-23):]
	}
	header.WriteString(fv.styles.Header.Render(fmt.Sprintf("📁 %s", pathDisplay)))
	
	// Mode indicator
	modeStr := fmt.Sprintf("[%s]", strings.ToUpper(string(fv.mode)))
	header.WriteString(" ")
	header.WriteString(fv.styles.Header.Render(modeStr))
	
	// File count
	count := fmt.Sprintf("(%d files)", len(fv.filteredItems))
	if len(fv.filteredItems) != len(fv.items) {
		count = fmt.Sprintf("(%d/%d files)", len(fv.filteredItems), len(fv.items))
	}
	header.WriteString(" ")
	header.WriteString(fv.styles.Header.Render(count))
	
	return header.String()
}

func (fv *FileView) renderFooter() string {
	var footer strings.Builder
	
	// Key bindings
	bindings := []string{
		fv.keyMap.Enter.Help().Key + " open",
		fv.keyMap.Edit.Help().Key + " edit",
		fv.keyMap.Search.Help().Key + " search",
		fv.keyMap.Back.Help().Key + " back",
		fv.keyMap.Help.Help().Key + " help",
		fv.keyMap.Quit.Help().Key + " quit",
	}
	
	footer.WriteString(fv.styles.Footer.Render(strings.Join(bindings, " • ")))
	
	return footer.String()
}

func (fv *FileView) renderSearch() string {
	var b strings.Builder
	
	b.WriteString(fv.styles.Search.Render("Search: "))
	b.WriteString(fv.searchInput.View())
	b.WriteString("\n\n")
	
	// Show filtered results
	if len(fv.filteredItems) > 0 {
		b.WriteString(fv.list.View())
	} else {
		b.WriteString(fv.styles.Error.Render("No files found matching search"))
	}
	
	return b.String()
}

func (fv *FileView) renderLoading() string {
	return fv.styles.Loading.Render("Loading files...")
}

func (fv *FileView) renderError() string {
	return fv.styles.Error.Render(fmt.Sprintf("Error: %s", fv.error))
}

func (fv *FileView) renderContent() string {
	if fv.config.EnablePreview && fv.mode == FileViewModePreview {
		return fv.renderSplitView()
	}
	
	return fv.list.View()
}

func (fv *FileView) renderSplitView() string {
	listView := fv.list.View()
	previewView := fv.preview.View()
	
	// Split horizontally
	listLines := strings.Split(listView, "\n")
	previewLines := strings.Split(previewView, "\n")
	
	maxLines := len(listLines)
	if len(previewLines) > maxLines {
		maxLines = len(previewLines)
	}
	
	var result strings.Builder
	listWidth := fv.width * 2 / 3
	
	for i := 0; i < maxLines; i++ {
		var listLine, previewLine string
		
		if i < len(listLines) {
			listLine = listLines[i]
		}
		if i < len(previewLines) {
			previewLine = previewLines[i]
		}
		
		// Truncate list line to fit
		if len(listLine) > listWidth {
			listLine = listLine[:listWidth-1]
		}
		
		line := fmt.Sprintf("%-*s │ %s", listWidth, listLine, previewLine)
		result.WriteString(line)
		if i < maxLines-1 {
			result.WriteString("\n")
		}
	}
	
	return result.String()
}

// Helper functions

func fuzzyMatch(text, pattern string) bool {
	textRunes := []rune(text)
	patternRunes := []rune(pattern)
	
	textIndex := 0
	patternIndex := 0
	
	for textIndex < len(textRunes) && patternIndex < len(patternRunes) {
		if textRunes[textIndex] == patternRunes[patternIndex] {
			patternIndex++
		}
		textIndex++
	}
	
	return patternIndex == len(patternRunes)
}

func getFileIcon(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	
	switch ext {
	case ".go":
		return "🐹"
	case ".py":
		return "🐍"
	case ".js", ".ts":
		return "📜"
	case ".rs":
		return "🦀"
	case ".md":
		return "📝"
	case ".yaml", ".yml":
		return "⚙️"
	case ".json":
		return "📋"
	case ".docker", ".dockerfile":
		return "🐳"
	case ".git":
		return "🔧"
	default:
		return "📄"
	}
}

func getGitStatusIcon(status string) string {
	switch status {
	case "A", "AA":
		return "✅" // Added
	case "M", "MM":
		return "🔄" // Modified
	case "D", "DD":
		return "❌" // Deleted
	case "R", "RR":
		return "📝" // Renamed
	case "U":
		return "⚠️" // Unmerged
	case "??":
		return "❓" // Untracked
	default:
		return ""
	}
}

func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func createFileViewStyles() FileViewStyles {
	return FileViewStyles{
		Base: lipgloss.NewStyle().
			Padding(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#374151")),
			
		Header: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3b82f6")).
			Bold(true),
			
		Footer: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6b7280")).
			Italic(true),
			
		List: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#d1d5db")),
			
		Preview: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#d1d5db")).
			Padding(1),
			
		Search: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b5cf6")).
			Bold(true),
			
		Directory: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3b82f6")).
			Bold(true),
			
		File: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#374151")),
			
		Selected: lipgloss.NewStyle().
			Background(lipgloss.Color("#dbeafe")).
			Foreground(lipgloss.Color("#1e40af")),
			
		GitAdded: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10b981")),
			
		GitModified: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f59e0b")),
			
		GitDeleted: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ef4444")),
			
		Error: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#dc2626")).
			Bold(true),
			
		Loading: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6b7280")).
			Italic(true),
			
		Border: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#d1d5db")),
	}
}

// FileViewResult represents the result of a file view session
type FileViewResult struct {
	Action       FileViewAction `json:"action"`
	SelectedFile *FileItem      `json:"selected_file,omitempty"`
	CurrentPath  string         `json:"current_path"`
	Duration     time.Duration  `json:"duration"`
	Error        string         `json:"error,omitempty"`
}

// RunFileView runs an interactive file browser session
func RunFileView(config FileViewConfig, discovery *appcontext.FileDiscovery, parser *treesitter.TreeSitterParser) (*FileViewResult, error) {
	startTime := time.Now()
	
	fileView := NewFileView(config, discovery, parser)
	
	// Load initial directory
	if err := fileView.LoadDirectory(config.RootPath); err != nil {
		return &FileViewResult{
			Action:      FileActionQuit,
			CurrentPath: config.RootPath,
			Duration:    time.Since(startTime),
			Error:       err.Error(),
		}, err
	}
	
	program := tea.NewProgram(fileView, tea.WithAltScreen())
	
	if _, err := program.Run(); err != nil {
		return &FileViewResult{
			Action:      FileActionQuit,
			CurrentPath: fileView.currentPath,
			Duration:    time.Since(startTime),
			Error:       err.Error(),
		}, err
	}
	
	return &FileViewResult{
		Action:       fileView.GetAction(),
		SelectedFile: fileView.selectedFile,
		CurrentPath:  fileView.currentPath,
		Duration:     time.Since(startTime),
	}, nil
}