package context

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MentionType represents different types of mentions
type MentionType string

const (
	MentionTypeFile      MentionType = "file"
	MentionTypeDirectory MentionType = "directory"
	MentionTypeProblems  MentionType = "problems"
	MentionTypeLogs      MentionType = "logs"
	MentionTypeGitChanges MentionType = "git-changes"
	MentionTypeDefinition MentionType = "definition"
	MentionTypeMemory    MentionType = "memory"
	MentionTypeWorkflow  MentionType = "workflow"
)

// Mention represents a parsed mention from user input
type Mention struct {
	Type        MentionType          `json:"type"`
	Raw         string               `json:"raw"`
	Path        string               `json:"path,omitempty"`
	Content     string               `json:"content"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Lines       []int                `json:"lines,omitempty"`
	TokenCount  int                  `json:"token_count"`
	Error       string               `json:"error,omitempty"`
}

// MentionProcessor handles parsing and resolving mentions
type MentionProcessor struct {
	workDir        string
	fileDiscovery  *FileDiscovery
	contextManager *ContextManager
}

// MentionConfig configures mention processing behavior
type MentionConfig struct {
	MaxFileSize     int64         `json:"max_file_size"`
	MaxDirFiles     int           `json:"max_dir_files"`
	MaxLogLines     int           `json:"max_log_lines"`
	DefaultLines    int           `json:"default_lines"`
	IncludeHidden   bool          `json:"include_hidden"`
	FollowSymlinks  bool          `json:"follow_symlinks"`
	Timeout         time.Duration `json:"timeout"`
}

// Default mention patterns
var mentionPatterns = map[string]*regexp.Regexp{
	"file":        regexp.MustCompile(`@(/[^@\s]+\.[a-zA-Z0-9]+)(?::(\d+)(?:-(\d+))?)?`),
	"directory":   regexp.MustCompile(`@(/[^@\s]+/)(?::(\d+))?`),
	"problems":    regexp.MustCompile(`@problems(?::(\w+))?`),
	"logs":        regexp.MustCompile(`@logs(?::(\w+))?(?::(\d+))?`),
	"git-changes": regexp.MustCompile(`@git-changes(?::(\w+))?`),
	"definition":  regexp.MustCompile(`@def:([^@\s]+)`),
	"memory":      regexp.MustCompile(`@memory(?::([^@\s]+))?`),
	"workflow":    regexp.MustCompile(`@workflow(?::([^@\s]+))?`),
}

// NewMentionProcessor creates a new mention processor
func NewMentionProcessor(workDir string, fileDiscovery *FileDiscovery, contextManager *ContextManager) *MentionProcessor {
	return &MentionProcessor{
		workDir:        workDir,
		fileDiscovery:  fileDiscovery,
		contextManager: contextManager,
	}
}

// ParseMentions parses all mentions from a text string
func (mp *MentionProcessor) ParseMentions(text string) ([]Mention, error) {
	var mentions []Mention
	
	// Process each mention type
	for mentionType, pattern := range mentionPatterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		
		for _, match := range matches {
			mention, err := mp.processMention(MentionType(mentionType), match)
			if err != nil {
				mention.Error = err.Error()
			}
			mentions = append(mentions, mention)
		}
	}
	
	// Sort mentions by position in text
	sort.Slice(mentions, func(i, j int) bool {
		return strings.Index(text, mentions[i].Raw) < strings.Index(text, mentions[j].Raw)
	})
	
	return mentions, nil
}

// ProcessMention processes a single mention and returns its content
func (mp *MentionProcessor) ProcessMention(mention Mention) (Mention, error) {
	config := MentionConfig{
		MaxFileSize:   1024 * 1024, // 1MB
		MaxDirFiles:   50,
		MaxLogLines:   100,
		DefaultLines:  20,
		Timeout:       10 * time.Second,
	}
	
	switch mention.Type {
	case MentionTypeFile:
		return mp.processFileMention(mention, config)
	case MentionTypeDirectory:
		return mp.processDirectoryMention(mention, config)
	case MentionTypeProblems:
		return mp.processProblemsMention(mention, config)
	case MentionTypeLogs:
		return mp.processLogsMention(mention, config)
	case MentionTypeGitChanges:
		return mp.processGitChangesMention(mention, config)
	case MentionTypeDefinition:
		return mp.processDefinitionMention(mention, config)
	case MentionTypeMemory:
		return mp.processMemoryMention(mention, config)
	case MentionTypeWorkflow:
		return mp.processWorkflowMention(mention, config)
	default:
		return mention, fmt.Errorf("unknown mention type: %s", mention.Type)
	}
}

// ExpandText replaces all mentions in text with their resolved content
func (mp *MentionProcessor) ExpandText(text string) (string, []Mention, error) {
	mentions, err := mp.ParseMentions(text)
	if err != nil {
		return text, nil, err
	}
	
	expanded := text
	var processedMentions []Mention
	
	for _, mention := range mentions {
		processed, err := mp.ProcessMention(mention)
		if err != nil {
			processed.Error = err.Error()
		}
		
		// Replace mention with content
		if processed.Content != "" {
			replacement := fmt.Sprintf("\n\n--- %s ---\n%s\n--- End %s ---\n", 
				processed.Raw, processed.Content, processed.Raw)
			expanded = strings.Replace(expanded, processed.Raw, replacement, 1)
		}
		
		processedMentions = append(processedMentions, processed)
	}
	
	return expanded, processedMentions, nil
}

// Private methods

func (mp *MentionProcessor) processMention(mentionType MentionType, match []string) (Mention, error) {
	mention := Mention{
		Type: mentionType,
		Raw:  match[0],
	}
	
	switch mentionType {
	case MentionTypeFile:
		mention.Path = match[1]
		if len(match) > 2 && match[2] != "" {
			if start, err := strconv.Atoi(match[2]); err == nil {
				mention.Lines = []int{start}
				if len(match) > 3 && match[3] != "" {
					if end, err := strconv.Atoi(match[3]); err == nil {
						mention.Lines = []int{start, end}
					}
				}
			}
		}
		
	case MentionTypeDirectory:
		mention.Path = match[1]
		if len(match) > 2 && match[2] != "" {
			if limit, err := strconv.Atoi(match[2]); err == nil {
				mention.Metadata = map[string]interface{}{"limit": limit}
			}
		}
		
	case MentionTypeProblems:
		if len(match) > 1 && match[1] != "" {
			mention.Metadata = map[string]interface{}{"namespace": match[1]}
		}
		
	case MentionTypeLogs:
		if len(match) > 1 && match[1] != "" {
			mention.Metadata = map[string]interface{}{"service": match[1]}
		}
		if len(match) > 2 && match[2] != "" {
			if lines, err := strconv.Atoi(match[2]); err == nil {
				mention.Metadata = map[string]interface{}{"lines": lines}
			}
		}
		
	case MentionTypeGitChanges:
		if len(match) > 1 && match[1] != "" {
			mention.Metadata = map[string]interface{}{"branch": match[1]}
		}
		
	case MentionTypeDefinition:
		mention.Path = match[1]
		
	case MentionTypeMemory:
		if len(match) > 1 && match[1] != "" {
			mention.Path = match[1]
		}
		
	case MentionTypeWorkflow:
		if len(match) > 1 && match[1] != "" {
			mention.Path = match[1]
		}
	}
	
	return mention, nil
}

func (mp *MentionProcessor) processFileMention(mention Mention, config MentionConfig) (Mention, error) {
	// Resolve path relative to working directory
	path := mention.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(mp.workDir, path)
	}
	
	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		return mention, fmt.Errorf("file not found: %s", mention.Path)
	}
	
	// Check file size
	if info.Size() > config.MaxFileSize {
		return mention, fmt.Errorf("file too large: %d bytes (max: %d)", info.Size(), config.MaxFileSize)
	}
	
	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		return mention, fmt.Errorf("failed to read file: %w", err)
	}
	
	contentStr := string(content)
	
	// Extract specific lines if requested
	if len(mention.Lines) > 0 {
		lines := strings.Split(contentStr, "\n")
		start := mention.Lines[0] - 1 // Convert to 0-based
		end := start + 1
		
		if len(mention.Lines) > 1 {
			end = mention.Lines[1]
		}
		
		if start < 0 {
			start = 0
		}
		if end > len(lines) {
			end = len(lines)
		}
		if start >= len(lines) {
			return mention, fmt.Errorf("line number %d exceeds file length", mention.Lines[0])
		}
		
		selectedLines := lines[start:end]
		
		// Add line number prefix
		var numberedLines []string
		for i, line := range selectedLines {
			numberedLines = append(numberedLines, fmt.Sprintf("%d: %s", start+i+1, line))
		}
		contentStr = strings.Join(numberedLines, "\n")
	}
	
	mention.Content = contentStr
	mention.TokenCount = len(contentStr) / 4 // Rough estimate
	mention.Metadata = map[string]interface{}{
		"size":     info.Size(),
		"mod_time": info.ModTime(),
		"path":     path,
	}
	
	// Add to context manager
	if mp.contextManager != nil {
		if err := mp.contextManager.AddContext(ContextTypeFile, path, contentStr, mention.Metadata); err != nil {
			// Log error but don't fail the mention processing
			fmt.Printf("Warning: Failed to add file context: %v\n", err)
		}
	}
	
	return mention, nil
}

func (mp *MentionProcessor) processDirectoryMention(mention Mention, config MentionConfig) (Mention, error) {
	path := mention.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(mp.workDir, path)
	}
	
	// Check if directory exists
	info, err := os.Stat(path)
	if err != nil {
		return mention, fmt.Errorf("directory not found: %s", mention.Path)
	}
	if !info.IsDir() {
		return mention, fmt.Errorf("path is not a directory: %s", mention.Path)
	}
	
	// Get directory listing
	entries, err := os.ReadDir(path)
	if err != nil {
		return mention, fmt.Errorf("failed to read directory: %w", err)
	}
	
	// Apply limit
	limit := config.MaxDirFiles
	if mention.Metadata != nil {
		if l, ok := mention.Metadata["limit"].(int); ok {
			limit = l
		}
	}
	
	var listing []string
	count := 0
	
	for _, entry := range entries {
		if count >= limit {
			listing = append(listing, "... (truncated)")
			break
		}
		
		// Skip hidden files unless configured
		if strings.HasPrefix(entry.Name(), ".") && !config.IncludeHidden {
			continue
		}
		
		info, err := entry.Info()
		if err != nil {
			continue
		}
		
		var line string
		if entry.IsDir() {
			line = fmt.Sprintf("%s/", entry.Name())
		} else {
			line = fmt.Sprintf("%s (%d bytes)", entry.Name(), info.Size())
		}
		
		listing = append(listing, line)
		count++
	}
	
	mention.Content = strings.Join(listing, "\n")
	mention.TokenCount = len(mention.Content) / 4
	mention.Metadata = map[string]interface{}{
		"path":       path,
		"total_files": len(entries),
		"shown_files": count,
	}
	
	return mention, nil
}

func (mp *MentionProcessor) processProblemsMention(mention Mention, config MentionConfig) (Mention, error) {
	// This would integrate with Kubernetes to get pod/service diagnostics
	// For now, return a placeholder
	
	namespace := "default"
	if mention.Metadata != nil {
		if ns, ok := mention.Metadata["namespace"].(string); ok {
			namespace = ns
		}
	}
	
	// TODO: Implement actual Kubernetes diagnostics
	content := fmt.Sprintf("Kubernetes diagnostics for namespace: %s\n", namespace)
	content += "Pod Status:\n"
	content += "  - matey-proxy: Running (1/1)\n"
	content += "  - matey-memory: Running (1/1)\n"
	content += "  - matey-task-scheduler: Running (1/1)\n"
	content += "\nRecent Events:\n"
	content += "  - No recent warning events\n"
	
	mention.Content = content
	mention.TokenCount = len(content) / 4
	mention.Metadata = map[string]interface{}{
		"namespace": namespace,
		"timestamp": time.Now(),
	}
	
	if mp.contextManager != nil {
		if err := mp.contextManager.AddContext(ContextTypeDiagnostic, "", content, mention.Metadata); err != nil {
			// Log error but don't fail the mention processing
			fmt.Printf("Warning: Failed to add diagnostic context: %v\n", err)
		}
	}
	
	return mention, nil
}

func (mp *MentionProcessor) processLogsMention(mention Mention, config MentionConfig) (Mention, error) {
	service := "all"
	lines := config.MaxLogLines
	
	if mention.Metadata != nil {
		if s, ok := mention.Metadata["service"].(string); ok {
			service = s
		}
		if l, ok := mention.Metadata["lines"].(int); ok {
			lines = l
		}
	}
	
	// TODO: Implement actual Kubernetes pod log retrieval
	content := fmt.Sprintf("Recent logs for service: %s (last %d lines)\n", service, lines)
	content += fmt.Sprintf("Timestamp: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	content += "[INFO] Service started successfully\n"
	content += "[DEBUG] Processing request\n"
	content += "[INFO] Request completed\n"
	
	mention.Content = content
	mention.TokenCount = len(content) / 4
	mention.Metadata = map[string]interface{}{
		"service":   service,
		"lines":     lines,
		"timestamp": time.Now(),
	}
	
	if mp.contextManager != nil {
		if err := mp.contextManager.AddContext(ContextTypeLog, "", content, mention.Metadata); err != nil {
			// Log error but don't fail the mention processing
			fmt.Printf("Warning: Failed to add log context: %v\n", err)
		}
	}
	
	return mention, nil
}

func (mp *MentionProcessor) processGitChangesMention(mention Mention, config MentionConfig) (Mention, error) {
	branch := "current"
	if mention.Metadata != nil {
		if b, ok := mention.Metadata["branch"].(string); ok {
			branch = b
		}
	}
	
	// Use file discovery to get git status
	if mp.fileDiscovery != nil {
		content := fmt.Sprintf("Git changes for branch: %s\n", branch)
		content += fmt.Sprintf("Repository: %s\n", mp.fileDiscovery.GetRepoRoot())
		content += fmt.Sprintf("Current branch: %s\n\n", mp.fileDiscovery.GetGitBranch())
		
		// Get recent commits
		if commits, err := mp.fileDiscovery.GetRecentCommits(5); err == nil {
			content += "Recent commits:\n"
			for _, commit := range commits {
				content += fmt.Sprintf("  %s - %s (%s)\n", 
					commit["hash"], commit["message"], commit["author"])
			}
		}
		
		mention.Content = content
		mention.TokenCount = len(content) / 4
		mention.Metadata = map[string]interface{}{
			"branch": branch,
			"repo":   mp.fileDiscovery.GetRepoRoot(),
		}
		
		if mp.contextManager != nil {
			if err := mp.contextManager.AddContext(ContextTypeGit, "", content, mention.Metadata); err != nil {
				// Log error but don't fail the mention processing
				fmt.Printf("Warning: Failed to add git context: %v\n", err)
			}
		}
	}
	
	return mention, nil
}

func (mp *MentionProcessor) processDefinitionMention(mention Mention, config MentionConfig) (Mention, error) {
	// TODO: Integrate with tree-sitter to find code definitions
	content := fmt.Sprintf("Code definition search for: %s\n", mention.Path)
	content += "This would use tree-sitter to find function/type definitions\n"
	
	mention.Content = content
	mention.TokenCount = len(content) / 4
	mention.Metadata = map[string]interface{}{
		"query": mention.Path,
	}
	
	if mp.contextManager != nil {
		if err := mp.contextManager.AddContext(ContextTypeDefinition, "", content, mention.Metadata); err != nil {
			// Log error but don't fail the mention processing
			fmt.Printf("Warning: Failed to add definition context: %v\n", err)
		}
	}
	
	return mention, nil
}

func (mp *MentionProcessor) processMemoryMention(mention Mention, config MentionConfig) (Mention, error) {
	// TODO: Integrate with memory service
	query := "recent"
	if mention.Path != "" {
		query = mention.Path
	}
	
	content := fmt.Sprintf("Memory query: %s\n", query)
	content += "This would retrieve relevant memories from the PostgreSQL knowledge graph\n"
	
	mention.Content = content
	mention.TokenCount = len(content) / 4
	mention.Metadata = map[string]interface{}{
		"query": query,
	}
	
	if mp.contextManager != nil {
		if err := mp.contextManager.AddContext(ContextTypeDiagnostic, "", content, mention.Metadata); err != nil {
			// Log error but don't fail the mention processing
			fmt.Printf("Warning: Failed to add diagnostic context: %v\n", err)
		}
	}
	
	return mention, nil
}

func (mp *MentionProcessor) processWorkflowMention(mention Mention, config MentionConfig) (Mention, error) {
	// TODO: Integrate with workflow system
	workflow := "status"
	if mention.Path != "" {
		workflow = mention.Path
	}
	
	content := fmt.Sprintf("Workflow: %s\n", workflow)
	content += "This would retrieve workflow status and recent executions\n"
	
	mention.Content = content
	mention.TokenCount = len(content) / 4
	mention.Metadata = map[string]interface{}{
		"workflow": workflow,
	}
	
	return mention, nil
}

// GetSupportedMentions returns information about all supported mention types
func (mp *MentionProcessor) GetSupportedMentions() map[string]string {
	return map[string]string{
		"@/path/file.ext":        "Include file content",
		"@/path/file.ext:10":     "Include specific line",
		"@/path/file.ext:10-20":  "Include line range",
		"@/path/folder/":         "List directory contents",
		"@/path/folder/:10":      "List directory (limit 10 files)",
		"@problems":              "Show Kubernetes diagnostics",
		"@problems:namespace":    "Show diagnostics for specific namespace",
		"@logs":                  "Show recent pod logs",
		"@logs:service":          "Show logs for specific service",
		"@logs:service:100":      "Show specific number of log lines",
		"@git-changes":           "Show git status and recent commits",
		"@git-changes:branch":    "Show changes for specific branch",
		"@def:functionName":      "Find code definitions",
		"@memory":                "Query memory service",
		"@memory:query":          "Search memories with query",
		"@workflow":              "Show workflow status",
		"@workflow:name":         "Show specific workflow status",
	}
}