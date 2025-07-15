// internal/cmd/proxy.go
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	
	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
)

func NewProxyCommand() *cobra.Command {
	var port int
	var namespace string
	var apiKey string

	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Run a Kubernetes-native MCP proxy server",
		Long: `Run a Kubernetes-native proxy server that automatically discovers and routes to MCP services
using Kubernetes service discovery. The proxy uses cluster DNS and Kubernetes APIs to find and
connect to MCP servers without requiring static configuration.

Key features:
- Automatic service discovery via Kubernetes labels
- Dynamic connection management
- Real-time service updates
- Support for HTTP, SSE, and STDIO protocols (where supported)
- Built-in health checking and retry logic`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProxy(cmd, port, namespace, apiKey)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 9876, "Port to run the proxy server on")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace to discover services in")
	cmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "API key for proxy authentication (optional)")

	return cmd
}

func runProxy(cmd *cobra.Command, port int, namespace, apiKey string) error {
	// Load configuration if available
	file, _ := cmd.Flags().GetString("file")
	
	if file != "" {
		_, err := config.LoadConfig(file)
		if err != nil {
			fmt.Printf("Warning: Failed to load config file %s: %v\n", file, err)
			fmt.Println("Continuing with default configuration...")
		}
	}

	// Get API key from environment if not provided
	if apiKey == "" {
		apiKey = os.Getenv("MCP_API_KEY")
	}

	fmt.Printf("Creating Kubernetes-native MCP proxy...\n")
	fmt.Printf("Namespace: %s\n", namespace)
	fmt.Printf("Port: %d\n", port)
	if apiKey != "" {
		fmt.Printf("Authentication: Enabled\n")
	} else {
		fmt.Printf("Authentication: Disabled\n")
	}

	// Create MCPProxy resource instead of running directly
	if err := createMCPProxyResource(namespace, port, apiKey); err != nil {
		return fmt.Errorf("failed to create MCPProxy resource: %w", err)
	}

	fmt.Printf("MCPProxy resource created successfully\n")
	fmt.Printf("The controller will deploy the proxy automatically\n")
	fmt.Printf("Check deployment status with: kubectl get mcpproxy -n %s\n", namespace)
	return nil
}

func createMCPProxyResource(namespace string, port int, apiKey string) error {
	// Create Kubernetes client
	config, err := createK8sConfig()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes config: %w", err)
	}

	// Create scheme with our CRDs
	scheme := runtime.NewScheme()
	if err := crd.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add CRD scheme: %w", err)
	}

	// Create client
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create MCPProxy resource
	replicas := int32(1)
	mcpProxy := &crd.MCPProxy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-proxy",
			Namespace: namespace,
			Labels: map[string]string{
				"app":                        "matey",
				"app.kubernetes.io/name":     "matey",
				"app.kubernetes.io/instance": "matey-proxy",
			},
		},
		Spec: crd.MCPProxySpec{
			Port:        int32(port),
			Replicas:    &replicas,
			ServiceType: "NodePort",
			Auth: &crd.ProxyAuthConfig{
				Enabled: apiKey != "",
				APIKey:  apiKey,
			},
			ServiceAccount: "matey-controller",
			Ingress: &crd.IngressConfig{
				Enabled: true,
				Host:    "mcp.robotrad.io",
				Annotations: map[string]string{
					"nginx.ingress.kubernetes.io/rewrite-target":       "/",
					"nginx.ingress.kubernetes.io/cors-allow-origin":    "*",
					"nginx.ingress.kubernetes.io/cors-allow-methods":   "GET, POST, OPTIONS",
					"nginx.ingress.kubernetes.io/cors-allow-headers":   "Authorization, Content-Type",
				},
			},
		},
	}

	// Create the resource
	ctx := context.Background()
	err = k8sClient.Create(ctx, mcpProxy)
	if err != nil {
		// Check if it already exists
		if errors.IsAlreadyExists(err) {
			existing := &crd.MCPProxy{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(mcpProxy), existing); err != nil {
				return fmt.Errorf("failed to get existing MCPProxy: %w", err)
			}
			existing.Spec = mcpProxy.Spec
			if err := k8sClient.Update(ctx, existing); err != nil {
				return fmt.Errorf("failed to update MCPProxy: %w", err)
			}
			fmt.Printf("Updated existing MCPProxy resource\n")
			return nil
		}
		return fmt.Errorf("failed to create MCPProxy: %w", err)
	}

	return nil
}


