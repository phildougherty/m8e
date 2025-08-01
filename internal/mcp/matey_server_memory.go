package mcp

import (
	"context"
	"fmt"
)

// Memory graph tool implementations - these delegate to the internal memory tools

// createEntities creates multiple new entities in the knowledge graph
func (m *MateyMCPServer) createEntities(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.memoryTools == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Memory system not initialized. Ensure MCPMemory service is running."}},
			IsError: true,
		}, fmt.Errorf("memory system not initialized")
	}
	
	result, err := m.memoryTools.ExecuteMCPTool(ctx, "create_entities", args)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error creating entities: %v", err)}},
			IsError: true,
		}, err
	}
	
	// Convert result to text content
	resultText := fmt.Sprintf("Successfully created entities: %v", result)
	return &ToolResult{
		Content: []Content{{Type: "text", Text: resultText}},
	}, nil
}

// deleteEntities deletes multiple entities and their associated relations
func (m *MateyMCPServer) deleteEntities(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.memoryTools == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Memory system not initialized. Ensure MCPMemory service is running."}},
			IsError: true,
		}, fmt.Errorf("memory system not initialized")
	}
	
	result, err := m.memoryTools.ExecuteMCPTool(ctx, "delete_entities", args)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error deleting entities: %v", err)}},
			IsError: true,
		}, err
	}
	
	resultText := fmt.Sprintf("Successfully deleted entities: %v", result)
	return &ToolResult{
		Content: []Content{{Type: "text", Text: resultText}},
	}, nil
}

// addObservations adds new observations to existing entities
func (m *MateyMCPServer) addObservations(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.memoryTools == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Memory system not initialized. Ensure MCPMemory service is running."}},
			IsError: true,
		}, fmt.Errorf("memory system not initialized")
	}
	
	result, err := m.memoryTools.ExecuteMCPTool(ctx, "add_observations", args)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error adding observations: %v", err)}},
			IsError: true,
		}, err
	}
	
	resultText := fmt.Sprintf("Successfully added observations: %v", result)
	return &ToolResult{
		Content: []Content{{Type: "text", Text: resultText}},
	}, nil
}


// createRelations creates typed relationships between entities
func (m *MateyMCPServer) createRelations(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.memoryTools == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Memory system not initialized. Ensure MCPMemory service is running."}},
			IsError: true,
		}, fmt.Errorf("memory system not initialized")
	}
	
	result, err := m.memoryTools.ExecuteMCPTool(ctx, "create_relations", args)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error creating relations: %v", err)}},
			IsError: true,
		}, err
	}
	
	resultText := fmt.Sprintf("Successfully created relations: %v", result)
	return &ToolResult{
		Content: []Content{{Type: "text", Text: resultText}},
	}, nil
}


// readGraph retrieves the entire knowledge graph
func (m *MateyMCPServer) readGraph(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.memoryTools == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Memory system not initialized. Ensure MCPMemory service is running."}},
			IsError: true,
		}, fmt.Errorf("memory system not initialized")
	}
	
	result, err := m.memoryTools.ExecuteMCPTool(ctx, "read_graph", args)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error reading graph: %v", err)}},
			IsError: true,
		}, err
	}
	
	resultText := fmt.Sprintf("Knowledge graph: %v", result)
	return &ToolResult{
		Content: []Content{{Type: "text", Text: resultText}},
	}, nil
}

// searchNodes searches for nodes using text queries with full-text search
func (m *MateyMCPServer) searchNodes(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.memoryTools == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Memory system not initialized. Ensure MCPMemory service is running."}},
			IsError: true,
		}, fmt.Errorf("memory system not initialized")
	}
	
	result, err := m.memoryTools.ExecuteMCPTool(ctx, "search_nodes", args)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error searching nodes: %v", err)}},
			IsError: true,
		}, err
	}
	
	resultText := fmt.Sprintf("Search results: %v", result)
	return &ToolResult{
		Content: []Content{{Type: "text", Text: resultText}},
	}, nil
}



