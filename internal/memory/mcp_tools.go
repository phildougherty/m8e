// internal/memory/mcp_tools.go
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
)

// MCPMemoryTools provides MCP protocol tools for memory management
type MCPMemoryTools struct {
	store  *MemoryStore
	logger logr.Logger
}

func NewMCPMemoryTools(store *MemoryStore, logger logr.Logger) *MCPMemoryTools {
	return &MCPMemoryTools{
		store:  store,
		logger: logger,
	}
}

// MCPToolDefinition represents an MCP tool definition for memory
type MCPToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// GetMCPTools returns all available memory MCP tools
func (mt *MCPMemoryTools) GetMCPTools() []MCPToolDefinition {
	return []MCPToolDefinition{
		// Entity Management Tools
		{
			Name:        "create_entities",
			Description: "Create multiple new entities in the knowledge graph",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entities": map[string]interface{}{
						"type":        "array",
						"description": "Array of entity objects to create",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name": map[string]interface{}{
									"type":        "string",
									"description": "Entity name",
								},
								"entityType": map[string]interface{}{
									"type":        "string",
									"description": "Type of entity (person, place, concept, etc.)",
								},
								"observations": map[string]interface{}{
									"type":        "array",
									"description": "Initial observations for this entity",
									"items": map[string]interface{}{
										"type": "string",
									},
								},
							},
							"required": []string{"name", "entityType"},
						},
					},
				},
				"required": []string{"entities"},
			},
		},
		{
			Name:        "delete_entities",
			Description: "Delete multiple entities and their associated relations",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entityNames": map[string]interface{}{
						"type":        "array",
						"description": "Array of entity names to delete",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
				},
				"required": []string{"entityNames"},
			},
		},
		// Observation Management Tools
		{
			Name:        "add_observations",
			Description: "Add new observations to existing entities",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"observations": map[string]interface{}{
						"type":        "array",
						"description": "Array of observations to add",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"entityName": map[string]interface{}{
									"type":        "string",
									"description": "Name of the entity",
								},
								"contents": map[string]interface{}{
									"type":        "array",
									"description": "Array of observation content",
									"items": map[string]interface{}{
										"type": "string",
									},
								},
							},
							"required": []string{"entityName", "contents"},
						},
					},
				},
				"required": []string{"observations"},
			},
		},
		{
			Name:        "delete_observations",
			Description: "Delete specific observations from entities",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"deletions": map[string]interface{}{
						"type":        "array",
						"description": "Array of observations to delete",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"entityName": map[string]interface{}{
									"type":        "string",
									"description": "Name of the entity",
								},
								"observations": map[string]interface{}{
									"type":        "array",
									"description": "Array of observation content to remove",
									"items": map[string]interface{}{
										"type": "string",
									},
								},
							},
							"required": []string{"entityName", "observations"},
						},
					},
				},
				"required": []string{"deletions"},
			},
		},
		// Relationship Management Tools
		{
			Name:        "create_relations",
			Description: "Create typed relationships between entities",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"relations": map[string]interface{}{
						"type":        "array",
						"description": "Array of relations to create",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"from": map[string]interface{}{
									"type":        "string",
									"description": "Source entity name",
								},
								"to": map[string]interface{}{
									"type":        "string",
									"description": "Target entity name",
								},
								"relationType": map[string]interface{}{
									"type":        "string",
									"description": "Type of relationship",
								},
							},
							"required": []string{"from", "to", "relationType"},
						},
					},
				},
				"required": []string{"relations"},
			},
		},
		{
			Name:        "delete_relations",
			Description: "Delete specific relationships between entities",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"relations": map[string]interface{}{
						"type":        "array",
						"description": "Array of relations to delete",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"from": map[string]interface{}{
									"type":        "string",
									"description": "Source entity name",
								},
								"to": map[string]interface{}{
									"type":        "string",
									"description": "Target entity name",
								},
								"relationType": map[string]interface{}{
									"type":        "string",
									"description": "Type of relationship",
								},
							},
							"required": []string{"from", "to", "relationType"},
						},
					},
				},
				"required": []string{"relations"},
			},
		},
		// Query and Retrieval Tools
		{
			Name:        "read_graph",
			Description: "Retrieve the entire knowledge graph",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "search_nodes",
			Description: "Search for nodes using text queries with full-text search",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query string",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "open_nodes",
			Description: "Retrieve specific nodes by their names",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"names": map[string]interface{}{
						"type":        "array",
						"description": "Array of entity names to retrieve",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
				},
				"required": []string{"names"},
			},
		},
		// System Tools
		{
			Name:        "memory_health_check",
			Description: "Check the health of the memory system",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "memory_stats",
			Description: "Get statistics about the knowledge graph",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

// ExecuteMCPTool executes an MCP tool with the given parameters
func (mt *MCPMemoryTools) ExecuteMCPTool(ctx context.Context, toolName string, parameters map[string]interface{}) (map[string]interface{}, error) {
	mt.logger.Info("Executing memory MCP tool", "tool", toolName, "parameters", parameters)

	switch toolName {
	case "create_entities":
		return mt.createEntities(ctx, parameters)
	case "delete_entities":
		return mt.deleteEntities(ctx, parameters)
	case "add_observations":
		return mt.addObservations(ctx, parameters)
	case "delete_observations":
		return mt.deleteObservations(ctx, parameters)
	case "create_relations":
		return mt.createRelations(ctx, parameters)
	case "delete_relations":
		return mt.deleteRelations(ctx, parameters)
	case "read_graph":
		return mt.readGraph(ctx, parameters)
	case "search_nodes":
		return mt.searchNodes(ctx, parameters)
	case "open_nodes":
		return mt.openNodes(ctx, parameters)
	case "memory_health_check":
		return mt.healthCheck(ctx, parameters)
	case "memory_stats":
		return mt.getStats(ctx, parameters)
	default:
		return nil, fmt.Errorf("unknown memory tool: %s", toolName)
	}
}

// Tool implementations

func (mt *MCPMemoryTools) createEntities(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	entitiesData, ok := params["entities"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("entities parameter must be an array")
	}

	var entities []Entity
	for i, entityData := range entitiesData {
		entityMap, ok := entityData.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("entity %d must be an object", i)
		}

		name, ok := entityMap["name"].(string)
		if !ok {
			return nil, fmt.Errorf("entity %d: name is required", i)
		}

		entityType, ok := entityMap["entityType"].(string)
		if !ok {
			return nil, fmt.Errorf("entity %d: entityType is required", i)
		}

		entity := Entity{
			Name:       name,
			EntityType: entityType,
		}

		// Parse observations if provided
		if obsData, exists := entityMap["observations"]; exists {
			if obsArray, ok := obsData.([]interface{}); ok {
				for _, obs := range obsArray {
					if obsStr, ok := obs.(string); ok {
						entity.Observations = append(entity.Observations, obsStr)
					}
				}
			}
		}

		entities = append(entities, entity)
	}

	if err := mt.store.CreateEntities(entities); err != nil {
		return nil, fmt.Errorf("failed to create entities: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Created %d entities", len(entities)),
		"entities": entities,
	}, nil
}

func (mt *MCPMemoryTools) deleteEntities(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	namesData, ok := params["entityNames"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("entityNames parameter must be an array")
	}

	var names []string
	for _, nameData := range namesData {
		if name, ok := nameData.(string); ok {
			names = append(names, name)
		}
	}

	if err := mt.store.DeleteEntities(names); err != nil {
		return nil, fmt.Errorf("failed to delete entities: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Deleted %d entities", len(names)),
		"deletedEntities": names,
	}, nil
}

func (mt *MCPMemoryTools) addObservations(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	observationsData, ok := params["observations"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("observations parameter must be an array")
	}

	observations := make(map[string][]string)
	for i, obsData := range observationsData {
		obsMap, ok := obsData.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("observation %d must be an object", i)
		}

		entityName, ok := obsMap["entityName"].(string)
		if !ok {
			return nil, fmt.Errorf("observation %d: entityName is required", i)
		}

		contentsData, ok := obsMap["contents"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("observation %d: contents must be an array", i)
		}

		var contents []string
		for _, contentData := range contentsData {
			if content, ok := contentData.(string); ok {
				contents = append(contents, content)
			}
		}

		observations[entityName] = append(observations[entityName], contents...)
	}

	if err := mt.store.AddObservations(observations); err != nil {
		return nil, fmt.Errorf("failed to add observations: %w", err)
	}

	totalObservations := 0
	for _, contents := range observations {
		totalObservations += len(contents)
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Added %d observations to %d entities", totalObservations, len(observations)),
		"observations": observations,
	}, nil
}

func (mt *MCPMemoryTools) deleteObservations(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	deletionsData, ok := params["deletions"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("deletions parameter must be an array")
	}

	deletions := make(map[string][]string)
	for i, delData := range deletionsData {
		delMap, ok := delData.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("deletion %d must be an object", i)
		}

		entityName, ok := delMap["entityName"].(string)
		if !ok {
			return nil, fmt.Errorf("deletion %d: entityName is required", i)
		}

		observationsData, ok := delMap["observations"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("deletion %d: observations must be an array", i)
		}

		var observations []string
		for _, obsData := range observationsData {
			if obs, ok := obsData.(string); ok {
				observations = append(observations, obs)
			}
		}

		deletions[entityName] = append(deletions[entityName], observations...)
	}

	if err := mt.store.DeleteObservations(deletions); err != nil {
		return nil, fmt.Errorf("failed to delete observations: %w", err)
	}

	totalDeletions := 0
	for _, contents := range deletions {
		totalDeletions += len(contents)
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Deleted %d observations from %d entities", totalDeletions, len(deletions)),
		"deletions": deletions,
	}, nil
}

func (mt *MCPMemoryTools) createRelations(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	relationsData, ok := params["relations"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("relations parameter must be an array")
	}

	var relations []Relation
	for i, relData := range relationsData {
		relMap, ok := relData.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("relation %d must be an object", i)
		}

		from, ok := relMap["from"].(string)
		if !ok {
			return nil, fmt.Errorf("relation %d: from is required", i)
		}

		to, ok := relMap["to"].(string)
		if !ok {
			return nil, fmt.Errorf("relation %d: to is required", i)
		}

		relationType, ok := relMap["relationType"].(string)
		if !ok {
			return nil, fmt.Errorf("relation %d: relationType is required", i)
		}

		relations = append(relations, Relation{
			From:         from,
			To:           to,
			RelationType: relationType,
		})
	}

	if err := mt.store.CreateRelations(relations); err != nil {
		return nil, fmt.Errorf("failed to create relations: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Created %d relations", len(relations)),
		"relations": relations,
	}, nil
}

func (mt *MCPMemoryTools) deleteRelations(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	relationsData, ok := params["relations"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("relations parameter must be an array")
	}

	var relations []Relation
	for i, relData := range relationsData {
		relMap, ok := relData.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("relation %d must be an object", i)
		}

		from, ok := relMap["from"].(string)
		if !ok {
			return nil, fmt.Errorf("relation %d: from is required", i)
		}

		to, ok := relMap["to"].(string)
		if !ok {
			return nil, fmt.Errorf("relation %d: to is required", i)
		}

		relationType, ok := relMap["relationType"].(string)
		if !ok {
			return nil, fmt.Errorf("relation %d: relationType is required", i)
		}

		relations = append(relations, Relation{
			From:         from,
			To:           to,
			RelationType: relationType,
		})
	}

	if err := mt.store.DeleteRelations(relations); err != nil {
		return nil, fmt.Errorf("failed to delete relations: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Deleted %d relations", len(relations)),
		"relations": relations,
	}, nil
}

func (mt *MCPMemoryTools) readGraph(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	graph, err := mt.store.ReadGraph()
	if err != nil {
		return nil, fmt.Errorf("failed to read graph: %w", err)
	}

	// Convert to JSON-serializable format
	graphData, err := json.Marshal(graph)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize graph: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(graphData, &result); err != nil {
		return nil, fmt.Errorf("failed to deserialize graph: %w", err)
	}

	// Add metadata
	result["timestamp"] = time.Now().Format(time.RFC3339)
	result["entityCount"] = len(graph.Entities)
	result["relationCount"] = len(graph.Relations)

	return result, nil
}

func (mt *MCPMemoryTools) searchNodes(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	query, ok := params["query"].(string)
	if !ok {
		return nil, fmt.Errorf("query parameter is required")
	}

	results, err := mt.store.SearchNodes(query)
	if err != nil {
		return nil, fmt.Errorf("failed to search nodes: %w", err)
	}

	return map[string]interface{}{
		"query":   query,
		"results": results,
		"count":   len(results),
	}, nil
}

func (mt *MCPMemoryTools) openNodes(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	namesData, ok := params["names"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("names parameter must be an array")
	}

	var names []string
	for _, nameData := range namesData {
		if name, ok := nameData.(string); ok {
			names = append(names, name)
		}
	}

	entities, err := mt.store.OpenNodes(names)
	if err != nil {
		return nil, fmt.Errorf("failed to open nodes: %w", err)
	}

	return map[string]interface{}{
		"requestedNames": names,
		"entities":      entities,
		"count":         len(entities),
	}, nil
}

func (mt *MCPMemoryTools) healthCheck(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	if err := mt.store.HealthCheck(); err != nil {
		return map[string]interface{}{
			"status":    "unhealthy",
			"error":     err.Error(),
			"timestamp": time.Now().Format(time.RFC3339),
		}, nil
	}

	return map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"database":  "connected",
	}, nil
}

func (mt *MCPMemoryTools) getStats(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	stats, err := mt.store.GetStats()
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	stats["timestamp"] = time.Now().Format(time.RFC3339)
	return stats, nil
}