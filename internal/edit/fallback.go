package edit

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/sahilm/fuzzy"
)

// MatchStrategy represents different matching strategies for diff application
type MatchStrategy int

const (
	StrategyExact MatchStrategy = iota
	StrategyLineTrimmed
	StrategyBlockAnchor
	StrategyFuzzyContext
	StrategyLineNumber
	StrategyRegexPattern
)

// MatchResult contains information about a successful match
type MatchResult struct {
	Strategy      MatchStrategy `json:"strategy"`
	StartIndex    int           `json:"start_index"`
	EndIndex      int           `json:"end_index"`
	Confidence    float64       `json:"confidence"`
	MatchedText   string        `json:"matched_text"`
	Context       string        `json:"context,omitempty"`
	Modifications []string      `json:"modifications,omitempty"`
}

// FallbackMatcher implements sophisticated matching strategies for diff application
type FallbackMatcher struct {
	content        string
	searchContent  string
	startIndex     int
	minConfidence  float64
	maxDistance    int
	contextLines   int
	strictMode     bool
}

// MatcherConfig configures the FallbackMatcher behavior
type MatcherConfig struct {
	MinConfidence float64 // Minimum confidence score (0.0-1.0)
	MaxDistance   int     // Maximum edit distance for fuzzy matching
	ContextLines  int     // Number of context lines to consider
	StrictMode    bool    // Whether to use strict matching rules
}

// NewFallbackMatcher creates a new matcher with the given configuration
func NewFallbackMatcher(content, searchContent string, startIndex int, config MatcherConfig) *FallbackMatcher {
	if config.MinConfidence == 0 {
		config.MinConfidence = 0.7
	}
	if config.MaxDistance == 0 {
		config.MaxDistance = 50
	}
	if config.ContextLines == 0 {
		config.ContextLines = 3
	}

	return &FallbackMatcher{
		content:       content,
		searchContent: searchContent,
		startIndex:    startIndex,
		minConfidence: config.MinConfidence,
		maxDistance:   config.MaxDistance,
		contextLines:  config.ContextLines,
		strictMode:    config.StrictMode,
	}
}

// FindBestMatch attempts to find the best match using all available strategies
func (fm *FallbackMatcher) FindBestMatch() (*MatchResult, error) {
	strategies := []MatchStrategy{
		StrategyExact,
		StrategyLineTrimmed,
		StrategyBlockAnchor,
		StrategyLineNumber,
		StrategyRegexPattern,
		StrategyFuzzyContext,
	}

	var bestMatch *MatchResult
	
	for _, strategy := range strategies {
		match, err := fm.tryStrategy(strategy)
		if err != nil {
			continue // Try next strategy
		}
		
		if match != nil && match.Confidence >= fm.minConfidence {
			if bestMatch == nil || match.Confidence > bestMatch.Confidence {
				bestMatch = match
			}
			
			// If we have a high-confidence exact match, use it
			if match.Strategy == StrategyExact && match.Confidence >= 0.95 {
				return match, nil
			}
		}
	}
	
	if bestMatch == nil {
		return nil, fmt.Errorf("no suitable match found with confidence >= %.2f", fm.minConfidence)
	}
	
	return bestMatch, nil
}

// tryStrategy attempts a specific matching strategy
func (fm *FallbackMatcher) tryStrategy(strategy MatchStrategy) (*MatchResult, error) {
	switch strategy {
	case StrategyExact:
		return fm.exactMatch()
	case StrategyLineTrimmed:
		return fm.lineTrimmedMatch()
	case StrategyBlockAnchor:
		return fm.blockAnchorMatch()
	case StrategyFuzzyContext:
		return fm.fuzzyContextMatch()
	case StrategyLineNumber:
		return fm.lineNumberMatch()
	case StrategyRegexPattern:
		return fm.regexPatternMatch()
	default:
		return nil, fmt.Errorf("unknown strategy: %d", strategy)
	}
}

// exactMatch performs exact string matching
func (fm *FallbackMatcher) exactMatch() (*MatchResult, error) {
	if index := strings.Index(fm.content[fm.startIndex:], fm.searchContent); index != -1 {
		absoluteIndex := fm.startIndex + index
		return &MatchResult{
			Strategy:    StrategyExact,
			StartIndex:  absoluteIndex,
			EndIndex:    absoluteIndex + len(fm.searchContent),
			Confidence:  1.0,
			MatchedText: fm.searchContent,
		}, nil
	}
	return nil, fmt.Errorf("no exact match found")
}

// lineTrimmedMatch performs line-by-line matching with whitespace tolerance
func (fm *FallbackMatcher) lineTrimmedMatch() (*MatchResult, error) {
	contentLines := strings.Split(fm.content, "\n")
	searchLines := strings.Split(fm.searchContent, "\n")
	
	// Remove trailing empty line from search if present
	if len(searchLines) > 0 && searchLines[len(searchLines)-1] == "" {
		searchLines = searchLines[:len(searchLines)-1]
	}
	
	// Find starting line number from character index
	startLineNum := fm.getLineNumberFromIndex(fm.startIndex)
	
	// Try to match from each possible starting position
	for i := startLineNum; i <= len(contentLines)-len(searchLines); i++ {
		confidence, modifications := fm.calculateLineMatchConfidence(contentLines[i:i+len(searchLines)], searchLines)
		
		if confidence >= fm.minConfidence {
			startIndex := fm.getIndexFromLineNumber(i)
			endIndex := fm.getIndexFromLineNumber(i + len(searchLines))
			
			matchedText := strings.Join(contentLines[i:i+len(searchLines)], "\n")
			
			return &MatchResult{
				Strategy:      StrategyLineTrimmed,
				StartIndex:    startIndex,
				EndIndex:      endIndex,
				Confidence:    confidence,
				MatchedText:   matchedText,
				Modifications: modifications,
			}, nil
		}
	}
	
	return nil, fmt.Errorf("no line-trimmed match found")
}

// blockAnchorMatch matches using first and last lines as anchors
func (fm *FallbackMatcher) blockAnchorMatch() (*MatchResult, error) {
	searchLines := strings.Split(fm.searchContent, "\n")
	
	// Only use this approach for blocks of 3+ lines
	if len(searchLines) < 3 {
		return nil, fmt.Errorf("insufficient lines for block anchor matching")
	}
	
	// Remove trailing empty line if present
	if searchLines[len(searchLines)-1] == "" {
		searchLines = searchLines[:len(searchLines)-1]
	}
	
	firstLine := strings.TrimSpace(searchLines[0])
	lastLine := strings.TrimSpace(searchLines[len(searchLines)-1])
	searchBlockSize := len(searchLines)
	
	contentLines := strings.Split(fm.content, "\n")
	startLineNum := fm.getLineNumberFromIndex(fm.startIndex)
	
	// Look for matching start and end anchors
	for i := startLineNum; i <= len(contentLines)-searchBlockSize; i++ {
		// Check if first line matches
		if strings.TrimSpace(contentLines[i]) != firstLine {
			continue
		}
		
		// Check if last line matches at expected position
		if strings.TrimSpace(contentLines[i+searchBlockSize-1]) != lastLine {
			continue
		}
		
		// Calculate confidence based on middle lines
		middleContentLines := contentLines[i+1 : i+searchBlockSize-1]
		middleSearchLines := searchLines[1 : len(searchLines)-1]
		
		confidence, modifications := fm.calculateLineMatchConfidence(middleContentLines, middleSearchLines)
		
		// Boost confidence for anchor matching
		confidence = math.Min(1.0, confidence*1.1)
		
		if confidence >= fm.minConfidence {
			startIndex := fm.getIndexFromLineNumber(i)
			endIndex := fm.getIndexFromLineNumber(i + searchBlockSize)
			matchedText := strings.Join(contentLines[i:i+searchBlockSize], "\n")
			
			return &MatchResult{
				Strategy:      StrategyBlockAnchor,
				StartIndex:    startIndex,
				EndIndex:      endIndex,
				Confidence:    confidence,
				MatchedText:   matchedText,
				Modifications: modifications,
			}, nil
		}
	}
	
	return nil, fmt.Errorf("no block anchor match found")
}

// fuzzyContextMatch performs fuzzy matching with context awareness
func (fm *FallbackMatcher) fuzzyContextMatch() (*MatchResult, error) {
	if fm.strictMode {
		return nil, fmt.Errorf("fuzzy matching disabled in strict mode")
	}
	
	contentLines := strings.Split(fm.content, "\n")
	searchLines := strings.Split(fm.searchContent, "\n")
	
	if len(searchLines) == 0 {
		return nil, fmt.Errorf("empty search content")
	}
	
	// Use the first search line as the primary match target
	primaryLine := strings.TrimSpace(searchLines[0])
	if primaryLine == "" && len(searchLines) > 1 {
		primaryLine = strings.TrimSpace(searchLines[1])
	}
	
	// Create fuzzy matches for content lines
	var candidates []string
	lineMap := make(map[string]int)
	startLineNum := fm.getLineNumberFromIndex(fm.startIndex)
	
	for i := startLineNum; i < len(contentLines); i++ {
		line := strings.TrimSpace(contentLines[i])
		if line != "" {
			candidates = append(candidates, line)
			lineMap[line] = i
		}
	}
	
	// Find fuzzy matches
	matches := fuzzy.Find(primaryLine, candidates)
	
	for _, match := range matches {
		if match.Score < 0 { // fuzzy library uses negative scores
			continue
		}
		
		lineNum := lineMap[match.Str]
		
		// Try to match the full block starting from this line
		if lineNum+len(searchLines) <= len(contentLines) {
			blockContentLines := contentLines[lineNum : lineNum+len(searchLines)]
			confidence, modifications := fm.calculateLineMatchConfidence(blockContentLines, searchLines)
			
			// Adjust confidence based on fuzzy match score
			fuzzyConfidence := float64(-match.Score) / 100.0 // Convert to 0-1 range
			confidence = (confidence + fuzzyConfidence) / 2.0
			
			if confidence >= fm.minConfidence {
				startIndex := fm.getIndexFromLineNumber(lineNum)
				endIndex := fm.getIndexFromLineNumber(lineNum + len(searchLines))
				matchedText := strings.Join(blockContentLines, "\n")
				
				return &MatchResult{
					Strategy:      StrategyFuzzyContext,
					StartIndex:    startIndex,
					EndIndex:      endIndex,
					Confidence:    confidence,
					MatchedText:   matchedText,
					Modifications: modifications,
					Context:       fmt.Sprintf("Fuzzy match score: %d", match.Score),
				}, nil
			}
		}
	}
	
	return nil, fmt.Errorf("no fuzzy context match found")
}

// lineNumberMatch attempts matching based on line number hints in the search content
func (fm *FallbackMatcher) lineNumberMatch() (*MatchResult, error) {
	// Look for line number patterns in search content (e.g., "// line 42", "# line 42")
	lineNumRegex := regexp.MustCompile(`(?i)line\s+(\d+)`)
	matches := lineNumRegex.FindStringSubmatch(fm.searchContent)
	
	if len(matches) < 2 {
		return nil, fmt.Errorf("no line number hints found")
	}
	
	lineNum, err := strconv.Atoi(matches[1])
	if err != nil {
		return nil, fmt.Errorf("invalid line number: %s", matches[1])
	}
	
	// Adjust for 0-based indexing
	lineNum--
	
	contentLines := strings.Split(fm.content, "\n")
	searchLines := strings.Split(fm.searchContent, "\n")
	
	// Check if we can match at the suggested line number
	if lineNum >= 0 && lineNum+len(searchLines) <= len(contentLines) {
		blockContentLines := contentLines[lineNum : lineNum+len(searchLines)]
		confidence, modifications := fm.calculateLineMatchConfidence(blockContentLines, searchLines)
		
		if confidence >= fm.minConfidence {
			startIndex := fm.getIndexFromLineNumber(lineNum)
			endIndex := fm.getIndexFromLineNumber(lineNum + len(searchLines))
			matchedText := strings.Join(blockContentLines, "\n")
			
			return &MatchResult{
				Strategy:      StrategyLineNumber,
				StartIndex:    startIndex,
				EndIndex:      endIndex,
				Confidence:    confidence,
				MatchedText:   matchedText,
				Modifications: modifications,
				Context:       fmt.Sprintf("Line number hint: %d", lineNum+1),
			}, nil
		}
	}
	
	return nil, fmt.Errorf("line number match failed")
}

// regexPatternMatch attempts to match using regex patterns detected in the search content
func (fm *FallbackMatcher) regexPatternMatch() (*MatchResult, error) {
	if fm.strictMode {
		return nil, fmt.Errorf("regex matching disabled in strict mode")
	}
	
	// Extract potential regex patterns from search content
	patterns := fm.extractRegexPatterns(fm.searchContent)
	
	for _, pattern := range patterns {
		regex, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		
		matches := regex.FindStringIndex(fm.content[fm.startIndex:])
		if matches != nil {
			startIndex := fm.startIndex + matches[0]
			endIndex := fm.startIndex + matches[1]
			matchedText := fm.content[startIndex:endIndex]
			
			// Calculate confidence based on pattern complexity and length
			confidence := fm.calculateRegexConfidence(pattern, matchedText)
			
			if confidence >= fm.minConfidence {
				return &MatchResult{
					Strategy:    StrategyRegexPattern,
					StartIndex:  startIndex,
					EndIndex:    endIndex,
					Confidence:  confidence,
					MatchedText: matchedText,
					Context:     fmt.Sprintf("Regex pattern: %s", pattern),
				}, nil
			}
		}
	}
	
	return nil, fmt.Errorf("no regex pattern match found")
}

// Helper methods

func (fm *FallbackMatcher) getLineNumberFromIndex(index int) int {
	return strings.Count(fm.content[:index], "\n")
}

func (fm *FallbackMatcher) getIndexFromLineNumber(lineNum int) int {
	lines := strings.Split(fm.content, "\n")
	if lineNum <= 0 {
		return 0
	}
	if lineNum >= len(lines) {
		return len(fm.content)
	}
	
	index := 0
	for i := 0; i < lineNum; i++ {
		index += len(lines[i]) + 1 // +1 for newline
	}
	return index
}

func (fm *FallbackMatcher) calculateLineMatchConfidence(contentLines, searchLines []string) (float64, []string) {
	if len(contentLines) != len(searchLines) {
		return 0.0, nil
	}
	
	var modifications []string
	totalLines := len(searchLines)
	exactMatches := 0
	trimmedMatches := 0
	
	for i, searchLine := range searchLines {
		contentLine := contentLines[i]
		
		// Exact match
		if contentLine == searchLine {
			exactMatches++
			continue
		}
		
		// Trimmed match
		if strings.TrimSpace(contentLine) == strings.TrimSpace(searchLine) {
			trimmedMatches++
			modifications = append(modifications, fmt.Sprintf("Line %d: whitespace differences", i+1))
			continue
		}
		
		// Calculate edit distance for fuzzy matching
		distance := fm.editDistance(strings.TrimSpace(contentLine), strings.TrimSpace(searchLine))
		if distance <= fm.maxDistance {
			// Partial match based on edit distance
			partialScore := 1.0 - float64(distance)/float64(len(searchLine))
			if partialScore > 0.5 {
				trimmedMatches++
				modifications = append(modifications, fmt.Sprintf("Line %d: %d character differences", i+1, distance))
				continue
			}
		}
		
		// No match
		modifications = append(modifications, fmt.Sprintf("Line %d: no match", i+1))
	}
	
	// Calculate confidence score
	confidence := (float64(exactMatches) + float64(trimmedMatches)*0.8) / float64(totalLines)
	return confidence, modifications
}

func (fm *FallbackMatcher) extractRegexPatterns(content string) []string {
	var patterns []string
	
	// Look for function/method signatures
	if strings.Contains(content, "func ") {
		// Go function pattern
		funcPattern := `func\s+\w+\s*\([^)]*\)(?:\s*\([^)]*\))?\s*{`
		patterns = append(patterns, funcPattern)
	}
	
	// Look for struct definitions
	if strings.Contains(content, "type ") && strings.Contains(content, "struct") {
		structPattern := `type\s+\w+\s+struct\s*{`
		patterns = append(patterns, structPattern)
	}
	
	// Look for import statements
	if strings.Contains(content, "import") {
		importPattern := `import\s*\([^)]*\)`
		patterns = append(patterns, importPattern)
	}
	
	return patterns
}

func (fm *FallbackMatcher) calculateRegexConfidence(pattern, matchedText string) float64 {
	// Base confidence based on pattern complexity
	confidence := 0.6
	
	// Boost confidence for longer patterns
	if len(pattern) > 20 {
		confidence += 0.1
	}
	
	// Boost confidence for longer matches
	if len(matchedText) > 50 {
		confidence += 0.1
	}
	
	// Penalize very simple patterns
	if len(pattern) < 10 {
		confidence -= 0.2
	}
	
	return math.Max(0.0, math.Min(1.0, confidence))
}

// editDistance calculates the Levenshtein distance between two strings
func (fm *FallbackMatcher) editDistance(a, b string) int {
	la := len(a)
	lb := len(b)
	
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	
	// Create a 2D slice
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
	}
	
	// Initialize first row and column
	for i := 0; i <= la; i++ {
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}
	
	// Fill the matrix
	for i := 1; i <= la; i++ {
		for j := 1; lb >= j; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			
			d[i][j] = min(
				d[i-1][j]+1,      // deletion
				d[i][j-1]+1,      // insertion
				d[i-1][j-1]+cost, // substitution
			)
		}
	}
	
	return d[la][lb]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// MatchConfidenceThresholds defines confidence thresholds for different use cases
var MatchConfidenceThresholds = struct {
	Safe       float64 // Conservative threshold for automated application
	Interactive float64 // Threshold for interactive approval
	Experimental float64 // Threshold for experimental/debugging
}{
	Safe:         0.9,
	Interactive:  0.7,
	Experimental: 0.5,
}