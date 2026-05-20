package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	contextpkg "github.com/phildougherty/m8e/internal/context"
)

// mentionTools owns the context-mention tools (process_mentions and
// expand_mentions). It is a thin wrapper around contextpkg.MentionProcessor:
// MateyMCPServer injects whatever Kubernetes/memory dependencies it has so the
// processor's @problems / @logs / @def / @memory / @workflow paths reach real
// backends instead of dead code.
type mentionTools struct {
	processor *contextpkg.MentionProcessor
}

func newMentionTools(processor *contextpkg.MentionProcessor) *mentionTools {
	return &mentionTools{processor: processor}
}

// notInitialized is the shared guard result for when no mention processor is
// wired (e.g. the file-discovery init step failed during server startup).
func (mt *mentionTools) notInitialized() (*ToolResult, error) {
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Mention processor not initialized. Context-mention tools are unavailable."}},
		IsError: true,
	}, fmt.Errorf("mention processor not initialized")
}

// processMentions parses the provided text, resolves each @mention against its
// real backend (k8s for @problems/@logs/@workflow, tree-sitter for @def, memory
// store for @memory, the filesystem for @file/@directory), and returns a JSON
// payload with the resolved mentions and their content.
func (mt *mentionTools) processMentions(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if mt.processor == nil {
		return mt.notInitialized()
	}

	text, ok := args["text"].(string)
	if !ok || text == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: text is required"}},
			IsError: true,
		}, fmt.Errorf("text is required")
	}

	mentions, err := mt.processor.ParseMentions(text)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error parsing mentions: %v", err)}},
			IsError: true,
		}, err
	}

	processed := make([]contextpkg.Mention, 0, len(mentions))
	for _, m := range mentions {
		out, perr := mt.processor.ProcessMention(m)
		if perr != nil {
			// Surface per-mention errors via the Error field rather than
			// failing the whole tool call; a single broken @workflow should
			// not poison the rest of the response.
			out.Error = perr.Error()
		}
		processed = append(processed, out)
	}

	payload := map[string]interface{}{
		"text":     text,
		"count":    len(processed),
		"mentions": processed,
	}
	jsonBytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error formatting mentions: %v", err)}},
			IsError: true,
		}, err
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: string(jsonBytes)}},
	}, nil
}

// expandMentions returns the input text with every @mention replaced inline by
// its resolved content (wrapped in --- markers). Useful when the caller wants
// a single self-contained prompt instead of structured per-mention data.
func (mt *mentionTools) expandMentions(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if mt.processor == nil {
		return mt.notInitialized()
	}

	text, ok := args["text"].(string)
	if !ok || text == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: text is required"}},
			IsError: true,
		}, fmt.Errorf("text is required")
	}

	expanded, mentions, err := mt.processor.ExpandText(text)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error expanding mentions: %v", err)}},
			IsError: true,
		}, err
	}

	// Surface any per-mention failures as a trailing note so callers can see
	// that, e.g., the memory store was unavailable — without dropping the
	// successful mentions on the floor.
	var failureNotes []string
	for _, m := range mentions {
		if m.Error != "" {
			failureNotes = append(failureNotes, fmt.Sprintf("%s: %s", m.Raw, m.Error))
		}
	}
	if len(failureNotes) > 0 {
		expanded += "\n\n[mention-processor warnings]\n  " + strings.Join(failureNotes, "\n  ")
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: expanded}},
	}, nil
}
