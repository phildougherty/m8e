package cmd

import (
	"github.com/spf13/cobra"
	"github.com/phildougherty/m8e/internal/chat"
)

// NewChatCommand creates the chat command using the refactored chat package
func NewChatCommand() *cobra.Command {
	return chat.NewChatCommand()
}