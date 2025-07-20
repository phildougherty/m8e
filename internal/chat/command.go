package chat

import (
	"github.com/spf13/cobra"
)

// NewChatCommand creates the chat command that integrates with the Cobra CLI
func NewChatCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Start an interactive AI chat session for MCP server orchestration",
		Long: `Start an interactive AI chat session with Matey's specialized assistant.

The chat interface provides:
• Expert knowledge of Kubernetes-native MCP server orchestration
• Real-time AI assistance for infrastructure management
• Function calling for direct system interaction
• Multiple approval modes (MANUAL, AUTO, YOLO)
• Enhanced UI with colorful formatting and progress tracking

The AI assistant specializes in:
• Deploying and scaling microservices
• Setting up monitoring and alerting
• Creating CI/CD pipelines
• Managing Kubernetes resources
• MCP protocol orchestration
• Infrastructure automation

Keyboard shortcuts:
• ESC: Cancel current operation
• Ctrl+C: Exit chat
• Ctrl+L: Clear screen
• Ctrl+Y: Toggle YOLO/MANUAL mode
• Tab: Toggle AUTO-EDIT/MANUAL mode
• Ctrl+R: Toggle verbose/compact output
• Ctrl+V: Toggle voice mode
• Spacebar: Push-to-talk (when voice enabled)

Slash commands:
• /auto - Enable smart automation mode
• /yolo - Enable maximum autonomy mode
• /status - Show system status
• /help - Show all commands`,
		RunE: func(cmd *cobra.Command, args []string) error {
			tc := NewTermChat()
			return tc.Run()
		},
	}
}