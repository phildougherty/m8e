package mcp

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FilesystemTools provides basic filesystem operations similar to Gemini CLI
type FilesystemTools struct {
	workingDirectory string
}

// NewFilesystemTools creates a new filesystem tools instance
func NewFilesystemTools(workingDirectory string) *FilesystemTools {
	if workingDirectory == "" {
		workingDirectory, _ = os.Getwd()
	}
	return &FilesystemTools{
		workingDirectory: workingDirectory,
	}
}

// BasicReadFileRequest represents a request to read a file
type BasicReadFileRequest struct {
	AbsolutePath string `json:"absolute_path"`
	Offset       int    `json:"offset,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

// BasicReadFileResult represents the result of reading a file
type BasicReadFileResult struct {
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

// BasicWriteFileRequest represents a request to write a file
type BasicWriteFileRequest struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

// BasicWriteFileResult represents the result of writing a file
type BasicWriteFileResult struct {
	Success  bool   `json:"success"`
	FilePath string `json:"file_path"`
	Error    string `json:"error,omitempty"`
}

// BasicListFilesRequest represents a request to list files
type BasicListFilesRequest struct {
	DirectoryPath string `json:"directory_path,omitempty"`
	Recursive     bool   `json:"recursive,omitempty"`
	MaxResults    int    `json:"max_results,omitempty"`
}

// FileInfo represents information about a file
type FileInfo struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	IsDirectory  bool   `json:"is_directory"`
	Size         int64  `json:"size"`
	ModTime      string `json:"mod_time"`
	Mode         string `json:"mode"`
}

// BasicListFilesResult represents the result of listing files
type BasicListFilesResult struct {
	Files []FileInfo `json:"files"`
	Count int        `json:"count"`
	Error string     `json:"error,omitempty"`
}

// BasicGetWorkingDirectoryResult represents the result of getting working directory
type BasicGetWorkingDirectoryResult struct {
	WorkingDirectory string `json:"working_directory"`
	AbsolutePath     string `json:"absolute_path"`
}

// ReadFile reads a file with optional line range
func (ft *FilesystemTools) ReadFile(ctx context.Context, request BasicReadFileRequest) (*BasicReadFileResult, error) {
	result := &BasicReadFileResult{}

	// Validate path
	if request.AbsolutePath == "" {
		result.Error = "absolute_path is required"
		return result, fmt.Errorf("absolute_path is required")
	}

	// Convert to absolute path if needed
	filePath := request.AbsolutePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(ft.workingDirectory, filePath)
	}

	// Check if path is within working directory (security check)
	if !ft.isWithinWorkingDirectory(filePath) {
		result.Error = fmt.Sprintf("path must be within working directory (%s): %s", ft.workingDirectory, filePath)
		return result, fmt.Errorf("path outside working directory")
	}

	// Read file
	content, err := os.ReadFile(filePath)
	if err != nil {
		result.Error = fmt.Sprintf("failed to read file: %v", err)
		return result, err
	}

	contentStr := string(content)

	// Handle line range if specified
	if request.Offset > 0 || request.Limit > 0 {
		lines := strings.Split(contentStr, "\n")
		
		start := request.Offset
		if start < 0 {
			start = 0
		}
		
		end := len(lines)
		if request.Limit > 0 {
			end = start + request.Limit
			if end > len(lines) {
				end = len(lines)
			}
		}
		
		if start >= len(lines) {
			result.Error = fmt.Sprintf("offset %d exceeds file length", request.Offset)
			return result, fmt.Errorf("offset exceeds file length")
		}
		
		selectedLines := lines[start:end]
		contentStr = strings.Join(selectedLines, "\n")
	}

	result.Content = contentStr
	return result, nil
}

// WriteFile writes content to a file
func (ft *FilesystemTools) WriteFile(ctx context.Context, request BasicWriteFileRequest) (*BasicWriteFileResult, error) {
	result := &BasicWriteFileResult{
		FilePath: request.FilePath,
	}

	// Validate path
	if request.FilePath == "" {
		result.Error = "file_path is required"
		return result, fmt.Errorf("file_path is required")
	}

	// Convert to absolute path if needed
	filePath := request.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(ft.workingDirectory, filePath)
	}

	// Check if path is within working directory (security check)
	if !ft.isWithinWorkingDirectory(filePath) {
		result.Error = fmt.Sprintf("path must be within working directory (%s): %s", ft.workingDirectory, filePath)
		return result, fmt.Errorf("path outside working directory")
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		result.Error = fmt.Sprintf("failed to create directory: %v", err)
		return result, err
	}

	// Write file
	if err := os.WriteFile(filePath, []byte(request.Content), 0644); err != nil {
		result.Error = fmt.Sprintf("failed to write file: %v", err)
		return result, err
	}

	result.Success = true
	return result, nil
}

// ListFiles lists files in a directory
func (ft *FilesystemTools) ListFiles(ctx context.Context, request BasicListFilesRequest) (*BasicListFilesResult, error) {
	result := &BasicListFilesResult{
		Files: make([]FileInfo, 0),
	}

	// Default to working directory if no path specified
	dirPath := request.DirectoryPath
	if dirPath == "" {
		dirPath = ft.workingDirectory
	}

	// Convert to absolute path if needed
	if !filepath.IsAbs(dirPath) {
		dirPath = filepath.Join(ft.workingDirectory, dirPath)
	}

	// Check if path is within working directory (security check)
	if !ft.isWithinWorkingDirectory(dirPath) {
		result.Error = fmt.Sprintf("path must be within working directory (%s): %s", ft.workingDirectory, dirPath)
		return result, fmt.Errorf("path outside working directory")
	}

	// Set default max results
	maxResults := request.MaxResults
	if maxResults == 0 {
		maxResults = 100
	}

	var files []FileInfo
	fileCount := 0

	if request.Recursive {
		// Recursive walk
		err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // Skip files we can't access
			}

			if fileCount >= maxResults {
				return filepath.SkipAll
			}

			info, err := d.Info()
			if err != nil {
				return nil // Skip files we can't stat
			}

			relPath, _ := filepath.Rel(ft.workingDirectory, path)
			fileInfo := FileInfo{
				Name:         d.Name(),
				Path:         path,
				RelativePath: relPath,
				IsDirectory:  d.IsDir(),
				Size:         info.Size(),
				ModTime:      info.ModTime().Format(time.RFC3339),
				Mode:         info.Mode().String(),
			}

			files = append(files, fileInfo)
			fileCount++
			return nil
		})

		if err != nil {
			result.Error = fmt.Sprintf("failed to walk directory: %v", err)
			return result, err
		}
	} else {
		// Non-recursive listing
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			result.Error = fmt.Sprintf("failed to read directory: %v", err)
			return result, err
		}

		for _, entry := range entries {
			if fileCount >= maxResults {
				break
			}

			info, err := entry.Info()
			if err != nil {
				continue // Skip entries we can't stat
			}

			fullPath := filepath.Join(dirPath, entry.Name())
			relPath, _ := filepath.Rel(ft.workingDirectory, fullPath)
			
			fileInfo := FileInfo{
				Name:         entry.Name(),
				Path:         fullPath,
				RelativePath: relPath,
				IsDirectory:  entry.IsDir(),
				Size:         info.Size(),
				ModTime:      info.ModTime().Format(time.RFC3339),
				Mode:         info.Mode().String(),
			}

			files = append(files, fileInfo)
			fileCount++
		}
	}

	result.Files = files
	result.Count = len(files)
	return result, nil
}

// GetWorkingDirectory returns the current working directory
func (ft *FilesystemTools) GetWorkingDirectory(ctx context.Context) (*BasicGetWorkingDirectoryResult, error) {
	absPath, err := filepath.Abs(ft.workingDirectory)
	if err != nil {
		absPath = ft.workingDirectory
	}

	return &BasicGetWorkingDirectoryResult{
		WorkingDirectory: ft.workingDirectory,
		AbsolutePath:     absPath,
	}, nil
}

// isWithinWorkingDirectory checks if a path is within the working directory
func (ft *FilesystemTools) isWithinWorkingDirectory(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	absWorkingDir, err := filepath.Abs(ft.workingDirectory)
	if err != nil {
		return false
	}

	// Check if the path is within or equal to the working directory
	rel, err := filepath.Rel(absWorkingDir, absPath)
	if err != nil {
		return false
	}

	// If rel starts with "..", it's outside the working directory
	return !strings.HasPrefix(rel, "..")
}