package chat

import (
	"context"
	"time"

	"github.com/charmbracelet/glamour"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/phildougherty/m8e/internal/ai"
	"github.com/phildougherty/m8e/internal/mcp"
)

// ApprovalMode defines the level of autonomous behavior
type ApprovalMode int

const (
	DEFAULT ApprovalMode = iota
	AUTO_EDIT
	YOLO
)

// String returns the string representation of the approval mode
func (am ApprovalMode) String() string {
	switch am {
	case DEFAULT:
		return "DEFAULT"
	case AUTO_EDIT:
		return "AUTO_EDIT"
	case YOLO:
		return "YOLO"
	default:
		return "UNKNOWN"
	}
}

// GetModeIndicatorNoEmoji returns the mode indicator without emojis for UI display
func (am ApprovalMode) GetModeIndicatorNoEmoji() string {
	switch am {
	case DEFAULT:
		return "MANUAL"
	case AUTO_EDIT:
		return "AUTO"
	case YOLO:
		return "YOLO"
	default:
		return "MANUAL"
	}
}

// Description returns a description of what the mode does
func (am ApprovalMode) Description() string {
	switch am {
	case DEFAULT:
		return "Ask for confirmation before executing actions"
	case AUTO_EDIT:
		return "Auto-approve safe operations, confirm destructive ones"
	case YOLO:
		return "Auto-approve all function calls without confirmation"
	default:
		return "Unknown mode"
	}
}

// ShouldConfirm returns whether a function should be confirmed based on the approval mode
func (am ApprovalMode) ShouldConfirm(functionName string) bool {
	switch am {
	case YOLO:
		return false // Never confirm in YOLO mode
	case AUTO_EDIT:
		// Auto-approve safe operations, confirm potentially destructive ones
		safeOperations := []string{"get_status", "list_services", "get_logs", "get_metrics"}
		for _, safe := range safeOperations {
			if functionName == safe {
				return false
			}
		}
		return true
	default:
		return true // Always confirm in DEFAULT mode
	}
}

// TermChat represents a split-screen terminal chat interface
type TermChat struct {
	aiManager              *ai.Manager
	mcpClient              *mcp.MCPClient
	ctx                    context.Context
	cancel                 context.CancelFunc
	currentProvider        string
	currentModel           string
	chatHistory            []TermChatMessage
	termWidth              int
	termHeight             int
	markdownRenderer       *glamour.TermRenderer
	verboseMode            bool // Toggle for compact/verbose output
	functionResults        map[string]string // Store full results for toggle
	approvalMode           ApprovalMode // Controls autonomous behavior level
	maxTurns               int          // Maximum continuation turns to prevent infinite loops
	currentTurns           int          // Current turn count for this conversation
	uiConfirmationCallback func(functionName, arguments string) bool // Callback for UI-based confirmations
}

// TermChatMessage represents a chat message
type TermChatMessage struct {
	Role      string
	Content   string    // Markdown content (for AI context)
	Timestamp time.Time
}

// ChatUI represents the terminal UI model for the chat interface
type ChatUI struct {
	termChat              *TermChat
	input                 string
	cursor                int
	viewport              []string
	statusLine            string
	width                 int
	height                int
	inputFocused          bool
	ready                 bool
	loading               bool
	spinnerFrame          int
	confirmationMode      bool
	pendingFunctionCall   *FunctionConfirmation
}

// FunctionConfirmation represents a pending function call confirmation
type FunctionConfirmation struct {
	FunctionName string
	Arguments    string
	Callback     func(bool) // Callback to execute when confirmation is received
}

// Global variable for UI program access
var uiProgram *tea.Program

// GetUIProgram returns the global UI program reference
func GetUIProgram() *tea.Program {
	return uiProgram
}

// AiStreamMsg represents streaming AI content (exported for turn.go)
type AiStreamMsg struct {
	Content string
}

// AiResponseMsg represents complete AI response (exported for turn.go)
type AiResponseMsg struct {
	Content string
}

// Message types for the chat UI
type statusUpdateMsg struct {
	status string
}

type aiResponseMsg struct {
	content string
}

type aiStreamMsg struct {
	content string
}

type userInputMsg struct {
	input       string
	userDisplay string
}

type spinnerMsg struct{}

type functionConfirmationMsg struct {
	functionName string
	arguments    string
	callback     func(bool)
}

type startSpinnerMsg struct{}

// Color theme constants for consistent styling
var (
	ArmyGreen   = lipgloss.Color("58")   // #5f5f00 - darker army green
	LightGreen  = lipgloss.Color("64")   // #5f8700 - lighter army green  
	Brown       = lipgloss.Color("94")   // #875f00 - brown
	Yellow      = lipgloss.Color("226")  // #ffff00 - bright yellow
	GoldYellow  = lipgloss.Color("220")  // #ffd700 - gold yellow
	Red         = lipgloss.Color("196")  // #ff0000 - red
	Tan         = lipgloss.Color("180")  // #d7af87 - tan
)