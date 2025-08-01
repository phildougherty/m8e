// internal/cmd/proxy.go
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	
	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/controllers"
	"github.com/phildougherty/m8e/internal/crd"
)

func NewProxyCommand() *cobra.Command {
	var port int
	var namespace string
	var apiKey string

	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Run a system MCP proxy server",
		Long: `Run a system proxy server that automatically discovers and routes to MCP services
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
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace to discover services in")
	cmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "API key for proxy authentication (optional)")

	return cmd
}

func runProxy(cmd *cobra.Command, port int, namespace, apiKey string) error {
	// Load configuration if available
	file, _ := cmd.Flags().GetString("file")
	var cfg *config.ComposeConfig
	
	if file != "" {
		var err error
		cfg, err = config.LoadConfig(file)
		if err != nil {
			fmt.Printf("Warning: Failed to load config file %s: %v\n", file, err)
			fmt.Println("Continuing with default configuration...")
			cfg = &config.ComposeConfig{}
		}
	} else {
		cfg = &config.ComposeConfig{}
	}

	// Get API key from environment if not provided
	if apiKey == "" {
		apiKey = os.Getenv("MCP_API_KEY")
	}

	fmt.Printf("Creating system MCP proxy...\n")
	fmt.Printf("Namespace: %s\n", namespace)
	fmt.Printf("Port: %d\n", port)
	if apiKey != "" {
		fmt.Printf("Authentication: Enabled\n")
	} else {
		fmt.Printf("Authentication: Disabled\n")
	}
	
	if cfg.OAuth != nil && cfg.OAuth.Enabled {
		fmt.Printf("OAuth: Enabled (issuer: %s)\n", cfg.OAuth.Issuer)
	}

	// Create MCPProxy resource 
	if err := createMCPProxyResource(namespace, port, apiKey, cfg); err != nil {
		return fmt.Errorf("failed to create MCPProxy resource: %w", err)
	}

	fmt.Printf("MCPProxy resource created successfully\n")
	fmt.Printf("Starting controller manager to deploy the proxy...\n")

	// Start controller manager to handle the MCPProxy resource
	controllerCfg := &config.ComposeConfig{} // Create minimal config for controller
	controllerManager, err := controllers.NewControllerManager(namespace, controllerCfg)
	if err != nil {
		return fmt.Errorf("failed to create controller manager: %w", err)
	}

	// Start the controller manager in a separate goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := controllerManager.Start(ctx); err != nil {
			fmt.Printf("Controller manager error: %v\n", err)
		}
	}()

	fmt.Printf("Controller manager started successfully\n")
	fmt.Printf("Proxy deployment in progress...\n")
	fmt.Printf("Check deployment status with: kubectl get mcpproxy -n %s\n", namespace)
	fmt.Printf("Press Ctrl+C to stop the controller manager\n")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Printf("\nShutting down controller manager...\n")
	cancel()
	if err := controllerManager.Stop(); err != nil {
		fmt.Printf("Warning: Controller manager stop returned error: %v\n", err)
	}

	// Give some time for graceful shutdown
	time.Sleep(2 * time.Second)
	fmt.Printf("Controller manager stopped\n")
	
	return nil
}

func createMCPProxyResource(namespace string, port int, apiKey string, cfg *config.ComposeConfig) error {
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
			OAuth:          buildOAuthConfig(cfg),
			ServiceAccount: "matey-controller",
			Ingress: &crd.IngressConfig{
				Enabled: true,
				Host:    getIngressHost(cfg),
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

func buildOAuthConfig(cfg *config.ComposeConfig) *crd.OAuthConfig {
	if cfg == nil || cfg.OAuth == nil || !cfg.OAuth.Enabled {
		return nil
	}
	
	oauth := &crd.OAuthConfig{
		Enabled: true,
		Issuer:  cfg.OAuth.Issuer,
	}
	
	// Convert endpoints
	if cfg.OAuth.Endpoints != (config.OAuthEndpoints{}) {
		oauth.Endpoints = crd.OAuthEndpoints{
			Authorization: cfg.OAuth.Endpoints.Authorization,
			Token:         cfg.OAuth.Endpoints.Token,
			UserInfo:      cfg.OAuth.Endpoints.UserInfo,  
			Revoke:        cfg.OAuth.Endpoints.Revoke,
			Discovery:     cfg.OAuth.Endpoints.Discovery,
		}
	}
	
	// Convert tokens
	if cfg.OAuth.Tokens != (config.TokenConfig{}) {
		oauth.Tokens = crd.TokenConfig{
			AccessTokenTTL:  cfg.OAuth.Tokens.AccessTokenTTL,
			RefreshTokenTTL: cfg.OAuth.Tokens.RefreshTokenTTL,
			CodeTTL:         cfg.OAuth.Tokens.CodeTTL,
			Algorithm:       cfg.OAuth.Tokens.Algorithm,
		}
	}
	
	// Convert security
	oauth.Security = crd.OAuthSecurityConfig{
		RequirePKCE: cfg.OAuth.Security.RequirePKCE,
	}
	
	// Convert arrays
	oauth.GrantTypes = cfg.OAuth.GrantTypes
	oauth.ResponseTypes = cfg.OAuth.ResponseTypes
	oauth.ScopesSupported = cfg.OAuth.ScopesSupported
	
	return oauth
}

// getIngressHost returns the ingress host from config or a localhost default
func getIngressHost(cfg *config.ComposeConfig) string {
	if cfg != nil && cfg.Proxy.URL != "" {
		// Extract host from proxy URL
		proxyURL := cfg.GetProxyURL()
		// Remove protocol prefix
		host := proxyURL
		if strings.HasPrefix(host, "https://") {
			host = strings.TrimPrefix(host, "https://")
		} else if strings.HasPrefix(host, "http://") {
			host = strings.TrimPrefix(host, "http://")
		}
		// Remove port if present
		if idx := strings.Index(host, ":"); idx != -1 {
			host = host[:idx]
		}
		return host
	}
	// Default to localhost for local development
	return "localhost"
}