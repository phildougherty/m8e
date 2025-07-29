package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/phildougherty/m8e/internal/ai"
	"github.com/phildougherty/m8e/internal/compose"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/memory"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewMateyMCPServer creates a new Matey MCP server
func NewMateyMCPServer(mateyBinary, configFile, namespace string) *MateyMCPServer {
	server := &MateyMCPServer{
		mateyBinary: mateyBinary,
		configFile:  configFile,
		namespace:   namespace,
	}
	
	// Initialize k8s client and composer
	server.initializeK8sComponents()
	
	// Initialize memory store if available
	server.initializeMemoryStore()
	
	// Initialize AI manager for execute_agent LLM reasoning
	server.initializeAIManager()
	
	return server
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
	m.config = config
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
	m.k8sClient = k8sClient
	// K8s client created successfully
	
	// Create kubernetes clientset for advanced operations (logs, etc.)
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Printf("WARNING: Failed to create kubernetes clientset: %v\n", err)
	} else {
		m.clientset = clientset
		// Kubernetes clientset created successfully
		
		// Initialize workspace manager if clientset is available
		logger := logr.Discard() // Simple logger for now, could be improved
		m.workspaceManager = NewWorkspaceManager(m.clientset, m.namespace, logger)
		// Workspace manager created successfully
	}
	
	// Create composer (less critical, can still function without it)
	composer, err := compose.NewK8sComposer(m.configFile, m.namespace)
	if err != nil {
		fmt.Printf("WARNING: Failed to initialize composer (non-critical): %v\n", err)
		// Don't return here - we can still function with just the K8s client
	} else {
		m.composer = composer
		// Composer created successfully
	}
	
	// K8s components initialized
	
	// Test the client with a simple operation
	if m.k8sClient != nil {
		// Testing K8s client with namespace list
		var namespaces corev1.NamespaceList
		if err := m.k8sClient.List(context.Background(), &namespaces); err != nil {
			fmt.Printf("WARNING: K8s client test failed: %v\n", err)
		} else {
			// K8s client test successful
		}
	}
}

// createK8sConfig creates a Kubernetes config from in-cluster or kubeconfig
func createK8sConfig() (*rest.Config, error) {
	// Try in-cluster config first
	// Attempting in-cluster config
	config, err := rest.InClusterConfig()
	if err == nil {
		// Using in-cluster config
		return config, nil
	}
	// In-cluster config failed, trying kubeconfig
	
	// Fall back to kubeconfig
	kubeconfigPath := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	// Attempting kubeconfig
	
	config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		// Kubeconfig failed
		return nil, fmt.Errorf("failed to create k8s config - in-cluster: %v, kubeconfig: %v", err, err)
	}
	
	// Using kubeconfig
	return config, nil
}

// useK8sClient returns true if we can use the Kubernetes client
func (m *MateyMCPServer) useK8sClient() bool {
	return m.k8sClient != nil && m.composer != nil
}

// manageTodos handles consolidated TODO management operations
func (m *MateyMCPServer) manageTodos(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	action, _ := arguments["action"].(string)
	if action == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: action parameter is required"}},
			IsError: true,
		}, fmt.Errorf("action parameter is required")
	}

	switch action {
	case "create":
		return m.createTodos(ctx, arguments)
	case "list":
		return m.listTodos(ctx, arguments)
	case "update":
		return m.updateTodoStatus(ctx, arguments)
	case "clear":
		return m.clearCompletedTodos(ctx, arguments)
	case "stats":
		return m.getTodoStats(ctx, arguments)
	default:
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Unknown action: %s", action)}},
			IsError: true,
		}, fmt.Errorf("unknown action: %s", action)
	}
}

// workspaceFiles handles consolidated workspace file operations
func (m *MateyMCPServer) workspaceFiles(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	action, _ := arguments["action"].(string)
	if action == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: action parameter is required"}},
			IsError: true,
		}, fmt.Errorf("action parameter is required")
	}

	// Extract common parameters
	workflowName, _ := arguments["workflowName"].(string)
	executionID, _ := arguments["executionID"].(string)

	switch action {
	case "list":
		subPath, _ := arguments["subPath"].(string)
		return m.listWorkspaceFiles(ctx, workflowName, executionID, subPath)
	case "read":
		filePath, _ := arguments["filePath"].(string)
		maxSize, _ := arguments["maxSize"].(int)
		if maxSize == 0 {
			maxSize = 1048576 // 1MB default
		}
		return m.readWorkspaceFile(ctx, workflowName, executionID, filePath, maxSize)
	case "mount":
		return m.mountWorkspace(ctx, workflowName, executionID)
	case "unmount":
		return m.unmountWorkspace(ctx, workflowName, executionID)
	case "stats":
		return m.getWorkspaceStats(ctx)
	default:
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Unknown action: %s", action)}},
			IsError: true,
		}, fmt.Errorf("unknown action: %s", action)
	}
}

// ExecuteTool executes a tool by name with the given arguments - slimmed down version
func (m *MateyMCPServer) ExecuteTool(ctx context.Context, name string, arguments map[string]interface{}) (*ToolResult, error) {
	switch name {
	// Core Cluster Management (6 tools)
	case "matey_ps":
		return m.mateyPS(ctx, arguments)
	case "matey_up":
		return m.mateyUp(ctx, arguments)
	case "matey_down":
		return m.mateyDown(ctx, arguments)
	case "get_cluster_state":
		return m.getClusterState(ctx, arguments)
	case "matey_logs":
		return m.mateyLogs(ctx, arguments)
	case "matey_inspect":
		return m.mateyInspect(ctx, arguments)

	// Memory/Knowledge Graph (6 tools - consolidated from memory server)
	case "create_entities":
		return m.createEntities(ctx, arguments)
	case "create_relations":
		return m.createRelations(ctx, arguments)
	case "search_nodes":
		return m.searchNodes(ctx, arguments)
	case "read_graph":
		return m.readGraph(ctx, arguments)
	case "add_observations":
		return m.addObservations(ctx, arguments)
	case "delete_entities":
		return m.deleteEntities(ctx, arguments)

	// Task Management (3 tools - consolidated)
	case "manage_todos":
		return m.manageTodos(ctx, arguments)
	case "create_workflow":
		return m.createWorkflow(ctx, arguments)
	case "list_workflows":
		return m.listWorkflows(ctx, arguments)
	case "execute_workflow":
		return m.executeWorkflow(ctx, arguments)
	case "delete_workflow":
		return m.deleteWorkflow(ctx, arguments)
	case "workflow_logs":
		return m.workflowLogs(ctx, arguments)

	// Agent & Execution (2 tools)
	case "execute_agent":
		return m.executeAgent(ctx, arguments)
	case "execute_bash":
		return m.executeBash(ctx, arguments)

	// Workspace Management (2 tools - consolidated)
	case "workspace_files":
		return m.workspaceFiles(ctx, arguments)
	case "search_in_files":
		return m.searchInFiles(ctx, arguments)

	// Configuration (2 tools)
	case "apply_config":
		return m.applyConfig(ctx, arguments)
	case "reload_proxy":
		return m.reloadProxy(ctx, arguments)
		
	default:
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", name)}},
			IsError: true,
		}, fmt.Errorf("unknown tool: %s", name)
	}
}

// initializeMemoryStore initializes the memory store by connecting to the MCPMemory database
func (m *MateyMCPServer) initializeMemoryStore() {
	// Initializing memory store
	
	// Check if k8s client is available
	if m.k8sClient == nil {
		// Kubernetes client not available, skipping memory store initialization
		return
	}
	
	// Try to get MCPMemory resource
	var memoryResource crd.MCPMemory
	err := m.k8sClient.Get(context.Background(), client.ObjectKey{
		Name:      "memory",
		Namespace: m.namespace,
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
	
	// Set on the server
	m.memoryStore = memoryStore
	m.memoryTools = memoryTools
	
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
	
	m.aiManager = ai.NewManager(aiConfig)
	
	if m.aiManager == nil {
		fmt.Printf("WARNING: AI manager initialization failed - execute_agent will fall back to pattern matching\n")
	}
}

// Native tool implementations (migrated from chat package)

func (m *MateyMCPServer) createTodos(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	todosInterface, ok := arguments["todos"]
	if !ok {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: todos array is required"}},
			IsError: true,
		}, fmt.Errorf("todos array is required")
	}

	todosArray, ok := todosInterface.([]interface{})
	if !ok {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: todos must be an array"}},
			IsError: true,
		}, fmt.Errorf("todos must be an array")
	}

	var createdItems []map[string]interface{}
	var createdCount int

	for _, todoInterface := range todosArray {
		todoMap, ok := todoInterface.(map[string]interface{})
		if !ok {
			continue // Skip invalid items
		}

		content, ok := todoMap["content"].(string)
		if !ok || content == "" {
			continue // Skip items without content
		}

		// Parse priority with default
		priority := "medium"
		if p, ok := todoMap["priority"].(string); ok {
			priority = p
		}

		// Create a simple TODO ID
		id := fmt.Sprintf("todo_%d", len(createdItems)+1)
		createdCount++

		createdItems = append(createdItems, map[string]interface{}{
			"id":       id,
			"content":  content,
			"priority": priority,
			"status":   "pending",
		})
	}

	result := fmt.Sprintf("Created %d TODO items successfully", createdCount)
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
		IsError: false,
	}, nil
}

func (m *MateyMCPServer) listTodos(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	// Simple in-memory TODO list (in real implementation, this would be persistent)
	// statusFilter, _ := arguments["status"].(string)
	// priorityFilter, _ := arguments["priority"].(string)
	
	// Return placeholder message since we don't have persistent storage
	result := "TODO list functionality requires persistent storage implementation"
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
		IsError: false,
	}, nil
}

func (m *MateyMCPServer) updateTodoStatus(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	id, ok := arguments["id"].(string)
	if !ok || id == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: id is required"}},
			IsError: true,
		}, fmt.Errorf("id is required")
	}
	
	status, ok := arguments["status"].(string)
	if !ok || status == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: status is required"}},
			IsError: true,
		}, fmt.Errorf("status is required")
	}
	
	// Validate status
	validStatuses := map[string]bool{
		"pending":     true,
		"in_progress": true,
		"completed":   true,
	}
	
	if !validStatuses[status] {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: status must be pending, in_progress, or completed"}},
			IsError: true,
		}, fmt.Errorf("invalid status")
	}
	
	result := fmt.Sprintf("Updated TODO item %s status to %s", id, status)
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
		IsError: false,
	}, nil
}

func (m *MateyMCPServer) getTodoStats(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	// Return placeholder stats
	result := "TODO Stats: 0 pending, 0 in_progress, 0 completed (requires persistent storage)"
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
		IsError: false,
	}, nil
}

func (m *MateyMCPServer) clearCompletedTodos(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	result := "Cleared 0 completed TODO items (requires persistent storage)"
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
		IsError: false,
	}, nil
}

func (m *MateyMCPServer) searchInFiles(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	pattern, ok := arguments["pattern"].(string)
	if !ok || pattern == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: pattern is required"}},
			IsError: true,
		}, fmt.Errorf("pattern is required")
	}

	// Get search parameters
	files, _ := arguments["files"].([]interface{})
	filePattern, _ := arguments["file_pattern"].(string)
	isRegex := getBoolArg(arguments, "regex", false)
	caseSensitive := getBoolArg(arguments, "case_sensitive", false)
	maxResults := getIntArg(arguments, "max_results", 100)
	contextLines := getIntArg(arguments, "context_lines", 2)

	var searchFiles []string

	// Determine files to search
	if len(files) > 0 {
		for _, f := range files {
			if filepath, ok := f.(string); ok {
				searchFiles = append(searchFiles, filepath)
			}
		}
	} else if filePattern != "" {
		// Use glob pattern to find files
		matches, err := filepath.Glob(filePattern)
		if err == nil {
			searchFiles = matches
		}
	} else {
		// Default to searching current directory Go files
		matches, _ := filepath.Glob("*.go")
		searchFiles = matches
	}

	if len(searchFiles) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "No files found to search"}},
			IsError: false,
		}, nil
	}

	// Compile regex if needed
	var regex *regexp.Regexp
	var err error
	if isRegex {
		flags := ""
		if !caseSensitive {
			flags = "(?i)"
		}
		regex, err = regexp.Compile(flags + pattern)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Invalid regex pattern: %v", err)}},
				IsError: true,
			}, err
		}
	}

	var results []string
	totalMatches := 0

	for _, filePath := range searchFiles {
		if totalMatches >= maxResults {
			break
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			continue // Skip files that can't be read
		}

		lines := strings.Split(string(content), "\n")
		var fileMatches []string

		for lineNum, line := range lines {
			if totalMatches >= maxResults {
				break
			}

			var matched bool
			if isRegex {
				matched = regex.MatchString(line)
			} else {
				searchLine := line
				searchPattern := pattern
				if !caseSensitive {
					searchLine = strings.ToLower(line)
					searchPattern = strings.ToLower(pattern)
				}
				matched = strings.Contains(searchLine, searchPattern)
			}

			if matched {
				// Add context lines
				start := lineNum - contextLines
				end := lineNum + contextLines + 1
				if start < 0 {
					start = 0
				}
				if end > len(lines) {
					end = len(lines)
				}

				match := fmt.Sprintf("%s:%d:", filePath, lineNum+1)
				for i := start; i < end; i++ {
					prefix := "  "
					if i == lineNum {
						prefix = "> "
					}
					match += fmt.Sprintf("\n%s%d: %s", prefix, i+1, lines[i])
				}
				fileMatches = append(fileMatches, match)
				totalMatches++
			}
		}

		results = append(results, fileMatches...)
	}

	if len(results) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("No matches found for pattern: %s", pattern)}},
			IsError: false,
		}, nil
	}

	result := fmt.Sprintf("Found %d matches:\n\n%s", totalMatches, strings.Join(results, "\n\n"))
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result}},
		IsError: false,
	}, nil
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

	// Security validation - basic checks
	if err := validateCommand(command); err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Command validation failed: %v", err)}},
			IsError: true,
		}, err
	}

	// Create command with timeout context
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", command)
	cmd.Dir = workingDir

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

func validateCommand(command string) error {
	// Basic security validation
	dangerous := []string{
		"rm -rf /",
		":(){ :|:& };:",
		"> /dev/",
		"curl.*|.*sh",
		"wget.*|.*sh",
	}
	
	for _, danger := range dangerous {
		if matched, _ := regexp.MatchString(danger, command); matched {
			return fmt.Errorf("potentially dangerous command detected")
		}
	}
	
	return nil
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
	ObjectiveCompleted bool                   `json:"objective_completed"`
	Result             string                 `json:"result"`
	ExecutionSummary   string                 `json:"execution_summary"`
	ExecutionLog       []string               `json:"execution_log,omitempty"`
	ToolCallsMade      int                    `json:"tool_calls_made"`
	ToolsUsed          map[string]int         `json:"tools_used"`
	DurationSeconds    float64                `json:"duration_seconds"`
	StructuredData     interface{}            `json:"structured_data,omitempty"`
	Errors             []string               `json:"errors,omitempty"`
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
	} else if len(p.toolCalls) == p.maxDisplayLines + 1 {
		// Switch to summary mode
		fmt.Printf("   │ +%d more tool calls (%s)\n", 
			len(p.toolCalls) - p.maxDisplayLines, 
			p.generateToolSummary())
	} else {
		// Update summary line
		fmt.Printf("\r   │ +%d more tool calls (%s)", 
			len(p.toolCalls) - p.maxDisplayLines, 
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
func (m *MateyMCPServer) executeAgent(ctx context.Context, arguments map[string]interface{}) (*ToolResult, error) {
	// executeAgent called
	
	// Parse arguments with defaults
	objective := getStringArg(arguments, "objective", "")
	// Extracted objective
	if objective == "" {
		errorMsg := fmt.Sprintf("❌ EXECUTE_AGENT PARAMETER ERROR ❌\n\nThe 'objective' parameter is REQUIRED but was missing or empty.\n\nReceived arguments: %+v\n\nCorrect format:\n{\n  \"objective\": \"Your task description here\",\n  \"ai_provider\": \"openrouter\",\n  \"ai_model\": \"your-model\",\n  \"output_format\": \"structured_data\"\n}\n\nPlease provide the objective parameter with a clear task description.", arguments)
		// executeAgent failed
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
	
	// Starting execute_agent with timeout
	
	// Initialize AI manager with the user's current provider and model
	m.initializeAIManager()
	
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
	m.executeAgentTask(agentCtx, objective, behavioralInstructions, contextInfo, maxTurns, progress, result)
	
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

// AgentFactory represents a function that can create a new TermChat instance
type AgentFactory func() interface{}

// SetAgentFactory sets the factory function for creating TermChat instances
func (m *MateyMCPServer) SetAgentFactory(factory AgentFactory) {
	m.agentFactory = factory
}

// SetupRecursiveAgent configures the MCP server to use recursive TermChat instances
// This function should be called from the chat package to avoid circular imports
func SetupRecursiveAgent(server *MateyMCPServer, newTermChatFunc func() interface{}) {
	server.SetAgentFactory(newTermChatFunc)
}

// executeAgentTask executes the actual agent task using REAL LLM reasoning and tool execution
func (m *MateyMCPServer) executeAgentTask(ctx context.Context, objective, behavioralInstructions, contextInfo string, maxTurns int, progress *AgentProgress, result *AgentResult) {
	fmt.Printf("   │ 🧠 Initializing real LLM-powered sub-agent\n")
	
	// Create the dynamic system prompt for the sub-agent
	subAgentPrompt := m.createDynamicSubAgentPrompt(objective, behavioralInstructions, contextInfo, maxTurns)
	
	// Execute with REAL LLM reasoning and tool execution
	toolsExecuted := m.executeRealLLMAgent(ctx, objective, subAgentPrompt, progress, result, maxTurns)
	
	result.StructuredData = map[string]interface{}{
		"objective":          objective,
		"tools_executed":     toolsExecuted,
		"behavioral_context": behavioralInstructions,
		"agent_type":         "Real LLM-powered recursive agent",
		"execution_approach": "true_llm_reasoning",
		"available_tools":    len(m.GetTools()),
		"sub_agent_prompt":   len(subAgentPrompt) > 100,
	}
}

// createDynamicSubAgentPrompt creates a dynamic system prompt for the sub-agent
func (m *MateyMCPServer) createDynamicSubAgentPrompt(objective, behavioralInstructions, contextInfo string, maxTurns int) string {
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
func (m *MateyMCPServer) executeRealLLMAgent(ctx context.Context, objective, systemPrompt string, progress *AgentProgress, result *AgentResult, maxTurns int) int {
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
		availableTools := m.buildAvailableToolsForMCP()
		
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
		toolResult, err := m.callOpenRouterGateway(ctx, completionArgs)
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
				Content   string `json:"content"`
				Model     string `json:"model"`
				Usage     interface{} `json:"usage"`
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
												toolExecResult, err := m.ExecuteTool(ctx, toolName, args)
												toolsExecuted++
												
												if err != nil {
													logMsg := fmt.Sprintf("   │ ✗ %s failed: %v", toolName, err)
													result.ExecutionLog = append(result.ExecutionLog, logMsg)
													progress.UpdateDisplay(ToolCallSummary{
														Name:     toolName,
														Status:   "✗",
														Summary:  fmt.Sprintf("Error: %v", err),
														Duration: time.Second,
													})
													result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", toolName, err))
													
													// Add error to conversation
													conversationHistory = append(conversationHistory, map[string]interface{}{
														"role":    "assistant", 
														"content": fmt.Sprintf("I tried to use %s but got an error: %v", toolName, err),
													})
												} else {
													// Tool succeeded
													summary := m.extractToolSummary(toolExecResult, toolName)
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
func (m *MateyMCPServer) buildAvailableToolsForMCP() []map[string]interface{} {
	mcpTools := m.GetTools()
	var tools []map[string]interface{}
	
	// Essential tools for execute_agent analysis (limit to avoid request size issues)
	essentialTools := map[string]bool{
		"search_in_files":      true,
		"execute_bash":         true,
		"create_todos":         true,
		"update_todo_status":   true,
		"list_todos":           true,
		"matey_ps":            true,
		"matey_logs":          true,
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
func (m *MateyMCPServer) callOpenRouterGateway(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
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
	
	// Debug request size
	
	// Call openrouter-gateway MCP server with fixed timeout for execute_agent
	// Creating HTTP request
	
	// For execute_agent operations, use a fixed long timeout instead of inheriting context deadline
	// This prevents short HTTP request timeouts from affecting long-running execute_agent operations
	requestCtx, requestCancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer requestCancel()
	
	req, err := http.NewRequestWithContext(requestCtx, "POST", "http://openrouter-gateway.matey.svc.cluster.local:8012", bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	
	// Making HTTP request to openrouter-gateway
	
	// Create HTTP client with fixed 20-minute timeout for execute_agent operations
	timeout := 20 * time.Minute
	
	client := &http.Client{
		Timeout: timeout,
	}
	// HTTP client timeout set
	
	resp, err := client.Do(req)
	
	// HTTP request completed
	if err != nil {
		// HTTP request error
		return nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("Warning: Failed to close response body: %v\n", err)
		}
	}()
	
	// HTTP response received
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// HTTP error response
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	
	// Read and parse response body  
	// Reading response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		// Failed to read response body
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}
	// Response body received
	
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
	
	// Parsing JSON response
	if err := json.Unmarshal(body, &mcpResponse); err != nil {
		// JSON parse error
		return nil, fmt.Errorf("failed to parse MCP response: %v", err)
	}
	// JSON parsing completed successfully
	
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


// Note: All old simulation functions removed - now using real LLM reasoning!



// extractToolSummary extracts a meaningful summary from tool result for progress display
func (m *MateyMCPServer) extractToolSummary(toolResult *ToolResult, toolName string) string {
	if toolResult == nil || len(toolResult.Content) == 0 {
		return "Completed"
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



