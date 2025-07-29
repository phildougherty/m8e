package edit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// FileEdit represents a single file editing operation
type FileEdit struct {
	FilePath    string `json:"file_path"`
	OldContent  string `json:"old_content,omitempty"`
	NewContent  string `json:"new_content"`
	Backup      string `json:"backup,omitempty"`
	Timestamp   int64  `json:"timestamp"`
	Applied     bool   `json:"applied"`
	Error       string `json:"error,omitempty"`
}

// MultiEditOperation represents a collection of related file edits
type MultiEditOperation struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Edits       []FileEdit `json:"edits"`
	Timestamp   int64      `json:"timestamp"`
	Applied     bool       `json:"applied"`
	Rollback    bool       `json:"rollback"`
}

// FileEditor handles file operations with backup, rollback, and validation
type FileEditor struct {
	backupDir     string
	maxBackups    int
	validateFunc  func(string, string) error
	k8sIntegrated bool
}

// EditorConfig configures the FileEditor behavior
type EditorConfig struct {
	BackupDir     string
	MaxBackups    int
	ValidateFunc  func(string, string) error
	K8sIntegrated bool
}

// NewFileEditor creates a new FileEditor with the given configuration
func NewFileEditor(config EditorConfig) *FileEditor {
	if config.BackupDir == "" {
		config.BackupDir = filepath.Join(os.TempDir(), "matey-edit-backups")
	}
	if config.MaxBackups == 0 {
		config.MaxBackups = 10
	}

	// Ensure backup directory exists
	if err := os.MkdirAll(config.BackupDir, 0755); err != nil {
		// Log error but continue - backup is optional
		fmt.Printf("Warning: Failed to create backup directory: %v\n", err)
	}

	return &FileEditor{
		backupDir:     config.BackupDir,
		maxBackups:    config.MaxBackups,
		validateFunc:  config.ValidateFunc,
		k8sIntegrated: config.K8sIntegrated,
	}
}

// ReadFile reads a file with automatic encoding detection
func (fe *FileEditor) ReadFile(filePath string) (string, error) {
	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", filePath)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: Failed to close file: %v\n", err)
		}
	}()

	// Read first chunk to detect encoding
	buffer := make([]byte, 1024)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Reset file position
	if _, err := file.Seek(0, 0); err != nil {
		return "", fmt.Errorf("failed to seek to beginning of file: %w", err)
	}

	// Detect encoding
	encoding := fe.detectEncoding(buffer[:n])
	
	var reader io.Reader = file
	if encoding != nil {
		reader = transform.NewReader(file, encoding)
	}

	// Read entire file
	content, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read file content %s: %w", filePath, err)
	}

	// Validate UTF-8
	if !utf8.Valid(content) {
		return "", fmt.Errorf("file %s contains invalid UTF-8", filePath)
	}

	return string(content), nil
}

// WriteFile writes content to a file with backup and validation
func (fe *FileEditor) WriteFile(filePath string, content string) error {
	// Create backup before writing
	backup, err := fe.createBackup(filePath)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Validate content if validator is provided
	if fe.validateFunc != nil {
		if err := fe.validateFunc(filePath, content); err != nil {
			return fmt.Errorf("content validation failed: %w", err)
		}
	}

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write to temporary file first
	tempFile := filePath + ".tmp"
	if err := os.WriteFile(tempFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, filePath); err != nil {
		if err := os.Remove(tempFile); err != nil {
			// Log error but don't fail - cleanup is best effort
			fmt.Printf("Warning: Failed to remove temp file %s: %v\n", tempFile, err)
		}
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Store backup reference for potential rollback
	if backup != "" {
		fe.recordOperation(filePath, backup, "write")
	}

	return nil
}

// ApplyDiff applies a diff to a file using the DiffProcessor
func (fe *FileEditor) ApplyDiff(filePath string, diffContent string) (*FileEdit, error) {
	// Read current file content
	originalContent, err := fe.ReadFile(filePath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Create diff processor
	processor := NewDiffProcessor(originalContent)
	
	// Parse the diff
	if err := processor.ParseDiff(diffContent, true); err != nil {
		return nil, fmt.Errorf("failed to parse diff: %w", err)
	}

	// Apply the diff
	newContent, err := processor.ApplyDiff()
	if err != nil {
		return nil, fmt.Errorf("failed to apply diff: %w", err)
	}

	// Create backup
	backup, err := fe.createBackup(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}

	edit := &FileEdit{
		FilePath:   filePath,
		OldContent: originalContent,
		NewContent: newContent,
		Backup:     backup,
		Timestamp:  time.Now().Unix(),
		Applied:    false,
	}

	// Write the new content
	if err := fe.WriteFile(filePath, newContent); err != nil {
		edit.Error = err.Error()
		return edit, err
	}

	edit.Applied = true
	return edit, nil
}

// ApplyMultiEdit applies multiple edits as a single atomic operation
func (fe *FileEditor) ApplyMultiEdit(operation MultiEditOperation) (*MultiEditOperation, error) {
	// Start transaction-like behavior
	backups := make(map[string]string)
	appliedEdits := make([]FileEdit, 0, len(operation.Edits))

	// Create backups for all files first
	for _, edit := range operation.Edits {
		backup, err := fe.createBackup(edit.FilePath)
		if err != nil {
			// Rollback any backups created so far
			fe.rollbackBackups(backups)
			return nil, fmt.Errorf("failed to create backup for %s: %w", edit.FilePath, err)
		}
		backups[edit.FilePath] = backup
	}

	// Apply all edits
	for i, edit := range operation.Edits {
		newEdit := edit
		newEdit.Backup = backups[edit.FilePath]
		newEdit.Timestamp = time.Now().Unix()

		// Apply the edit
		if err := fe.WriteFile(edit.FilePath, edit.NewContent); err != nil {
			// Rollback all applied edits
			fe.rollbackEdits(appliedEdits)
			
			newEdit.Error = err.Error()
			newEdit.Applied = false
			
			result := operation
			result.Applied = false
			result.Edits[i] = newEdit
			return &result, fmt.Errorf("failed to apply edit to %s: %w", edit.FilePath, err)
		}

		newEdit.Applied = true
		appliedEdits = append(appliedEdits, newEdit)
		operation.Edits[i] = newEdit
	}

	operation.Applied = true
	operation.Timestamp = time.Now().Unix()

	return &operation, nil
}

// RollbackEdit rolls back a single edit operation
func (fe *FileEditor) RollbackEdit(edit FileEdit) error {
	if !edit.Applied {
		return fmt.Errorf("edit was not successfully applied")
	}

	if edit.Backup == "" {
		return fmt.Errorf("no backup available for rollback")
	}

	// Read backup content
	backupContent, err := fe.ReadFile(edit.Backup)
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	// Restore original content
	if err := fe.WriteFile(edit.FilePath, backupContent); err != nil {
		return fmt.Errorf("failed to restore file: %w", err)
	}

	return nil
}

// RollbackMultiEdit rolls back a multi-edit operation
func (fe *FileEditor) RollbackMultiEdit(operation MultiEditOperation) error {
	if !operation.Applied {
		return fmt.Errorf("multi-edit was not successfully applied")
	}

	// Rollback in reverse order
	for i := len(operation.Edits) - 1; i >= 0; i-- {
		edit := operation.Edits[i]
		if edit.Applied {
			if err := fe.RollbackEdit(edit); err != nil {
				return fmt.Errorf("failed to rollback edit %d: %w", i, err)
			}
		}
	}

	return nil
}

// GetBackupHistory returns the backup history for a file
func (fe *FileEditor) GetBackupHistory(filePath string) ([]string, error) {
	pattern := fe.getBackupPattern(filePath)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to find backups: %w", err)
	}
	return matches, nil
}

// CleanupBackups removes old backups beyond the max backup limit
func (fe *FileEditor) CleanupBackups(filePath string) error {
	backups, err := fe.GetBackupHistory(filePath)
	if err != nil {
		return err
	}

	if len(backups) <= fe.maxBackups {
		return nil
	}

	// Sort by modification time and remove oldest
	// This is a simplified version - in practice, you'd want proper sorting
	for i := 0; i < len(backups)-fe.maxBackups; i++ {
		if err := os.Remove(backups[i]); err != nil {
			return fmt.Errorf("failed to remove old backup: %w", err)
		}
	}

	return nil
}

// Private helper methods

func (fe *FileEditor) detectEncoding(data []byte) transform.Transformer {
	// Simple encoding detection - in practice, you might want a more sophisticated approach
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		// UTF-8 BOM
		return unicode.UTF8.NewDecoder()
	}
	
	if len(data) >= 2 {
		if data[0] == 0xFF && data[1] == 0xFE {
			// UTF-16 LE BOM
			return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()
		}
		if data[0] == 0xFE && data[1] == 0xFF {
			// UTF-16 BE BOM
			return unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder()
		}
	}

	// Check if it's valid UTF-8
	if utf8.Valid(data) {
		return nil // Already UTF-8
	}

	// Fallback to ISO-8859-1 (Latin-1)
	return charmap.ISO8859_1.NewDecoder()
}

func (fe *FileEditor) createBackup(filePath string) (string, error) {
	// Skip backup for non-existent files (new file creation)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", nil
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := filepath.Base(filePath)
	backupName := fmt.Sprintf("%s.%s.backup", filename, timestamp)
	backupPath := filepath.Join(fe.backupDir, backupName)

	// Read original content
	content, err := fe.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read original file: %w", err)
	}

	// Write backup
	if err := os.WriteFile(backupPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write backup: %w", err)
	}

	// Cleanup old backups
	go func() {
		if err := fe.CleanupBackups(filePath); err != nil {
			// Log error in cleanup goroutine
			fmt.Printf("Warning: Backup cleanup failed for %s: %v\n", filePath, err)
		}
	}()

	return backupPath, nil
}

func (fe *FileEditor) getBackupPattern(filePath string) string {
	filename := filepath.Base(filePath)
	return filepath.Join(fe.backupDir, fmt.Sprintf("%s.*.backup", filename))
}

func (fe *FileEditor) rollbackBackups(backups map[string]string) {
	for _, backup := range backups {
		if backup != "" {
			if err := os.Remove(backup); err != nil {
				// Log error but continue - cleanup is best effort
				fmt.Printf("Warning: Failed to remove backup %s: %v\n", backup, err)
			}
		}
	}
}

func (fe *FileEditor) rollbackEdits(edits []FileEdit) {
	for i := len(edits) - 1; i >= 0; i-- {
		if err := fe.RollbackEdit(edits[i]); err != nil {
			fmt.Printf("Warning: Failed to rollback edit: %v\n", err)
		}
	}
}

func (fe *FileEditor) recordOperation(filePath, backup, operation string) {
	// TODO: Integrate with Kubernetes ConfigMaps for persistence
	// For now, this is a placeholder for audit logging
	record := map[string]interface{}{
		"file_path": filePath,
		"backup":    backup,
		"operation": operation,
		"timestamp": time.Now().Unix(),
	}
	
	if data, err := json.Marshal(record); err == nil {
		// In a real implementation, this would be stored in Kubernetes ConfigMaps
		// or a persistent audit log
		_ = data // Suppress unused variable warning
	}
}

// ValidationFunc is a type for content validation functions
type ValidationFunc func(filePath, content string) error

// DefaultValidators provides common validation functions
var DefaultValidators = struct {
	Go         ValidationFunc
	JSON       ValidationFunc
	YAML       ValidationFunc
	Dockerfile ValidationFunc
}{
	Go: func(filePath, content string) error {
		if !strings.HasSuffix(filePath, ".go") {
			return nil
		}
		// Basic Go syntax validation - in practice, you'd use go/parser
		if !strings.Contains(content, "package ") {
			return fmt.Errorf("go file must contain a package declaration")
		}
		return nil
	},
	
	JSON: func(filePath, content string) error {
		if !strings.HasSuffix(filePath, ".json") {
			return nil
		}
		var js json.RawMessage
		if err := json.Unmarshal([]byte(content), &js); err != nil {
			return fmt.Errorf("invalid JSON: %w", err)
		}
		return nil
	},
	
	YAML: func(filePath, content string) error {
		if !strings.HasSuffix(filePath, ".yaml") && !strings.HasSuffix(filePath, ".yml") {
			return nil
		}
		// Basic YAML validation - in practice, you'd use gopkg.in/yaml.v3
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if strings.Contains(line, "\t") {
				return fmt.Errorf("YAML file contains tabs at line %d (use spaces)", i+1)
			}
		}
		return nil
	},
	
	Dockerfile: func(filePath, content string) error {
		if filepath.Base(filePath) != "Dockerfile" && !strings.HasSuffix(filePath, ".dockerfile") {
			return nil
		}
		if !strings.Contains(strings.ToUpper(content), "FROM ") {
			return fmt.Errorf("dockerfile must contain a FROM instruction")
		}
		return nil
	},
}

// CombineValidators combines multiple validation functions
func CombineValidators(validators ...ValidationFunc) ValidationFunc {
	return func(filePath, content string) error {
		for _, validator := range validators {
			if err := validator(filePath, content); err != nil {
				return err
			}
		}
		return nil
	}
}