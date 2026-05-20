package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/ai"
)

// agentRunner owns the execute_agent tool: it drives a recursive LLM-powered
// sub-agent that selects and chains the server's other tools to accomplish an
// objective. It holds a back-reference to the owning server so it can dispatch
// tool calls (ExecuteTool), enumerate the tool registry (GetTools), and reuse
// the shared error-summarising helper.
type agentRunner struct {
	server       *MateyMCPServer
	aiManager    *ai.Manager  // AI provider for real LLM reasoning
	agentFactory AgentFactory // Factory function for creating recursive TermChat instances
}

func newAgentRunner(server *MateyMCPServer) *agentRunner {
	return &agentRunner{server: server}
}

// AgentProgress tracks progress of a sub-agent execution
type AgentProgress struct {
	toolCalls       []ToolCallSummary
	displayedCount  int
	maxDisplayLines int // 5 lines before "+N more"
	startTime       time.Time
	lastUpdate      time.Time
}

// ToolCallSummary represents a summary of a tool call for progress display
type ToolCallSummary struct {
	Name     string        // Tool name
	Status   string        // ✓, ⚠, ✗
	Summary  string        // "Found 42 files", "Analyzed 156 lines"
	Duration time.Duration // How long the call took
}

// AgentResult represents the structured result from agent execution
type AgentResult struct {
	ObjectiveCompleted bool           `json:"objective_completed"`
	Result             string         `json:"result"`
	ExecutionSummary   string         `json:"execution_summary"`
	ExecutionLog       []string       `json:"execution_log,omitempty"`
	ToolCallsMade      int            `json:"tool_calls_made"`
	ToolsUsed          map[string]int `json:"tools_used"`
	DurationSeconds    float64        `json:"duration_seconds"`
	StructuredData     interface{}    `json:"structured_data,omitempty"`
	Errors             []string       `json:"errors,omitempty"`
}

// NewAgentProgress creates a new progress tracker
func NewAgentProgress() *AgentProgress {
	return &AgentProgress{
		toolCalls:       make([]ToolCallSummary, 0),
		displayedCount:  0,
		maxDisplayLines: 5,
		startTime:       time.Now(),
		lastUpdate:      time.Now(),
	}
}

// UpdateDisplay updates the progress display with a new tool call
func (p *AgentProgress) UpdateDisplay(toolCall ToolCallSummary) {
	p.toolCalls = append(p.toolCalls, toolCall)
	p.lastUpdate = time.Now()

	if len(p.toolCalls) <= p.maxDisplayLines {
		// Show individual tool call
		fmt.Printf("   │ %s %s - %s\n", toolCall.Status, toolCall.Name, toolCall.Summary)
	} else if len(p.toolCalls) == p.maxDisplayLines+1 {
		// Switch to summary mode
		fmt.Printf("   │ +%d more tool calls (%s)\n",
			len(p.toolCalls)-p.maxDisplayLines,
			p.generateToolSummary())
	} else {
		// Update summary line
		fmt.Printf("\r   │ +%d more tool calls (%s)",
			len(p.toolCalls)-p.maxDisplayLines,
			p.generateToolSummary())
	}
}

// generateToolSummary creates a summary of remaining tool calls
func (p *AgentProgress) generateToolSummary() string {
	// Count tool usage from the hidden calls
	toolCounts := make(map[string]int)
	for i := p.maxDisplayLines; i < len(p.toolCalls); i++ {
		toolCounts[p.toolCalls[i].Name]++
	}

	var parts []string
	for tool, count := range toolCounts {
		if count > 1 {
			parts = append(parts, fmt.Sprintf("%s×%d", tool, count))
		} else {
			parts = append(parts, tool)
		}
	}
	return strings.Join(parts, ", ")
}

// GetSummary returns a final summary of the agent execution
func (p *AgentProgress) GetSummary() string {
	duration := time.Since(p.startTime)
	return fmt.Sprintf("Executed %d tool calls in %v", len(p.toolCalls), duration.Truncate(time.Millisecond))
}

// executeAgent executes a focused sub-agent with custom behavioral instructions
func (a *agentRunner) executeAgent(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	// Parse arguments with defaults
	objective := getStringArg(arguments, "objective", "")
	if objective == "" {
		errorMsg := fmt.Sprintf("❌ EXECUTE_AGENT PARAMETER ERROR ❌\n\nThe 'objective' parameter is REQUIRED but was missing or empty.\n\nReceived arguments: %+v\n\nCorrect format:\n{\n  \"objective\": \"Your task description here\",\n  \"ai_provider\": \"openrouter\",\n  \"ai_model\": \"your-model\",\n  \"output_format\": \"structured_data\"\n}\n\nPlease provide the objective parameter with a clear task description.", arguments)
		return &ToolResult{
			Content: []Content{{Type: "text", Text: errorMsg}},
			IsError: true,
		}, fmt.Errorf("objective parameter is required but was missing or empty")
	}

	behavioralInstructions := getStringArg(arguments, "behavioral_instructions", "")
	outputFormat := getStringArg(arguments, "output_format", "structured_data")
	contextInfo := getStringArg(arguments, "context", "")
	maxTurns := getIntArg(arguments, "max_turns", 20)
	timeoutSeconds := getIntArg(arguments, "timeout_seconds", 900) // Increased to 15 minutes for longer requests

	// Create timeout context using Background() to avoid inheriting HTTP request timeouts
	// The parent context 'ctx' might have a short deadline (like 30s HTTP timeout)
	// but execute_agent needs its own long timeout for complex operations
	agentCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Initialize AI manager with the user's current provider and model
	a.server.initializeAIManager()

	// Set up progress tracking
	progress := NewAgentProgress()

	// Display initial progress box
	fmt.Printf("\nexecute_agent(objective=\"%s\")\n", objective)
	fmt.Printf("   ┌─ Agent Task: %s\n", objective)

	// Create structured result
	result := &AgentResult{
		ObjectiveCompleted: false,
		Result:             "",
		ExecutionSummary:   "",
		ToolCallsMade:      0,
		ToolsUsed:          make(map[string]int),
		DurationSeconds:    0,
		StructuredData:     nil,
		Errors:             make([]string, 0),
	}

	// Execute the agent task
	// NOTE: Full TermChat integration would require architectural changes to avoid
	// circular imports (mcp -> chat -> mcp). For now, we use intelligent simulation
	// that demonstrates the complete flow and could be replaced with actual execution.
	a.executeAgentTask(agentCtx, objective, behavioralInstructions, contextInfo, maxTurns, progress, result)

	// Calculate final metrics
	duration := time.Since(progress.startTime)
	result.DurationSeconds = duration.Seconds()
	result.ToolCallsMade = len(progress.toolCalls)
	result.ObjectiveCompleted = true
	result.ExecutionSummary = fmt.Sprintf("Completed objective: %s", objective)

	// Display completion
	fmt.Printf("   └─ ✅ Complete: %s (%.1fs)\n", result.ExecutionSummary, result.DurationSeconds)

	// Format result based on output format
	var resultContent string
	switch outputFormat {
	case "json":
		jsonBytes := fmt.Sprintf("%+v", result)
		resultContent = jsonBytes
	case "markdown":
		resultContent = formatResultAsMarkdown(result)
	default: // structured_data
		// Format as real-time execution display
		var outputBuilder strings.Builder
		// Show tools used in Claude Code style
		if len(result.ToolsUsed) > 0 {
			for toolName, count := range result.ToolsUsed {
				if count == 1 {
					outputBuilder.WriteString(fmt.Sprintf("  \x1b[90m│\x1b[0m   \x1b[32m%s\x1b[0m\n", toolName))
				} else {
					outputBuilder.WriteString(fmt.Sprintf("  \x1b[90m│\x1b[0m   \x1b[32m%s\x1b[0m \x1b[90m(%d calls)\x1b[0m\n", toolName, count))
				}
			}
			outputBuilder.WriteString("\n")
		}
		outputBuilder.WriteString(fmt.Sprintf("**Completed:** %s (%d tools, %.1fs)",
			result.ExecutionSummary, result.ToolCallsMade, result.DurationSeconds))
		resultContent = outputBuilder.String()
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: resultContent}},
		IsError: false,
	}, nil
}

// executeAgentTask executes the actual agent task using REAL LLM reasoning and tool execution
func (a *agentRunner) executeAgentTask(ctx context.Context, objective, behavioralInstructions, contextInfo string, maxTurns int, progress *AgentProgress, result *AgentResult) {
	fmt.Printf("   │ 🧠 Initializing real LLM-powered sub-agent\n")

	// Create the dynamic system prompt for the sub-agent
	subAgentPrompt := a.createDynamicSubAgentPrompt(objective, behavioralInstructions, contextInfo, maxTurns)

	// Execute with REAL LLM reasoning and tool execution
	toolsExecuted := a.executeRealLLMAgent(ctx, objective, subAgentPrompt, progress, result, maxTurns)

	result.StructuredData = map[string]interface{}{
		"objective":          objective,
		"tools_executed":     toolsExecuted,
		"behavioral_context": behavioralInstructions,
		"agent_type":         "Real LLM-powered recursive agent",
		"execution_approach": "true_llm_reasoning",
		"available_tools":    len(a.server.GetTools()),
		"sub_agent_prompt":   len(subAgentPrompt) > 100,
	}
}

// createDynamicSubAgentPrompt creates a dynamic system prompt for the sub-agent
func (a *agentRunner) createDynamicSubAgentPrompt(objective, behavioralInstructions, contextInfo string, maxTurns int) string {
	// Use the proven system prompt structure from chat/system_prompt.go with execute_agent modifications
	baseSystemPrompt := `You are Matey AI, the expert autonomous assistant for the Matey (m8e) Kubernetes-native MCP orchestration platform.

# Core Operating Principles

## Autonomous Execution Framework
You are a HIGHLY AUTONOMOUS agent with expert-level capabilities. Execute immediately without asking permission unless potentially destructive.

**Your Prime Directives:**
1. **IMMEDIATE ACTION**: Analyze → Plan → Execute → Verify → Report
2. **STRUCTURED WORKFLOW**: Use TODO planning for multi-step operations
3. **PARALLEL EXECUTION**: Batch tool calls whenever possible for efficiency
4. **VERIFICATION**: Always validate results and confirm success

# Tool Usage Protocol (CRITICAL EXECUTION RULES)

## Tool Execution Standards
- **ALWAYS use absolute paths** for file operations - never relative paths
- **BATCH multiple tool calls** in single messages when operations are independent
- **EXPLAIN potentially destructive commands** before execution (delete, restart, apply_config)
- **VERIFY tool results** before proceeding to next steps
- **USE parallel execution** whenever tools don't depend on each other

## Strategic Tool Selection Hierarchy
1. **TODO Planning (create_todos)** - MANDATORY for any multi-step task
2. **Strategic Delegation**:
   - **Native Functions** - Direct file/code operations (search_in_files, execute_bash)
   - **MCP Platform Tools** - Matey operations (matey_ps, matey_logs, memory_*, workflows)
   - **External MCP Tools** - Specialized discovered tools

**AUTONOMOUS ACTION**:
- Take action FIRST, explain later
- Use tools immediately when problems are mentioned
- Don't ask "Would you like me to..." - just DO IT
- Chain multiple tool calls to solve problems completely
- Continue investigating until root cause is found`

	// Add execute_agent specific instructions
	executeAgentInstructions := fmt.Sprintf(`

# EXECUTE_AGENT DELEGATION CONTEXT

**SPECIFIC OBJECTIVE**: %s

**CONTEXT**: %s
**BEHAVIORAL INSTRUCTIONS**: %s
**MAXIMUM TURNS**: %d

## Critical Execute_Agent Rules
- You are a SUB-AGENT focused on this specific objective
- **START IMMEDIATELY with a tool call** - do not provide text explanations first
- Use tools systematically to accomplish the objective
- For file analysis: use search_in_files, execute_bash, or read_file
- Chain tool calls logically based on results
- **Your first response MUST be a tool call, not text**

Begin now by calling the most appropriate tool for this objective.`,
		objective, contextInfo, behavioralInstructions, maxTurns)

	return baseSystemPrompt + executeAgentInstructions
}

// executeRealLLMAgent executes using a REAL LLM to select and chain tools dynamically
func (a *agentRunner) executeRealLLMAgent(ctx context.Context, objective, systemPrompt string, progress *AgentProgress, result *AgentResult, maxTurns int) int {
	logMsg := fmt.Sprintf("   │ Initializing real LLM sub-agent for objective: %s", objective)
	result.ExecutionLog = append(result.ExecutionLog, logMsg)

	toolsExecuted := 0
	maxTools := 15
	conversationHistory := []map[string]interface{}{
		{"role": "user", "content": fmt.Sprintf("Execute this objective: %s", objective)},
	}

	// Execute LLM reasoning loop using openrouter-gateway MCP server
	for turn := 0; turn < maxTurns && toolsExecuted < maxTools; turn++ {
		fmt.Printf("   │ LLM reasoning turn %d/%d at %s\n", turn+1, maxTurns, time.Now().Format("15:04:05"))

		// Check if context is already cancelled
		select {
		case <-ctx.Done():
			// Context already cancelled before LLM call
			return toolsExecuted
		default:
			// Continue
		}

		// Build available tools for the LLM
		availableTools := a.buildAvailableToolsForMCP()

		// Call openrouter-gateway create_completion tool
		completionArgs := map[string]interface{}{
			"model":         "google/gemini-2.5-flash-lite",
			"system_prompt": systemPrompt,
			"messages":      conversationHistory,
			"tools":         availableTools,
			"temperature":   0.7,
			"max_tokens":    2048, // Reduced to get faster responses
		}

		// About to call openrouter-gateway
		toolResult, err := a.callOpenRouterGateway(ctx, completionArgs)
		if err != nil {
			fmt.Printf("   │ ✗ LLM call failed at %s: %v\n", time.Now().Format("15:04:05"), err)

			// Check if it's a context timeout and provide helpful error messages
			if ctx.Err() != nil {
				if ctx.Err() == context.DeadlineExceeded {
					fmt.Printf("   │ TIMEOUT: Context deadline exceeded - request took longer than expected\n")
					result.Errors = append(result.Errors, "Request timeout: The operation took longer than the configured timeout")
				} else if ctx.Err() == context.Canceled {
					fmt.Printf("   │ CANCELLED: Context was cancelled\n")
					result.Errors = append(result.Errors, "Request cancelled")
				} else {
					// Context error detected
					result.Errors = append(result.Errors, fmt.Sprintf("Context error: %v", ctx.Err()))
				}
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("LLM call failed: %v", err))
			}
			break
		}
		// OpenRouter gateway call completed

		// Extract response from tool result
		responseText := ""
		if len(toolResult.Content) > 0 && toolResult.Content[0].Type == "text" {
			responseText = toolResult.Content[0].Text
		}

		// Debug with truncated response
		debugText := responseText
		if len(debugText) > 200 {
			debugText = debugText[:200] + "..."
		}
		logMsg := fmt.Sprintf("   │ DEBUG: Raw LLM response: %s", debugText)
		result.ExecutionLog = append(result.ExecutionLog, logMsg)

		// Try to parse as JSON if it looks like a structured response
		if strings.HasPrefix(responseText, "{") {
			// First try to parse the openrouter-gateway wrapped format
			var gatewayResponse struct {
				Content      string                   `json:"content"`
				Model        string                   `json:"model"`
				Usage        interface{}              `json:"usage"`
				Conversation []map[string]interface{} `json:"conversation"`
			}

			if err := json.Unmarshal([]byte(responseText), &gatewayResponse); err == nil {
				// Look for tool calls in the conversation history
				for _, msg := range gatewayResponse.Conversation {
					if role, ok := msg["role"].(string); ok && role == "assistant" {
						if toolCalls, exists := msg["tool_calls"]; exists {
							if toolCallsArray, ok := toolCalls.([]interface{}); ok {
								for _, tc := range toolCallsArray {
									if toolsExecuted >= maxTools {
										break
									}

									if toolCallMap, ok := tc.(map[string]interface{}); ok {
										if function, exists := toolCallMap["function"]; exists {
											if funcMap, ok := function.(map[string]interface{}); ok {
												toolName := funcMap["name"].(string)
												argsStr := funcMap["arguments"].(string)

												// Handle empty arguments
												var args map[string]interface{}
												if argsStr == "" || argsStr == "{}" {
													args = make(map[string]interface{})
												} else {
													if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
														fmt.Printf("   │ ✗ Failed to parse tool arguments: %v\n", err)
														continue
													}
												}

												// Execute the tool that the LLM selected
												logMsg := fmt.Sprintf("   │ Executing tool: %s with args: %+v", toolName, args)
												result.ExecutionLog = append(result.ExecutionLog, logMsg)
												toolExecResult, err := a.server.ExecuteTool(ctx, toolName, args)
												toolsExecuted++

												// A tool call has failed if the dispatcher returned an
												// error, OR no result came back, OR the tool itself
												// reported failure via ToolResult.IsError. Checking only
												// err != nil is how an unreachable backend turns into a
												// hallucinated answer: the LLM gets told "Successfully
												// used X" and fabricates a plausible-looking result.
												if err != nil || toolExecResult == nil || toolExecResult.IsError {
													failureMsg := describeToolFailure(err, toolExecResult)
													logMsg := fmt.Sprintf("   │ ✗ %s failed: %s", toolName, failureMsg)
													result.ExecutionLog = append(result.ExecutionLog, logMsg)
													progress.UpdateDisplay(ToolCallSummary{
														Name:     toolName,
														Status:   "✗",
														Summary:  fmt.Sprintf("Error: %s", failureMsg),
														Duration: time.Second,
													})
													result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", toolName, failureMsg))

													// Tell the LLM the truth: the tool failed. Never
													// let it proceed as if it had a real result.
													conversationHistory = append(conversationHistory, map[string]interface{}{
														"role":    "assistant",
														"content": fmt.Sprintf("I tried to use %s but it failed: %s", toolName, failureMsg),
													})
												} else {
													// Tool genuinely succeeded.
													summary := a.server.extractToolSummary(toolExecResult, toolName)
													logMsg := fmt.Sprintf("   │ ✓ %s - %s", toolName, summary)
													result.ExecutionLog = append(result.ExecutionLog, logMsg)
													progress.UpdateDisplay(ToolCallSummary{
														Name:     toolName,
														Status:   "✓",
														Summary:  summary,
														Duration: time.Second,
													})
													result.ToolsUsed[toolName]++

													// Add successful result to conversation
													conversationHistory = append(conversationHistory, map[string]interface{}{
														"role":    "assistant",
														"content": fmt.Sprintf("Successfully used %s. Result: %s", toolName, summary),
													})
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		} else {
			// Response is plain text, assume objective is complete
			fmt.Printf("   │ LLM reports objective complete (plain text response)\n")
			break
		}
	}

	fmt.Printf("   │ Real LLM reasoning complete at %s: %d tools executed\n", time.Now().Format("15:04:05"), toolsExecuted)
	return toolsExecuted
}

// buildAvailableToolsForMCP converts MCP tools to openrouter-gateway format (limited subset)
func (a *agentRunner) buildAvailableToolsForMCP() []map[string]interface{} {
	mcpTools := a.server.GetTools()
	var tools []map[string]interface{}

	// Essential tools for execute_agent analysis (limit to avoid request size issues)
	essentialTools := map[string]bool{
		"search_in_files":      true,
		"execute_bash":         true,
		"create_todos":         true,
		"update_todo_status":   true,
		"list_todos":           true,
		"matey_ps":             true,
		"matey_logs":           true,
		"read_workspace_file":  true,
		"list_workspace_files": true,
		"mount_workspace":      true,
		"unmount_workspace":    true,
	}

	for _, mcpTool := range mcpTools {
		// Skip execute_agent to prevent recursion
		if mcpTool.Name == "execute_agent" {
			continue
		}

		// Only include essential tools to keep request size manageable
		if !essentialTools[mcpTool.Name] {
			continue
		}

		// Convert MCP tool to openrouter-gateway format
		tool := map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        mcpTool.Name,
				"description": mcpTool.Description,
				"parameters":  mcpTool.InputSchema,
			},
		}

		tools = append(tools, tool)
	}

	return tools
}

// callOpenRouterGateway calls the openrouter-gateway MCP server directly
func (a *agentRunner) callOpenRouterGateway(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Create MCP JSON-RPC request
	mcpRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "create_completion",
			"arguments": args,
		},
	}

	reqBytes, err := json.Marshal(mcpRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	// Call openrouter-gateway MCP server with fixed timeout for execute_agent
	// For execute_agent operations, use a fixed long timeout instead of inheriting context deadline
	// This prevents short HTTP request timeouts from affecting long-running execute_agent operations
	requestCtx, requestCancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer requestCancel()

	req, err := http.NewRequestWithContext(requestCtx, "POST", "http://openrouter-gateway.matey.svc.cluster.local:8012", bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Create HTTP client with fixed 20-minute timeout for execute_agent operations
	timeout := 20 * time.Minute

	client := &http.Client{
		Timeout: timeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("Warning: Failed to close response body: %v\n", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Read and parse response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	// Parse MCP response
	var mcpResponse struct {
		Jsonrpc string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error interface{} `json:"error,omitempty"`
	}

	if err := json.Unmarshal(body, &mcpResponse); err != nil {
		return nil, fmt.Errorf("failed to parse MCP response: %v", err)
	}

	if mcpResponse.Error != nil {
		return nil, fmt.Errorf("MCP error: %v", mcpResponse.Error)
	}

	// Convert to ToolResult format
	var content []Content
	for _, c := range mcpResponse.Result.Content {
		content = append(content, Content{
			Type: c.Type,
			Text: c.Text,
		})
	}

	return &ToolResult{
		Content: content,
		IsError: mcpResponse.Result.IsError,
	}, nil
}

// formatResultAsMarkdown formats the agent result as markdown
func formatResultAsMarkdown(result *AgentResult) string {
	var md strings.Builder
	md.WriteString("# Agent Execution Result\n\n")
	md.WriteString(fmt.Sprintf("**Status**: %s\n", map[bool]string{true: "✅ Completed", false: "❌ Failed"}[result.ObjectiveCompleted]))
	md.WriteString(fmt.Sprintf("**Summary**: %s\n", result.ExecutionSummary))
	md.WriteString(fmt.Sprintf("**Duration**: %.1fs\n", result.DurationSeconds))
	md.WriteString(fmt.Sprintf("**Tool Calls**: %d\n\n", result.ToolCallsMade))

	if len(result.ToolsUsed) > 0 {
		md.WriteString("## Tools Used\n")
		for tool, count := range result.ToolsUsed {
			md.WriteString(fmt.Sprintf("- **%s**: %d calls\n", tool, count))
		}
		md.WriteString("\n")
	}

	if len(result.Errors) > 0 {
		md.WriteString("## Errors\n")
		for _, err := range result.Errors {
			md.WriteString(fmt.Sprintf("- %s\n", err))
		}
	}

	return md.String()
}
