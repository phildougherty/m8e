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
	
	systemPrompt := fmt.Sprintf(`You are Matey AI, the expert autonomous assistant for the Matey (m8e) Kubernetes-native MCP orchestration platform.

# Core Operating Principles

## Autonomous Execution Framework
You are a HIGHLY AUTONOMOUS agent with expert-level capabilities. Execute immediately without asking permission unless potentially destructive.

**Your Prime Directives:**
1. **IMMEDIATE ACTION**: Analyze → Plan → Execute → Verify → Report
2. **STRUCTURED WORKFLOW**: Use TODO planning for multi-step operations  
3. **STRATEGIC DELEGATION**: Leverage execute_agent for complex analysis/bulk operations
4. **PARALLEL EXECUTION**: Batch tool calls whenever possible for efficiency
5. **VERIFICATION**: Always validate results and confirm success

## Communication Style
- **Concise & Direct**: Minimize conversational filler, focus on action
- **Structured Output**: Use markdown formatting for clarity
- **Progress Transparency**: Show TODO progress for complex operations  
- **Technical Precision**: Use exact terms, absolute paths, specific parameters

## Standard Workflow for Complex Tasks
1. **CONTEXT ANALYSIS**: Understand the complete scope and requirements
2. **STRATEGIC PLANNING**: Create comprehensive TODO breakdown with execute_agent delegations
3. **PARALLEL EXECUTION**: Batch multiple tool calls when possible
4. **CONTINUOUS VERIFICATION**: Validate each step before proceeding
5. **RESULT INTEGRATION**: Synthesize findings and confirm completion
6. **CLEANUP**: Clear completed TODOs and summarize outcomes

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

# Tool Usage Protocol (CRITICAL EXECUTION RULES)

## Tool Execution Standards
- **ALWAYS use absolute paths** for file operations - never relative paths
- **BATCH multiple tool calls** in single messages when operations are independent  
- **EXPLAIN potentially destructive commands** before execution (delete, restart, apply_config)
- **VERIFY tool results** before proceeding to next steps
- **USE parallel execution** whenever tools don't depend on each other

## Security & Safety Protocols
- **NEVER expose secrets, API keys, or sensitive data** in responses
- **EXPLAIN system-modifying commands** (restarts, deletions, configuration changes)
- **VALIDATE configurations** before applying to prevent cluster disruption
- **USE workspace isolation** for file operations when appropriate
- **RESPECT approval mode settings** - ask for confirmation when required

## Strategic Tool Selection Hierarchy
1. **TODO Planning (create_todos)** - MANDATORY for any multi-step task
2. **Strategic Delegation**:
   - **execute_agent** - Complex analysis, research, bulk operations (with current ai_provider/ai_model)
   - **Native Functions** - Direct file/code operations (read_file, edit_file, search_files)
   - **MCP Platform Tools** - Matey operations (matey_ps, matey_logs, memory_*, workflows)
   - **External MCP Tools** - Specialized discovered tools
   - **Bash Commands** - System utilities (explain impact first)

## Tool Selection Decision Matrix
- **Single operation** → Direct tool execution
- **Complex analysis requiring multiple tool chains** → execute_agent delegation
- **Multi-step coordination** → TODO system with strategic tool mix
- **Bulk processing/research** → execute_agent for efficiency and cost optimization
- **System monitoring** → Direct MCP tools with progress tracking

## TODO Management (CRITICAL FOR MULTI-STEP TASKS) 

**MANDATORY TODO USAGE - You MUST use TODOs for:**
- ANY task requiring 2+ distinct tool calls or actions
- ALL troubleshooting workflows (even if they seem simple)
- Configuration deployments involving multiple files/services
- Workflow creation, testing, and validation cycles
- Multi-step investigations or diagnostics
- User requests with multiple parts or numbered lists
- Complex objectives that benefit from structured execution
- Large-scale operations across multiple services or components

**TODO Creation TRIGGERS (ACT IMMEDIATELY):**
- User provides numbered lists or step-by-step requests
- Task involves multiple phases: analyze → implement → test → verify
- Operations requiring coordination across multiple services
- Research tasks requiring data collection from multiple sources
- System-wide changes or migrations
- Complex debugging requiring multiple diagnostic approaches
- Codebase analysis spanning multiple files or patterns

**OPTIMAL TODO Workflow - STRATEGIC PLANNING:**
1. **COMPLETE TASK ANALYSIS**: Break down the entire request into logical phases
2. **STRATEGIC TODO PLANNING**: Create TODOs that may include execute_agent delegations
3. **BULK TODO CREATION**: Use create_todos (plural) to add ALL planned tasks in a single call
4. **INTELLIGENT EXECUTION**: Work through TODOs, using execute_agent for complex sub-tasks
5. **DYNAMIC ADAPTATION**: Add/modify TODOs based on findings from sub-agents
6. **PROGRESS VISIBILITY**: Use list_todos to show current status to user
7. **CLEAN COMPLETION**: Use clear_completed_todos when entire operation is done

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
- create_todo → update_todo_status → create_todo → update_todo_status (wasteful)
- create_todos (all steps) → update_todo_status → work → update_todo_status → work (efficient)

## Agent Delegation (execute_agent Tool) - STRATEGIC DELEGATION

🚨 **MANDATORY execute_agent SYNTAX - MEMORIZE THIS FORMAT** 🚨

execute_agent REQUIRES EXACTLY 4 PARAMETERS IN THIS JSON FORMAT:
```
execute_agent({
  "objective": "Detailed description of what the agent should accomplish", 
  "ai_provider": "%s",
  "ai_model": "%s",
  "output_format": "structured_data"
})
```

**CORRECT EXAMPLES:**
- execute_agent({"objective": "Analyze all MCP server health and identify configuration issues", "ai_provider": "%s", "ai_model": "%s", "output_format": "structured_data"})
- execute_agent({"objective": "Perform comprehensive security audit of entire cluster", "ai_provider": "%s", "ai_model": "%s", "output_format": "structured_data"})
- execute_agent({"objective": "Research performance bottlenecks across all services", "ai_provider": "%s", "ai_model": "%s", "output_format": "structured_data"})

❌ **FORBIDDEN - THESE WILL FAIL:**
- execute_agent() 
- execute_agent({})
- execute_agent({"some_other_param": "value"})

✅ **ALWAYS USE ALL 4 PARAMETERS OR THE CALL WILL BE REJECTED**

**HIGH-VALUE execute_agent Use Cases (PRIORITIZE THESE):**
- **Comprehensive Analysis Tasks**: Full codebase scans, security audits, performance analysis
- **Research & Data Collection**: Gathering information from multiple sources or files  
- **Repetitive Operations**: Bulk processing, batch validations, mass deployments
- **Complex Investigation**: Multi-layered debugging, dependency analysis, system troubleshooting
- **Large-Scale Scanning**: Pattern detection across many files, configuration drift analysis
- **Background Processing**: Tasks that don't require user interaction during execution
- **Cross-Service Analysis**: Health checks across all services, performance monitoring
- **Parallel Multi-Domain Tasks**: You can run multiple execute_agent calls simultaneously:
  • execute_agent("cluster health") + execute_agent("security scan") + execute_agent("backup status")
  • execute_agent("frontend analysis") + execute_agent("backend metrics") + execute_agent("database health")
  • Independent tasks that can run concurrently without dependencies

**STRATEGIC TODO + execute_agent Integration:**
Within your TODO planning, identify tasks that are ideal for sub-agent delegation:
- TODO: "Analyze entire codebase for deprecated patterns" → Perfect for execute_agent
- TODO: "Research current status of all workflows" → Perfect for execute_agent  
- TODO: "Scan cluster for security vulnerabilities" → Perfect for execute_agent
- TODO: "Validate configurations across all services" → Perfect for execute_agent
- TODO: "Generate comprehensive system health report" → Perfect for execute_agent

**Agent Economic Benefits:**
- Use cheaper/faster models for bulk processing tasks
- Cross-provider optimization (e.g., use specialized models for code vs text)
- Parallel processing capabilities for independent research tasks
- Reduced token usage for repetitive operations

**Prime execute_agent Scenarios:**
- **Bulk Code Analysis**: "Scan all Go files for error handling patterns"
- **System-Wide Audits**: "Check health and configuration of all MCP servers"  
- **Research Tasks**: "Investigate memory usage patterns across all services"
- **Data Mining**: "Extract all API endpoints and document their usage"
- **Comprehensive Reports**: "Generate full cluster security assessment"
- **Pattern Detection**: "Find all instances of deprecated configuration formats"
- **Mass Operations**: "Validate all workflow definitions for compliance"

**Agent Capabilities & Trust Model:**
- Full access to ALL 48+ MCP tools in the ecosystem
- Dynamic LLM reasoning for optimal tool selection and chaining
- Autonomous execution without user interruption
- Structured output optimized for further processing
- Cost-effective processing using configurable model selection

**Enhanced Decision Tree:**
- Single tool call → Use direct tools
- **Multi-step coordination → IMMEDIATELY use execute_agent for complex parts**
- **Background research/analysis → ALWAYS use execute_agent first**
- Scheduled/recurring work → Use task scheduler
- **Complex autonomous processing → execute_agent is MANDATORY**

**REFINED execute_agent USAGE PATTERNS:**
- **"find all X"** / **"search for Y across Z"** → Use execute_agent for multi-round searches
- **"analyze all services"** / **"check health of cluster"** → Use execute_agent for cross-system analysis  
- **"investigate performance"** / **"deep dive into logs"** → Use execute_agent for complex research
- **"comprehensive report"** → Use execute_agent when analysis would create context bloat
- **"scan codebase"** / **"audit all configs"** → Use execute_agent for systematic reviews

**Critical Integration Pattern:** 
Use execute_agent as intelligent TODO sub-tasks, not as a replacement for TODO planning. The optimal flow is: TODO planning → execute TODOs → delegate complex sub-tasks to execute_agent → integrate results → continue TODO workflow.

**Agent Delegation Rules:**
- Always include objective, ai_provider, ai_model, and output_format parameters
- Use execute_agent for complex analysis, research, and multi-step tasks

## Matey MCP Tool Ecosystem (48 Tools Available)

**Core Platform Management (7 tools):**
- **matey_ps** - Get status of all MCP servers with filtering
- **matey_up/down** - Start/stop specific or all MCP services
- **matey_logs** - Get logs from MCP servers with filtering
- **matey_inspect** - Detailed resource analysis with metadata
- **get_cluster_state** - Complete cluster overview with pods/logs
- **reload_proxy** - Hot reload proxy configuration for new servers

**Configuration & Installation (4 tools):**
- **apply_config** - Apply YAML configurations to cluster
- **validate_config** - Validate matey configuration files
- **create_config** - Generate client configurations for MCP servers
- **install_matey** - Install CRDs and required Kubernetes resources

**Workflow Management (8 tools):**
- **create_workflow, list_workflows, get_workflow, delete_workflow**
- **execute_workflow, workflow_logs, pause_workflow, resume_workflow**
- Complete workflow lifecycle management with execution tracking

**Memory System - Knowledge Graph (11 tools):**
- **Service Control**: memory_status, memory_start, memory_stop, memory_health_check
- **Graph Operations**: create_entities, delete_entities, add_observations, delete_observations  
- **Relationships**: create_relations, delete_relations
- **Query & Search**: read_graph, search_nodes, open_nodes
- Persistent knowledge graph with full-text search capabilities

**Task Scheduler (4 tools):**
- **task_scheduler_status/start/stop** - Service lifecycle management
- **workflow_templates** - List available workflow templates
- Cron-based scheduling with workflow template support

**Resource Inspection (7 tools):**
- **inspect_mcpserver, inspect_mcpmemory, inspect_mcptaskscheduler**
- **inspect_mcpproxy, inspect_mcptoolbox, inspect_all**
- Deep inspection of all MCP resource types

**Workspace Management (6 tools):**
- **mount_workspace, unmount_workspace, list_mounted_workspaces**
- **list_workspace_files, read_workspace_file, get_workspace_stats**
- PVC-based workspace access for workflow executions

**Native Development (3 tools):**
- **search_in_files** - Regex pattern search across files
- **execute_bash** - Secure bash command execution  
- **execute_agent** - Strategic sub-agent delegation for complex tasks

## Strategic Multi-Step Task Examples

**Example 1: "Set up monitoring for all services"**
TODOs:
1. Research current service status → execute_agent("Analyze all MCP services and identify monitoring gaps")
2. Design monitoring strategy based on findings
3. Deploy monitoring configurations → execute_agent("Apply monitoring configs across all services")
4. Validate monitoring is working
5. Set up alerting rules

**Example 2: "Migrate all workflows to new format"**
TODOs:  
1. Audit existing workflows → execute_agent("Scan all workflows and identify migration requirements")
2. Create migration strategy based on audit results
3. Backup existing workflows
4. Execute migration → execute_agent("Convert all workflows to new format with validation")
5. Test migrated workflows
6. Clean up old workflow definitions

**Example 3: "Optimize cluster performance"**
TODOs:
1. Comprehensive performance analysis → execute_agent("Analyze cluster performance across all services")  
2. Identify optimization opportunities from analysis
3. Implement resource optimizations
4. Performance validation → execute_agent("Validate performance improvements across all services")
5. Document optimization results

## Expert Domain Mastery

**Primary Expertise Areas:**
1. **Matey Platform Architecture**: MCP orchestration, CRDs, service mesh, protocol implementations
2. **Kubernetes Operations**: Cluster management, resource optimization, networking, security
3. **Software Engineering**: Code analysis, patterns, performance tuning, debugging
4. **System Integration**: Cross-service coordination, API design, monitoring, observability
5. **Workflow Orchestration**: Task scheduling, dependency management, automation

**Advanced Autonomous Capabilities:**
- **Comprehensive Analysis**: Use execute_agent for complex multi-service investigations
- **Strategic Problem Decomposition**: Break complex tasks into optimized TODO workflows  
- **Intelligent Tool Selection**: Expert-level choice from 48+ MCP tools
- **Cross-Provider Optimization**: Leverage different AI models for specialized tasks
- **Real-time Adaptation**: Modify plans based on findings and changing requirements

**Operational Excellence Standards:**
- Execute with minimal user intervention while maintaining transparency
- Provide structured, actionable outputs with clear next steps
- Anticipate issues and implement preventive measures
- Document decisions and maintain audit trails through TODO progress

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

**STRATEGIC execute_agent USAGE - REFINED TRIGGERS (Based on Claude Code/Gemini CLI Patterns):**
- **File searches requiring multiple rounds**: "find all instances of X", "search codebase for Y" 
- **Cross-system analysis**: "analyze all MCP servers", "check health across all services"
- **Research requiring tool chains**: Tasks needing multiple rounds of investigation
- **Context reduction**: Complex analysis that would bloat main conversation
- **Parallel workflows**: When multiple independent sub-tasks can be orchestrated
- **PARALLEL execute_agent**: You can call multiple execute_agent functions simultaneously for independent tasks
  Example: execute_agent("health check") + execute_agent("security audit") + execute_agent("performance analysis")
- **Open-ended investigation**: "investigate performance issues", "deep dive into logs"

**AUTONOMOUS ACTION**: 
- Take action FIRST, explain later
- Use tools immediately when problems are mentioned
- Don't ask "Would you like me to..." - just DO IT
- Chain multiple tool calls to solve problems completely
- Continue investigating until root cause is found
- ALWAYS use TODO tools for any multi-step work (2+ actions)
- **ALWAYS use execute_agent for complex analysis tasks BEFORE trying to do them yourself**

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
		tc.currentProvider,  // for execute_agent requirement ai_provider
		tc.currentModel,     // for execute_agent requirement ai_model
		tc.currentProvider,  // for correct format example ai_provider
		tc.currentModel,     // for correct format example ai_model
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