package edit

import (
	"testing"
)

func TestNewFallbackMatcher(t *testing.T) {
	content := "line 1\nline 2\nline 3\n"
	searchContent := "line 2"
	
	// Test with default config
	matcher := NewFallbackMatcher(content, searchContent, 0, MatcherConfig{})
	
	if matcher == nil {
		t.Fatal("Expected non-nil matcher")
	}
	
	if matcher.minConfidence != 0.7 {
		t.Errorf("Expected default minConfidence 0.7, got %f", matcher.minConfidence)
	}
	
	if matcher.maxDistance != 50 {
		t.Errorf("Expected default maxDistance 50, got %d", matcher.maxDistance)
	}
	
	if matcher.contextLines != 3 {
		t.Errorf("Expected default contextLines 3, got %d", matcher.contextLines)
	}
}

func TestExactMatch(t *testing.T) {
	content := "hello world\ntest line\nfoo bar"
	searchContent := "test line"
	
	matcher := NewFallbackMatcher(content, searchContent, 0, MatcherConfig{
		MinConfidence: 0.5,
	})
	
	result, err := matcher.exactMatch()
	if err != nil {
		t.Fatalf("Expected exact match to succeed: %v", err)
	}
	
	if result.Strategy != StrategyExact {
		t.Errorf("Expected StrategyExact, got %v", result.Strategy)
	}
	
	if result.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0, got %f", result.Confidence)
	}
}

func TestMinFunction(t *testing.T) {
	// Test the min function used in edit distance calculation
	result := min(5, 3, 7)
	if result != 3 {
		t.Errorf("Expected min(5,3,7) = 3, got %d", result)
	}
	
	result = min(1, 1, 1)
	if result != 1 {
		t.Errorf("Expected min(1,1,1) = 1, got %d", result)
	}
}

func TestEditDistance(t *testing.T) {
	matcher := &FallbackMatcher{}
	
	// Test identical strings
	dist := matcher.editDistance("hello", "hello")
	if dist != 0 {
		t.Errorf("Expected distance 0 for identical strings, got %d", dist)
	}
	
	// Test empty strings
	dist = matcher.editDistance("", "")
	if dist != 0 {
		t.Errorf("Expected distance 0 for empty strings, got %d", dist)
	}
	
	// Test one empty string
	dist = matcher.editDistance("hello", "")
	if dist != 5 {
		t.Errorf("Expected distance 5, got %d", dist)
	}
	
	dist = matcher.editDistance("", "world")
	if dist != 5 {
		t.Errorf("Expected distance 5, got %d", dist)
	}
}

func TestMatchConfidenceThresholds(t *testing.T) {
	// Test that confidence thresholds are reasonable
	if MatchConfidenceThresholds.Safe != 0.9 {
		t.Errorf("Expected Safe threshold 0.9, got %f", MatchConfidenceThresholds.Safe)
	}
	
	if MatchConfidenceThresholds.Interactive != 0.7 {
		t.Errorf("Expected Interactive threshold 0.7, got %f", MatchConfidenceThresholds.Interactive)
	}
	
	if MatchConfidenceThresholds.Experimental != 0.5 {
		t.Errorf("Expected Experimental threshold 0.5, got %f", MatchConfidenceThresholds.Experimental)
	}
}