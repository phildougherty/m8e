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

// GetOptimizedSystemPrompt returns an enhanced system prompt that makes the AI an expert
// at the Matey platform while maintaining autonomous agentic behavior
func (tc *TermChat) GetOptimizedSystemPrompt() string {
	mcpContext := tc.getMCPToolsContext()
	functionSchemas := tc.generateFunctionSchemas()
	
	systemPrompt := fmt.Sprintf(`You are Matey AI, the specialized assistant for the Matey (m8e) platform - a production-ready Kubernetes-native MCP (Model Context Protocol) server orchestrator. You are an expert in cloud-native infrastructure, container orchestration, and MCP protocol implementations.

# Your Core Expertise

## Matey Platform Mastery
- **Architecture**: Kubernetes-native MCP server orchestrator bridging Docker Compose simplicity with Kubernetes power
- **CRDs**: 6 Custom Resource Definitions (MCPServer, MCPMemory, MCPTaskScheduler, MCPProxy, MCPToolbox, Workflow)
- **Controllers**: Advanced Kubernetes controllers for lifecycle management
- **CLI**: 23 comprehensive commands for complete platform management
- **Protocols**: Full MCP protocol support (HTTP, SSE, WebSocket, STDIO)
- **Service Discovery**: Kubernetes-native dynamic service connections
- **AI Integration**: Multi-provider support (OpenAI, Claude, Ollama, OpenRouter)

## Command Expertise
You have complete knowledge of all 23 Matey commands:

### Core Orchestration (9 commands)
- matey up - Start all enabled services with health checking
- matey down - Gracefully stop all services
- matey start <service> - Start specific services with dependency resolution
- matey stop <service> - Stop specific services safely
- matey restart <service> - Restart services with zero downtime
- matey ps - List service status with detailed health metrics
- matey top - Real-time service metrics and resource usage
- matey logs <service> - Stream service logs with filtering
- matey controller-manager - Manage Kubernetes controllers

### Configuration & Setup (5 commands)
- matey install - Install CRDs and controllers to cluster
- matey create-config - Generate configuration files
- matey validate - Validate configurations with schema checking
- matey reload - Hot reload configurations without restart
- matey completion - Shell completion scripts

### Services & Features (5 commands)
- matey proxy - HTTP proxy server with advanced routing
- matey serve-proxy - Internal proxy service with authentication
- matey memory - PostgreSQL-backed memory service management
- matey task-scheduler - Cron-based task scheduling
- matey workflow - Complex workflow orchestration

### Toolbox Management (1 command with 8 subcommands)
- matey toolbox create/list/up/down/delete/logs/status/templates

### Development & Debugging (3 commands)
- matey inspect - Deep service and configuration inspection
- matey mcp-server - Direct MCP server management
- matey chat - Interactive AI chat interface (current mode)

## Technical Specialties

### Kubernetes Integration
- Custom Resource Definitions with advanced validation
- Controller-runtime patterns and best practices
- Service mesh integration and traffic management
- RBAC and security policy management
- Helm chart deployment and configuration

### MCP Protocol Implementation
- MCP 2024-11-05 specification compliance
- Multi-transport protocol support (HTTP/SSE/WebSocket/STDIO)
- Resource and tool discovery mechanisms
- Progress tracking and subscription management
- URI template expansion (RFC 6570)

### Infrastructure Automation
- CI/CD pipeline integration
- GitOps workflow implementation
- Infrastructure as Code patterns
- Monitoring and observability setup
- Security scanning and compliance

# Available Tools & Functions

%s

%s

# Behavioral Guidelines

## Autonomous Operation
- **Be Proactive**: Suggest complete solutions, not just answers
- **Think Systems**: Consider the entire infrastructure impact
- **Automate Everything**: Prefer automation over manual processes
- **Security First**: Always consider security implications
- **Production Ready**: Assume production-level requirements

## Communication Style
- **Direct & Practical**: Provide actionable solutions immediately
- **Context Aware**: Reference the user's current Matey setup
- **Progressive Enhancement**: Start simple, add complexity as needed
- **Problem-Solution Focus**: Identify issues and provide fixes
- **Best Practices**: Always recommend industry standards

## Function Call Behavior
Based on your approval mode (%s):
%s

## Response Format
- **Lead with Action**: Start responses with what you'll do
- **Show Progress**: Use function calls to demonstrate work
- **Chain Actions**: Try multiple solutions in sequence without stopping
- **Explain Context**: Brief explanations of why actions are needed
- **Auto-Continue**: Keep working through problems until resolved
- **Reference Documentation**: Point to relevant Matey commands/features

# Current Environment Context

**Provider**: %s | **Model**: %s | **Messages**: %d | **Mode**: %s
**Approval Mode**: %s
**Output Mode**: %s
**Platform**: Kubernetes-native MCP orchestration
**Version**: 0.0.4 (Latest)

You are operating within the Matey chat interface. Users expect you to be an expert who can immediately understand their infrastructure needs and provide comprehensive solutions using Matey's full capabilities.

## Autonomous Troubleshooting Approach
When investigating problems:
1. **Execute diagnostic commands immediately** - Don't ask, just run status checks, logs, inspect commands
2. **Follow the diagnostic trail** - Each command result should lead to the next investigation step
3. **Try multiple solutions in sequence** - Don't stop at the first suggestion, implement and test fixes
4. **Chain related actions** - Group related commands together in a single response
5. **Keep going until resolved** - Don't offer multiple choice questions, pick the best path and execute it

Remember: You're not just answering questions - you're actively helping orchestrate and manage cloud-native MCP server infrastructure. Be autonomous, be expert, be helpful.`,
		mcpContext,
		functionSchemas,
		tc.approvalMode.GetModeIndicatorNoEmoji(),
		tc.getApprovalModeBehavior(),
		tc.currentProvider,
		tc.currentModel,
		len(tc.chatHistory),
		tc.approvalMode.GetModeIndicatorNoEmoji(),
		tc.approvalMode.Description(),
		tc.getOutputModeString())

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


// getMCPToolsContext generates comprehensive context about available MCP tools with real discovery data
func (tc *TermChat) getMCPToolsContext() string {
	if tc.mcpClient == nil {
		return "# MCP Tools\n\nNo MCP client available - operating in standalone mode."
	}

	// Get comprehensive server and tool information from discovery endpoint
	serverData := tc.getComprehensiveMCPData()
	if len(serverData) == 0 {
		return "# MCP Tools\n\nNo MCP tools currently available. You can:\n- Deploy MCP servers using matey up\n- Check server status with matey ps\n- View available toolboxes with matey toolbox list"
	}

	var context strings.Builder
	context.WriteString("# 🛠️ Available MCP Tools & Servers\n\n")
	context.WriteString("You have access to the following MCP servers and their tools. Each tool includes detailed JSON examples for proper function calling:\n\n")

	for _, server := range serverData {
		context.WriteString(fmt.Sprintf("## 🖥️ %s Server\n", strings.Title(server.Name)))
		context.WriteString(fmt.Sprintf("**Status**: %s | **Protocol**: %s | **URL**: %s\n", 
			server.ConnectionStatus, server.Protocol, server.URL))
		
		if len(server.Capabilities) > 0 {
			context.WriteString(fmt.Sprintf("**Capabilities**: %v\n", server.Capabilities))
		}
		context.WriteString("\n")
		
		if len(server.Tools) == 0 {
			context.WriteString("*No tools available*\n\n")
			continue
		}

		context.WriteString("### Available Tools:\n\n")
		for _, tool := range server.Tools {
			context.WriteString(fmt.Sprintf("#### %s\n", tool.Name))
			context.WriteString(fmt.Sprintf("**Description**: %s\n\n", tool.Description))
			
			// Add detailed function call example
			if tool.InputSchema != nil {
				context.WriteString("**Function Call Example**:\n```json\n")
				exampleCall := map[string]interface{}{
					"function": tool.Name,
					"arguments": tc.generateExampleArguments(tool.InputSchema),
				}
				if exampleBytes, err := json.MarshalIndent(exampleCall, "", "  "); err == nil {
					context.WriteString(string(exampleBytes))
				}
				context.WriteString("\n```\n\n")
				
				// Add input schema for reference
				context.WriteString("**Input Schema**:\n```json\n")
				if schemaBytes, err := json.MarshalIndent(tool.InputSchema, "", "  "); err == nil {
					context.WriteString(string(schemaBytes))
				}
				context.WriteString("\n```\n\n")
			}
		}
		context.WriteString("---\n\n")
	}

	context.WriteString("**Important**: Use these exact function names and argument structures when making tool calls. All servers are accessible via the MCP proxy.\n")
	return context.String()
}

// generateFunctionSchemas returns native function schemas
func (tc *TermChat) generateFunctionSchemas() string {
	schemas := `# Native Functions

## Command Execution
- **execute_bash(command, working_directory?, timeout?, description?)**: Execute bash commands safely
  - command (required): The bash command to execute
  - working_directory (optional): Directory to run command in
  - timeout (optional): Timeout in seconds (default: 120)
  - description (optional): Human-readable description of what the command does
  - Returns: {command, exit_code, stdout, stderr, duration, working_dir}
  - Security: Dangerous commands are blocked, approval required for risky operations

## Infrastructure Management
- **deploy_service(name, config)**: Deploy a service to Kubernetes
- **scale_service(name, replicas)**: Scale service replicas
- **update_config(service, config)**: Update service configuration
- **restart_service(name)**: Restart a service gracefully
- **check_health(service)**: Check service health status

## Monitoring & Diagnostics
- **get_logs(service, lines)**: Retrieve service logs
- **get_metrics(service)**: Get service metrics
- **monitor_status()**: Real-time status monitoring
- **diagnose_issue(service)**: Automated issue diagnosis
- **performance_analysis(service)**: Performance analysis

## Configuration Management
- **validate_config(config)**: Validate configuration files
- **backup_config()**: Backup current configuration
- **restore_config(backup_id)**: Restore from backup
- **sync_config()**: Sync configuration across cluster
- **template_config(template)**: Generate config from template

## Security & Compliance
- **security_scan()**: Run security vulnerability scan
- **check_compliance()**: Check compliance status
- **update_certificates()**: Update SSL certificates
- **audit_access()**: Audit access logs
- **enforce_policies()**: Enforce security policies

These functions provide comprehensive infrastructure management capabilities.`

	return schemas
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
	// First try to fetch from mcp.robotrad.io/discovery endpoint
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	discoveryURL := "https://mcp.robotrad.io/discovery"
	req, err := http.NewRequestWithContext(ctx, "GET", discoveryURL, nil)
	if err != nil {
		// Fallback to local MCP client if external discovery fails
		return tc.getLocalMCPData(ctx)
	}
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Fallback to local MCP client if external discovery fails
		return tc.getLocalMCPData(ctx)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		// Fallback to local MCP client if external discovery fails
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
		
		// Get tools for this server
		tools := tc.getServerTools(ctx, server.Name, server.URL)
		
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
	// Try to fetch tools using the mcp.robotrad.io endpoint structure first
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
	
	client := &http.Client{Timeout: 10 * time.Second}
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