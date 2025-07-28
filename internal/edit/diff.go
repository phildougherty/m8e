package edit

import (
	"fmt"
	"regexp"
	"strings"
)

// DiffFormat defines the structure of a SEARCH/REPLACE diff block
type DiffFormat struct {
	SearchContent  string
	ReplaceContent string
	StartLine      int
	EndLine        int
}

// DiffOperation represents a single diff operation with its context
type DiffOperation struct {
	Type        string // "search", "replace", "apply"
	Content     string
	LineNumber  int
	Confidence  float64 // 0-1, higher means more confident match
}

// DiffProcessor handles parsing and applying SEARCH/REPLACE format diffs
type DiffProcessor struct {
	originalContent string
	blocks          []DiffFormat
	operations      []DiffOperation
}

// Diff markers inspired by Cline's format
const (
	SearchBlockStart = "------- SEARCH"
	SearchBlockEnd   = "======="
	ReplaceBlockEnd  = "+++++++ REPLACE"
)

// Alternative legacy markers for flexibility
var (
	SearchBlockStartRegex = regexp.MustCompile(`^[-]{3,}\s*SEARCH>?$`)
	SearchBlockEndRegex   = regexp.MustCompile(`^[=]{3,}$`)
	ReplaceBlockEndRegex  = regexp.MustCompile(`^[+]{3,}\s*REPLACE>?$`)
	
	// Legacy format support
	LegacySearchStartRegex = regexp.MustCompile(`^[<]{3,}\s*SEARCH>?$`)
	LegacyReplaceEndRegex  = regexp.MustCompile(`^[>]{3,}\s*REPLACE>?$`)
)

// NewDiffProcessor creates a new diff processor for the given original content
func NewDiffProcessor(originalContent string) *DiffProcessor {
	return &DiffProcessor{
		originalContent: originalContent,
		blocks:          make([]DiffFormat, 0),
		operations:      make([]DiffOperation, 0),
	}
}

// ParseDiff parses SEARCH/REPLACE format diff content into structured blocks
func (dp *DiffProcessor) ParseDiff(diffContent string, isFinal bool) error {
	lines := strings.Split(diffContent, "\n")
	
	// Remove incomplete partial markers at the end
	if !isFinal && len(lines) > 0 {
		lastLine := lines[len(lines)-1]
		if dp.isPartialMarker(lastLine) {
			lines = lines[:len(lines)-1]
		}
	}
	
	var currentBlock DiffFormat
	inSearch := false
	inReplace := false
	
	for i, line := range lines {
		// Check for search block start
		if dp.isSearchBlockStart(line) {
			if inSearch || inReplace {
				return fmt.Errorf("malformed diff: nested search blocks at line %d", i+1)
			}
			inSearch = true
			currentBlock = DiffFormat{StartLine: i + 1}
			continue
		}
		
		// Check for search block end (transition to replace)
		if dp.isSearchBlockEnd(line) {
			if !inSearch {
				return fmt.Errorf("malformed diff: unexpected search block end at line %d", i+1)
			}
			inSearch = false
			inReplace = true
			continue
		}
		
		// Check for replace block end
		if dp.isReplaceBlockEnd(line) {
			if !inReplace {
				return fmt.Errorf("malformed diff: unexpected replace block end at line %d", i+1)
			}
			inReplace = false
			currentBlock.EndLine = i + 1
			dp.blocks = append(dp.blocks, currentBlock)
			currentBlock = DiffFormat{}
			continue
		}
		
		// Accumulate content
		if inSearch {
			if currentBlock.SearchContent != "" {
				currentBlock.SearchContent += "\n"
			}
			currentBlock.SearchContent += line
		} else if inReplace {
			if currentBlock.ReplaceContent != "" {
				currentBlock.ReplaceContent += "\n"
			}
			currentBlock.ReplaceContent += line
		}
	}
	
	// Handle case where we're still in replace mode at end (for streaming)
	if isFinal && inReplace {
		currentBlock.EndLine = len(lines)
		dp.blocks = append(dp.blocks, currentBlock)
	}
	
	return nil
}

// ApplyDiff applies all parsed diff blocks to the original content
func (dp *DiffProcessor) ApplyDiff() (string, error) {
	result := dp.originalContent
	offset := 0
	
	// Sort blocks by their position in the original content for proper application
	// This is a simplified version - in practice, we'd need more sophisticated ordering
	
	for _, block := range dp.blocks {
		newContent, newOffset, err := dp.applyBlock(result, block, offset)
		if err != nil {
			return "", fmt.Errorf("failed to apply diff block: %w", err)
		}
		result = newContent
		offset = newOffset
	}
	
	return result, nil
}

// applyBlock applies a single diff block using multi-level fallback matching
func (dp *DiffProcessor) applyBlock(content string, block DiffFormat, offset int) (string, int, error) {
	searchContent := block.SearchContent
	replaceContent := block.ReplaceContent
	
	// Handle empty search (full file replacement or new file)
	if searchContent == "" {
		if content == "" {
			// New file creation
			return replaceContent, len(replaceContent), nil
		} else {
			// Full file replacement
			return replaceContent, len(replaceContent), nil
		}
	}
	
	// Try exact string matching first
	if index := strings.Index(content[offset:], searchContent); index != -1 {
		absoluteIndex := offset + index
		before := content[:absoluteIndex]
		after := content[absoluteIndex+len(searchContent):]
		result := before + replaceContent + after
		return result, absoluteIndex + len(replaceContent), nil
	}
	
	// Fallback 1: Line-trimmed matching
	if matchStart, matchEnd := dp.lineTrimmedMatch(content, searchContent, offset); matchStart != -1 {
		before := content[:matchStart]
		after := content[matchEnd:]
		result := before + replaceContent + after
		return result, matchStart + len(replaceContent), nil
	}
	
	// Fallback 2: Block anchor matching for larger blocks
	if matchStart, matchEnd := dp.blockAnchorMatch(content, searchContent, offset); matchStart != -1 {
		before := content[:matchStart]
		after := content[matchEnd:]
		result := before + replaceContent + after
		return result, matchStart + len(replaceContent), nil
	}
	
	// If all matching strategies fail
	return "", 0, fmt.Errorf("could not find match for search content: %s", 
		dp.truncateForError(searchContent))
}

// lineTrimmedMatch performs line-by-line matching ignoring leading/trailing whitespace
func (dp *DiffProcessor) lineTrimmedMatch(content, searchContent string, startIndex int) (int, int) {
	contentLines := strings.Split(content, "\n")
	searchLines := strings.Split(searchContent, "\n")
	
	// Remove trailing empty line from search if present
	if len(searchLines) > 0 && searchLines[len(searchLines)-1] == "" {
		searchLines = searchLines[:len(searchLines)-1]
	}
	
	// Find starting line number from character index
	startLineNum := 0
	charCount := 0
	for i, line := range contentLines {
		if charCount >= startIndex {
			startLineNum = i
			break
		}
		charCount += len(line) + 1 // +1 for newline
	}
	
	// Try to match from each possible starting position
	for i := startLineNum; i <= len(contentLines)-len(searchLines); i++ {
		matches := true
		
		// Check if all search lines match (trimmed)
		for j := 0; j < len(searchLines); j++ {
			contentTrimmed := strings.TrimSpace(contentLines[i+j])
			searchTrimmed := strings.TrimSpace(searchLines[j])
			
			if contentTrimmed != searchTrimmed {
				matches = false
				break
			}
		}
		
		if matches {
			// Calculate character positions
			matchStart := 0
			for k := 0; k < i; k++ {
				matchStart += len(contentLines[k]) + 1
			}
			
			matchEnd := matchStart
			for k := 0; k < len(searchLines); k++ {
				matchEnd += len(contentLines[i+k]) + 1
			}
			
			return matchStart, matchEnd
		}
	}
	
	return -1, -1
}

// blockAnchorMatch matches blocks using first and last lines as anchors
func (dp *DiffProcessor) blockAnchorMatch(content, searchContent string, startIndex int) (int, int) {
	searchLines := strings.Split(searchContent, "\n")
	
	// Only use this approach for blocks of 3+ lines
	if len(searchLines) < 3 {
		return -1, -1
	}
	
	// Remove trailing empty line if present
	if searchLines[len(searchLines)-1] == "" {
		searchLines = searchLines[:len(searchLines)-1]
	}
	
	firstLineSearch := strings.TrimSpace(searchLines[0])
	lastLineSearch := strings.TrimSpace(searchLines[len(searchLines)-1])
	searchBlockSize := len(searchLines)
	
	contentLines := strings.Split(content, "\n")
	
	// Find starting line number from character index
	startLineNum := 0
	charCount := 0
	for i, line := range contentLines {
		if charCount >= startIndex {
			startLineNum = i
			break
		}
		charCount += len(line) + 1
	}
	
	// Look for matching start and end anchors
	for i := startLineNum; i <= len(contentLines)-searchBlockSize; i++ {
		// Check if first line matches
		if strings.TrimSpace(contentLines[i]) != firstLineSearch {
			continue
		}
		
		// Check if last line matches at expected position
		if strings.TrimSpace(contentLines[i+searchBlockSize-1]) != lastLineSearch {
			continue
		}
		
		// Calculate character positions
		matchStart := 0
		for k := 0; k < i; k++ {
			matchStart += len(contentLines[k]) + 1
		}
		
		matchEnd := matchStart
		for k := 0; k < searchBlockSize; k++ {
			matchEnd += len(contentLines[i+k]) + 1
		}
		
		return matchStart, matchEnd
	}
	
	return -1, -1
}

// Helper functions for marker detection

func (dp *DiffProcessor) isSearchBlockStart(line string) bool {
	return SearchBlockStartRegex.MatchString(line) || LegacySearchStartRegex.MatchString(line)
}

func (dp *DiffProcessor) isSearchBlockEnd(line string) bool {
	return SearchBlockEndRegex.MatchString(line)
}

func (dp *DiffProcessor) isReplaceBlockEnd(line string) bool {
	return ReplaceBlockEndRegex.MatchString(line) || LegacyReplaceEndRegex.MatchString(line)
}

func (dp *DiffProcessor) isPartialMarker(line string) bool {
	// Check if line looks like it could be part of a marker but isn't complete
	return (strings.HasPrefix(line, "-") || 
			strings.HasPrefix(line, "<") || 
			strings.HasPrefix(line, "=") || 
			strings.HasPrefix(line, "+") || 
			strings.HasPrefix(line, ">")) &&
			!dp.isSearchBlockStart(line) &&
			!dp.isSearchBlockEnd(line) &&
			!dp.isReplaceBlockEnd(line)
}

func (dp *DiffProcessor) truncateForError(content string) string {
	if len(content) > 100 {
		return content[:100] + "..."
	}
	return content
}

// GetBlocks returns the parsed diff blocks
func (dp *DiffProcessor) GetBlocks() []DiffFormat {
	return dp.blocks
}

// GetOperations returns the diff operations
func (dp *DiffProcessor) GetOperations() []DiffOperation {
	return dp.operations
}