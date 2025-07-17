package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/phildougherty/m8e/internal/mcp"
)

func main() {
	// Create MCP client pointing to local server
	client := mcp.NewMCPClient("http://localhost:8081")
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	// Test create_workflow with proper parameters
	arguments := map[string]interface{}{
		"name": "test-glucose-workflow",
		"description": "Test glucose tracking workflow", 
		"steps": []map[string]interface{}{
			{
				"name": "check-glucose",
				"tool": "get_current_glucose",
			},
		},
	}
	
	fmt.Printf("Testing create_workflow with arguments: %s\n", mustJSON(arguments))
	
	// Execute the tool call
	result := client.ExecuteToolCall(ctx, "matey", "create_workflow", arguments)
	
	fmt.Printf("Result: %s\n", result)
}

func mustJSON(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}