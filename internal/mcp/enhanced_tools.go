package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	appcontext "github.com/phildougherty/m8e/internal/context"
	"github.com/phildougherty/m8e/internal/edit"
	"github.com/phildougherty/m8e/internal/treesitter"
	"github.com/phildougherty/m8e/internal/visual"
)

// EnhancedToolConfig configures the enhanced MCP tools
type EnhancedToolConfig struct {
	EnableVisualDiff  bool   `json:"enable_visual_diff"`
	EnablePreview     bool   `json:"enable_preview"`
	AutoApprove       bool   `json:"auto_approve"`
	MaxFileSize       int64  `json:"max_file_size"`
	ContextTracking   bool   `json:"context_tracking"`
	BackupEnabled     bool   `json:"backup_enabled"`
	ValidationEnabled bool   `json:"validation_enabled"`
	DiffMode          string `json:"diff_mode"` // unified, side-by-side, inline
}

// EnhancedTools provides MCP tools with advanced visual and context features
type EnhancedTools struct {
	config         EnhancedToolConfig
	fileEditor     *edit.FileEditor
	contextManager *appcontext.ContextManager
	fileDiscovery  *appcontext.FileDiscovery
	parser         *treesitter.TreeSitterParser
	mentionProcessor *appcontext.MentionProcessor
}

// NewEnhancedTools creates a new enhanced tools instance
func NewEnhancedTools(
	config EnhancedToolConfig,
	fileEditor *edit.FileEditor,
	contextManager *appcontext.ContextManager,
	fileDiscovery *appcontext.FileDiscovery,
	parser *treesitter.TreeSitterParser,
	mentionProcessor *appcontext.MentionProcessor,
) *EnhancedTools {
	if config.MaxFileSize == 0 {
		config.MaxFileSize = 1024 * 1024 // 1MB default
	}
	if config.DiffMode == "" {
		config.DiffMode = "unified"
	}

	return &EnhancedTools{
		config:           config,
		fileEditor:       fileEditor,
		contextManager:   contextManager,
		fileDiscovery:    fileDiscovery,
		parser:           parser,
		mentionProcessor: mentionProcessor,
	}
}

// EditFileRequest represents a request to edit a file
type EditFileRequest struct {
	FilePath    string `json:"file_path"`
	Content     string `json:"content,omitempty"`
	DiffContent string `json:"diff_content,omitempty"`
	CreateFile  bool   `json:"create_file,omitempty"`
	ShowPreview bool   `json:"show_preview,omitempty"`
	AutoApprove bool   `json:"auto_approve,omitempty"`
}

// EditFileResponse represents the response from editing a file
type EditFileResponse struct {
	Success     bool                    `json:"success"`
	FilePath    string                  `json:"file_path"`
	Applied     bool                    `json:"applied"`
	Preview     string                  `json:"preview,omitempty"`
	DiffPreview string                  `json:"diff_preview,omitempty"`
	Backup      string                  `json:"backup,omitempty"`
	Error       string                  `json:"error,omitempty"`
	Metadata    map[string]interface{}  `json:"metadata,omitempty"`
}

// ReadFileRequest represents a request to read a file with context tracking
type ReadFileRequest struct {
	FilePath      string `json:"file_path"`
	StartLine     int    `json:"start_line,omitempty"`
	EndLine       int    `json:"end_line,omitempty"`
	IncludeContext bool  `json:"include_context,omitempty"`
	TrackInContext bool  `json:"track_in_context,omitempty"`
}

// ReadFileResponse represents the response from reading a file
type ReadFileResponse struct {
	Success     bool                    `json:"success"`
	FilePath    string                  `json:"file_path"`
	Content     string                  `json:"content"`
	Language    string                  `json:"language,omitempty"`
	LineCount   int                     `json:"line_count"`
	Size        int64                   `json:"size"`
	Context     string                  `json:"context,omitempty"`
	Error       string                  `json:"error,omitempty"`
	Metadata    map[string]interface{}  `json:"metadata,omitempty"`
}

// SearchFilesRequest represents a request to search files
type SearchFilesRequest struct {
	Query          string   `json:"query"`
	Extensions     []string `json:"extensions,omitempty"`
	MaxResults     int      `json:"max_results,omitempty"`
	IncludeContent bool     `json:"include_content,omitempty"`
	FuzzySearch    bool     `json:"fuzzy_search,omitempty"`
}

// SearchFilesResponse represents the response from searching files
type SearchFilesResponse struct {
	Success bool                    `json:"success"`
	Query   string                  `json:"query"`
	Results []FileSearchResult      `json:"results"`
	Count   int                     `json:"count"`
	Error   string                  `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// FileSearchResult represents a single file search result
type FileSearchResult struct {
	FilePath     string  `json:"file_path"`
	RelativePath string  `json:"relative_path"`
	Language     string  `json:"language,omitempty"`
	Size         int64   `json:"size"`
	ModTime      string  `json:"mod_time"`
	Score        int     `json:"score,omitempty"`
	Preview      string  `json:"preview,omitempty"`
	GitStatus    string  `json:"git_status,omitempty"`
}

// EditFile implements the enhanced edit_file MCP tool
func (et *EnhancedTools) EditFile(ctx context.Context, request EditFileRequest) (*EditFileResponse, error) {
	response := &EditFileResponse{
		FilePath: request.FilePath,
		Metadata: make(map[string]interface{}),
	}

	// Validate file path
	if request.FilePath == "" {
		response.Error = "file_path is required"
		return response, fmt.Errorf("file_path is required")
	}

	// Read current content if file exists
	var originalContent string
	var err error
	
	if et.fileEditor != nil {
		originalContent, err = et.fileEditor.ReadFile(request.FilePath)
		if err != nil && !request.CreateFile {
			response.Error = fmt.Sprintf("failed to read file: %v", err)
			return response, err
		}
	}

	var newContent string
	var diffApplied bool

	// Determine edit method
	if request.DiffContent != "" {
		// Apply diff using edit system
		if et.fileEditor != nil {
			editResult, err := et.fileEditor.ApplyDiff(request.FilePath, request.DiffContent)
			if err != nil {
				response.Error = fmt.Sprintf("failed to apply diff: %v", err)
				return response, err
			}
			
			newContent = editResult.NewContent
			response.Backup = editResult.Backup
			diffApplied = editResult.Applied
			
			// Add edit metadata
			response.Metadata["edit_result"] = map[string]interface{}{
				"timestamp": editResult.Timestamp,
				"applied":   editResult.Applied,
			}
		} else {
			response.Error = "file editor not available"
			return response, fmt.Errorf("file editor not available")
		}
	} else if request.Content != "" {
		// Direct content replacement
		newContent = request.Content
	} else {
		response.Error = "either content or diff_content must be provided"
		return response, fmt.Errorf("either content or diff_content must be provided")
	}

	// Generate diff preview
	if et.config.EnableVisualDiff && originalContent != "" {
		response.DiffPreview = visual.ShowDiffPreview(originalContent, newContent, 20)
	}

	// Show interactive approval if enabled
	if et.config.EnableVisualDiff && !et.config.AutoApprove && !request.AutoApprove {
		diffConfig := visual.DiffViewConfig{
			Mode:            visual.DiffModeUnified,
			ShowLineNumbers: true,
			ContextLines:    3,
			EnableSyntax:    true,
			AutoApprove:     false,
		}
		
		result, err := visual.RunDiffView(originalContent, newContent, request.FilePath, diffConfig)
		if err != nil {
			response.Error = fmt.Sprintf("diff view error: %v", err)
			return response, err
		}
		
		response.Applied = result.Approved
		response.Metadata["diff_session"] = map[string]interface{}{
			"action":   string(result.Action),
			"duration": result.Duration.Milliseconds(),
		}
		
		// Only apply if approved
		if !result.Approved {
			response.Success = true
			response.DiffPreview = "Edit rejected by user"
			return response, nil
		}
	} else {
		response.Applied = true
	}

	// Apply the edit if approved or auto-approved
	if response.Applied {
		if et.fileEditor != nil {
			err = et.fileEditor.WriteFile(request.FilePath, newContent)
			if err != nil {
				response.Error = fmt.Sprintf("failed to write file: %v", err)
				return response, err
			}
		}

		// Track in context if enabled
		if et.config.ContextTracking && et.contextManager != nil {
			metadata := map[string]interface{}{
				"edit_type":   "mcp_tool",
				"file_size":   len(newContent),
				"diff_applied": diffApplied,
			}
			
			err = et.contextManager.AddContext(
				appcontext.ContextTypeEdit,
				request.FilePath,
				newContent,
				metadata,
			)
			if err != nil {
				// Log error but don't fail the operation
				response.Metadata["context_error"] = err.Error()
			}
		}
	}

	// Generate preview if requested
	if request.ShowPreview && et.config.EnablePreview {
		response.Preview = et.generateFilePreview(request.FilePath, newContent)
	}

	response.Success = true
	response.Metadata["lines_changed"] = strings.Count(newContent, "\n") - strings.Count(originalContent, "\n")
	
	return response, nil
}

// ReadFile implements the enhanced read_file MCP tool
func (et *EnhancedTools) ReadFile(ctx context.Context, request ReadFileRequest) (*ReadFileResponse, error) {
	response := &ReadFileResponse{
		FilePath: request.FilePath,
		Metadata: make(map[string]interface{}),
	}

	// Validate file path
	if request.FilePath == "" {
		response.Error = "file_path is required"
		return response, fmt.Errorf("file_path is required")
	}

	// Read file content
	var content string
	var err error
	
	if et.fileEditor != nil {
		content, err = et.fileEditor.ReadFile(request.FilePath)
		if err != nil {
			response.Error = fmt.Sprintf("failed to read file: %v", err)
			return response, err
		}
	} else {
		response.Error = "file editor not available"
		return response, fmt.Errorf("file editor not available")
	}

	// Extract specific lines if requested
	if request.StartLine > 0 || request.EndLine > 0 {
		lines := strings.Split(content, "\n")
		
		start := request.StartLine - 1 // Convert to 0-based
		if start < 0 {
			start = 0
		}
		
		end := request.EndLine
		if end <= 0 || end > len(lines) {
			end = len(lines)
		}
		
		if start >= len(lines) {
			response.Error = fmt.Sprintf("start_line %d exceeds file length", request.StartLine)
			return response, fmt.Errorf("start_line exceeds file length")
		}
		
		selectedLines := lines[start:end]
		content = strings.Join(selectedLines, "\n")
		
		response.Metadata["line_range"] = map[string]int{
			"start": start + 1,
			"end":   end,
		}
	}

	// Detect language
	if et.parser != nil {
		language := et.parser.DetectLanguage(request.FilePath)
		response.Language = string(language)
	}

	// Include context if requested
	if request.IncludeContext && et.contextManager != nil {
		contextItems, err := et.contextManager.GetContextByFile(request.FilePath)
		if err == nil && len(contextItems) > 0 {
			var contextParts []string
			for _, item := range contextItems {
				contextParts = append(contextParts, fmt.Sprintf("[%s] %s", item.Type, item.Content[:min(100, len(item.Content))]))
			}
			response.Context = strings.Join(contextParts, "\n")
		}
	}

	// Track in context if enabled
	if request.TrackInContext && et.config.ContextTracking && et.contextManager != nil {
		metadata := map[string]interface{}{
			"read_type": "mcp_tool",
			"file_size": len(content),
		}
		
		err = et.contextManager.AddContext(
			appcontext.ContextTypeFile,
			request.FilePath,
			content,
			metadata,
		)
		if err != nil {
			response.Metadata["context_error"] = err.Error()
		}
	}

	response.Success = true
	response.Content = content
	response.LineCount = strings.Count(content, "\n") + 1
	response.Size = int64(len(content))
	
	return response, nil
}

// SearchFiles implements the enhanced search_files MCP tool
func (et *EnhancedTools) SearchFiles(ctx context.Context, request SearchFilesRequest) (*SearchFilesResponse, error) {
	response := &SearchFilesResponse{
		Query:    request.Query,
		Results:  make([]FileSearchResult, 0),
		Metadata: make(map[string]interface{}),
	}

	// Validate query
	if request.Query == "" {
		response.Error = "query is required"
		return response, fmt.Errorf("query is required")
	}

	// Set defaults
	maxResults := request.MaxResults
	if maxResults == 0 {
		maxResults = 50
	}

	// Perform search using file discovery
	if et.fileDiscovery != nil {
		searchOptions := appcontext.SearchOptions{
			Pattern:     request.Query,
			Extensions:  request.Extensions,
			MaxResults:  maxResults,
			Concurrent:  true,
		}
		
		results, err := et.fileDiscovery.Search(ctx, searchOptions)
		if err != nil {
			response.Error = fmt.Sprintf("search failed: %v", err)
			return response, err
		}

		// Convert results
		for _, result := range results {
			searchResult := FileSearchResult{
				FilePath:     result.Path,
				RelativePath: result.RelativePath,
				Size:         result.Size,
				ModTime:      result.ModTime.Format(time.RFC3339),
				GitStatus:    result.GitStatus,
			}

			// Detect language
			if et.parser != nil {
				language := et.parser.DetectLanguage(result.Path)
				searchResult.Language = string(language)
			}

			// Add score if available
			if result.Score != 0 {
				searchResult.Score = result.Score
			}

			// Include content preview if requested
			if request.IncludeContent && et.fileEditor != nil {
				if result.Size < et.config.MaxFileSize { // Only for small files
					content, err := et.fileEditor.ReadFile(result.Path)
					if err == nil {
						// Generate preview (first few lines)
						lines := strings.Split(content, "\n")
						previewLines := 5
						if len(lines) < previewLines {
							previewLines = len(lines)
						}
						searchResult.Preview = strings.Join(lines[:previewLines], "\n")
						if len(lines) > previewLines {
							searchResult.Preview += "\n..."
						}
					}
				}
			}

			response.Results = append(response.Results, searchResult)
		}

		response.Count = len(response.Results)
		response.Metadata["search_time"] = time.Since(time.Now()).Milliseconds()
	} else {
		response.Error = "file discovery not available"
		return response, fmt.Errorf("file discovery not available")
	}

	response.Success = true
	return response, nil
}

// GenerateFilePreview creates a preview of file content
func (et *EnhancedTools) generateFilePreview(filePath, content string) string {
	var preview strings.Builder
	
	preview.WriteString(fmt.Sprintf("File: %s\n", filePath))
	preview.WriteString(fmt.Sprintf("Size: %d bytes\n", len(content)))
	preview.WriteString(fmt.Sprintf("Lines: %d\n", strings.Count(content, "\n")+1))
	
	// Language detection
	if et.parser != nil {
		language := et.parser.DetectLanguage(filePath)
		if language != treesitter.LanguageUnknown {
			preview.WriteString(fmt.Sprintf("Language: %s\n", language))
		}
	}
	
	preview.WriteString("\nContent Preview:\n")
	preview.WriteString(strings.Repeat("-", 40))
	preview.WriteString("\n")
	
	// Show first 20 lines
	lines := strings.Split(content, "\n")
	maxLines := 20
	if len(lines) < maxLines {
		maxLines = len(lines)
	}
	
	for i := 0; i < maxLines; i++ {
		preview.WriteString(fmt.Sprintf("%3d: %s\n", i+1, lines[i]))
	}
	
	if len(lines) > maxLines {
		preview.WriteString("... (truncated)")
	}
	
	return preview.String()
}

// ParseCodeRequest represents a request to parse code structure
type ParseCodeRequest struct {
	FilePath    string   `json:"file_path"`
	Content     string   `json:"content,omitempty"`
	QueryTypes  []string `json:"query_types,omitempty"`
	IncludeBody bool     `json:"include_body,omitempty"`
}

// ParseCodeResponse represents the response from parsing code
type ParseCodeResponse struct {
	Success     bool                           `json:"success"`
	FilePath    string                         `json:"file_path"`
	Language    string                         `json:"language"`
	Definitions []treesitter.Definition        `json:"definitions"`
	Structure   map[string]interface{}         `json:"structure,omitempty"`
	Error       string                         `json:"error,omitempty"`
	Metadata    map[string]interface{}         `json:"metadata,omitempty"`
}

// ParseCode implements the parse_code MCP tool
func (et *EnhancedTools) ParseCode(ctx context.Context, request ParseCodeRequest) (*ParseCodeResponse, error) {
	response := &ParseCodeResponse{
		FilePath: request.FilePath,
		Metadata: make(map[string]interface{}),
	}

	// Get content
	var content string
	if request.Content != "" {
		content = request.Content
	} else if request.FilePath != "" && et.fileEditor != nil {
		var err error
		content, err = et.fileEditor.ReadFile(request.FilePath)
		if err != nil {
			response.Error = fmt.Sprintf("failed to read file: %v", err)
			return response, err
		}
	} else {
		response.Error = "either file_path or content must be provided"
		return response, fmt.Errorf("either file_path or content must be provided")
	}

	// Parse with tree-sitter
	if et.parser == nil {
		response.Error = "parser not available"
		return response, fmt.Errorf("parser not available")
	}

	language := et.parser.DetectLanguage(request.FilePath)
	response.Language = string(language)

	parseResult, err := et.parser.ParseContent(content, language, request.FilePath)
	if err != nil {
		response.Error = fmt.Sprintf("failed to parse: %v", err)
		return response, err
	}

	// Extract definitions
	extractor := treesitter.NewDefinitionExtractor(et.parser)
	definitions, err := extractor.ExtractDefinitions(parseResult)
	if err != nil {
		response.Error = fmt.Sprintf("failed to extract definitions: %v", err)
		return response, err
	}

	// Filter by query types if specified
	if len(request.QueryTypes) > 0 {
		var filtered []treesitter.Definition
		for _, def := range definitions {
			for _, queryType := range request.QueryTypes {
				if string(def.Type) == queryType {
					filtered = append(filtered, def)
					break
				}
			}
		}
		definitions = filtered
	}

	// Remove body content if not requested
	if !request.IncludeBody {
		for i := range definitions {
			if len(definitions[i].Content) > 200 {
				definitions[i].Content = definitions[i].Content[:200] + "..."
			}
		}
	}

	response.Success = true
	response.Definitions = definitions
	response.Metadata["parse_time_ms"] = parseResult.ParseTime.Milliseconds()
	response.Metadata["node_count"] = parseResult.NodeCount
	response.Metadata["definition_count"] = len(definitions)

	return response, nil
}

// ListDefinitionsRequest represents a request to list code definitions
type ListDefinitionsRequest struct {
	Directory   string   `json:"directory,omitempty"`
	FilePaths   []string `json:"file_paths,omitempty"`
	Types       []string `json:"types,omitempty"`
	Languages   []string `json:"languages,omitempty"`
	MaxResults  int      `json:"max_results,omitempty"`
}

// ListDefinitionsResponse represents the response from listing definitions
type ListDefinitionsResponse struct {
	Success     bool                         `json:"success"`
	Definitions []treesitter.Definition      `json:"definitions"`
	Count       int                          `json:"count"`
	Error       string                       `json:"error,omitempty"`
	Metadata    map[string]interface{}       `json:"metadata,omitempty"`
}

// ListDefinitions implements the list_definitions MCP tool
func (et *EnhancedTools) ListDefinitions(ctx context.Context, request ListDefinitionsRequest) (*ListDefinitionsResponse, error) {
	response := &ListDefinitionsResponse{
		Definitions: make([]treesitter.Definition, 0),
		Metadata:    make(map[string]interface{}),
	}

	// Set defaults
	maxResults := request.MaxResults
	if maxResults == 0 {
		maxResults = 100
	}

	var filePaths []string

	// Get file paths
	if len(request.FilePaths) > 0 {
		filePaths = request.FilePaths
	} else if request.Directory != "" && et.fileDiscovery != nil {
		// Search directory for code files
		searchOptions := appcontext.SearchOptions{
			Root:       request.Directory,
			Extensions: []string{"go", "py", "js", "ts", "rs"},
			MaxResults: maxResults * 2, // Get more files than needed
		}

		results, err := et.fileDiscovery.Search(ctx, searchOptions)
		if err != nil {
			response.Error = fmt.Sprintf("directory search failed: %v", err)
			return response, err
		}

		for _, result := range results {
			if !strings.Contains(result.RelativePath, "/") || !strings.HasPrefix(result.RelativePath, ".") {
				filePaths = append(filePaths, result.Path)
			}
		}
	} else {
		response.Error = "either directory or file_paths must be provided"
		return response, fmt.Errorf("either directory or file_paths must be provided")
	}

	// Parse files and extract definitions
	var allDefinitions []treesitter.Definition
	processedFiles := 0

	for _, filePath := range filePaths {
		if processedFiles >= maxResults {
			break
		}

		// Read file
		content, err := et.fileEditor.ReadFile(filePath)
		if err != nil {
			continue // Skip files that can't be read
		}

		// Parse file
		language := et.parser.DetectLanguage(filePath)
		
		// Filter by language if specified
		if len(request.Languages) > 0 {
			languageMatch := false
			for _, lang := range request.Languages {
				if string(language) == lang {
					languageMatch = true
					break
				}
			}
			if !languageMatch {
				continue
			}
		}

		parseResult, err := et.parser.ParseContent(content, language, filePath)
		if err != nil {
			continue // Skip files that can't be parsed
		}

		// Extract definitions
		extractor := treesitter.NewDefinitionExtractor(et.parser)
		definitions, err := extractor.ExtractDefinitions(parseResult)
		if err != nil {
			continue // Skip files with extraction errors
		}

		// Filter by types if specified
		if len(request.Types) > 0 {
			var filtered []treesitter.Definition
			for _, def := range definitions {
				for _, defType := range request.Types {
					if string(def.Type) == defType {
						filtered = append(filtered, def)
						break
					}
				}
			}
			definitions = filtered
		}

		allDefinitions = append(allDefinitions, definitions...)
		processedFiles++
	}

	// Limit results
	if len(allDefinitions) > maxResults {
		allDefinitions = allDefinitions[:maxResults]
	}

	response.Success = true
	response.Definitions = allDefinitions
	response.Count = len(allDefinitions)
	response.Metadata["files_processed"] = processedFiles
	response.Metadata["total_files"] = len(filePaths)

	return response, nil
}

// Helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}