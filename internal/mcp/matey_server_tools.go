package mcp

// GetTools returns the available MCP tools - slimmed down version with 21 consolidated tools
func (m *MateyMCPServer) GetTools() []Tool {
	return []Tool{
		// Core Cluster Management (5 tools)
		{
			Name:        "matey_ps",
			Description: "Get status of all MCP servers in the cluster",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"watch": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to watch for live updates",
						"default":     false,
					},
					"filter": map[string]interface{}{
						"type":        "string",
						"description": "Filter by status, namespace, or labels",
					},
				},
			},
		},
		{
			Name:        "matey_up",
			Description: "Start all or specific MCP services",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"services": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Specific services to start (empty for all)",
					},
				},
			},
		},
		{
			Name:        "matey_down",
			Description: "Stop all or specific MCP services",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"services": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Specific services to stop (empty for all)",
					},
				},
			},
		},
		{
			Name:        "get_cluster_state",
			Description: "Get current state of the cluster and MCP resources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"include_pods": map[string]interface{}{
						"type":        "boolean",
						"description": "Include pod information",
						"default":     true,
					},
					"include_logs": map[string]interface{}{
						"type":        "boolean",
						"description": "Include recent logs",
						"default":     false,
					},
				},
			},
		},
		{
			Name:        "matey_logs",
			Description: "Get logs from MCP servers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"server": map[string]interface{}{
						"type":        "string",
						"description": "Server name to get logs from",
					},
					"follow": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to follow logs",
						"default":     false,
					},
					"tail": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines to tail",
						"default":     100,
					},
				},
			},
		},
		{
			Name:        "matey_inspect",
			Description: "Get detailed information about MCP resources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"resource_type": map[string]interface{}{
						"type":        "string",
						"description": "Resource type (server, memory, task-scheduler, workflow, proxy)",
					},
					"resource_name": map[string]interface{}{
						"type":        "string",
						"description": "Specific resource name",
					},
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
			},
		},

		// Memory/Knowledge Graph (6 tools - consolidated from memory server)
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
			Name:        "read_graph",
			Description: "Retrieve the entire knowledge graph",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
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

		// Task Management (3 tools - consolidated)
		{
			Name:        "manage_todos",
			Description: "Create, update, list, and clear TODO items with unified interface",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "Action to perform (create, list, update, clear, stats)",
						"enum":        []string{"create", "list", "update", "clear", "stats"},
					},
					"todos": map[string]interface{}{
						"type":        "array",
						"description": "Array of TODO items (for create action)",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"content": map[string]interface{}{
									"type":        "string",
									"description": "TODO item content",
								},
								"priority": map[string]interface{}{
									"type":        "string",
									"description": "Priority level (low, medium, high, urgent)",
									"default":     "medium",
								},
							},
							"required": []string{"content"},
						},
					},
					"id": map[string]interface{}{
						"type":        "string",
						"description": "TODO item ID (for update action)",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"description": "Status filter (for list) or new status (for update)",
						"enum":        []string{"pending", "in_progress", "completed"},
					},
					"priority": map[string]interface{}{
						"type":        "string",
						"description": "Priority filter (for list action)",
						"enum":        []string{"low", "medium", "high", "urgent"},
					},
				},
				"required": []string{"action"},
			},
		},
		{
			Name:        "create_workflow",
			Description: "Create a workflow from provided configuration",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Workflow description",
					},
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "Cron schedule for workflow",
					},
					"timezone": map[string]interface{}{
						"type":        "string",
						"description": "Timezone for schedule (e.g., America/New_York)",
						"default":     "UTC",
					},
					"enabled": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the workflow is enabled",
						"default":     true,
					},
					"concurrencyPolicy": map[string]interface{}{
						"type":        "string",
						"description": "Concurrency policy (Allow, Forbid, Replace)",
						"default":     "Allow",
					},
					"timeout": map[string]interface{}{
						"type":        "string",
						"description": "Workflow timeout (e.g., '30m', '1h')",
					},
					"retryPolicy": map[string]interface{}{
						"type":        "object",
						"description": "Retry policy configuration",
						"properties": map[string]interface{}{
							"maxRetries": map[string]interface{}{
								"type":        "integer",
								"description": "Maximum retry attempts",
							},
							"retryDelay": map[string]interface{}{
								"type":        "string",
								"description": "Delay between retries (e.g., '5m')",
							},
							"backoffStrategy": map[string]interface{}{
								"type":        "string",
								"description": "Backoff strategy (Linear, Exponential)",
							},
						},
					},
					"steps": map[string]interface{}{
						"type":        "array",
						"description": "Workflow steps",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name": map[string]interface{}{
									"type":        "string",
									"description": "Step name",
								},
								"tool": map[string]interface{}{
									"type":        "string",
									"description": "Tool to execute",
								},
								"parameters": map[string]interface{}{
									"type":        "object",
									"description": "Parameters for the tool",
								},
								"condition": map[string]interface{}{
									"type":        "string",
									"description": "Condition for step execution",
								},
								"continueOnError": map[string]interface{}{
									"type":        "boolean",
									"description": "Continue workflow if step fails",
									"default":     false,
								},
								"dependsOn": map[string]interface{}{
									"type":        "array",
									"items":       map[string]interface{}{"type": "string"},
									"description": "Steps this step depends on",
								},
								"timeout": map[string]interface{}{
									"type":        "string",
									"description": "Step timeout (e.g., '5m')",
								},
							},
							"required": []string{"name", "tool"},
						},
					},
				},
				"required": []string{"name", "steps"},
			},
		},
		{
			Name:        "list_workflows",
			Description: "List all workflows in the cluster",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"all_namespaces": map[string]interface{}{
						"type":        "boolean",
						"description": "List workflows from all namespaces",
						"default":     false,
					},
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
			},
		},
		{
			Name:        "execute_workflow",
			Description: "Manually execute a workflow",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "delete_workflow",
			Description: "Delete a workflow from the task scheduler",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name to delete",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "workflow_logs",
			Description: "Get consolidated workflow execution logs and history with detailed step information",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name to get logs for",
					},
					"execution_id": map[string]interface{}{
						"type":        "string",
						"description": "Specific execution ID to get detailed logs for (optional)",
					},
					"step": map[string]interface{}{
						"type":        "string",
						"description": "Specific step name to focus on (optional)",
					},
					"tail": map[string]interface{}{
						"type":        "integer",
						"description": "Number of recent executions to show (default: 10)",
						"default":     10,
					},
				},
				"required": []string{"name"},
			},
		},

		// Agent & Execution (2 tools)
		{
			Name:        "execute_agent",
			Description: "Spawn a focused sub-agent for specialized tasks like research, file analysis, or code extraction",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"objective": map[string]interface{}{
						"type":        "string",
						"description": "The specific objective or research question for the agent",
					},
					"behavioral_instructions": map[string]interface{}{
						"type":        "string",
						"description": "Custom behavior modifications (e.g., 'RESEARCH ONLY - return structured findings', 'NO EXPLANATIONS - data only')",
					},
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Expected output format: json, markdown, structured_data, list",
						"default":     "structured_data",
					},
					"context": map[string]interface{}{
						"type":        "string",
						"description": "Additional context for the agent",
					},
					"max_turns": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum conversation turns for the agent",
						"default":     20,
					},
					"timeout_seconds": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum execution time in seconds",
						"default":     600,
					},
					"ai_provider": map[string]interface{}{
						"type":        "string",
						"description": "AI provider to use (openrouter, claude, openai, ollama)",
					},
					"ai_model": map[string]interface{}{
						"type":        "string",
						"description": "AI model to use for reasoning",
					},
				},
				"required": []string{"objective"},
			},
		},
		{
			Name:        "execute_bash",
			Description: "Execute bash commands with security validation",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Bash command to execute",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Timeout in seconds (max 600)",
						"default":     120,
					},
					"working_directory": map[string]interface{}{
						"type":        "string",
						"description": "Working directory for command execution",
					},
				},
				"required": []string{"command"},
			},
		},

		// Workspace Management (2 tools - consolidated)
		{
			Name:        "workspace_files",
			Description: "List, read, and manage files in workspaces with unified interface",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "Action to perform (list, read, mount, unmount, stats)",
						"enum":        []string{"list", "read", "mount", "unmount", "stats"},
					},
					"workflowName": map[string]interface{}{
						"type":        "string",
						"description": "Name of the workflow",
					},
					"executionID": map[string]interface{}{
						"type":        "string",
						"description": "Execution ID of the workflow run",
					},
					"filePath": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file within the workspace (for read action)",
					},
					"subPath": map[string]interface{}{
						"type":        "string",
						"description": "Subdirectory path within the workspace (for list action)",
						"default":     "",
					},
					"maxSize": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum file size to read in bytes (default: 1MB)",
						"default":     1048576,
					},
				},
				"required": []string{"action"},
			},
		},
		{
			Name:        "search_in_files",
			Description: "Search for patterns within file contents with regex support",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Search pattern or regex",
					},
					"files": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Specific file paths to search in",
					},
					"file_pattern": map[string]interface{}{
						"type":        "string",
						"description": "Glob pattern for files to search (e.g., '*.go', 'internal/**/*.go')",
					},
					"regex": map[string]interface{}{
						"type":        "boolean",
						"description": "Treat pattern as regex",
						"default":     false,
					},
					"case_sensitive": map[string]interface{}{
						"type":        "boolean",
						"description": "Case sensitive search",
						"default":     false,
					},
					"max_results": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results per file",
						"default":     100,
					},
					"context_lines": map[string]interface{}{
						"type":        "integer",
						"description": "Number of context lines to show around matches",
						"default":     2,
					},
				},
				"required": []string{"pattern"},
			},
		},

		// Configuration (2 tools)
		{
			Name:        "apply_config",
			Description: "Apply a YAML configuration to the cluster",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"config_yaml": map[string]interface{}{
						"type":        "string",
						"description": "YAML configuration to apply",
					},
					"config_type": map[string]interface{}{
						"type":        "string",
						"description": "Type of config (matey, workflow, etc.)",
					},
				},
				"required": []string{"config_yaml"},
			},
		},
		{
			Name:        "reload_proxy",
			Description: "Reload MCP proxy configuration to discover new servers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}