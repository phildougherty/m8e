package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	appcontext "github.com/phildougherty/m8e/internal/context"
	"github.com/phildougherty/m8e/internal/treesitter"
)

// ContextToolConfig configures the context-aware MCP tools
type ContextToolConfig struct {
	MaxTokens        int     `json:"max_tokens"`
	TruncationStrategy string `json:"truncation_strategy"`
	EnableMentions   bool    `json:"enable_mentions"`
	AutoTrack        bool    `json:"auto_track"`
	RelevanceThreshold float64 `json:"relevance_threshold"`
	MaxContextItems  int     `json:"max_context_items"`
}

// ContextTools provides MCP tools with intelligent context management
type ContextTools struct {
	config           ContextToolConfig
	contextManager   *appcontext.ContextManager
	fileDiscovery    *appcontext.FileDiscovery
	mentionProcessor *appcontext.MentionProcessor
	parser           *treesitter.TreeSitterParser
}

// NewContextTools creates a new context tools instance
func NewContextTools(
	config ContextToolConfig,
	contextManager *appcontext.ContextManager,
	fileDiscovery *appcontext.FileDiscovery,
	mentionProcessor *appcontext.MentionProcessor,
	parser *treesitter.TreeSitterParser,
) *ContextTools {
	if config.MaxTokens == 0 {
		config.MaxTokens = 32768
	}
	if config.TruncationStrategy == "" {
		config.TruncationStrategy = "intelligent"
	}
	if config.RelevanceThreshold == 0 {
		config.RelevanceThreshold = 0.5
	}
	if config.MaxContextItems == 0 {
		config.MaxContextItems = 50
	}

	return &ContextTools{
		config:           config,
		contextManager:   contextManager,
		fileDiscovery:    fileDiscovery,
		mentionProcessor: mentionProcessor,
		parser:           parser,
	}
}

// GetContextRequest represents a request to get current context
type GetContextRequest struct {
	IncludeFiles     bool     `json:"include_files,omitempty"`
	IncludeEdits     bool     `json:"include_edits,omitempty"`
	IncludeMentions  bool     `json:"include_mentions,omitempty"`
	IncludeLogs      bool     `json:"include_logs,omitempty"`
	FilterTypes      []string `json:"filter_types,omitempty"`
	MaxItems         int      `json:"max_items,omitempty"`
	SinceTimestamp   int64    `json:"since_timestamp,omitempty"`
}

// GetContextResponse represents the response from getting context
type GetContextResponse struct {
	Success      bool                         `json:"success"`
	Context      *appcontext.ContextWindow       `json:"context"`
	Items        []appcontext.ContextItem        `json:"items"`
	TotalTokens  int                          `json:"total_tokens"`
	TotalItems   int                          `json:"total_items"`
	Truncated    bool                         `json:"truncated"`
	Strategy     string                       `json:"strategy,omitempty"`
	Error        string                       `json:"error,omitempty"`
	Metadata     map[string]interface{}       `json:"metadata,omitempty"`
}

// AddContextRequest represents a request to add context
type AddContextRequest struct {
	Type        string                 `json:"type"`
	FilePath    string                 `json:"file_path,omitempty"`
	Content     string                 `json:"content"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Priority    float64                `json:"priority,omitempty"`
}

// AddContextResponse represents the response from adding context
type AddContextResponse struct {
	Success   bool                   `json:"success"`
	ItemID    string                 `json:"item_id,omitempty"`
	TokenCount int                   `json:"token_count"`
	Error     string                 `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ProcessMentionsRequest represents a request to process @-mentions
type ProcessMentionsRequest struct {
	Text         string `json:"text"`
	ExpandInline bool   `json:"expand_inline,omitempty"`
	TrackContext bool   `json:"track_context,omitempty"`
}

// ProcessMentionsResponse represents the response from processing mentions
type ProcessMentionsResponse struct {
	Success        bool                      `json:"success"`
	OriginalText   string                    `json:"original_text"`
	ExpandedText   string                    `json:"expanded_text,omitempty"`
	Mentions       []appcontext.Mention         `json:"mentions"`
	TotalTokens    int                       `json:"total_tokens"`
	Error          string                    `json:"error,omitempty"`
	Metadata       map[string]interface{}    `json:"metadata,omitempty"`
}

// GetRelevantFilesRequest represents a request to get relevant files
type GetRelevantFilesRequest struct {
	Query          string   `json:"query"`
	CurrentFile    string   `json:"current_file,omitempty"`
	FileTypes      []string `json:"file_types,omitempty"`
	MaxResults     int      `json:"max_results,omitempty"`
	IncludeContent bool     `json:"include_content,omitempty"`
	UseContext     bool     `json:"use_context,omitempty"`
}

// GetRelevantFilesResponse represents the response from getting relevant files
type GetRelevantFilesResponse struct {
	Success    bool                   `json:"success"`
	Query      string                 `json:"query"`
	Files      []RelevantFile         `json:"files"`
	Count      int                    `json:"count"`
	Error      string                 `json:"error,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// RelevantFile represents a file with relevance scoring
type RelevantFile struct {
	FilePath      string  `json:"file_path"`
	RelativePath  string  `json:"relative_path"`
	RelevanceScore float64 `json:"relevance_score"`
	Language      string  `json:"language,omitempty"`
	Size          int64   `json:"size"`
	ModTime       string  `json:"mod_time"`
	Reason        string  `json:"reason,omitempty"`
	Preview       string  `json:"preview,omitempty"`
	Context       string  `json:"context,omitempty"`
}

// GetContext implements the get_context MCP tool
func (ct *ContextTools) GetContext(ctx context.Context, request GetContextRequest) (*GetContextResponse, error) {
	response := &GetContextResponse{
		Metadata: make(map[string]interface{}),
	}

	if ct.contextManager == nil {
		response.Error = "context manager not available"
		return response, fmt.Errorf("context manager not available")
	}

	// Get current context window
	window := ct.contextManager.GetCurrentWindow()
	response.Context = window
	response.TotalTokens = window.TotalTokens
	response.Truncated = window.Truncated
	response.Strategy = window.Strategy

	// Filter context items based on request
	var filteredItems []appcontext.ContextItem
	
	for _, item := range window.Items {
		// Apply type filters
		if len(request.FilterTypes) > 0 {
			typeMatch := false
			for _, filterType := range request.FilterTypes {
				if string(item.Type) == filterType {
					typeMatch = true
					break
				}
			}
			if !typeMatch {
				continue
			}
		}

		// Apply specific inclusion filters
		include := true
		switch item.Type {
		case appcontext.ContextTypeFile:
			include = request.IncludeFiles
		case appcontext.ContextTypeEdit:
			include = request.IncludeEdits
		case appcontext.ContextTypeMention:
			include = request.IncludeMentions
		case appcontext.ContextTypeLog:
			include = request.IncludeLogs
		}

		if !include {
			continue
		}

		// Apply timestamp filter
		if request.SinceTimestamp > 0 {
			if item.Timestamp.Unix() < request.SinceTimestamp {
				continue
			}
		}

		filteredItems = append(filteredItems, item)
	}

	// Apply max items limit
	maxItems := request.MaxItems
	if maxItems == 0 {
		maxItems = ct.config.MaxContextItems
	}

	if len(filteredItems) > maxItems {
		// Sort by priority and recency
		sort.Slice(filteredItems, func(i, j int) bool {
			if filteredItems[i].Priority != filteredItems[j].Priority {
				return filteredItems[i].Priority > filteredItems[j].Priority
			}
			return filteredItems[i].LastAccess.After(filteredItems[j].LastAccess)
		})
		filteredItems = filteredItems[:maxItems]
	}

	response.Success = true
	response.Items = filteredItems
	response.TotalItems = len(filteredItems)
	response.Metadata["filter_applied"] = len(request.FilterTypes) > 0
	response.Metadata["original_item_count"] = len(window.Items)

	return response, nil
}

// AddContext implements the add_context MCP tool
func (ct *ContextTools) AddContext(ctx context.Context, request AddContextRequest) (*AddContextResponse, error) {
	response := &AddContextResponse{
		Metadata: make(map[string]interface{}),
	}

	if ct.contextManager == nil {
		response.Error = "context manager not available"
		return response, fmt.Errorf("context manager not available")
	}

	// Validate request
	if request.Content == "" {
		response.Error = "content is required"
		return response, fmt.Errorf("content is required")
	}

	// Convert type string to ContextType
	var contextType appcontext.ContextType
	switch request.Type {
	case "file":
		contextType = appcontext.ContextTypeFile
	case "edit":
		contextType = appcontext.ContextTypeEdit
	case "mention":
		contextType = appcontext.ContextTypeMention
	case "log":
		contextType = appcontext.ContextTypeLog
	case "diagnostic":
		contextType = appcontext.ContextTypeDiagnostic
	case "git":
		contextType = appcontext.ContextTypeGit
	case "definition":
		contextType = appcontext.ContextTypeDefinition
	default:
		response.Error = fmt.Sprintf("unknown context type: %s", request.Type)
		return response, fmt.Errorf("unknown context type: %s", request.Type)
	}

	// Add metadata
	metadata := request.Metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	
	if request.Priority > 0 {
		metadata["priority"] = request.Priority
	}
	metadata["added_via"] = "mcp_tool"
	metadata["timestamp"] = time.Now().Unix()

	// Add to context manager
	err := ct.contextManager.AddContext(contextType, request.FilePath, request.Content, metadata)
	if err != nil {
		response.Error = fmt.Sprintf("failed to add context: %v", err)
		return response, err
	}

	response.Success = true
	response.TokenCount = len(request.Content) / 4 // Rough estimate
	response.Metadata["context_type"] = request.Type
	response.Metadata["file_path"] = request.FilePath

	return response, nil
}

// ProcessMentions implements the process_mentions MCP tool
func (ct *ContextTools) ProcessMentions(ctx context.Context, request ProcessMentionsRequest) (*ProcessMentionsResponse, error) {
	response := &ProcessMentionsResponse{
		OriginalText: request.Text,
		Metadata:     make(map[string]interface{}),
	}

	if ct.mentionProcessor == nil {
		response.Error = "mention processor not available"
		return response, fmt.Errorf("mention processor not available")
	}

	// Parse mentions
	mentions, err := ct.mentionProcessor.ParseMentions(request.Text)
	if err != nil {
		response.Error = fmt.Sprintf("failed to parse mentions: %v", err)
		return response, err
	}

	// Process each mention
	var processedMentions []appcontext.Mention
	totalTokens := 0

	for _, mention := range mentions {
		processed, err := ct.mentionProcessor.ProcessMention(mention)
		if err != nil {
			processed.Error = err.Error()
		}

		processedMentions = append(processedMentions, processed)
		totalTokens += processed.TokenCount

		// Track in context if requested
		if request.TrackContext && ct.config.AutoTrack && ct.contextManager != nil && processed.Error == "" {
			metadata := map[string]interface{}{
				"mention_type": string(processed.Type),
				"mention_raw":  processed.Raw,
				"processed_via": "mcp_tool",
			}

			err = ct.contextManager.AddContext(
				appcontext.ContextTypeMention,
				processed.Path,
				processed.Content,
				metadata,
			)
			if err != nil {
				// Log error but don't fail the operation
				response.Metadata["context_error"] = err.Error()
			}
		}
	}

	// Expand text if requested
	if request.ExpandInline {
		expandedText, _, err := ct.mentionProcessor.ExpandText(request.Text)
		if err != nil {
			response.Error = fmt.Sprintf("failed to expand text: %v", err)
			return response, err
		}
		response.ExpandedText = expandedText
	}

	response.Success = true
	response.Mentions = processedMentions
	response.TotalTokens = totalTokens
	response.Metadata["mention_count"] = len(processedMentions)
	response.Metadata["unique_types"] = ct.getUniqueMentionTypes(processedMentions)

	return response, nil
}

// GetRelevantFiles implements the get_relevant_files MCP tool
func (ct *ContextTools) GetRelevantFiles(ctx context.Context, request GetRelevantFilesRequest) (*GetRelevantFilesResponse, error) {
	response := &GetRelevantFilesResponse{
		Query:    request.Query,
		Files:    make([]RelevantFile, 0),
		Metadata: make(map[string]interface{}),
	}

	if request.Query == "" {
		response.Error = "query is required"
		return response, fmt.Errorf("query is required")
	}

	// Set defaults
	maxResults := request.MaxResults
	if maxResults == 0 {
		maxResults = 10
	}

	var candidateFiles []RelevantFile

	// Search using file discovery
	if ct.fileDiscovery != nil {
		searchOptions := appcontext.SearchOptions{
			Pattern:     request.Query,
			Extensions:  request.FileTypes,
			MaxResults:  maxResults * 3, // Get more candidates for scoring
			Concurrent:  true,
		}

		results, err := ct.fileDiscovery.Search(ctx, searchOptions)
		if err != nil {
			response.Error = fmt.Sprintf("file search failed: %v", err)
			return response, err
		}

		for _, result := range results {
			relevantFile := RelevantFile{
				FilePath:     result.Path,
				RelativePath: result.RelativePath,
				Size:         result.Size,
				ModTime:      result.ModTime.Format(time.RFC3339),
				Reason:       "File name/path match",
			}

			// Calculate initial relevance score based on search score
			if result.Score != 0 {
				relevantFile.RelevanceScore = float64(result.Score) / 100.0
			} else {
				relevantFile.RelevanceScore = 0.5 // Default for exact matches
			}

			// Detect language
			if ct.parser != nil {
				language := ct.parser.DetectLanguage(result.Path)
				relevantFile.Language = string(language)
			}

			candidateFiles = append(candidateFiles, relevantFile)
		}
	}

	// Enhance relevance scoring using context if available
	if request.UseContext && ct.contextManager != nil {
		contextWindow := ct.contextManager.GetCurrentWindow()
		
		// Boost scores for files already in context
		contextFileMap := make(map[string]bool)
		for _, item := range contextWindow.Items {
			if item.FilePath != "" {
				contextFileMap[item.FilePath] = true
			}
		}

		for i := range candidateFiles {
			if contextFileMap[candidateFiles[i].FilePath] {
				candidateFiles[i].RelevanceScore += 0.2
				candidateFiles[i].Reason += " + In current context"
			}
		}
	}

	// Boost relevance for files related to current file
	if request.CurrentFile != "" {
		currentDir := filepath.Dir(request.CurrentFile)
		currentExt := filepath.Ext(request.CurrentFile)

		for i := range candidateFiles {
			file := &candidateFiles[i]
			
			// Same directory boost
			if strings.HasPrefix(file.FilePath, currentDir) {
				file.RelevanceScore += 0.15
				file.Reason += " + Same directory"
			}

			// Same extension boost
			if filepath.Ext(file.FilePath) == currentExt {
				file.RelevanceScore += 0.1
				file.Reason += " + Same file type"
			}
		}
	}

	// Filter by relevance threshold
	var relevantFiles []RelevantFile
	for _, file := range candidateFiles {
		if file.RelevanceScore >= ct.config.RelevanceThreshold {
			relevantFiles = append(relevantFiles, file)
		}
	}

	// Sort by relevance score
	sort.Slice(relevantFiles, func(i, j int) bool {
		return relevantFiles[i].RelevanceScore > relevantFiles[j].RelevanceScore
	})

	// Limit results
	if len(relevantFiles) > maxResults {
		relevantFiles = relevantFiles[:maxResults]
	}

	// Add content preview if requested
	if request.IncludeContent {
		for i := range relevantFiles {
			if relevantFiles[i].Size < 10240 { // 10KB limit
				// This would integrate with file editor to read content
				relevantFiles[i].Preview = fmt.Sprintf("Preview of %s would be shown here", relevantFiles[i].FilePath)
			}
		}
	}

	response.Success = true
	response.Files = relevantFiles
	response.Count = len(relevantFiles)
	response.Metadata["candidates_found"] = len(candidateFiles)
	response.Metadata["relevance_threshold"] = ct.config.RelevanceThreshold

	return response, nil
}

// ClearContextRequest represents a request to clear context
type ClearContextRequest struct {
	Types     []string `json:"types,omitempty"`
	OlderThan int64    `json:"older_than,omitempty"` // Unix timestamp
	FilePath  string   `json:"file_path,omitempty"`
}

// ClearContextResponse represents the response from clearing context
type ClearContextResponse struct {
	Success      bool                   `json:"success"`
	ItemsRemoved int                    `json:"items_removed"`
	Error        string                 `json:"error,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ClearContext implements the clear_context MCP tool
func (ct *ContextTools) ClearContext(ctx context.Context, request ClearContextRequest) (*ClearContextResponse, error) {
	response := &ClearContextResponse{
		Metadata: make(map[string]interface{}),
	}

	if ct.contextManager == nil {
		response.Error = "context manager not available"
		return response, fmt.Errorf("context manager not available")
	}

	itemsRemoved := 0

	// Clear by file path
	if request.FilePath != "" {
		err := ct.contextManager.RemoveContextByFile(request.FilePath)
		if err != nil {
			response.Error = fmt.Sprintf("failed to remove context for file: %v", err)
			return response, err
		}
		itemsRemoved++ // Simplified count
	}

	// Clear expired items
	if request.OlderThan > 0 {
		err := ct.contextManager.CleanupExpired()
		if err != nil {
			response.Error = fmt.Sprintf("failed to cleanup expired context: %v", err)
			return response, err
		}
		itemsRemoved++ // Simplified count
	}

	// Clear by types (would need additional context manager methods)
	if len(request.Types) > 0 {
		// This would require additional implementation in context manager
		response.Metadata["types_filter"] = request.Types
	}

	response.Success = true
	response.ItemsRemoved = itemsRemoved
	response.Metadata["operation_time"] = time.Now().Unix()

	return response, nil
}

// GetContextStatsRequest represents a request for context statistics
type GetContextStatsRequest struct {
	IncludeDetails bool `json:"include_details,omitempty"`
}

// GetContextStatsResponse represents context statistics
type GetContextStatsResponse struct {
	Success    bool                   `json:"success"`
	Stats      map[string]interface{} `json:"stats"`
	Error      string                 `json:"error,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// GetContextStats implements the get_context_stats MCP tool
func (ct *ContextTools) GetContextStats(ctx context.Context, request GetContextStatsRequest) (*GetContextStatsResponse, error) {
	response := &GetContextStatsResponse{
		Metadata: make(map[string]interface{}),
	}

	if ct.contextManager == nil {
		response.Error = "context manager not available"
		return response, fmt.Errorf("context manager not available")
	}

	// Get stats from context manager
	stats := ct.contextManager.GetStats()
	response.Stats = stats

	// Add additional details if requested
	if request.IncludeDetails {
		window := ct.contextManager.GetCurrentWindow()
		response.Stats["current_window"] = map[string]interface{}{
			"total_tokens": window.TotalTokens,
			"max_tokens":   window.MaxTokens,
			"item_count":   len(window.Items),
			"truncated":    window.Truncated,
			"strategy":     window.Strategy,
		}
	}

	response.Success = true
	response.Metadata["timestamp"] = time.Now().Unix()

	return response, nil
}

// Helper methods

func (ct *ContextTools) getUniqueMentionTypes(mentions []appcontext.Mention) []string {
	typeSet := make(map[string]bool)
	for _, mention := range mentions {
		typeSet[string(mention.Type)] = true
	}

	var types []string
	for t := range typeSet {
		types = append(types, t)
	}

	return types
}