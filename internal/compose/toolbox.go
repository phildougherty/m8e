// internal/compose/toolbox.go
package compose

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
)

// ToolboxManager manages MCPToolbox resources
type ToolboxManager struct {
	k8sClient client.Client
	namespace string
	logger    *logging.Logger
}

// NewToolboxManager creates a new toolbox manager
func NewToolboxManager(namespace string) (*ToolboxManager, error) {
	if namespace == "" {
		namespace = "default"
	}

	logger := logging.NewLogger("info")

	// Create Kubernetes client
	k8sClient, err := createToolboxK8sClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return &ToolboxManager{
		k8sClient: k8sClient,
		namespace: namespace,
		logger:    logger,
	}, nil
}

// createToolboxK8sClient creates a Kubernetes client for toolbox operations
func createToolboxK8sClient() (client.Client, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	// Create the scheme
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

// CreateToolbox creates a new MCPToolbox resource
func CreateToolbox(name, template, file, namespace string) error {
	manager, err := NewToolboxManager(namespace)
	if err != nil {
		return err
	}

	return manager.CreateToolbox(name, template, file)
}

// ListToolboxes lists all MCPToolbox resources
func ListToolboxes(namespace, format string) error {
	manager, err := NewToolboxManager(namespace)
	if err != nil {
		return err
	}

	return manager.ListToolboxes(format)
}

// StartToolbox starts an MCPToolbox
func StartToolbox(name, namespace string, wait bool, timeout time.Duration) error {
	manager, err := NewToolboxManager(namespace)
	if err != nil {
		return err
	}

	return manager.StartToolbox(name, wait, timeout)
}

// StopToolbox stops an MCPToolbox
func StopToolbox(name, namespace string, removeVolumes, force bool) error {
	manager, err := NewToolboxManager(namespace)
	if err != nil {
		return err
	}

	return manager.StopToolbox(name, removeVolumes, force)
}

// DeleteToolbox deletes an MCPToolbox
func DeleteToolbox(name, namespace string, removeVolumes, force bool) error {
	manager, err := NewToolboxManager(namespace)
	if err != nil {
		return err
	}

	return manager.DeleteToolbox(name, removeVolumes, force)
}

// ToolboxLogs shows logs from a toolbox
func ToolboxLogs(name, namespace, server string, follow bool, tail int) error {
	manager, err := NewToolboxManager(namespace)
	if err != nil {
		return err
	}

	return manager.ToolboxLogs(name, server, follow, tail)
}

// ToolboxStatus shows status of a toolbox
func ToolboxStatus(name, namespace, format string, watch bool) error {
	manager, err := NewToolboxManager(namespace)
	if err != nil {
		return err
	}

	return manager.ToolboxStatus(name, format, watch)
}

// ListToolboxTemplates lists available toolbox templates
func ListToolboxTemplates(format, template string) error {
	manager, err := NewToolboxManager("default")
	if err != nil {
		return err
	}

	return manager.ListToolboxTemplates(format, template)
}

// CreateToolbox creates a new MCPToolbox resource
func (tm *ToolboxManager) CreateToolbox(name, template, file string) error {
	ctx := context.Background()

	// Check if toolbox already exists
	existingToolbox := &crd.MCPToolbox{}
	err := tm.k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: tm.namespace,
	}, existingToolbox)

	if err == nil {
		return fmt.Errorf("toolbox %s already exists in namespace %s", name, tm.namespace)
	}

	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to check if toolbox exists: %w", err)
	}

	// Create new toolbox
	toolbox := &crd.MCPToolbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tm.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "mcp-toolbox",
				"app.kubernetes.io/instance":   name,
				"app.kubernetes.io/component":  "toolbox",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "toolbox",
			},
		},
		Spec: crd.MCPToolboxSpec{},
	}

	// Set template if provided
	if template != "" {
		toolbox.Spec.Template = template
	}

	// TODO: Load from file if provided
	if file != "" {
		return fmt.Errorf("loading toolbox from file not yet implemented")
	}

	// Create the toolbox
	tm.logger.Info("Creating MCPToolbox resource: %s", name)
	if err := tm.k8sClient.Create(ctx, toolbox); err != nil {
		return fmt.Errorf("failed to create MCPToolbox %s: %w", name, err)
	}

	fmt.Printf("Toolbox %s created successfully\n", name)
	return nil
}

// ListToolboxes lists all MCPToolbox resources
func (tm *ToolboxManager) ListToolboxes(format string) error {
	ctx := context.Background()

	toolboxList := &crd.MCPToolboxList{}
	err := tm.k8sClient.List(ctx, toolboxList, client.InNamespace(tm.namespace))
	if err != nil {
		return fmt.Errorf("failed to list toolboxes: %w", err)
	}

	switch format {
	case "json":
		// TODO: Implement JSON output
		return fmt.Errorf("JSON format not yet implemented")
	case "yaml":
		// TODO: Implement YAML output
		return fmt.Errorf("YAML format not yet implemented")
	default:
		// Table format
		if len(toolboxList.Items) == 0 {
			fmt.Printf("No toolboxes found in namespace %s\n", tm.namespace)
			return nil
		}

		fmt.Printf("%-20s %-15s %-10s %-8s %-8s %-15s\n", "NAME", "TEMPLATE", "PHASE", "SERVERS", "READY", "AGE")
		fmt.Println(strings.Repeat("-", 80))

		for _, toolbox := range toolboxList.Items {
			template := toolbox.Spec.Template
			if template == "" {
				template = "custom"
			}

			phase := string(toolbox.Status.Phase)
			if phase == "" {
				phase = "Pending"
			}

			age := time.Since(toolbox.CreationTimestamp.Time).Round(time.Second)

			fmt.Printf("%-20s %-15s %-10s %-8d %-8d %-15s\n",
				toolbox.Name,
				template,
				phase,
				toolbox.Status.ServerCount,
				toolbox.Status.ReadyServers,
				age.String(),
			)
		}
	}

	return nil
}

// StartToolbox starts an MCPToolbox (it should already be running via controller)
func (tm *ToolboxManager) StartToolbox(name string, wait bool, timeout time.Duration) error {
	ctx := context.Background()

	// Get the toolbox
	toolbox := &crd.MCPToolbox{}
	err := tm.k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: tm.namespace,
	}, toolbox)

	if err != nil {
		return fmt.Errorf("toolbox %s not found: %w", name, err)
	}

	fmt.Printf("Toolbox %s is managed by the controller and should start automatically\n", name)

	if wait {
		fmt.Printf("Waiting for toolbox %s to be ready...\n", name)
		return tm.waitForToolboxReady(name, timeout)
	}

	return nil
}

// StopToolbox stops an MCPToolbox by deleting it
func (tm *ToolboxManager) StopToolbox(name string, removeVolumes, force bool) error {
	ctx := context.Background()

	// Get the toolbox
	toolbox := &crd.MCPToolbox{}
	err := tm.k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: tm.namespace,
	}, toolbox)

	if err != nil {
		return fmt.Errorf("toolbox %s not found: %w", name, err)
	}

	if !force {
		fmt.Printf("Are you sure you want to stop toolbox %s? This will delete the toolbox resource. (y/N): ", name)
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Operation cancelled")
			return nil
		}
	}

	// Delete the toolbox (controller will handle cleanup)
	tm.logger.Info("Deleting MCPToolbox resource: %s", name)
	if err := tm.k8sClient.Delete(ctx, toolbox); err != nil {
		return fmt.Errorf("failed to delete MCPToolbox %s: %w", name, err)
	}

	fmt.Printf("Toolbox %s stopped (deleted)\n", name)
	return nil
}

// DeleteToolbox deletes an MCPToolbox
func (tm *ToolboxManager) DeleteToolbox(name string, removeVolumes, force bool) error {
	// For now, delete and stop are the same operation
	return tm.StopToolbox(name, removeVolumes, force)
}

// ToolboxLogs shows logs from a toolbox
func (tm *ToolboxManager) ToolboxLogs(name, server string, follow bool, tail int) error {
	fmt.Printf("Logs for toolbox %s:\n", name)
	fmt.Println("Use kubectl to view individual server logs:")
	
	if server != "" {
		fmt.Printf("kubectl logs -n %s -l mcp.matey.ai/toolbox=%s,mcp.matey.ai/server-name=%s", tm.namespace, name, server)
	} else {
		fmt.Printf("kubectl logs -n %s -l mcp.matey.ai/toolbox=%s", tm.namespace, name)
	}
	
	if follow {
		fmt.Print(" -f")
	}
	
	if tail > 0 {
		fmt.Printf(" --tail=%d", tail)
	}
	
	fmt.Println()
	
	return nil
}

// ToolboxStatus shows status of a toolbox
func (tm *ToolboxManager) ToolboxStatus(name, format string, watch bool) error {
	ctx := context.Background()

	// Get the toolbox
	toolbox := &crd.MCPToolbox{}
	err := tm.k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: tm.namespace,
	}, toolbox)

	if err != nil {
		return fmt.Errorf("toolbox %s not found: %w", name, err)
	}

	switch format {
	case "json":
		// TODO: Implement JSON output
		return fmt.Errorf("JSON format not yet implemented")
	case "yaml":
		// TODO: Implement YAML output
		return fmt.Errorf("YAML format not yet implemented")
	default:
		// Table format
		fmt.Printf("Toolbox: %s\n", name)
		fmt.Printf("Namespace: %s\n", tm.namespace)
		fmt.Printf("Template: %s\n", toolbox.Spec.Template)
		fmt.Printf("Phase: %s\n", toolbox.Status.Phase)
		fmt.Printf("Servers: %d total, %d ready\n", toolbox.Status.ServerCount, toolbox.Status.ReadyServers)
		
		if len(toolbox.Status.ServerStatuses) > 0 {
			fmt.Println("\nServer Status:")
			fmt.Printf("%-20s %-10s %-8s %-15s\n", "NAME", "PHASE", "READY", "HEALTH")
			fmt.Println(strings.Repeat("-", 60))
			
			for serverName, status := range toolbox.Status.ServerStatuses {
				ready := "No"
				if status.Ready {
					ready = "Yes"
				}
				
				fmt.Printf("%-20s %-10s %-8s %-15s\n",
					serverName,
					status.Phase,
					ready,
					status.Health,
				)
			}
		}
		
		if len(toolbox.Status.Conditions) > 0 {
			fmt.Println("\nConditions:")
			for _, condition := range toolbox.Status.Conditions {
				fmt.Printf("  %s: %s (%s) - %s\n",
					condition.Type,
					condition.Status,
					condition.Reason,
					condition.Message,
				)
			}
		}
	}

	return nil
}

// ListToolboxTemplates lists available toolbox templates
func (tm *ToolboxManager) ListToolboxTemplates(format, template string) error {
	templates := map[string]struct {
		Description string
		Servers     []string
	}{
		"coding-assistant": {
			Description: "Complete coding assistant with filesystem, git, web search, and memory",
			Servers:     []string{"filesystem", "git", "web-search", "memory"},
		},
		"rag-stack": {
			Description: "Retrieval Augmented Generation stack with memory, search, and document processing",
			Servers:     []string{"memory", "web-search", "document-processor"},
		},
		"research-agent": {
			Description: "Research workflow with web search, memory, and document analysis",
			Servers:     []string{"web-search", "memory", "document-processor", "filesystem"},
		},
	}

	if template != "" {
		// Show specific template details
		if tmpl, exists := templates[template]; exists {
			fmt.Printf("Template: %s\n", template)
			fmt.Printf("Description: %s\n", tmpl.Description)
			fmt.Printf("Servers: %s\n", strings.Join(tmpl.Servers, ", "))
		} else {
			return fmt.Errorf("template %s not found", template)
		}
		return nil
	}

	switch format {
	case "json":
		// TODO: Implement JSON output
		return fmt.Errorf("JSON format not yet implemented")
	case "yaml":
		// TODO: Implement YAML output
		return fmt.Errorf("YAML format not yet implemented")
	default:
		// Table format
		fmt.Printf("%-20s %-50s %-30s\n", "TEMPLATE", "DESCRIPTION", "SERVERS")
		fmt.Println(strings.Repeat("-", 100))

		for name, tmpl := range templates {
			servers := strings.Join(tmpl.Servers, ", ")
			if len(servers) > 28 {
				servers = servers[:25] + "..."
			}

			description := tmpl.Description
			if len(description) > 48 {
				description = description[:45] + "..."
			}

			fmt.Printf("%-20s %-50s %-30s\n", name, description, servers)
		}
	}

	return nil
}

// waitForToolboxReady waits for a toolbox to be ready
func (tm *ToolboxManager) waitForToolboxReady(name string, timeout time.Duration) error {
	ctx := context.Background()
	
	if timeout == 0 {
		timeout = 5 * time.Minute // Default timeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for toolbox %s to be ready", name)
		case <-ticker.C:
			toolbox := &crd.MCPToolbox{}
			err := tm.k8sClient.Get(ctx, client.ObjectKey{
				Name:      name,
				Namespace: tm.namespace,
			}, toolbox)

			if err != nil {
				continue // Keep trying
			}

			if toolbox.Status.Phase == crd.MCPToolboxPhaseRunning {
				fmt.Printf("Toolbox %s is ready!\n", name)
				return nil
			}

			fmt.Printf("Toolbox %s status: %s (%d/%d servers ready)\n",
				name,
				toolbox.Status.Phase,
				toolbox.Status.ReadyServers,
				toolbox.Status.ServerCount,
			)
		}
	}
}