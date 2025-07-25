package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
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
	
	return server
}

// initializeK8sComponents initializes the Kubernetes client and composer
func (m *MateyMCPServer) initializeK8sComponents() {
	fmt.Printf("DEBUG: Initializing K8s components for MCP server...\n")
	fmt.Printf("DEBUG: Config file: %s, Namespace: %s\n", m.configFile, m.namespace)
	
	// Create k8s config first (most critical component)
	fmt.Printf("DEBUG: Creating K8s config...\n")
	config, err := createK8sConfig()
	if err != nil {
		fmt.Printf("ERROR: Failed to create k8s config, falling back to binary execution: %v\n", err)
		return
	}
	m.config = config
	fmt.Printf("DEBUG: K8s config created successfully\n")
	
	// Add CRD scheme
	fmt.Printf("DEBUG: Adding CRD scheme...\n")
	scheme := runtime.NewScheme()
	if err := crd.AddToScheme(scheme); err != nil {
		fmt.Printf("ERROR: Failed to add CRD scheme, falling back to binary execution: %v\n", err)
		return
	}
	fmt.Printf("DEBUG: CRD scheme added successfully\n")
	
	// Create k8s client
	fmt.Printf("DEBUG: Creating K8s client...\n")
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		fmt.Printf("ERROR: Failed to create k8s client, falling back to binary execution: %v\n", err)
		return
	}
	m.k8sClient = k8sClient
	fmt.Printf("DEBUG: K8s client created successfully\n")
	
	// Create kubernetes clientset for advanced operations (logs, etc.)
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Printf("WARNING: Failed to create kubernetes clientset: %v\n", err)
	} else {
		m.clientset = clientset
		fmt.Printf("DEBUG: Kubernetes clientset created successfully\n")
		
		// Initialize workspace manager if clientset is available
		logger := logr.Discard() // Simple logger for now, could be improved
		m.workspaceManager = NewWorkspaceManager(m.clientset, m.namespace, logger)
		fmt.Printf("DEBUG: Workspace manager created successfully\n")
	}
	
	// Create composer (less critical, can still function without it)
	fmt.Printf("DEBUG: Creating K8s composer...\n")
	composer, err := compose.NewK8sComposer(m.configFile, m.namespace)
	if err != nil {
		fmt.Printf("WARNING: Failed to initialize composer (non-critical): %v\n", err)
		// Don't return here - we can still function with just the K8s client
	} else {
		m.composer = composer
		fmt.Printf("DEBUG: Composer created successfully\n")
	}
	
	fmt.Printf("DEBUG: K8s components initialized - useK8sClient() will return: %v\n", m.useK8sClient())
	
	// Test the client with a simple operation
	if m.k8sClient != nil {
		fmt.Printf("DEBUG: Testing K8s client with namespace list...\n")
		var namespaces corev1.NamespaceList
		if err := m.k8sClient.List(context.Background(), &namespaces); err != nil {
			fmt.Printf("WARNING: K8s client test failed: %v\n", err)
		} else {
			fmt.Printf("DEBUG: K8s client test successful - found %d namespaces\n", len(namespaces.Items))
		}
	}
}

// createK8sConfig creates a Kubernetes config from in-cluster or kubeconfig
func createK8sConfig() (*rest.Config, error) {
	// Try in-cluster config first
	fmt.Printf("DEBUG: Attempting in-cluster config...\n")
	config, err := rest.InClusterConfig()
	if err == nil {
		fmt.Printf("DEBUG: Using in-cluster config\n")
		return config, nil
	}
	fmt.Printf("DEBUG: In-cluster config failed: %v\n", err)
	
	// Fall back to kubeconfig
	kubeconfigPath := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	fmt.Printf("DEBUG: Attempting kubeconfig at: %s\n", kubeconfigPath)
	
	config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		fmt.Printf("DEBUG: Kubeconfig failed: %v\n", err)
		return nil, fmt.Errorf("failed to create k8s config - in-cluster: %v, kubeconfig: %v", err, err)
	}
	
	fmt.Printf("DEBUG: Using kubeconfig\n")
	return config, nil
}

// useK8sClient returns true if we can use the Kubernetes client
func (m *MateyMCPServer) useK8sClient() bool {
	return m.k8sClient != nil && m.composer != nil
}

// ExecuteTool executes a tool by name with the given arguments
func (m *MateyMCPServer) ExecuteTool(ctx context.Context, name string, arguments map[string]interface{}) (*ToolResult, error) {
	switch name {
	// Core operations
	case "matey_ps":
		return m.mateyPS(ctx, arguments)
	case "matey_up":
		return m.mateyUp(ctx, arguments)
	case "matey_down":
		return m.mateyDown(ctx, arguments)
	case "matey_logs":
		return m.mateyLogs(ctx, arguments)
	case "matey_inspect":
		return m.mateyInspect(ctx, arguments)
	case "apply_config":
		return m.applyConfig(ctx, arguments)
	case "get_cluster_state":
		return m.getClusterState(ctx, arguments)

	// Workflow operations
	case "create_workflow":
		return m.createWorkflow(ctx, arguments)
	case "list_workflows":
		return m.listWorkflows(ctx, arguments)
	case "get_workflow":
		return m.getWorkflow(ctx, arguments)
	case "delete_workflow":
		return m.deleteWorkflow(ctx, arguments)
	case "execute_workflow":
		return m.executeWorkflow(ctx, arguments)
	case "pause_workflow":
		return m.pauseWorkflow(ctx, arguments)
	case "resume_workflow":
		return m.resumeWorkflow(ctx, arguments)
	case "workflow_logs":
		return m.workflowLogs(ctx, arguments)
	case "workflow_templates":
		return m.workflowTemplates(ctx, arguments)

	// Service management
	case "start_service":
		return m.startService(ctx, arguments)
	case "stop_service":
		return m.stopService(ctx, arguments)
	case "memory_status":
		return m.memoryStatus(ctx, arguments)
	case "memory_start":
		return m.memoryStart(ctx, arguments)
	case "memory_stop":
		return m.memoryStop(ctx, arguments)
	
	// Memory graph tools
	case "create_entities":
		return m.createEntities(ctx, arguments)
	case "delete_entities":
		return m.deleteEntities(ctx, arguments)
	case "add_observations":
		return m.addObservations(ctx, arguments)
	case "delete_observations":
		return m.deleteObservations(ctx, arguments)
	case "create_relations":
		return m.createRelations(ctx, arguments)
	case "delete_relations":
		return m.deleteRelations(ctx, arguments)
	case "read_graph":
		return m.readGraph(ctx, arguments)
	case "search_nodes":
		return m.searchNodes(ctx, arguments)
	case "open_nodes":
		return m.openNodes(ctx, arguments)
	case "memory_health_check":
		return m.memoryHealthCheck(ctx, arguments)
	case "memory_stats":
		return m.memoryStats(ctx, arguments)
		
	case "task_scheduler_status":
		return m.taskSchedulerStatus(ctx, arguments)
	case "task_scheduler_start":
		return m.taskSchedulerStart(ctx, arguments)
	case "task_scheduler_stop":
		return m.taskSchedulerStop(ctx, arguments)
	case "reload_proxy":
		return m.reloadProxy(ctx, arguments)

	// Toolbox and configuration
	case "list_toolboxes":
		return m.listToolboxes(ctx, arguments)
	case "get_toolbox":
		return m.getToolbox(ctx, arguments)
	case "validate_config":
		return m.validateConfig(ctx, arguments)
	case "create_config":
		return m.createConfig(ctx, arguments)
	case "install_matey":
		return m.installMatey(ctx, arguments)

	// Inspection operations
	case "inspect_mcpserver":
		resourceName, _ := arguments["resource_name"].(string)
		outputFormat, _ := arguments["output_format"].(string)
		if outputFormat == "" {
			outputFormat = "table"
		}
		return m.inspectMCPServers(ctx, resourceName, outputFormat)
	case "inspect_mcpmemory":
		resourceName, _ := arguments["resource_name"].(string)
		outputFormat, _ := arguments["output_format"].(string)
		if outputFormat == "" {
			outputFormat = "table"
		}
		return m.inspectMCPMemory(ctx, resourceName, outputFormat)
	case "inspect_mcptaskscheduler":
		resourceName, _ := arguments["resource_name"].(string)
		outputFormat, _ := arguments["output_format"].(string)
		if outputFormat == "" {
			outputFormat = "table"
		}
		return m.inspectMCPTaskScheduler(ctx, resourceName, outputFormat)
	case "inspect_mcpproxy":
		resourceName, _ := arguments["resource_name"].(string)
		outputFormat, _ := arguments["output_format"].(string)
		if outputFormat == "" {
			outputFormat = "table"
		}
		return m.inspectMCPProxy(ctx, resourceName, outputFormat)
	case "inspect_mcptoolbox":
		resourceName, _ := arguments["resource_name"].(string)
		outputFormat, _ := arguments["output_format"].(string)
		if outputFormat == "" {
			outputFormat = "table"
		}
		return m.inspectMCPToolbox(ctx, resourceName, outputFormat)
	case "inspect_all":
		outputFormat, _ := arguments["output_format"].(string)
		if outputFormat == "" {
			outputFormat = "table"
		}
		return m.inspectAllResources(ctx, outputFormat)

		
	// Workspace Access Tools
	case "mount_workspace":
		workflowName, _ := arguments["workflowName"].(string)
		executionID, _ := arguments["executionID"].(string)
		return m.mountWorkspace(ctx, workflowName, executionID)
	case "list_workspace_files":
		workflowName, _ := arguments["workflowName"].(string)
		executionID, _ := arguments["executionID"].(string)
		subPath, _ := arguments["subPath"].(string)
		return m.listWorkspaceFiles(ctx, workflowName, executionID, subPath)
	case "read_workspace_file":
		workflowName, _ := arguments["workflowName"].(string)
		executionID, _ := arguments["executionID"].(string)
		filePath, _ := arguments["filePath"].(string)
		maxSize, _ := arguments["maxSize"].(int)
		if maxSize == 0 {
			maxSize = 1024 * 1024 // 1MB default
		}
		return m.readWorkspaceFile(ctx, workflowName, executionID, filePath, maxSize)
	case "unmount_workspace":
		workflowName, _ := arguments["workflowName"].(string)
		executionID, _ := arguments["executionID"].(string)
		return m.unmountWorkspace(ctx, workflowName, executionID)
	case "list_mounted_workspaces":
		return m.listMountedWorkspaces(ctx)
	case "get_workspace_stats":
		return m.getWorkspaceStats(ctx)
		
	// Native tools (migrated from chat package)
	case "create_todos":
		return m.createTodos(ctx, arguments)
	case "search_in_files":
		return m.searchInFiles(ctx, arguments)
	case "execute_bash":
		return m.executeBash(ctx, arguments)
		
	default:
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", name)}},
			IsError: true,
		}, fmt.Errorf("unknown tool: %s", name)
	}
}

// initializeMemoryStore initializes the memory store by connecting to the MCPMemory database
func (m *MateyMCPServer) initializeMemoryStore() {
	fmt.Printf("DEBUG: Initializing memory store...\n")
	
	// Check if k8s client is available
	if m.k8sClient == nil {
		fmt.Printf("DEBUG: Kubernetes client not available, skipping memory store initialization\n")
		return
	}
	
	// Try to get MCPMemory resource
	var memoryResource crd.MCPMemory
	err := m.k8sClient.Get(context.Background(), client.ObjectKey{
		Name:      "memory",
		Namespace: m.namespace,
	}, &memoryResource)
	
	if err != nil {
		fmt.Printf("DEBUG: MCPMemory resource not found, memory graph tools will not be available: %v\n", err)
		return
	}
	
	// Check if the memory service is running
	if memoryResource.Status.Phase != crd.MCPMemoryPhaseRunning || memoryResource.Status.ReadyReplicas == 0 {
		fmt.Printf("DEBUG: Memory service is not running (phase: %s, ready: %d), memory graph tools will not be available\n", 
			memoryResource.Status.Phase, memoryResource.Status.ReadyReplicas)
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
		fmt.Printf("DEBUG: MCPMemory resource has no database configuration\n")
		return
	}
	
	fmt.Printf("DEBUG: Using memory database URL: %s\n", databaseURL)
	
	// Initialize memory store with the correct database URL
	memoryStore, err := memory.NewMemoryStore(databaseURL, logr.Discard())
	if err != nil {
		fmt.Printf("DEBUG: Failed to initialize memory store: %v (this is expected if memory service is not ready)\n", err)
		return
	}
	
	// Test the connection
	if err := memoryStore.HealthCheck(); err != nil {
		fmt.Printf("DEBUG: Memory store health check failed: %v (this is expected if memory service is not ready)\n", err)
		memoryStore.Close() // Clean up the failed connection
		return
	}
	
	// Initialize memory tools
	memoryTools := memory.NewMCPMemoryTools(memoryStore, logr.Discard())
	
	// Set on the server
	m.memoryStore = memoryStore
	m.memoryTools = memoryTools
	
	fmt.Printf("DEBUG: Memory store initialized successfully\n")
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
