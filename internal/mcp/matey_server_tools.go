package mcp

// GetTools returns the available MCP tools
func (m *MateyMCPServer) GetTools() []Tool {
	return []Tool{
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
						"description": "Resource type (server, memory, task-scheduler, toolbox, workflow)",
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
						"description": "Type of config (matey, workflow, toolbox, etc.)",
					},
				},
				"required": []string{"config_yaml"},
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
					"steps": map[string]interface{}{
						"type":        "array",
						"description": "Workflow steps",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name":       map[string]interface{}{"type": "string"},
								"tool":       map[string]interface{}{"type": "string"},
								"parameters": map[string]interface{}{"type": "object"},
							},
							"required": []string{"name", "tool"},
						},
					},
				},
				"required": []string{"name", "steps"},
			},
		},
		// Workflow management tools
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
			Name:        "get_workflow",
			Description: "Get details of a specific workflow",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "delete_workflow",
			Description: "Delete a workflow",
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
			Name:        "workflow_logs",
			Description: "Get workflow execution logs",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
					"step": map[string]interface{}{
						"type":        "string",
						"description": "Get logs for specific step",
					},
					"follow": map[string]interface{}{
						"type":        "boolean",
						"description": "Follow log output",
						"default":     false,
					},
					"tail": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines to show from the end",
						"default":     100,
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "pause_workflow",
			Description: "Pause a workflow",
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
			Name:        "resume_workflow",
			Description: "Resume a paused workflow",
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
		// Service management tools
		{
			Name:        "start_service",
			Description: "Start specific MCP services",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"services": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Service names to start",
					},
				},
				"required": []string{"services"},
			},
		},
		{
			Name:        "stop_service",
			Description: "Stop specific MCP services",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"services": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Service names to stop",
					},
				},
				"required": []string{"services"},
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
		// Memory service management
		{
			Name:        "memory_status",
			Description: "Get memory service status",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "memory_start",
			Description: "Start memory service",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "memory_stop",
			Description: "Stop memory service",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		// Memory graph tools
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
		// Task scheduler management
		{
			Name:        "task_scheduler_status",
			Description: "Get task scheduler status",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "task_scheduler_start",
			Description: "Start task scheduler service",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "task_scheduler_stop",
			Description: "Stop task scheduler service",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "workflow_templates",
			Description: "List available workflow templates in the task scheduler",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Filter by category",
					},
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
			},
		},
		// Toolbox management
		{
			Name:        "list_toolboxes",
			Description: "List all toolboxes in the cluster",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
			},
		},
		{
			Name:        "get_toolbox",
			Description: "Get details of a specific toolbox",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Toolbox name",
					},
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
				"required": []string{"name"},
			},
		},
		// Configuration management
		{
			Name:        "validate_config",
			Description: "Validate the matey configuration file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"config_file": map[string]interface{}{
						"type":        "string",
						"description": "Path to config file to validate",
					},
				},
			},
		},
		{
			Name:        "create_config",
			Description: "Create client configuration for MCP servers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"output_file": map[string]interface{}{
						"type":        "string",
						"description": "Output file path",
					},
				},
			},
		},
		{
			Name:        "install_matey",
			Description: "Install Matey CRDs and required Kubernetes resources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		// Inspection tools
		{
			Name:        "inspect_mcpserver",
			Description: "Inspect MCPServer resources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Server name (empty for all)",
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
			Name:        "inspect_mcpmemory",
			Description: "Inspect MCPMemory resources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Memory service name (empty for all)",
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
			Name:        "inspect_mcptaskscheduler",
			Description: "Inspect MCPTaskScheduler resources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Task scheduler name (empty for all)",
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
			Name:        "inspect_mcpproxy",
			Description: "Inspect MCPProxy resources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Proxy name (empty for all)",
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
			Name:        "inspect_mcptoolbox",
			Description: "Inspect MCPToolbox resources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Toolbox name (empty for all)",
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
			Name:        "inspect_all",
			Description: "Inspect all MCP resources in the cluster",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
			},
		},
		// Workspace Access Tools
		{
			Name:        "mount_workspace",
			Description: "Mount workspace PVC for a workflow execution to enable file access",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"workflowName": map[string]interface{}{
						"type":        "string",
						"description": "Name of the workflow",
					},
					"executionID": map[string]interface{}{
						"type":        "string",
						"description": "Execution ID of the workflow run",
					},
				},
				"required": []string{"workflowName", "executionID"},
			},
		},
		{
			Name:        "list_workspace_files",
			Description: "List files in a mounted workspace directory",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"workflowName": map[string]interface{}{
						"type":        "string",
						"description": "Name of the workflow",
					},
					"executionID": map[string]interface{}{
						"type":        "string",
						"description": "Execution ID of the workflow run",
					},
					"subPath": map[string]interface{}{
						"type":        "string",
						"description": "Subdirectory path within the workspace (optional)",
						"default":     "",
					},
				},
				"required": []string{"workflowName", "executionID"},
			},
		},
		{
			Name:        "read_workspace_file",
			Description: "Read content of a file from a mounted workspace",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
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
						"description": "Path to the file within the workspace",
					},
					"maxSize": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum file size to read in bytes (default: 1MB)",
						"default":     1048576,
					},
				},
				"required": []string{"workflowName", "executionID", "filePath"},
			},
		},
		{
			Name:        "unmount_workspace",
			Description: "Unmount a workspace PVC to free resources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"workflowName": map[string]interface{}{
						"type":        "string",
						"description": "Name of the workflow",
					},
					"executionID": map[string]interface{}{
						"type":        "string",
						"description": "Execution ID of the workflow run",
					},
				},
				"required": []string{"workflowName", "executionID"},
			},
		},
		{
			Name:        "list_mounted_workspaces",
			Description: "List all currently mounted workspaces",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "get_workspace_stats",
			Description: "Get statistics about workspace PVCs and retention policies",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		// Native tools (migrated from chat package)
		{
			Name:        "create_todos",
			Description: "Create multiple TODO items",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"todos": map[string]interface{}{
						"type": "array",
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
						"description": "Array of TODO items to create",
					},
				},
				"required": []string{"todos"},
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
	}
}