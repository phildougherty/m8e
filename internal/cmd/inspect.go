// internal/cmd/inspect.go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/phildougherty/m8e/internal/crd"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func NewInspectCommand() *cobra.Command {
	var outputFormat string
	var showConditions bool
	var showWide bool

	cmd := &cobra.Command{
		Use:   "inspect [resource-type] [resource-name]",
		Short: "Display detailed information about MCP resources",
		Long: `Inspect MCP resources and display detailed information about their configuration and status.

Resource types:
  memory         - MCPMemory resources
  server         - MCPServer resources  
  task-scheduler - MCPTaskScheduler resources
  proxy          - MCPProxy resources
  toolbox        - MCPToolbox resources
  workflow       - Workflow resources
  all            - All resource types (default)

Examples:
  matey inspect                          # Show all resources
  matey inspect memory                   # Show all memory resources
  matey inspect server my-server         # Show specific server
  matey inspect task-scheduler -o json   # Show task scheduler in JSON format
  matey inspect memory --wide            # Show memory resources with extra columns`,
		ValidArgs: []string{"memory", "server", "task-scheduler", "proxy", "toolbox", "workflow", "all"},
		RunE: func(cmd *cobra.Command, args []string) error {
			namespace, _ := cmd.Flags().GetString("namespace")
			
			// Default to "all" if no resource type specified
			resourceType := "all"
			resourceName := ""
			
			if len(args) > 0 {
				resourceType = args[0]
			}
			if len(args) > 1 {
				resourceName = args[1]
			}

			// Validate resource type
			validTypes := []string{"memory", "server", "task-scheduler", "proxy", "toolbox", "workflow", "all"}
			if !contains(validTypes, resourceType) {
				return fmt.Errorf("invalid resource type '%s'. Valid types: %s", resourceType, strings.Join(validTypes, ", "))
			}

			// Validate output format
			validFormats := []string{"table", "json", "yaml"}
			if !contains(validFormats, outputFormat) {
				return fmt.Errorf("invalid output format '%s'. Valid formats: %s", outputFormat, strings.Join(validFormats, ", "))
			}

			return inspectResources(resourceType, resourceName, namespace, outputFormat, showConditions, showWide)
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format: table, json, yaml")
	cmd.Flags().BoolVar(&showConditions, "show-conditions", false, "Show condition details")
	cmd.Flags().BoolVar(&showWide, "wide", false, "Show additional columns in table format")

	return cmd
}

func inspectResources(resourceType, resourceName, namespace, outputFormat string, showConditions, showWide bool) error {
	// Create Kubernetes client
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	ctx := context.Background()
	var resources []ResourceInfo

	switch resourceType {
	case "memory":
		resources, err = getMemoryResources(ctx, k8sClient, namespace, resourceName)
	case "server":
		resources, err = getServerResources(ctx, k8sClient, namespace, resourceName)
	case "task-scheduler":
		resources, err = getTaskSchedulerResources(ctx, k8sClient, namespace, resourceName)
	case "proxy":
		resources, err = getProxyResources(ctx, k8sClient, namespace, resourceName)
	case "toolbox":
		resources, err = getToolboxResources(ctx, k8sClient, namespace, resourceName)
	case "workflow":
		resources, err = getWorkflowResources(ctx, k8sClient, namespace, resourceName)
	case "all":
		resources, err = getAllResources(ctx, k8sClient, namespace, resourceName)
	default:
		return fmt.Errorf("unsupported resource type: %s", resourceType)
	}

	if err != nil {
		return fmt.Errorf("failed to get resources: %w", err)
	}

	if len(resources) == 0 {
		if resourceName != "" {
			fmt.Printf("No %s resource found with name '%s' in namespace '%s'\n", resourceType, resourceName, namespace)
		} else {
			fmt.Printf("No %s resources found in namespace '%s'\n", resourceType, namespace)
		}
		return nil
	}

	return displayResources(resources, outputFormat, showConditions, showWide)
}

type ResourceInfo struct {
	Type      string      `json:"type"`
	Name      string      `json:"name"`
	Namespace string      `json:"namespace"`
	Phase     string      `json:"phase"`
	Age       string      `json:"age"`
	Ready     string      `json:"ready"`
	Status    string      `json:"status"`
	Endpoint  string      `json:"endpoint,omitempty"`
	Image     string      `json:"image,omitempty"`
	Protocol  string      `json:"protocol,omitempty"`
	Resource  interface{} `json:"resource,omitempty"`
	Conditions []ConditionInfo `json:"conditions,omitempty"`
}

type ConditionInfo struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

func getMemoryResources(ctx context.Context, k8sClient client.Client, namespace, resourceName string) ([]ResourceInfo, error) {
	var resources []ResourceInfo

	if resourceName != "" {
		// Get specific resource
		var memory crd.MCPMemory
		key := client.ObjectKey{Name: resourceName, Namespace: namespace}
		if err := k8sClient.Get(ctx, key, &memory); err != nil {
			return nil, err
		}
		resources = append(resources, convertMemoryToResourceInfo(&memory))
	} else {
		// List all resources
		var memoryList crd.MCPMemoryList
		if err := k8sClient.List(ctx, &memoryList, client.InNamespace(namespace)); err != nil {
			return nil, err
		}
		for _, memory := range memoryList.Items {
			resources = append(resources, convertMemoryToResourceInfo(&memory))
		}
	}

	return resources, nil
}

func getServerResources(ctx context.Context, k8sClient client.Client, namespace, resourceName string) ([]ResourceInfo, error) {
	var resources []ResourceInfo

	if resourceName != "" {
		// Get specific resource
		var server crd.MCPServer
		key := client.ObjectKey{Name: resourceName, Namespace: namespace}
		if err := k8sClient.Get(ctx, key, &server); err != nil {
			return nil, err
		}
		resources = append(resources, convertServerToResourceInfo(&server))
	} else {
		// List all resources
		var serverList crd.MCPServerList
		if err := k8sClient.List(ctx, &serverList, client.InNamespace(namespace)); err != nil {
			return nil, err
		}
		for _, server := range serverList.Items {
			resources = append(resources, convertServerToResourceInfo(&server))
		}
	}

	return resources, nil
}

func getTaskSchedulerResources(ctx context.Context, k8sClient client.Client, namespace, resourceName string) ([]ResourceInfo, error) {
	var resources []ResourceInfo

	if resourceName != "" {
		// Get specific resource
		var taskScheduler crd.MCPTaskScheduler
		key := client.ObjectKey{Name: resourceName, Namespace: namespace}
		if err := k8sClient.Get(ctx, key, &taskScheduler); err != nil {
			return nil, err
		}
		resources = append(resources, convertTaskSchedulerToResourceInfo(&taskScheduler))
	} else {
		// List all resources
		var taskSchedulerList crd.MCPTaskSchedulerList
		if err := k8sClient.List(ctx, &taskSchedulerList, client.InNamespace(namespace)); err != nil {
			return nil, err
		}
		for _, taskScheduler := range taskSchedulerList.Items {
			resources = append(resources, convertTaskSchedulerToResourceInfo(&taskScheduler))
		}
	}

	return resources, nil
}

func getProxyResources(ctx context.Context, k8sClient client.Client, namespace, resourceName string) ([]ResourceInfo, error) {
	var resources []ResourceInfo

	if resourceName != "" {
		// Get specific resource
		var proxy crd.MCPProxy
		key := client.ObjectKey{Name: resourceName, Namespace: namespace}
		if err := k8sClient.Get(ctx, key, &proxy); err != nil {
			return nil, err
		}
		resources = append(resources, convertProxyToResourceInfo(&proxy))
	} else {
		// List all resources
		var proxyList crd.MCPProxyList
		if err := k8sClient.List(ctx, &proxyList, client.InNamespace(namespace)); err != nil {
			return nil, err
		}
		for _, proxy := range proxyList.Items {
			resources = append(resources, convertProxyToResourceInfo(&proxy))
		}
	}

	return resources, nil
}

func getToolboxResources(ctx context.Context, k8sClient client.Client, namespace, resourceName string) ([]ResourceInfo, error) {
	var resources []ResourceInfo

	if resourceName != "" {
		// Get specific resource
		var toolbox crd.MCPToolbox
		key := client.ObjectKey{Name: resourceName, Namespace: namespace}
		if err := k8sClient.Get(ctx, key, &toolbox); err != nil {
			return nil, err
		}
		resources = append(resources, convertToolboxToResourceInfo(&toolbox))
	} else {
		// List all resources
		var toolboxList crd.MCPToolboxList
		if err := k8sClient.List(ctx, &toolboxList, client.InNamespace(namespace)); err != nil {
			return nil, err
		}
		for _, toolbox := range toolboxList.Items {
			resources = append(resources, convertToolboxToResourceInfo(&toolbox))
		}
	}

	return resources, nil
}

func getWorkflowResources(ctx context.Context, k8sClient client.Client, namespace, resourceName string) ([]ResourceInfo, error) {
	// TODO: Update to use MCPTaskScheduler workflow inspection instead of separate Workflow CRD
	// Return empty for now since workflows are being migrated to unified Task Scheduler
	return []ResourceInfo{}, nil
}

func getAllResources(ctx context.Context, k8sClient client.Client, namespace, resourceName string) ([]ResourceInfo, error) {
	var allResources []ResourceInfo

	// Get all resource types
	memoryResources, err := getMemoryResources(ctx, k8sClient, namespace, resourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory resources: %w", err)
	}
	allResources = append(allResources, memoryResources...)

	serverResources, err := getServerResources(ctx, k8sClient, namespace, resourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get server resources: %w", err)
	}
	allResources = append(allResources, serverResources...)

	taskSchedulerResources, err := getTaskSchedulerResources(ctx, k8sClient, namespace, resourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get task scheduler resources: %w", err)
	}
	allResources = append(allResources, taskSchedulerResources...)

	proxyResources, err := getProxyResources(ctx, k8sClient, namespace, resourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxy resources: %w", err)
	}
	allResources = append(allResources, proxyResources...)

	toolboxResources, err := getToolboxResources(ctx, k8sClient, namespace, resourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get toolbox resources: %w", err)
	}
	allResources = append(allResources, toolboxResources...)

	workflowResources, err := getWorkflowResources(ctx, k8sClient, namespace, resourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow resources: %w", err)
	}
	allResources = append(allResources, workflowResources...)

	return allResources, nil
}

func convertMemoryToResourceInfo(memory *crd.MCPMemory) ResourceInfo {
	age := time.Since(memory.CreationTimestamp.Time).Truncate(time.Second).String()
	
	// Handle nil Spec.Replicas pointer
	specReplicas := int32(1) // default value
	if memory.Spec.Replicas != nil {
		specReplicas = *memory.Spec.Replicas
	}
	ready := fmt.Sprintf("%d/%d", memory.Status.ReadyReplicas, specReplicas)
	
	endpoint := ""
	if memory.Status.DatabaseConnectionInfo != nil {
		endpoint = memory.Status.DatabaseConnectionInfo.Endpoint
	}

	conditions := make([]ConditionInfo, len(memory.Status.Conditions))
	for i, condition := range memory.Status.Conditions {
		conditions[i] = ConditionInfo{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		}
	}

	return ResourceInfo{
		Type:       "memory",
		Name:       memory.Name,
		Namespace:  memory.Namespace,
		Phase:      string(memory.Status.Phase),
		Age:        age,
		Ready:      ready,
		Status:     memory.Status.HealthStatus,
		Endpoint:   endpoint,
		Protocol:   "http",
		Resource:   memory,
		Conditions: conditions,
	}
}

func convertServerToResourceInfo(server *crd.MCPServer) ResourceInfo {
	age := time.Since(server.CreationTimestamp.Time).Truncate(time.Second).String()
	
	// Handle nil Spec.Replicas pointer
	specReplicas := int32(1) // default value
	if server.Spec.Replicas != nil {
		specReplicas = *server.Spec.Replicas
	}
	ready := fmt.Sprintf("%d/%d", server.Status.ReadyReplicas, specReplicas)
	
	endpoint := ""
	if server.Status.ConnectionInfo != nil {
		endpoint = server.Status.ConnectionInfo.Endpoint
	}

	conditions := make([]ConditionInfo, len(server.Status.Conditions))
	for i, condition := range server.Status.Conditions {
		conditions[i] = ConditionInfo{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		}
	}

	return ResourceInfo{
		Type:       "server",
		Name:       server.Name,
		Namespace:  server.Namespace,
		Phase:      string(server.Status.Phase),
		Age:        age,
		Ready:      ready,
		Status:     server.Status.HealthStatus,
		Endpoint:   endpoint,
		Image:      server.Spec.Image,
		Protocol:   server.Spec.Protocol,
		Resource:   server,
		Conditions: conditions,
	}
}

func convertTaskSchedulerToResourceInfo(taskScheduler *crd.MCPTaskScheduler) ResourceInfo {
	age := time.Since(taskScheduler.CreationTimestamp.Time).Truncate(time.Second).String()
	
	// Handle nil Spec.Replicas pointer
	specReplicas := int32(1) // default value
	if taskScheduler.Spec.Replicas != nil {
		specReplicas = *taskScheduler.Spec.Replicas
	}
	ready := fmt.Sprintf("%d/%d", taskScheduler.Status.ReadyReplicas, specReplicas)
	
	endpoint := ""
	if taskScheduler.Status.ConnectionInfo != nil {
		endpoint = taskScheduler.Status.ConnectionInfo.Endpoint
	}

	conditions := make([]ConditionInfo, len(taskScheduler.Status.Conditions))
	for i, condition := range taskScheduler.Status.Conditions {
		conditions[i] = ConditionInfo{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		}
	}

	return ResourceInfo{
		Type:       "task-scheduler",
		Name:       taskScheduler.Name,
		Namespace:  taskScheduler.Namespace,
		Phase:      string(taskScheduler.Status.Phase),
		Age:        age,
		Ready:      ready,
		Status:     taskScheduler.Status.HealthStatus,
		Endpoint:   endpoint,
		Image:      taskScheduler.Spec.Image,
		Protocol:   "http",
		Resource:   taskScheduler,
		Conditions: conditions,
	}
}

func convertProxyToResourceInfo(proxy *crd.MCPProxy) ResourceInfo {
	age := time.Since(proxy.CreationTimestamp.Time).Truncate(time.Second).String()
	
	// Handle nil Spec.Replicas pointer
	specReplicas := int32(1) // default value
	if proxy.Spec.Replicas != nil {
		specReplicas = *proxy.Spec.Replicas
	}
	ready := fmt.Sprintf("%d/%d", proxy.Status.ReadyReplicas, specReplicas)

	conditions := make([]ConditionInfo, len(proxy.Status.Conditions))
	for i, condition := range proxy.Status.Conditions {
		conditions[i] = ConditionInfo{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		}
	}

	return ResourceInfo{
		Type:       "proxy",
		Name:       proxy.Name,
		Namespace:  proxy.Namespace,
		Phase:      string(proxy.Status.Phase),
		Age:        age,
		Ready:      ready,
		Status:     proxy.Status.HealthStatus,
		Protocol:   "http",
		Resource:   proxy,
		Conditions: conditions,
	}
}

func convertToolboxToResourceInfo(toolbox *crd.MCPToolbox) ResourceInfo {
	age := time.Since(toolbox.CreationTimestamp.Time).Truncate(time.Second).String()
	ready := fmt.Sprintf("%d/%d", toolbox.Status.ReadyServers, toolbox.Status.ServerCount)

	conditions := make([]ConditionInfo, len(toolbox.Status.Conditions))
	for i, condition := range toolbox.Status.Conditions {
		conditions[i] = ConditionInfo{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		}
	}

	return ResourceInfo{
		Type:       "toolbox",
		Name:       toolbox.Name,
		Namespace:  toolbox.Namespace,
		Phase:      string(toolbox.Status.Phase),
		Age:        age,
		Ready:      ready,
		Status:     string(toolbox.Status.Phase),
		Resource:   toolbox,
		Conditions: conditions,
	}
}

// TODO: Remove this function when workflow inspection is updated for MCPTaskScheduler
// convertWorkflowToResourceInfo is disabled - workflows migrated to Task Scheduler

func displayResources(resources []ResourceInfo, outputFormat string, showConditions, showWide bool) error {
	switch outputFormat {
	case "json":
		return displayJSON(resources)
	case "yaml":
		return displayYAML(resources)
	default:
		return displayTable(resources, showConditions, showWide)
	}
}

func displayTable(resources []ResourceInfo, showConditions, showWide bool) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	// Header
	if showWide {
		fmt.Fprintln(w, "TYPE\tNAME\tNAMESPACE\tPHASE\tREADY\tSTATUS\tAGE\tENDPOINT\tIMAGE\tPROTOCOL")
	} else {
		fmt.Fprintln(w, "TYPE\tNAME\tNAMESPACE\tPHASE\tREADY\tSTATUS\tAGE")
	}

	// Rows
	for _, resource := range resources {
		if showWide {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				resource.Type, resource.Name, resource.Namespace, resource.Phase,
				resource.Ready, resource.Status, resource.Age, resource.Endpoint,
				resource.Image, resource.Protocol)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				resource.Type, resource.Name, resource.Namespace, resource.Phase,
				resource.Ready, resource.Status, resource.Age)
		}
	}

	// Show conditions if requested
	if showConditions && len(resources) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "CONDITIONS:")
		for _, resource := range resources {
			if len(resource.Conditions) > 0 {
				fmt.Fprintf(w, "\n%s/%s:\n", resource.Type, resource.Name)
				for _, condition := range resource.Conditions {
					fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", condition.Type, condition.Status, condition.Reason, condition.Message)
				}
			}
		}
	}

	return nil
}

func displayJSON(resources []ResourceInfo) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(resources)
}

func displayYAML(resources []ResourceInfo) error {
	data, err := yaml.Marshal(resources)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// createK8sClientWithScheme creates a Kubernetes client with CRD scheme
func createK8sClientWithScheme() (client.Client, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	// Create the scheme with CRDs
	scheme := runtime.NewScheme()
	if err := crd.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add CRD scheme: %w", err)
	}

	// Create the client
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return k8sClient, nil
}