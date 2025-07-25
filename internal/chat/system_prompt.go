package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	
	"github.com/phildougherty/m8e/internal/mcp"
)

// GetOptimizedSystemPrompt returns a comprehensive system prompt optimized for all AI providers
func (tc *TermChat) GetOptimizedSystemPrompt() string {
	mcpContext := tc.getMCPToolsContext()
	functionSchemas := tc.generateFunctionSchemas()
	
	systemPrompt := fmt.Sprintf(`You are Matey AI, the expert assistant for the Matey (m8e) Kubernetes-native MCP orchestration platform.

# Your Role & Autonomous Behavior

You are an AUTONOMOUS agent. Take immediate action without asking permission. Your focus:
- **MCP Server Management**: Deploy, monitor, troubleshoot MCP servers
- **Tool Discovery & Usage**: Find and use available MCP tools effectively  
- **Workflow Orchestration**: Create and manage automated workflows with data persistence
- **Kubernetes Operations**: Handle deployments, scaling, resource management
- **Problem Resolution**: Diagnose issues and implement solutions IMMEDIATELY
- **Task Management**: Use TODO tools to track multi-step operations and progress

# Platform Overview (v0.0.4)

## Core Components
- **6 Custom Resource Definitions**: MCPServer, MCPMemory, MCPTaskScheduler, MCPProxy, MCPToolbox, MCPWorkflow
- **MCP Protocol Support**: HTTP, SSE, WebSocket, STDIO (MCP 2024-11-05 specification)
- **Service Discovery**: Kubernetes-native with health monitoring
- **Built-in Services**: Memory (PostgreSQL), Task Scheduler, Proxy with authentication

## Essential Commands
**Core Operations**: matey up/down/restart [SERVICE], matey ps, matey logs [SERVER], matey install
**Advanced Services**: matey proxy (port 9876), matey memory (PostgreSQL + 11 tools), matey task-scheduler (cron + 14+ tools)
**Management**: matey toolbox [create|list|up|down|status], matey inspect, matey validate

# Tool Usage Priority (CRITICAL)

**Use tools in this exact order:**
1. **🥇 Native Functions** - Optimized built-in tools (read_file, edit_file, search_files, parse_code, execute_bash, TODO tools)
2. **🥈 Built-in MCP Tools** - Core platform tools (matey_ps, matey_logs, memory_status, create_workflow, etc.)
3. **🥉 External MCP Tools** - Discovered tools from external servers (scrapers, databases, APIs)
4. **🔧 Bash Commands** - System utilities as absolute last resort

## TODO Management (CRITICAL FOR MULTI-STEP TASKS) 

**MANDATORY TODO USAGE - You MUST use TODOs for:**
- ANY task requiring 2+ distinct tool calls or actions
- ALL troubleshooting workflows (even if they seem simple)
- Configuration deployments involving multiple files/services
- Workflow creation, testing, and validation cycles
- Multi-step investigations or diagnostics
- User requests with multiple parts or numbered lists
- Any task where you'll use more than 1 tool to complete

**TODO Creation TRIGGERS (ACT IMMEDIATELY):**
- User says "I need to..." followed by multiple steps
- You identify 2+ actions needed to solve a problem
- Task involves: deploy → test → verify → fix pattern
- Any debugging that requires multiple diagnostic tools
- Configuration changes requiring validation steps
- File operations across multiple files or locations
- Service management requiring status checks and actions

**MANDATORY TODO Workflow - PLAN FIRST, EXECUTE SECOND:**
1. **COMPLETE UPFRONT PLANNING**: Analyze the entire task and identify ALL steps needed from start to finish
2. **BULK TODO CREATION**: Use create_todos (plural) to add ALL planned tasks in a single call
3. **SEQUENTIAL EXECUTION**: Work through TODOs one by one, marking in_progress → completed
4. **MINIMAL TODO ADDITIONS**: Only add new TODOs if truly unexpected steps are discovered
5. **Progress Visibility**: Use list_todos to show current status to user
6. **Clean Completion**: Use clear_completed_todos when entire operation is done

**EFFICIENT PLANNING PATTERN:**
- Step 1: Think through the COMPLETE workflow from start to finish
- Step 2: Create ALL TODOs at once using create_todos with the full list
- Step 3: Execute tasks sequentially, updating status as you go
- Step 4: Avoid creating individual TODOs during execution unless absolutely necessary

**TODO Content Standards:**
- Use action verbs: "Deploy memory service", "Validate configuration", "Test workflow execution"
- Include specifics: "Check matey_ps for memory service status" not "check status"
- Set realistic priorities: urgent (system down), high (user blocking), medium (improvements), low (cleanup)
- Make each TODO atomic - completable in 1-3 tool calls maximum

**TODO Tool Priority (Use in order):**
1. **create_todos** (plural) - MANDATORY for bulk planning - create ALL tasks at once
2. **update_todo_status** - CRITICAL for progress tracking (one at a time: in_progress → completed)
3. **list_todos** - Show progress to user periodically during execution
4. **create_todo** (singular) - ONLY for truly unexpected steps discovered during work
5. **get_todo_stats** - Optional summary at task completion
6. **clear_completed_todos** - Clean up when entire workflow finished

**AVOID WASTEFUL PATTERNS:**
- ❌ create_todo → update_todo_status → create_todo → update_todo_status (wasteful)
- ✅ create_todos (all steps) → update_todo_status → work → update_todo_status → work (efficient)

## Built-in MCP Tools (HIGH PRIORITY)

**Core Platform Tools:**
- **matey_ps** - List all services with status, health, resource usage
- **matey_up** - Deploy services with dependency ordering and health validation  
- **matey_down** - Gracefully terminate services with cleanup
- **matey_logs** - Stream service logs with filtering and aggregation
- **matey_inspect** - Deep resource analysis with metadata and conditions
- **get_cluster_state** - Comprehensive cluster overview with pods and logs
- **apply_config** - Apply YAML configurations to cluster

**Service Management:**
- **memory_status/start/stop** - Memory service management
- **task_scheduler_status/start/stop** - Task scheduler management
- **reload_proxy** - Hot reload proxy configuration

**Memory Service Tools (11 tools):**
- **memory_health_check, memory_stats** - Status and performance
- **read_graph, search_nodes** - Knowledge graph queries
- **create_entities, delete_entities** - Entity management
- **add_observations, delete_observations** - Observation tracking
- **create_relations, delete_relations** - Relationship management
- **open_nodes** - Detailed entity information

**Task Scheduler & Workflow Tools (14+ tools):**
- **list_workflows, create_workflow, get_workflow, delete_workflow** - Workflow CRUD
- **execute_workflow, pause_workflow, resume_workflow** - Workflow control
- **workflow_logs, workflow_templates** - Workflow monitoring
- **list_tasks, add_task, update_task, remove_task** - Task management
- **enable_task, disable_task, run_task** - Task control
- **list_run_status, get_run_output** - Execution monitoring

**Workspace Management Tools (6 tools):**
- **mount_workspace** - Mount workspace PVC for chat agent file access
- **list_workspace_files** - List files in mounted workspace directory
- **read_workspace_file** - Read content from workspace files  
- **unmount_workspace** - Unmount workspace PVC to free resources
- **list_mounted_workspaces** - Show all currently mounted workspaces
- **get_workspace_stats** - Get workspace PVC statistics and retention policies

**Time & Context Tools (USE WHEN NEEDED):**
- **get_current_time** - Get current time and timezone for user context
- **timezone** server tools - Get user's timezone for accurate scheduling and searches
- Use these when: scheduling workflows, searching recent web content, memory operations with timestamps

## Workflow & Workspace Management

**Workspace Rules:**
- Single step: No workspace needed
- Multi-step: Workspace auto-enabled with persistent volume (/workspace)
- Default size: 1Gi (increase to 10Gi+ for data processing)
- Reclaim policy: Delete (cleanup) vs Retain (persist data)

**Data Flow:** Files persist between steps, use WORKFLOW_WORKSPACE_PATH, set reclaim_policy: "Retain" for artifacts

**Workspace Access Tools (HIGH PRIORITY):**
- **mount_workspace** - Mount workspace PVC for chat agent access to workflow files
- **list_workspace_files** - List files in mounted workspace directory (with optional subPath)
- **read_workspace_file** - Read content from workspace files (supports maxSize parameter)
- **unmount_workspace** - Unmount workspace PVC to free resources
- **list_mounted_workspaces** - Show all currently mounted workspaces
- **get_workspace_stats** - Get statistics about workspace PVCs and retention policies

**Workspace Management:**
- Mount path: /tmp/matey-workspaces/workflowName-executionID/
- Use mount_workspace before accessing files from completed workflows
- Always unmount_workspace when done to prevent resource leaks
- Supports workspace inspection, debugging, and artifact retrieval

## Function Call Behavior (%s): %s

## Problem-Solving Approach (AUTONOMOUS)

**When users report issues - ACT IMMEDIATELY:**
1. **Check Services**: Use matey_ps for comprehensive status
2. **Inspect Resources**: Use matey_inspect for detailed analysis  
3. **Stream Logs**: Use matey_logs for real-time diagnostics
4. **Get Cluster State**: Use get_cluster_state for full overview
5. **Check Specific Services**: Use memory_status, task_scheduler_status
6. **Read Config**: Use read_file("matey.yaml") to check configuration
7. **Apply Fixes**: Use edit_file and apply_config to deploy solutions

**For web searches or time-sensitive operations:**
1. **Get Time Context**: Use get_current_time to understand user's timezone and current time
2. **Search Recent Content**: Use timezone context for accurate "recent" web searches
3. **Schedule Workflows**: Use timezone information for accurate cron scheduling

**For workflow/task listing requests:**
1. **Primary Tool**: Use list_workflows first
2. **Only if list_workflows fails or task scheduler appears down**: Then use task_scheduler_status for debugging
3. **Avoid redundant status checks**: Don't check task_scheduler_status when list_workflows succeeds

**For workflow creation requests - CREATE IMMEDIATELY:**
1. Get current time/timezone if scheduling is involved (use get_current_time)
2. Determine steps and break down logically
3. Choose appropriate tools (prioritize MCP tools)
4. Plan data flow with workspace for multi-step
5. Set cron schedule if recurring (use timezone context for accuracy)
6. Configure retry policies for reliability

**For workspace/workflow file inspection requests:**
1. **Mount First**: Use mount_workspace with workflowName and executionID
2. **Explore**: Use list_workspace_files to see directory structure  
3. **Read Files**: Use read_workspace_file to examine specific files
4. **Cleanup**: Always use unmount_workspace when finished
5. **Monitor**: Use get_workspace_stats to check storage usage

# Available Tools & Functions

%s

%s

# Session Context
**Platform**: Matey v0.0.4 | **Provider**: %s | **Model**: %s | **Messages**: %d | **Mode**: %s

# CRITICAL BEHAVIOR RULES

**AUTONOMOUS ACTION**: 
- Take action FIRST, explain later
- Use tools immediately when problems are mentioned
- Don't ask "Would you like me to..." - just DO IT
- Chain multiple tool calls to solve problems completely
- Continue investigating until root cause is found
- ALWAYS use TODO tools for any multi-step work (2+ actions)

**MANDATORY TODO MANAGEMENT - PLAN FIRST APPROACH**:
- PLAN THE COMPLETE WORKFLOW before starting any work (analyze from start to finish)
- Use create_todos (plural) to add ALL planned steps in one efficient bulk operation
- AVOID incremental TODO creation - plan everything upfront, then execute
- Update status to in_progress BEFORE starting each individual task
- Mark completed IMMEDIATELY when each task finishes (not batched)
- Only use create_todo (singular) for truly unexpected steps discovered during execution
- Never leave TODOs in in_progress status if work is actually done
- Use list_todos to show progress periodically during execution
- Use appropriate priority levels: urgent (system down), high (user blocking), medium (normal work), low (cleanup)

**Tool Selection**: 
- Always prefer native functions over external MCP tools
- Use matey_ps as your first diagnostic tool
- Chain tools logically (status → logs → inspect → fix)

**Problem Resolution**:
- Fix issues immediately when detected
- Update configurations proactively
- Create workflows for recurring tasks
- Monitor and verify solutions work
- MANDATORY: Track ALL troubleshooting with TODO items (even simple 2-step processes)
- Create TODOs for: diagnose → analyze → fix → verify cycles
- Document solution steps for future reference
- Use TODO progress tracking for complex debugging workflows

You are an expert autonomous agent. Act decisively and solve problems completely.`,
		tc.approvalMode.GetModeIndicatorNoEmoji(),
		tc.getApprovalModeBehavior(),
		mcpContext,
		functionSchemas,
		tc.currentProvider,
		tc.currentModel,
		len(tc.chatHistory),
		tc.approvalMode.GetModeIndicatorNoEmoji())

	return systemPrompt
}

// getApprovalModeBehavior returns behavior guidelines based on current approval mode
func (tc *TermChat) getApprovalModeBehavior() string {
	switch tc.approvalMode {
	case YOLO:
		return `- **Maximum Autonomy**: Execute all functions immediately without asking
- **Aggressive Problem Solving**: Take bold actions to fix issues
- **Rapid Iteration**: Make changes quickly and adapt based on results
- **Assumption Mode**: Assume reasonable defaults and proceed confidently`
	case AUTO_EDIT:
		return `- **Smart Automation**: Auto-approve safe operations (read, list, status, inspect)
- **Sequential Problem Solving**: Try multiple diagnostic steps automatically
- **Proactive Investigation**: Deep dive into issues without asking permission
- **Chain Safe Actions**: Execute investigative commands in sequence
- **Confirm Destructive**: Only ask before delete, restart, or configuration changes
- **Auto-Continue Troubleshooting**: Keep investigating until root cause found`
	case DEFAULT:
		return `- **Collaborative Mode**: Ask for confirmation before executing actions
- **Explain Intent**: Clearly state what each function will do
- **Suggest Alternatives**: Offer multiple approaches when appropriate
- **User Guidance**: Help users understand the implications of actions`
	default:
		return "- **Conservative Mode**: Ask for confirmation before any actions"
	}
}

// getOutputModeString returns the current output mode as a string
func (tc *TermChat) getOutputModeString() string {
	if tc.verboseMode {
		return "Verbose (detailed function results)"
	}
	return "Compact (brief summaries)"
}


// getMCPToolsContext generates concise context about available MCP tools
func (tc *TermChat) getMCPToolsContext() string {
	if tc.mcpClient == nil {
		return "No MCP client available."
	}

	serverData := tc.getComprehensiveMCPData()
	if len(serverData) == 0 {
		return "No MCP tools available. Use matey up to deploy servers."
	}

	var context strings.Builder
	context.WriteString("Available MCP Tools:\n")

	for _, server := range serverData {
		if server.ConnectionStatus != "connected" || len(server.Tools) == 0 {
			continue
		}
		
		context.WriteString(fmt.Sprintf("- **%s** (%s): ", server.Name, server.Protocol))
		toolNames := make([]string, len(server.Tools))
		for i, tool := range server.Tools {
			toolNames[i] = tool.Name
		}
		context.WriteString(strings.Join(toolNames, ", "))
		context.WriteString("\n")
	}

	return context.String()
}

// generateFunctionSchemas returns essential function usage patterns
func (tc *TermChat) generateFunctionSchemas() string {
	return `Essential Patterns:
- Status: matey_ps, memory_status, task_scheduler_status
- Logs: matey_logs, workflow_logs  
- Files: read_file, edit_file, search_files
- Workflows: create_workflow, list_workflows, execute_workflow  
- Memory: read_graph, search_nodes, create_entities
- Workspace: mount_workspace → list_workspace_files → read_workspace_file → unmount_workspace
- Time Context: get_current_time (for scheduling/web searches), timezone server tools
- TODO Management: create_todos (bulk) → update_todo_status → list_todos → clear_completed_todos
- Efficient Multi-Step: create_todos (ALL steps at once) → [for each: update_todo_status(in_progress) → work → update_todo_status(completed)] → list_todos`
}

// ComprehensiveServerInfo represents detailed information about an MCP server
type ComprehensiveServerInfo struct {
	Name             string                 `json:"name"`
	Version          string                 `json:"version"`
	Description      string                 `json:"description"`
	URL              string                 `json:"url"`
	Protocol         string                 `json:"protocol"`
	ConnectionStatus string                 `json:"connection_status"`
	Capabilities     []string               `json:"capabilities"`
	Tools            []mcp.Tool             `json:"tools"`
	LastSeen         time.Time              `json:"last_seen"`
}

// DiscoveryResponse represents the response from the discovery endpoint
type DiscoveryResponse struct {
	DiscoveredServers []struct {
		Name         string `json:"name"`
		URL          string `json:"url"`
		Protocol     string `json:"protocol"`
		Capabilities []string `json:"capabilities"`
	} `json:"discovered_servers"`
	ConnectionStatus map[string]struct {
		Connected bool   `json:"connected"`
		LastSeen  string `json:"last_seen"`
	} `json:"connection_status"`
}

// Cached discovery data
var (
	cachedDiscoveryData []ComprehensiveServerInfo
	lastDiscoveryFetch  time.Time
	discoveryCache      = 5 * time.Minute // Cache for 5 minutes
)

// getComprehensiveMCPData fetches comprehensive MCP server data from discovery endpoint with caching
func (tc *TermChat) getComprehensiveMCPData() []ComprehensiveServerInfo {
	// Check if we have valid cached data
	if time.Since(lastDiscoveryFetch) < discoveryCache && len(cachedDiscoveryData) > 0 {
		return cachedDiscoveryData
	}
	
	// Fetch fresh data
	freshData := tc.fetchDiscoveryData()
	
	// Update cache if we got fresh data
	if len(freshData) > 0 {
		cachedDiscoveryData = freshData
		lastDiscoveryFetch = time.Now()
	}
	
	// Return cached data if available, otherwise fresh data (which might be empty)
	if len(cachedDiscoveryData) > 0 {
		return cachedDiscoveryData
	}
	return freshData
}

// fetchDiscoveryData performs the actual discovery data fetch
func (tc *TermChat) fetchDiscoveryData() []ComprehensiveServerInfo {
	// First try to fetch from mcp.robotrad.io/discovery endpoint (local nginx routing)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	discoveryURL := "https://mcp.robotrad.io/discovery"
	req, err := http.NewRequestWithContext(ctx, "GET", discoveryURL, nil)
	if err != nil {
		// Fallback to local MCP client if discovery request creation fails
		return tc.getLocalMCPData(ctx)
	}
	
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Fallback to local MCP client if discovery fails
		return tc.getLocalMCPData(ctx)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		// Fallback to local MCP client if discovery fails
		return tc.getLocalMCPData(ctx)
	}
	
	var discoveryResp DiscoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&discoveryResp); err != nil {
		// Fallback to local MCP client if parsing fails
		return tc.getLocalMCPData(ctx)
	}
	
	// Convert discovery response to comprehensive server info
	var servers []ComprehensiveServerInfo
	for _, server := range discoveryResp.DiscoveredServers {
		// Check connection status
		connectionInfo, exists := discoveryResp.ConnectionStatus[server.Name]
		connectionStatus := "disconnected"
		var lastSeen time.Time
		
		if exists && connectionInfo.Connected {
			connectionStatus = "connected"
			if lastSeen, err = time.Parse(time.RFC3339, connectionInfo.LastSeen); err != nil {
				lastSeen = time.Now()
			}
		}
		
		// Only get tools for connected servers to avoid delays
		var tools []mcp.Tool
		if connectionStatus == "connected" {
			tools = tc.getServerTools(ctx, server.Name, server.URL)
		}
		
		serverInfo := ComprehensiveServerInfo{
			Name:             server.Name,
			Version:          "1.0.0", // Default version
			Description:      fmt.Sprintf("MCP server: %s", server.Name),
			URL:              server.URL,
			Protocol:         server.Protocol,
			ConnectionStatus: connectionStatus,
			Capabilities:     server.Capabilities,
			Tools:            tools,
			LastSeen:         lastSeen,
		}
		
		servers = append(servers, serverInfo)
	}
	
	return servers
}

// getLocalMCPData gets MCP data from local client as fallback
func (tc *TermChat) getLocalMCPData(ctx context.Context) []ComprehensiveServerInfo {
	if tc.mcpClient == nil {
		return []ComprehensiveServerInfo{}
	}
	
	servers, err := tc.mcpClient.ListServers(ctx)
	if err != nil {
		return []ComprehensiveServerInfo{}
	}
	
	var comprehensiveServers []ComprehensiveServerInfo
	for _, server := range servers {
		comprehensiveServers = append(comprehensiveServers, ComprehensiveServerInfo{
			Name:             server.Name,
			Version:          server.Version,
			Description:      server.Description,
			URL:              "local",
			Protocol:         "http",
			ConnectionStatus: "connected",
			Capabilities:     []string{"tools", "resources"},
			Tools:            server.Tools,
			LastSeen:         time.Now(),
		})
	}
	
	return comprehensiveServers
}

// getServerTools fetches tools for a specific server using the discovery endpoint structure
func (tc *TermChat) getServerTools(ctx context.Context, serverName, serverURL string) []mcp.Tool {
	// Try to fetch tools using the mcp.robotrad.io endpoint structure first (local nginx routing)
	tools := tc.fetchToolsFromDiscoveryEndpoint(ctx, serverName)
	if len(tools) > 0 {
		return tools
	}
	
	// Fallback to local MCP client if available
	if tc.mcpClient == nil {
		return []mcp.Tool{}
	}
	
	tools, err := tc.mcpClient.GetServerTools(ctx, serverName)
	if err != nil {
		return []mcp.Tool{}
	}
	
	return tools
}

// fetchToolsFromDiscoveryEndpoint fetches tools directly from the discovery server endpoints
func (tc *TermChat) fetchToolsFromDiscoveryEndpoint(ctx context.Context, serverName string) []mcp.Tool {
	// Use the correct endpoint structure: https://mcp.robotrad.io/servers/{serverName}/tools/list
	endpointURL := fmt.Sprintf("https://mcp.robotrad.io/servers/%s/tools/list", serverName)
	
	// Create MCP JSON-RPC request
	mcpRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}
	
	reqBytes, err := json.Marshal(mcpRequest)
	if err != nil {
		return []mcp.Tool{}
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", endpointURL, strings.NewReader(string(reqBytes)))
	if err != nil {
		return []mcp.Tool{}
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return []mcp.Tool{}
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return []mcp.Tool{}
	}
	
	var mcpResponse struct {
		Jsonrpc string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			Tools []mcp.Tool `json:"tools"`
		} `json:"result"`
		Error interface{} `json:"error,omitempty"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&mcpResponse); err != nil {
		return []mcp.Tool{}
	}
	
	if mcpResponse.Error != nil {
		return []mcp.Tool{}
	}
	
	return mcpResponse.Result.Tools
}

// generateExampleArguments generates example arguments for a tool based on its input schema
func (tc *TermChat) generateExampleArguments(schema interface{}) map[string]interface{} {
	schemaMap, ok := schema.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	
	properties, ok := schemaMap["properties"].(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	
	examples := make(map[string]interface{})
	
	for propName, propSchema := range properties {
		propMap, ok := propSchema.(map[string]interface{})
		if !ok {
			continue
		}
		
		propType, ok := propMap["type"].(string)
		if !ok {
			continue
		}
		
		// Generate example values based on property type and name
		switch propType {
		case "string":
			examples[propName] = tc.generateStringExample(propName, propMap)
		case "integer", "number":
			examples[propName] = tc.generateNumberExample(propName, propMap)
		case "boolean":
			examples[propName] = tc.generateBooleanExample(propName, propMap)
		case "array":
			examples[propName] = tc.generateArrayExample(propName, propMap)
		case "object":
			examples[propName] = tc.generateObjectExample(propName, propMap)
		default:
			examples[propName] = tc.generateDefaultExample(propName)
		}
	}
	
	return examples
}

// generateStringExample generates string examples based on property name and schema
func (tc *TermChat) generateStringExample(propName string, schema map[string]interface{}) string {
	// Check for enum values first
	if enum, ok := schema["enum"].([]interface{}); ok && len(enum) > 0 {
		if str, ok := enum[0].(string); ok {
			return str
		}
	}
	
	// Generate contextual examples based on property name
	name := strings.ToLower(propName)
	switch {
	case strings.Contains(name, "path") || strings.Contains(name, "file"):
		return "/path/to/file.txt"
	case strings.Contains(name, "url") || strings.Contains(name, "endpoint"):
		return "https://example.com/api"
	case strings.Contains(name, "name"):
		return "example_name"
	case strings.Contains(name, "id"):
		return "example_id_123"
	case strings.Contains(name, "command"):
		return "ls -la"
	case strings.Contains(name, "message") || strings.Contains(name, "text"):
		return "Example message"
	case strings.Contains(name, "email"):
		return "user@example.com"
	case strings.Contains(name, "date") || strings.Contains(name, "time"):
		return time.Now().Format("2006-01-02T15:04:05Z")
	default:
		return "example_value"
	}
}

// generateNumberExample generates number examples
func (tc *TermChat) generateNumberExample(propName string, schema map[string]interface{}) interface{} {
	name := strings.ToLower(propName)
	switch {
	case strings.Contains(name, "count") || strings.Contains(name, "limit") || strings.Contains(name, "max"):
		return 10
	case strings.Contains(name, "days") || strings.Contains(name, "hours"):
		return 7
	case strings.Contains(name, "port"):
		return 8080
	case strings.Contains(name, "timeout"):
		return 30
	case strings.Contains(name, "size"):
		return 1024
	default:
		return 1
	}
}

// generateBooleanExample generates boolean examples
func (tc *TermChat) generateBooleanExample(propName string, schema map[string]interface{}) bool {
	name := strings.ToLower(propName)
	switch {
	case strings.Contains(name, "enable") || strings.Contains(name, "active"):
		return true
	case strings.Contains(name, "force") || strings.Contains(name, "recursive"):
		return false
	default:
		return true
	}
}

// generateArrayExample generates array examples
func (tc *TermChat) generateArrayExample(propName string, schema map[string]interface{}) []interface{} {
	// Return a simple array example
	return []interface{}{"item1", "item2"}
}

// generateObjectExample generates object examples
func (tc *TermChat) generateObjectExample(propName string, schema map[string]interface{}) map[string]interface{} {
	// Return a simple object example
	return map[string]interface{}{
		"key": "value",
	}
}

// generateDefaultExample generates default examples for unknown types
func (tc *TermChat) generateDefaultExample(propName string) string {
	return "example_value"
}