// internal/cmd/postgres.go
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
)

func NewPostgresCommand() *cobra.Command {
	var enable bool
	var disable bool

	cmd := &cobra.Command{
		Use:   "postgres",
		Short: "Manage the built-in PostgreSQL database service",
		Long: `Start, stop, enable, or disable the built-in PostgreSQL database service.
This is a shared PostgreSQL instance used by memory and task-scheduler services.

Features:
- PVC-backed persistent storage
- Automatic database initialization
- Health checks and monitoring
- Used by memory MCP for knowledge graph storage
- Used by task-scheduler for workflow execution history

Examples:
  matey postgres                  # Start postgres service
  matey postgres --enable         # Enable in config
  matey postgres --disable        # Disable service`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("file")
			namespace, _ := cmd.Flags().GetString("namespace")
			
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if enable {
				return enablePostgresService(configFile, cfg)
			}

			if disable {
				return disablePostgresService(configFile, cfg, namespace)
			}

			// Start the postgres service using Kubernetes
			return startK8sPostgresService(cfg, namespace)
		},
	}

	cmd.Flags().BoolVar(&enable, "enable", false, "Enable the postgres service in config")
	cmd.Flags().BoolVar(&disable, "disable", false, "Disable the postgres service")

	return cmd
}

func enablePostgresService(configFile string, cfg *config.ComposeConfig) error {
	fmt.Println("Enabling built-in PostgreSQL service...")

	// Create MCPPostgres resource instead of adding to servers config
	postgres := &crd.MCPPostgres{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-postgres",
			Namespace: "matey",
			Labels: map[string]string{
				"app.kubernetes.io/component":  "postgres",
				"app.kubernetes.io/managed-by": "matey",
				"app.kubernetes.io/name":       "postgres",
				"mcp.matey.ai/role":           "database",
			},
		},
		Spec: crd.MCPPostgresSpec{
			Database:         "matey",
			User:             "postgres",
			Password:         "password",
			Port:             5432,
			Version:          "15",
			StorageSize:      "10Gi",
			StorageClassName: "",
			Resources: &crd.ResourceRequirements{
				Requests: map[string]string{
					"cpu":    "100m",
					"memory": "256Mi",
				},
				Limits: map[string]string{
					"cpu":    "500m",
					"memory": "512Mi",
				},
			},
			SecurityContext: &crd.SecurityConfig{
				RunAsUser:          nil, // Allow postgres to run as root
				RunAsGroup:         nil,
				ReadOnlyRootFS:     false,
				AllowPrivilegedOps: false,
				TrustedImage:       true,
			},
		},
	}

	// Apply the MCPPostgres resource to Kubernetes
	kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	scheme := runtime.NewScheme()
	if err := crd.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add CRD scheme: %w", err)
	}

	client, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	ctx := context.Background()
	err = client.Create(ctx, postgres)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			fmt.Println("PostgreSQL service already exists, updating...")
			existing := &crd.MCPPostgres{}
			if err := client.Get(ctx, types.NamespacedName{Name: "matey-postgres", Namespace: "matey"}, existing); err != nil {
				return fmt.Errorf("failed to get existing postgres: %w", err)
			}
			existing.Spec = postgres.Spec
			if err := client.Update(ctx, existing); err != nil {
				return fmt.Errorf("failed to update postgres: %w", err)
			}
		} else {
			return fmt.Errorf("failed to create postgres resource: %w", err)
		}
	}

	fmt.Println("Built-in PostgreSQL service enabled as MCPPostgres resource.")
	return nil
}

// EnsurePostgresResource ensures the MCPPostgres resource exists without adding to servers config
func EnsurePostgresResource() error {
	// Create MCPPostgres resource if it doesn't exist
	postgres := &crd.MCPPostgres{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-postgres",
			Namespace: "matey",
			Labels: map[string]string{
				"app.kubernetes.io/component":  "postgres",
				"app.kubernetes.io/managed-by": "matey",
				"app.kubernetes.io/name":       "postgres",
				"mcp.matey.ai/role":           "database",
			},
		},
		Spec: crd.MCPPostgresSpec{
			Database:         "matey",
			User:             "postgres",
			Password:         "password",
			Port:             5432,
			Version:          "15",
			StorageSize:      "10Gi",
			StorageClassName: "",
			Resources: &crd.ResourceRequirements{
				Requests: map[string]string{
					"cpu":    "100m",
					"memory": "256Mi",
				},
				Limits: map[string]string{
					"cpu":    "500m",
					"memory": "512Mi",
				},
			},
			SecurityContext: &crd.SecurityConfig{
				RunAsUser:          nil, // Allow postgres to run as root
				RunAsGroup:         nil,
				ReadOnlyRootFS:     false,
				AllowPrivilegedOps: false,
				TrustedImage:       true,
			},
		},
	}

	// Apply the MCPPostgres resource to Kubernetes
	kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	scheme := runtime.NewScheme()
	if err := crd.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add CRD scheme: %w", err)
	}

	client, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	ctx := context.Background()
	err = client.Create(ctx, postgres)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create postgres resource: %w", err)
	}

	return nil
}

func disablePostgresService(configFile string, cfg *config.ComposeConfig, namespace string) error {
	fmt.Println("Disabling built-in PostgreSQL service...")

	// Create Kubernetes client
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	ctx := context.Background()

	// Delete MCPPostgres resource
	postgres := &crd.MCPPostgres{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-postgres",
			Namespace: namespace,
		},
	}
	if err := k8sClient.Delete(ctx, postgres); err != nil && client.IgnoreNotFound(err) != nil {
		fmt.Printf("Warning: failed to delete MCPPostgres resource: %v\n", err)
	}

	// Note: Keep PVC for data persistence - it will be managed by the controller

	// Remove from config
	if cfg.Servers != nil {
		delete(cfg.Servers, "matey-postgres")
	}
	if cfg.Volumes != nil {
		delete(cfg.Volumes, "matey-postgres-data")
	}

	fmt.Println("Built-in PostgreSQL service disabled.")
	return config.SaveConfig(configFile, cfg)
}

func startK8sPostgresService(cfg *config.ComposeConfig, namespace string) error {
	fmt.Println("Creating built-in PostgreSQL service...")
	fmt.Printf("Namespace: %s\n", namespace)
	
	// Create Kubernetes client
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	ctx := context.Background()

	// Create MCPPostgres resource
	postgres := &crd.MCPPostgres{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-postgres",
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "postgres",
				"app.kubernetes.io/instance":   "matey-postgres",
				"app.kubernetes.io/component":  "database",
				"app.kubernetes.io/managed-by": "matey",
				"mcp.matey.ai/role":           "database",
			},
		},
		Spec: crd.MCPPostgresSpec{
			Database:    "matey",
			User:        "postgres",
			Password:    "password",
			Port:        5432,
			StorageSize: "10Gi",
			Version:     "15-alpine",
			Replicas:    1,
			Resources: &crd.ResourceRequirements{
				Limits: crd.ResourceList{
					"cpu":    "1000m",
					"memory": "1Gi",
				},
				Requests: crd.ResourceList{
					"cpu":    "500m",
					"memory": "512Mi",
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, postgres); err != nil && client.IgnoreAlreadyExists(err) != nil {
		return fmt.Errorf("failed to create MCPPostgres resource: %w", err)
	}

	fmt.Println("Built-in PostgreSQL service created successfully")
	fmt.Printf("Service: matey-postgres.%s.svc.cluster.local:5432\n", namespace)
	fmt.Printf("Database: matey\n")
	fmt.Printf("User: postgres\n")
	fmt.Printf("Check status with: kubectl get mcppostgres matey-postgres -n %s\n", namespace)
	
	return nil
}