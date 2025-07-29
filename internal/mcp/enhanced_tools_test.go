package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEditFileRequest_Parsing(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		wantErr  bool
	}{
		{
			name: "legacy format with file_path",
			jsonData: `{
				"file_path": "/test/file.txt",
				"content": "new content"
			}`,
			wantErr: false,
		},
		{
			name: "AI format with path and edits",
			jsonData: `{
				"path": "/test/file.txt",
				"edits": [
					{
						"old_string": "old text",
						"new_string": "new text"
					}
				]
			}`,
			wantErr: false,
		},
		{
			name: "mixed format with path and content",
			jsonData: `{
				"path": "/test/file.txt",
				"content": "complete new content"
			}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req EditFileRequest
			err := json.Unmarshal([]byte(tt.jsonData), &req)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("JSON unmarshal error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify path handling
				expectedPath := "/test/file.txt"
				actualPath := req.FilePath
				if actualPath == "" {
					actualPath = req.Path
				}
				
				if actualPath != expectedPath {
					t.Errorf("Expected path %s, got %s", expectedPath, actualPath)
				}
			}
		})
	}
}

func TestEditFile_PathCompatibility(t *testing.T) {
	// Create a temporary file for testing
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := "line 1\nline 2\nline 3"
	
	err := os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create enhanced tools instance with minimal config
	config := EnhancedToolConfig{
		EnableVisualDiff: false,
		AutoApprove:     true,
		MaxFileSize:     1024 * 1024,
	}
	
	et := NewEnhancedTools(config, nil, nil, nil, nil, nil)

	ctx := context.Background()

	// Test legacy format
	t.Run("legacy format", func(t *testing.T) {
		req := EditFileRequest{
			FilePath: testFile,
			Content:  "new content from legacy format",
		}
		
		resp, err := et.EditFile(ctx, req)
		if err != nil {
			t.Fatalf("EditFile failed: %v", err)
		}
		
		if !resp.Success {
			t.Errorf("Expected success=true, got %v. Error: %s", resp.Success, resp.Error)
		}
	})

	// Reset file content
	err = os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatalf("Failed to reset test file: %v", err)
	}

	// Test AI format with edits
	t.Run("AI format with edits", func(t *testing.T) {
		req := EditFileRequest{
			Path: testFile,
			Edits: []EditOperation{
				{
					OldString: "line 2",
					NewString: "modified line 2",
				},
			},
		}
		
		resp, err := et.EditFile(ctx, req)
		if err != nil {
			t.Fatalf("EditFile failed: %v", err)
		}
		
		if !resp.Success {
			t.Errorf("Expected success=true, got %v. Error: %s", resp.Success, resp.Error)
		}

		// Verify the edit was applied
		content, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("Failed to read test file: %v", err)
		}
		
		expectedContent := "line 1\nmodified line 2\nline 3"
		if string(content) != expectedContent {
			t.Errorf("Expected content %q, got %q", expectedContent, string(content))
		}
	})
}