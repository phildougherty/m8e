package main

import (
	"fmt"
	"time"

	"github.com/phildougherty/m8e/internal/cmd"
)

func main() {
	// Create a new terminal chat instance to test the system prompt
	chat := cmd.NewTermChat()
	
	// Simulate getting the system context (this is what the AI sees)
	fmt.Println("=== TESTING IMPROVED SYSTEM PROMPT ===")
	fmt.Println("This is what the AI model receives as system context:")
	fmt.Println()
	
	// Wait a moment for MCP connection to establish
	time.Sleep(2 * time.Second)
	
	// Get the system context (this calls our improved getSystemContext function)
	systemPrompt := chat.GetSystemContext()
	
	fmt.Println(systemPrompt)
	
	fmt.Println()
	fmt.Println("=== END SYSTEM PROMPT ===")
}