package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
	
	// Use the main matey-postgres database with a separate database for internal memory
	internalDatabaseURL := "postgresql://postgres:password@matey-postgres:5432/matey_memory?sslmode=disable"
	
	fmt.Printf("DEBUG: Using internal memory database URL: %s\n", internalDatabaseURL)
	
	// Initialize memory store with internal database
	memoryStore, err := memory.NewMemoryStore(internalDatabaseURL, logr.Discard())
	if err != nil {
		fmt.Printf("DEBUG: Failed to initialize memory store: %v\n", err)
		return
	}
	
	// Initialize memory tools
	memoryTools := memory.NewMCPMemoryTools(memoryStore, logr.Discard())
	
	// Set on the server
	m.memoryStore = memoryStore
	m.memoryTools = memoryTools
	
	fmt.Printf("DEBUG: Memory store initialized successfully\n")
}