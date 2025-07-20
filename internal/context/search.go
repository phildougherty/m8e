package context

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/sahilm/fuzzy"
)

// SearchResult represents a file discovery result
type SearchResult struct {
	Path         string    `json:"path"`
	RelativePath string    `json:"relative_path"`
	Type         string    `json:"type"` // file, directory, symlink
	Size         int64     `json:"size"`
	ModTime      time.Time `json:"mod_time"`
	GitStatus    string    `json:"git_status,omitempty"`
	Score        int       `json:"score,omitempty"` // For fuzzy matching
	Matches      []string  `json:"matches,omitempty"` // Matched portions
}

// SearchOptions configures file discovery behavior
type SearchOptions struct {
	// Base search directory
	Root string `json:"root"`
	
	// Pattern matching
	Pattern    string   `json:"pattern,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
	Include    []string `json:"include,omitempty"` // Include patterns
	Exclude    []string `json:"exclude,omitempty"` // Exclude patterns
	
	// Search behavior
	MaxResults    int  `json:"max_results"`
	MaxDepth      int  `json:"max_depth"`
	FollowSymlinks bool `json:"follow_symlinks"`
	IncludeHidden bool `json:"include_hidden"`
	
	// Git integration
	RespectGitignore bool `json:"respect_gitignore"`
	IncludeGitStatus bool `json:"include_git_status"`
	
	// Performance
	Concurrent bool `json:"concurrent"`
	Timeout    time.Duration `json:"timeout"`
}

// FileDiscovery provides high-performance file discovery with git integration
type FileDiscovery struct {
	gitRepo     *git.Repository
	gitignore   []string
	gitPatterns []*regexp.Regexp
	workDir     string
	mutex       sync.RWMutex
}

// NewFileDiscovery creates a new file discovery instance
func NewFileDiscovery(workDir string) (*FileDiscovery, error) {
	fd := &FileDiscovery{
		workDir: workDir,
	}

	// Try to load git repository
	if repo, err := git.PlainOpen(workDir); err == nil {
		fd.gitRepo = repo
		if err := fd.loadGitignore(); err != nil {
			// Log error but don't fail
			fmt.Printf("Warning: failed to load gitignore: %v\n", err)
		}
	}

	return fd, nil
}

// Search performs file discovery with the given options
func (fd *FileDiscovery) Search(ctx context.Context, options SearchOptions) ([]SearchResult, error) {
	if options.Root == "" {
		options.Root = fd.workDir
	}
	if options.MaxResults == 0 {
		options.MaxResults = 1000
	}
	if options.MaxDepth == 0 {
		options.MaxDepth = 10
	}
	if options.Timeout == 0 {
		options.Timeout = 30 * time.Second
	}

	// Create context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()

	// Compile patterns
	includePatterns, err := fd.compilePatterns(options.Include)
	if err != nil {
		return nil, fmt.Errorf("failed to compile include patterns: %w", err)
	}
	
	excludePatterns, err := fd.compilePatterns(options.Exclude)
	if err != nil {
		return nil, fmt.Errorf("failed to compile exclude patterns: %w", err)
	}

	// Perform search
	var results []SearchResult
	if options.Concurrent {
		results, err = fd.searchConcurrent(timeoutCtx, options, includePatterns, excludePatterns)
	} else {
		results, err = fd.searchSequential(timeoutCtx, options, includePatterns, excludePatterns)
	}

	if err != nil {
		return nil, err
	}

	// Apply fuzzy matching if pattern is provided
	if options.Pattern != "" {
		results = fd.applyFuzzyMatching(results, options.Pattern)
	}

	// Sort results by relevance
	fd.sortResults(results, options.Pattern)

	// Limit results
	if len(results) > options.MaxResults {
		results = results[:options.MaxResults]
	}

	return results, nil
}

// SearchFiles is a convenience method for finding files by pattern
func (fd *FileDiscovery) SearchFiles(pattern string, maxResults int) ([]SearchResult, error) {
	options := SearchOptions{
		Pattern:          pattern,
		MaxResults:       maxResults,
		RespectGitignore: true,
		IncludeGitStatus: true,
		Concurrent:       true,
	}
	
	return fd.Search(context.Background(), options)
}

// SearchByExtension finds files with specific extensions
func (fd *FileDiscovery) SearchByExtension(extensions []string, maxResults int) ([]SearchResult, error) {
	options := SearchOptions{
		Extensions:       extensions,
		MaxResults:       maxResults,
		RespectGitignore: true,
		Concurrent:       true,
	}
	
	return fd.Search(context.Background(), options)
}

// FindRecentFiles finds recently modified files
func (fd *FileDiscovery) FindRecentFiles(since time.Time, maxResults int) ([]SearchResult, error) {
	options := SearchOptions{
		MaxResults:       maxResults,
		RespectGitignore: true,
		IncludeGitStatus: true,
		Concurrent:       true,
	}
	
	results, err := fd.Search(context.Background(), options)
	if err != nil {
		return nil, err
	}

	// Filter by modification time
	var filtered []SearchResult
	for _, result := range results {
		if result.ModTime.After(since) {
			filtered = append(filtered, result)
		}
	}

	// Sort by modification time (newest first)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ModTime.After(filtered[j].ModTime)
	})

	if len(filtered) > maxResults {
		filtered = filtered[:maxResults]
	}

	return filtered, nil
}

// GetDirectoryStructure returns a tree-like structure of the directory
func (fd *FileDiscovery) GetDirectoryStructure(rootPath string, maxDepth int) (map[string]interface{}, error) {
	if rootPath == "" {
		rootPath = fd.workDir
	}

	structure := make(map[string]interface{})
	
	err := fd.buildDirectoryStructure(rootPath, structure, 0, maxDepth)
	if err != nil {
		return nil, err
	}

	return structure, nil
}

// Private methods

func (fd *FileDiscovery) searchSequential(ctx context.Context, options SearchOptions, include, exclude []*regexp.Regexp) ([]SearchResult, error) {
	var results []SearchResult
	
	err := filepath.Walk(options.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check depth limit
		relPath, err := filepath.Rel(options.Root, path)
		if err != nil {
			return nil
		}
		
		depth := strings.Count(relPath, string(filepath.Separator))
		if depth > options.MaxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Apply filters
		if !fd.shouldInclude(path, relPath, info, options, include, exclude) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Create search result
		result := SearchResult{
			Path:         path,
			RelativePath: relPath,
			Type:         fd.getFileType(info),
			Size:         info.Size(),
			ModTime:      info.ModTime(),
		}

		// Add git status if requested
		if options.IncludeGitStatus && fd.gitRepo != nil {
			result.GitStatus = fd.getGitStatus(relPath)
		}

		results = append(results, result)

		// Check result limit
		if len(results) >= options.MaxResults*2 { // Allow some buffer for filtering
			return fmt.Errorf("result limit exceeded")
		}

		return nil
	})

	return results, err
}

func (fd *FileDiscovery) searchConcurrent(ctx context.Context, options SearchOptions, include, exclude []*regexp.Regexp) ([]SearchResult, error) {
	resultChan := make(chan SearchResult, 1000)
	errorChan := make(chan error, 1)
	doneChan := make(chan bool, 1)

	// Start walker goroutine
	go func() {
		defer close(resultChan)
		
		err := filepath.Walk(options.Root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip errors
			}

			// Check context cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			relPath, err := filepath.Rel(options.Root, path)
			if err != nil {
				return nil
			}
			
			depth := strings.Count(relPath, string(filepath.Separator))
			if depth > options.MaxDepth {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			if !fd.shouldInclude(path, relPath, info, options, include, exclude) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			result := SearchResult{
				Path:         path,
				RelativePath: relPath,
				Type:         fd.getFileType(info),
				Size:         info.Size(),
				ModTime:      info.ModTime(),
			}

			if options.IncludeGitStatus && fd.gitRepo != nil {
				result.GitStatus = fd.getGitStatus(relPath)
			}

			select {
			case resultChan <- result:
			case <-ctx.Done():
				return ctx.Err()
			}

			return nil
		})

		if err != nil {
			errorChan <- err
		}
		doneChan <- true
	}()

	// Collect results
	var results []SearchResult
	for {
		select {
		case result, ok := <-resultChan:
			if !ok {
				goto done
			}
			results = append(results, result)
			if len(results) >= options.MaxResults*2 {
				goto done
			}
		case err := <-errorChan:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-doneChan:
			// Drain remaining results
			for result := range resultChan {
				results = append(results, result)
			}
			goto done
		}
	}

done:
	return results, nil
}

func (fd *FileDiscovery) shouldInclude(path, relPath string, info os.FileInfo, options SearchOptions, include, exclude []*regexp.Regexp) bool {
	// Skip hidden files unless explicitly included
	if !options.IncludeHidden {
		if strings.HasPrefix(filepath.Base(path), ".") && filepath.Base(path) != "." {
			return false
		}
	}

	// Skip symbolic links unless following them
	if info.Mode()&os.ModeSymlink != 0 && !options.FollowSymlinks {
		return false
	}

	// Check gitignore patterns
	if options.RespectGitignore && fd.isGitIgnored(relPath) {
		return false
	}

	// Check exclude patterns
	for _, pattern := range exclude {
		if pattern.MatchString(relPath) || pattern.MatchString(filepath.Base(path)) {
			return false
		}
	}

	// Check include patterns (if any)
	if len(include) > 0 {
		included := false
		for _, pattern := range include {
			if pattern.MatchString(relPath) || pattern.MatchString(filepath.Base(path)) {
				included = true
				break
			}
		}
		if !included {
			return false
		}
	}

	// Check extensions (for files only)
	if !info.IsDir() && len(options.Extensions) > 0 {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != "" {
			ext = ext[1:] // Remove leading dot
		}
		
		found := false
		for _, allowedExt := range options.Extensions {
			if strings.ToLower(allowedExt) == ext {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func (fd *FileDiscovery) applyFuzzyMatching(results []SearchResult, pattern string) []SearchResult {
	if pattern == "" {
		return results
	}

	// Create candidates for fuzzy matching
	var candidates []string
	for _, result := range results {
		candidates = append(candidates, result.RelativePath)
	}

	// Perform fuzzy matching
	matches := fuzzy.Find(pattern, candidates)

	// Create new results with scores
	var fuzzyResults []SearchResult
	for _, match := range matches {
		// Find original result
		for _, result := range results {
			if result.RelativePath == match.Str {
				result.Score = match.Score
				result.Matches = make([]string, len(match.MatchedIndexes))
				for j, idx := range match.MatchedIndexes {
					result.Matches[j] = string(match.Str[idx])
				}
				fuzzyResults = append(fuzzyResults, result)
				break
			}
		}
	}

	return fuzzyResults
}

func (fd *FileDiscovery) sortResults(results []SearchResult, pattern string) {
	sort.Slice(results, func(i, j int) bool {
		// If we have fuzzy scores, use them
		if pattern != "" && (results[i].Score != 0 || results[j].Score != 0) {
			return results[i].Score > results[j].Score
		}

		// Otherwise sort by type (files first), then by name
		if results[i].Type != results[j].Type {
			if results[i].Type == "file" {
				return true
			}
			if results[j].Type == "file" {
				return false
			}
		}

		// Sort by path length (shorter paths first)
		if len(results[i].RelativePath) != len(results[j].RelativePath) {
			return len(results[i].RelativePath) < len(results[j].RelativePath)
		}

		// Finally sort alphabetically
		return results[i].RelativePath < results[j].RelativePath
	})
}

func (fd *FileDiscovery) loadGitignore() error {
	gitignorePath := filepath.Join(fd.workDir, ".gitignore")
	
	file, err := os.Open(gitignorePath)
	if err != nil {
		return nil // .gitignore doesn't exist, which is fine
	}
	defer file.Close()

	var patterns []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}

	fd.gitignore = patterns

	// Compile patterns into regexes
	for _, pattern := range patterns {
		regex, err := fd.gitignorePatternToRegex(pattern)
		if err == nil {
			fd.gitPatterns = append(fd.gitPatterns, regex)
		}
	}

	return scanner.Err()
}

func (fd *FileDiscovery) isGitIgnored(path string) bool {
	for _, pattern := range fd.gitPatterns {
		if pattern.MatchString(path) {
			return true
		}
	}
	return false
}

func (fd *FileDiscovery) getGitStatus(path string) string {
	if fd.gitRepo == nil {
		return ""
	}

	// This is a simplified git status - in a real implementation,
	// you'd use git.PlainOpen and check the working tree status
	worktree, err := fd.gitRepo.Worktree()
	if err != nil {
		return ""
	}

	status, err := worktree.Status()
	if err != nil {
		return ""
	}

	if fileStatus, exists := status[path]; exists {
		return string(fileStatus.Staging) + string(fileStatus.Worktree)
	}

	return ""
}

func (fd *FileDiscovery) getFileType(info os.FileInfo) string {
	if info.IsDir() {
		return "directory"
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "symlink"
	}
	return "file"
}

func (fd *FileDiscovery) compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	var compiled []*regexp.Regexp
	
	for _, pattern := range patterns {
		// Convert glob-style patterns to regex
		regexPattern := fd.globToRegex(pattern)
		regex, err := regexp.Compile(regexPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern %s: %w", pattern, err)
		}
		compiled = append(compiled, regex)
	}
	
	return compiled, nil
}

func (fd *FileDiscovery) globToRegex(pattern string) string {
	// Simple glob to regex conversion
	pattern = strings.ReplaceAll(pattern, ".", "\\.")
	pattern = strings.ReplaceAll(pattern, "*", ".*")
	pattern = strings.ReplaceAll(pattern, "?", ".")
	return "^" + pattern + "$"
}

func (fd *FileDiscovery) gitignorePatternToRegex(pattern string) (*regexp.Regexp, error) {
	// Simple gitignore pattern to regex conversion
	// This is a simplified implementation
	
	// Handle directory patterns
	if strings.HasSuffix(pattern, "/") {
		pattern = pattern[:len(pattern)-1]
		pattern = pattern + "($|/.*)"
	}
	
	// Handle wildcards
	pattern = strings.ReplaceAll(pattern, ".", "\\.")
	pattern = strings.ReplaceAll(pattern, "*", "[^/]*")
	pattern = strings.ReplaceAll(pattern, "?", "[^/]")
	
	// Handle double asterisk
	pattern = strings.ReplaceAll(pattern, "[^/]*[^/]*", ".*")
	
	return regexp.Compile(pattern)
}

func (fd *FileDiscovery) buildDirectoryStructure(root string, structure map[string]interface{}, currentDepth, maxDepth int) error {
	if currentDepth >= maxDepth {
		return nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		// Skip hidden files/directories
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		path := filepath.Join(root, entry.Name())
		
		if entry.IsDir() {
			subStructure := make(map[string]interface{})
			err := fd.buildDirectoryStructure(path, subStructure, currentDepth+1, maxDepth)
			if err == nil {
				structure[entry.Name()+"/"] = subStructure
			}
		} else {
			info, err := entry.Info()
			if err == nil {
				structure[entry.Name()] = map[string]interface{}{
					"size":     info.Size(),
					"mod_time": info.ModTime(),
				}
			}
		}
	}

	return nil
}

// GetRepoRoot finds the git repository root
func (fd *FileDiscovery) GetRepoRoot() string {
	if fd.gitRepo == nil {
		return fd.workDir
	}

	worktree, err := fd.gitRepo.Worktree()
	if err != nil {
		return fd.workDir
	}

	return worktree.Filesystem.Root()
}

// GetGitBranch returns the current git branch
func (fd *FileDiscovery) GetGitBranch() string {
	if fd.gitRepo == nil {
		return ""
	}

	head, err := fd.gitRepo.Head()
	if err != nil {
		return ""
	}

	return head.Name().Short()
}

// GetRecentCommits returns recent commits for context
func (fd *FileDiscovery) GetRecentCommits(limit int) ([]map[string]interface{}, error) {
	if fd.gitRepo == nil {
		return nil, fmt.Errorf("not a git repository")
	}

	commitIter, err := fd.gitRepo.Log(&git.LogOptions{})
	if err != nil {
		return nil, err
	}
	defer commitIter.Close()

	var commits []map[string]interface{}
	count := 0
	
	err = commitIter.ForEach(func(commit *object.Commit) error {
		if count >= limit {
			return io.EOF
		}

		commits = append(commits, map[string]interface{}{
			"hash":      commit.Hash.String()[:8],
			"message":   strings.Split(commit.Message, "\n")[0],
			"author":    commit.Author.Name,
			"timestamp": commit.Author.When,
		})
		
		count++
		return nil
	})

	if err != nil && err != io.EOF {
		return nil, err
	}

	return commits, nil
}