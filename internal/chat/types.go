package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/glamour"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/phildougherty/m8e/internal/ai"
	"github.com/phildougherty/m8e/internal/mcp"
	appcontext "github.com/phildougherty/m8e/internal/context"
)

// TodoStatus represents the status of a TODO item
type TodoStatus string

const (
	TodoStatusPending    TodoStatus = "pending"
	TodoStatusInProgress TodoStatus = "in_progress"
	TodoStatusCompleted  TodoStatus = "completed"
	TodoStatusCancelled  TodoStatus = "cancelled"
)

// TodoPriority represents the priority level of a TODO item
type TodoPriority string

const (
	TodoPriorityLow    TodoPriority = "low"
	TodoPriorityMedium TodoPriority = "medium"
	TodoPriorityHigh   TodoPriority = "high"
	TodoPriorityUrgent TodoPriority = "urgent"
)

// TodoItem represents a single TODO item in the AI's task list
type TodoItem struct {
	ID          string       `json:"id"`
	Content     string       `json:"content"`
	Status      TodoStatus   `json:"status"`
	Priority    TodoPriority `json:"priority"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
	CompletedAt *time.Time   `json:"completedAt,omitempty"`
}

// TodoList manages a collection of TODO items for the AI agent
type TodoList struct {
	Items []TodoItem `json:"items"`
}

// AddItem adds a new TODO item to the list
func (tl *TodoList) AddItem(content string, priority TodoPriority) string {
	now := time.Now()
	id := fmt.Sprintf("todo_%d", now.UnixNano())
	item := TodoItem{
		ID:        id,
		Content:   content,
		Status:    TodoStatusPending,
		Priority:  priority,
		CreatedAt: now,
		UpdatedAt: now,
	}
	tl.Items = append(tl.Items, item)
	return id
}

// UpdateItemStatus updates the status of a TODO item
func (tl *TodoList) UpdateItemStatus(id string, status TodoStatus) bool {
	for i, item := range tl.Items {
		if item.ID == id {
			tl.Items[i].Status = status
			tl.Items[i].UpdatedAt = time.Now()
			if status == TodoStatusCompleted {
				now := time.Now()
				tl.Items[i].CompletedAt = &now
			}
			return true
		}
	}
	return false
}

// RemoveItem removes a TODO item from the list
func (tl *TodoList) RemoveItem(id string) bool {
	for i, item := range tl.Items {
		if item.ID == id {
			tl.Items = append(tl.Items[:i], tl.Items[i+1:]...)
			return true
		}
	}
	return false
}

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
	voiceManager           *VoiceManager // Voice interaction manager
	contextManager         *appcontext.ContextManager // Context management for AI workflows
	fileDiscovery          *appcontext.FileDiscovery   // File discovery system
	mentionProcessor       *appcontext.MentionProcessor // @-mention processing
	todoList               *TodoList                    // TODO list for task management
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
	viewportOffset        int    // Scroll offset for viewport
	statusLine            string
	width                 int
	height                int
	inputFocused          bool
	ready                 bool
	loading               bool
	spinnerFrame          int
	currentSpinnerQuote   string // Store current quote for this loading session
	confirmationMode      bool
	pendingFunctionCall   *FunctionConfirmation
	aiResponseStartIdx    int    // Track where AI response content starts for replacement
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

type voiceTranscriptMsg struct {
	transcript string
}


type voiceTTSReadyMsg struct {
	audioData []byte
}

type contextUpdateMsg struct {
	action      string // "added", "removed", "cleared"
	path        string
	stats       map[string]interface{}
	windowStats *appcontext.ContextWindow
}

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