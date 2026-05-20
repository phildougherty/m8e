package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/phildougherty/m8e/internal/ai"
	"github.com/phildougherty/m8e/internal/audit"
	"github.com/phildougherty/m8e/internal/compose"
	contextpkg "github.com/phildougherty/m8e/internal/context"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/kube"
	"github.com/phildougherty/m8e/internal/memory"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewMateyMCPServer creates a new Matey MCP server
func NewMateyMCPServer(mateyBinary, configFile, namespace string) *MateyMCPServer {
	server := &MateyMCPServer{
		deps: clusterDeps{
			mateyBinary: mateyBinary,
			configFile:  configFile,
			namespace:   namespace,
		},
		bashPolicy: LoadBashPolicy(),
	}

	// Initialize k8s client and composer
	server.initializeK8sComponents()

	// Initialize memory store if available
	server.initializeMemoryStore()

	// Initialize AI manager for execute_agent LLM reasoning
	server.initializeAIManager()

	// Wire the managers now that dependencies are resolved.
	server.wireManagers()

	return server
}

// wireManagers fills in any managers that the construction-time init steps
// did not already set. initializeK8sComponents may set workspace,
// initializeMemoryStore may set memory, and initializeAIManager sets agent;
// this fills the remaining cluster/workflow/config managers and provides
// no-op fallbacks for the optional ones.
func (m *MateyMCPServer) wireManagers() {
	m.cluster = newClusterTools(m.deps)
	m.workflows = newWorkflowTools(m.deps)
	m.config = newConfigTools(m.deps)
	if m.workspace == nil {
		m.workspace = newWorkspaceTools(nil)
	}
	if m.memory == nil {
		m.memory = newMemoryTools(nil)
	}
	if m.agent == nil {
		m.agent = newAgentRunner(m)
	}
	if m.mention == nil {
		m.mention = newMentionTools(m.buildMentionProcessor())
	}
}

// buildMentionProcessor constructs a contextpkg.MentionProcessor wired to the
// same Kubernetes clients and memory store the rest of the server uses. When a
// dependency is unavailable the corresponding setter is skipped and the
// processor's real-backend code returns honest errors rather than placeholders.
// Returns nil only if file-discovery init fails (working directory unreadable).
func (m *MateyMCPServer) buildMentionProcessor() *contextpkg.MentionProcessor {
	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}
	fileDiscovery, err := contextpkg.NewFileDiscovery(workDir)
	if err != nil {
		fmt.Printf("WARNING: Failed to create file discovery for mention processor: %v\n", err)

		return nil
	}
	contextManager := contextpkg.NewContextManager(contextpkg.ContextConfig{MaxTokens: 32768}, nil)
	mp := contextpkg.NewMentionProcessor(workDir, fileDiscovery, contextManager)

	if m.deps.clientset != nil || m.deps.k8sClient != nil {
		mp.SetKubernetesClients(m.deps.clientset, m.deps.k8sClient)
	}
	if m.deps.namespace != "" {
		mp.SetNamespace(m.deps.namespace)
	}
	if m.memory != nil && m.memory.store != nil {
		mp.SetMemoryStore(m.memory.store)
	}

	return mp
}

// initializeK8sComponents initializes the Kubernetes client and composer
func (m *MateyMCPServer) initializeK8sComponents() {
	// Initializing K8s components for MCP server

	// Create k8s config first (most critical component)
	config, err := createK8sConfig()
	if err != nil {
		fmt.Printf("ERROR: Failed to create k8s config, falling back to binary execution: %v\n", err)
		return
	}
	m.deps.config = config
	// K8s config created successfully

	// Add CRD scheme
	scheme := runtime.NewScheme()
	if err := crd.AddToScheme(scheme); err != nil {
		fmt.Printf("ERROR: Failed to add CRD scheme, falling back to binary execution: %v\n", err)
		return
	}
	// CRD scheme added successfully

	// Create k8s client
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		fmt.Printf("ERROR: Failed to create k8s client, falling back to binary execution: %v\n", err)
		return
	}
	m.deps.k8sClient = k8sClient
	// K8s client created successfully

	// Create kubernetes clientset for advanced operations (logs, etc.)
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Printf("WARNING: Failed to create kubernetes clientset: %v\n", err)
	} else {
		m.deps.clientset = clientset
		// Kubernetes clientset created successfully

		// Initialize workspace manager if clientset is available
		logger := logr.Discard() // Simple logger for now, could be improved
		m.workspace = newWorkspaceTools(NewWorkspaceManager(m.deps.clientset, m.deps.namespace, logger))
		// Workspace manager created successfully
	}

	// Create composer (less critical, can still function without it)
	composer, err := compose.NewK8sComposer(m.deps.configFile, m.deps.namespace)
	if err != nil {
		fmt.Printf("WARNING: Failed to initialize composer (non-critical): %v\n", err)
		// Don't return here - we can still function with just the K8s client
	} else {
		m.deps.composer = composer
		// Composer created successfully
	}

	// K8s components initialized

	// Test the client with a simple operation. Bound the call so a slow or
	// unreachable API server cannot hang process startup indefinitely.
	if m.deps.k8sClient != nil {
		listCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var namespaces corev1.NamespaceList
		if err := m.deps.k8sClient.List(listCtx, &namespaces); err != nil {
			fmt.Printf("WARNING: K8s client test failed: %v\n", err)
		}
	}
}

// createK8sConfig creates a Kubernetes config from in-cluster or kubeconfig
func createK8sConfig() (*rest.Config, error) {
	config, err := kube.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s config: %w", err)
	}

	return config, nil
}

// ExecuteTool executes a tool by name with the given arguments. It is the
// public entry point and simply dispatches to the manager that owns the tool.
// Privileged tools (cluster mutations, command execution, config apply) emit
// audit events via auditWrap/auditCall on both success and failure paths so
// that operators have a tamper-evident trail of what the MCP server did and
// when. The audit calls are no-ops when no global audit logger is registered.
func (m *MateyMCPServer) ExecuteTool(ctx context.Context, name string, arguments map[string]interface{}) (*ToolResult, error) {
	switch name {
	// Core Cluster Management (6 tools)
	case "matey_ps":
		return m.cluster.mateyPS(ctx, arguments)
	case "matey_up":
		return auditCall(ctx, "tool.matey_up", arguments, m.cluster.mateyUp, mateyServiceAuditFields)
	case "matey_down":
		return auditCall(ctx, "tool.matey_down", arguments, m.cluster.mateyDown, mateyServiceAuditFields)
	case "get_cluster_state":
		return m.cluster.getClusterState(ctx, arguments)
	case "matey_logs":
		return m.cluster.mateyLogs(ctx, arguments)
	case "matey_inspect":
		return m.cluster.mateyInspect(ctx, arguments)

	// Memory/Knowledge Graph (6 tools - consolidated from memory server)
	case "create_entities":
		return m.memory.createEntities(ctx, arguments)
	case "create_relations":
		return m.memory.createRelations(ctx, arguments)
	case "search_nodes":
		return m.memory.searchNodes(ctx, arguments)
	case "read_graph":
		return m.memory.readGraph(ctx, arguments)
	case "add_observations":
		return m.memory.addObservations(ctx, arguments)
	case "delete_entities":
		return m.memory.deleteEntities(ctx, arguments)

	// Task Management (3 tools - consolidated)
	case "manage_todos":
		return m.workspace.manageTodos(ctx, arguments)
	case "create_workflow":
		return auditCall(ctx, "tool.create_workflow", arguments, m.workflows.createWorkflow, workflowAuditFields)
	case "list_workflows":
		return m.workflows.listWorkflows(ctx, arguments)
	case "execute_workflow":
		return auditCall(ctx, "tool.execute_workflow", arguments, m.workflows.executeWorkflow, workflowAuditFields)
	case "delete_workflow":
		return auditCall(ctx, "tool.delete_workflow", arguments, m.workflows.deleteWorkflow, workflowAuditFields)
	case "workflow_logs":
		return m.workflows.workflowLogs(ctx, arguments)

	// Agent & Execution (2 tools)
	case "execute_agent":
		return auditCall(ctx, "tool.execute_agent", arguments, m.agent.executeAgent, executeAgentAuditFields)
	case "execute_bash":
		return auditCall(ctx, "tool.execute_bash", arguments, m.executeBash, executeBashAuditFields)

	// Workspace Management (2 tools - consolidated)
	case "workspace_files":
		return m.workspace.workspaceFiles(ctx, arguments)
	case "search_in_files":
		return m.workspace.searchInFiles(ctx, arguments)

	// Context Mentions (2 tools)
	case "process_mentions":
		return m.mention.processMentions(ctx, arguments)
	case "expand_mentions":
		return m.mention.expandMentions(ctx, arguments)

	// Configuration (2 tools)
	case "apply_config":
		return auditCall(ctx, "tool.apply_config", arguments, m.config.applyConfig, applyConfigAuditFields)
	case "reload_proxy":
		return auditCall(ctx, "tool.reload_proxy", arguments, m.config.reloadProxy, reloadProxyAuditFields)

	default:
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", name)}},
			IsError: true,
		}, fmt.Errorf("unknown tool: %s", name)
	}
}

// toolFn is the shared shape of a privileged tool implementation.
type toolFn func(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error)

// auditFieldsFn builds the details map for an audit event from the tool's
// arguments and result. It runs after the tool returns so callers can include
// post-hoc data (e.g. exit code) — for tools that have nothing extra to add
// beyond duration_ms/error, return the same map you got.
type auditFieldsFn func(arguments map[string]interface{}, result *ToolResult, err error) map[string]interface{}

// auditCall executes fn, then emits an audit event for the named tool. The
// event success flag mirrors the tool outcome: err != nil, result == nil, or
// result.IsError all count as failure. The fields function decides which
// per-tool details to log; duration_ms is added unconditionally.
func auditCall(ctx context.Context, event string, arguments map[string]interface{}, fn toolFn, fields auditFieldsFn) (*ToolResult, error) {
	start := time.Now()
	result, err := fn(ctx, arguments)
	duration := time.Since(start)

	success := err == nil && result != nil && !result.IsError

	details := map[string]interface{}{
		"duration_ms": duration.Milliseconds(),
	}
	if fields != nil {
		for k, v := range fields(arguments, result, err) {
			details[k] = v
		}
	}
	if err != nil {
		details["error"] = err.Error()
	} else if !success && result != nil && len(result.Content) > 0 {
		// Surface the IsError-result content so audit consumers see why the
		// tool failed even when the dispatcher returned a nil error.
		details["error"] = truncateForAudit(result.Content[0].Text, 200)
	}

	audit.SafeLog(event, "", "", "", "", success, details, err)

	return result, err
}

// truncateForAudit shortens a string to maxLen, appending "..." when truncated.
// Used for credential-shy fields (commands, objectives, error blobs) so audit
// entries stay scannable and bounded.
func truncateForAudit(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}

	return s[:maxLen] + "..."
}

// executeBashAuditFields records the (truncated) command, working directory,
// and exit-code-ish status for the bash tool.
func executeBashAuditFields(arguments map[string]interface{}, result *ToolResult, err error) map[string]interface{} {
	cmd, _ := arguments["command"].(string)
	cwd, _ := arguments["working_directory"].(string)
	fields := map[string]interface{}{
		"command": truncateForAudit(cmd, 200),
	}
	if cwd != "" {
		fields["working_directory"] = cwd
	}
	// Exit code: we don't have direct access to *exec.Cmd here, but the tool
	// itself encodes success in ToolResult.IsError. Provide a numeric proxy
	// so downstream log analysers can group on it.
	if err != nil || (result != nil && result.IsError) {
		fields["exit_code"] = 1
	} else {
		fields["exit_code"] = 0
	}

	return fields
}

// executeAgentAuditFields records the agent's objective (truncated) and a
// rough tool-usage count drawn from the structured result content.
func executeAgentAuditFields(arguments map[string]interface{}, result *ToolResult, err error) map[string]interface{} {
	objective, _ := arguments["objective"].(string)
	fields := map[string]interface{}{
		"objective": truncateForAudit(objective, 200),
	}
	// Best-effort tools_used count: the agent's structured output embeds a
	// "(N tools, X.Ys)" suffix. Extract the integer count if present.
	if result != nil && len(result.Content) > 0 {
		fields["tools_used"] = countToolsUsed(result.Content[0].Text)
	}

	return fields
}

// countToolsUsed scans for the agent's "Completed: ... (N tools, ...)" tail and
// extracts N. Returns 0 if the pattern is absent. This is intentionally
// approximate — it is an audit signal, not accounting.
func countToolsUsed(text string) int {
	idx := strings.LastIndex(text, "(")
	if idx < 0 {
		return 0
	}
	tail := text[idx:]
	// Look for "N tools" inside the tail.
	tokens := strings.Fields(tail)
	for i, tok := range tokens {
		if tok == "tools," || tok == "tools" {
			if i == 0 {
				continue
			}
			// Strip a leading "(" from the count token if present.
			numStr := strings.TrimLeft(tokens[i-1], "(")
			n := 0
			for _, r := range numStr {
				if r < '0' || r > '9' {
					break
				}
				n = n*10 + int(r-'0')
			}

			return n
		}
	}

	return 0
}

// mateyServiceAuditFields records which services were affected by a
// matey_up/matey_down call. When no services were specified, the call was a
// fleet-wide operation; we record that explicitly.
func mateyServiceAuditFields(arguments map[string]interface{}, _ *ToolResult, _ error) map[string]interface{} {
	fields := map[string]interface{}{}
	if services, ok := arguments["services"].([]interface{}); ok && len(services) > 0 {
		names := make([]string, 0, len(services))
		for _, s := range services {
			if str, ok := s.(string); ok {
				names = append(names, str)
			}
		}
		fields["services"] = names
	} else {
		fields["services"] = "all"
	}

	return fields
}

// workflowAuditFields records the workflow name and namespace from the tool
// arguments — the same key all three workflow CRUD tools use.
func workflowAuditFields(arguments map[string]interface{}, _ *ToolResult, _ error) map[string]interface{} {
	fields := map[string]interface{}{}
	if name, ok := arguments["name"].(string); ok {
		fields["workflow_name"] = name
	}
	if ns, ok := arguments["namespace"].(string); ok && ns != "" {
		fields["namespace"] = ns
	}

	return fields
}

// applyConfigAuditFields records the config_type (the YAML blob itself is too
// large for an audit entry and may carry sensitive data).
func applyConfigAuditFields(arguments map[string]interface{}, _ *ToolResult, _ error) map[string]interface{} {
	fields := map[string]interface{}{}
	if cfgType, ok := arguments["config_type"].(string); ok && cfgType != "" {
		fields["config_type"] = cfgType
	}
	if yaml, ok := arguments["config_yaml"].(string); ok {
		fields["config_size_bytes"] = len(yaml)
	}

	return fields
}

// reloadProxyAuditFields is a no-op fields builder: reload_proxy takes no
// meaningful arguments, so only the event name + duration + error matter.
func reloadProxyAuditFields(_ map[string]interface{}, _ *ToolResult, _ error) map[string]interface{} {
	return map[string]interface{}{}
}

// initializeMemoryStore initializes the memory store by connecting to the MCPMemory database
func (m *MateyMCPServer) initializeMemoryStore() {
	// Initializing memory store

	// Check if k8s client is available
	if m.deps.k8sClient == nil {
		// Kubernetes client not available, skipping memory store initialization
		return
	}

	// Try to get MCPMemory resource. Bound the call so init can't hang on
	// a slow or unreachable API server.
	getCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var memoryResource crd.MCPMemory
	err := m.deps.k8sClient.Get(getCtx, client.ObjectKey{
		Name:      "memory",
		Namespace: m.deps.namespace,
	}, &memoryResource)

	if err != nil {
		// MCPMemory resource not found, memory graph tools will not be available
		return
	}

	// Check if the memory service is running
	if memoryResource.Status.Phase != crd.MCPMemoryPhaseRunning || memoryResource.Status.ReadyReplicas == 0 {
		// Memory service is not running, memory graph tools will not be available
		return
	}

	// Get the database URL from the MCPMemory resource
	var databaseURL string
	if memoryResource.Spec.DatabaseURL != "" {
		databaseURL = memoryResource.Spec.DatabaseURL
	} else if memoryResource.Spec.PostgresEnabled {
		// Build database URL from MCPMemory spec components
		databaseURL = fmt.Sprintf("postgresql://%s:%s@%s-postgres:%d/%s?sslmode=disable",
			memoryResource.Spec.PostgresUser,
			memoryResource.Spec.PostgresPassword,
			memoryResource.Name,
			memoryResource.Spec.PostgresPort,
			memoryResource.Spec.PostgresDB)
	} else {
		// MCPMemory resource has no database configuration
		return
	}

	// Using memory database

	// Initialize memory store with the correct database URL
	memoryStore, err := memory.NewMemoryStore(databaseURL, logr.Discard())
	if err != nil {
		// Failed to initialize memory store (expected if memory service is not ready)
		return
	}

	// Test the connection
	if err := memoryStore.HealthCheck(); err != nil {
		// Memory store health check failed (expected if memory service is not ready)
		if err := memoryStore.Close(); err != nil {
			fmt.Printf("Warning: Failed to close memory store: %v\n", err)
		} // Clean up the failed connection
		return
	}

	// Initialize memory tools
	memoryTools := memory.NewMCPMemoryTools(memoryStore, logr.Discard())

	// Set on the server via the memory manager
	m.memory = newMemoryTools(&memoryHandles{store: memoryStore, tools: memoryTools})

	// Memory store initialized successfully
}

// initializeAIManager initializes the AI manager for real LLM reasoning
func (m *MateyMCPServer) initializeAIManager() {

	// Initialize AI manager with OpenRouter as primary provider
	aiConfig := ai.Config{
		DefaultProvider:   "openrouter",
		FallbackProviders: []string{"claude", "ollama", "openai"},
		Providers: map[string]ai.ProviderConfig{
			"openrouter": {
				APIKey:       os.Getenv("OPENROUTER_API_KEY"),
				Endpoint:     "https://openrouter.ai/api/v1",
				DefaultModel: "moonshotai/kimi-k2",
			},
			"ollama": {
				Endpoint:     "http://localhost:11434",
				DefaultModel: "llama3",
			},
			"openai": {
				APIKey:       os.Getenv("OPENAI_API_KEY"),
				Endpoint:     "https://api.openai.com/v1",
				DefaultModel: "gpt-4",
			},
			"claude": {
				APIKey:       os.Getenv("ANTHROPIC_API_KEY"),
				Endpoint:     "https://api.anthropic.com/v1",
				DefaultModel: "claude-3-5-sonnet-20241022",
			},
		},
	}

	aiManager := ai.NewManager(aiConfig)
	if aiManager == nil {
		fmt.Printf("WARNING: AI manager initialization failed - execute_agent will fall back to pattern matching\n")
	}

	if m.agent == nil {
		m.agent = newAgentRunner(m)
	}
	m.agent.aiManager = aiManager
}

func (m *MateyMCPServer) executeBash(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	command, ok := arguments["command"].(string)
	if !ok || command == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: command is required"}},
			IsError: true,
		}, fmt.Errorf("command is required")
	}

	// Get timeout with default
	timeout := getIntArg(arguments, "timeout", 120)
	if timeout > 600 {
		timeout = 600 // Max 10 minutes
	}

	// Get working directory
	workingDir, _ := arguments["working_directory"].(string)
	if workingDir == "" {
		workingDir = "."
	}

	// Enforce the bash execution policy. Unlike the old regex blocklist,
	// this either allowlists every binary in the pipeline or requires the
	// operator to opt explicitly into unrestricted mode — see bash_policy.go.
	if err := m.bashPolicy.Check(command); err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, err
	}

	// Create command with timeout context
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", command)
	cmd.Dir = workingDir
	// Scrub credential-looking variables from the child environment so a
	// prompt-injected `env`/`printenv` cannot exfiltrate the cluster token
	// or provider API keys.
	cmd.Env = scrubbedEnviron()

	// Capture both stdout and stderr
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	// Prepare result
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Command: %s\n", command))
	result.WriteString(fmt.Sprintf("Directory: %s\n", workingDir))
	result.WriteString(fmt.Sprintf("Duration: %v\n", duration.Truncate(time.Millisecond)))

	if stdout.Len() > 0 {
		result.WriteString(fmt.Sprintf("\nSTDOUT:\n%s", stdout.String()))
	}

	if stderr.Len() > 0 {
		result.WriteString(fmt.Sprintf("\nSTDERR:\n%s", stderr.String()))
	}

	if err != nil {
		result.WriteString(fmt.Sprintf("\nError: %v", err))
		return &ToolResult{
			Content: []Content{{Type: "text", Text: result.String()}},
			IsError: true,
		}, nil // Don't return error since we want to show the output
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
		IsError: false,
	}, nil
}

// Helper functions for argument parsing
func getBoolArg(args map[string]interface{}, key string, defaultValue bool) bool {
	if val, ok := args[key].(bool); ok {
		return val
	}
	return defaultValue
}

func getIntArg(args map[string]interface{}, key string, defaultValue int) int {
	if val, ok := args[key].(float64); ok {
		return int(val)
	}
	if val, ok := args[key].(int); ok {
		return val
	}
	return defaultValue
}

func getStringArg(args map[string]interface{}, key string, defaultValue string) string {
	if val, ok := args[key].(string); ok {
		return val
	}
	return defaultValue
}

// AgentFactory represents a function that can create a new TermChat instance
type AgentFactory func() interface{}

// SetAgentFactory sets the factory function for creating TermChat instances
func (m *MateyMCPServer) SetAgentFactory(factory AgentFactory) {
	m.agent.agentFactory = factory
}

// SetupRecursiveAgent configures the MCP server to use recursive TermChat instances
// This function should be called from the chat package to avoid circular imports
func SetupRecursiveAgent(server *MateyMCPServer, newTermChatFunc func() interface{}) {
	server.SetAgentFactory(newTermChatFunc)
}

// describeToolFailure produces a human-readable reason a tool call failed,
// drawing on the dispatcher error and/or the tool's own error content. It
// never invents a result: an unreachable or erroring tool yields an explicit
// failure string, so the LLM is told the truth instead of a plausible lie.
func describeToolFailure(err error, result *ToolResult) string {
	if err != nil {
		return err.Error()
	}
	if result == nil {
		return "tool returned no result"
	}
	if result.IsError {
		if len(result.Content) > 0 && strings.TrimSpace(result.Content[0].Text) != "" {
			return strings.TrimSpace(result.Content[0].Text)
		}

		return "tool reported an error with no detail"
	}

	return "unknown failure"
}

// extractToolSummary extracts a meaningful summary from tool result for progress display
func (m *MateyMCPServer) extractToolSummary(toolResult *ToolResult, toolName string) string {
	if toolResult == nil || len(toolResult.Content) == 0 {
		return "Completed"
	}
	// Defense in depth: never summarise an error result as a success. The
	// per-tool branches below return hardcoded success phrases (e.g.
	// "Retrieved service status"), so an IsError result reaching this
	// function would otherwise be laundered into a fake success.
	if toolResult.IsError {
		text := strings.TrimSpace(toolResult.Content[0].Text)
		if text == "" {
			text = "tool reported an error"
		}

		return "FAILED: " + text
	}

	content := toolResult.Content[0].Text

	// Extract key information based on tool type
	switch toolName {
	case "execute_bash":
		lines := strings.Split(content, "\n")
		if len(lines) > 1 {
			return "Command executed"
		}
		return "Bash command completed"
	case "search_in_files":
		if strings.Contains(content, "Found") {
			// Try to extract the number of matches
			parts := strings.Split(content, " ")
			for i, part := range parts {
				if part == "Found" && i+1 < len(parts) {
					return fmt.Sprintf("Found %s matches", parts[i+1])
				}
			}
		}
		return "File search completed"
	case "matey_ps":
		return "Retrieved service status"
	case "get_cluster_state":
		return "Retrieved cluster state"
	default:
		// Generic summary - take first line or first 50 chars
		firstLine := strings.Split(content, "\n")[0]
		if len(firstLine) > 50 {
			return firstLine[:47] + "..."
		}
		return firstLine
	}
}
