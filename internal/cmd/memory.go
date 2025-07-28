// internal/cmd/memory.go
package cmd

import (
	"fmt"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/memory"

	"github.com/spf13/cobra"
)

func NewMemoryCommand() *cobra.Command {
	var enable bool
	var disable bool

	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage the postgres-backed memory MCP server",
		Long: `Start, stop, enable, or disable the postgres-backed memory MCP server using Kubernetes.
The memory server provides persistent knowledge graph storage with:
- PostgreSQL backend for reliability  
- Graph-based knowledge storage with 11 MCP tools
- Entity and relationship management
- Observation tracking
- Full-text search capabilities

Examples:
  matey memory                    # Start memory server via Kubernetes
  matey memory --enable           # Enable in config
  matey memory --disable          # Disable service`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("file")
			namespace, _ := cmd.Flags().GetString("namespace")
			
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if enable {
				return enableMemoryServer(configFile, cfg)
			}

			if disable {
				return disableMemoryServer(configFile, cfg, namespace)
			}

			// Check if memory is enabled in config
			if !cfg.Memory.Enabled {
				fmt.Println("Memory server is not enabled in configuration.")
				fmt.Println("Use --enable flag to enable it first.")
				return nil
			}

			// Start the memory server using Kubernetes
			return startK8sMemoryServer(cfg, namespace)
		},
	}

	cmd.Flags().BoolVar(&enable, "enable", false, "Enable the memory server in config")
	cmd.Flags().BoolVar(&disable, "disable", false, "Disable the memory server")

	return cmd
}

func enableMemoryServer(configFile string, cfg *config.ComposeConfig) error {
	fmt.Println("Enabling postgres-backed memory server...")

	// 1. Enable in the built-in memory section
	cfg.Memory.Enabled = true
	if cfg.Memory.Port == 0 {
		cfg.Memory.Port = 3001
	}
	if cfg.Memory.Host == "" {
		cfg.Memory.Host = "0.0.0.0"
	}
	if cfg.Memory.DatabaseURL == "" {
		cfg.Memory.DatabaseURL = "postgresql://postgres:password@matey-postgres.matey.svc.cluster.local:5432/memory_graph?sslmode=disable"
	}
	// Use shared matey-postgres by default, don't create separate postgres instance
	cfg.Memory.PostgresEnabled = false
	if cfg.Memory.PostgresPort == 0 {
		cfg.Memory.PostgresPort = 5432
	}
	if cfg.Memory.PostgresDB == "" {
		cfg.Memory.PostgresDB = "memory_graph"
	}
	if cfg.Memory.PostgresUser == "" {
		cfg.Memory.PostgresUser = "postgres"
	}
	if cfg.Memory.PostgresPassword == "" {
		cfg.Memory.PostgresPassword = "password"
	}
	if cfg.Memory.CPUs == "" {
		cfg.Memory.CPUs = "1.0"
	}
	if cfg.Memory.Memory == "" {
		cfg.Memory.Memory = "1g"
	}
	if cfg.Memory.PostgresCPUs == "" {
		cfg.Memory.PostgresCPUs = "2.0"
	}
	if cfg.Memory.PostgresMemory == "" {
		cfg.Memory.PostgresMemory = "2g"
	}
	if len(cfg.Memory.Volumes) == 0 {
		cfg.Memory.Volumes = []string{"postgres-memory-data:/var/lib/postgresql/data"}
	}
	if cfg.Memory.Authentication == nil {
		allowAPIKey := true
		cfg.Memory.Authentication = &config.ServerAuthConfig{
			Enabled:       true,
			RequiredScope: "mcp:tools",
			OptionalAuth:  false,
			AllowAPIKey:   &allowAPIKey,
		}
	}

	// 2. ALSO add to servers section for proxy discovery
	if cfg.Servers == nil {
		cfg.Servers = make(map[string]config.ServerConfig)
	}

	allowAPIKey := true

	// Add memory server to servers config (so proxy can find it)
	cfg.Servers["memory"] = config.ServerConfig{
		Build: config.BuildConfig{
			Context:    "github.com/phildougherty/m8e-memory.git",
			Dockerfile: "Dockerfile",
		},
		Command:      "./matey-memory",
		Args:         []string{"--host", "0.0.0.0", "--port", "3001"},
		Protocol:     "http",
		HttpPort:     constants.DefaultMemoryHTTPPort,
		User:         "root",
		ReadOnly:     false,
		Privileged:   false,
		SecurityOpt:  []string{"no-new-privileges:true"},
		Capabilities: []string{"tools", "resources"},
		Env: map[string]string{
			"NODE_ENV":     "production",
			"DATABASE_URL": cfg.Memory.DatabaseURL,
		},
		Networks: []string{"mcp-net"},
		Authentication: &config.ServerAuthConfig{
			Enabled:       true,
			RequiredScope: "mcp:tools",
			OptionalAuth:  false,
			AllowAPIKey:   &allowAPIKey,
		},
		// DependsOn removed - postgres should be managed as MCPPostgres resource
	}

	// Ensure postgres resource exists (don't add to servers config)
	if err := EnsurePostgresResource(); err != nil {
		fmt.Printf("Warning: Failed to ensure postgres resource: %v\n", err)
	}

	fmt.Printf("Memory server enabled in both built-in config and servers list (port: %d).\n", cfg.Memory.Port)

	return config.SaveConfig(configFile, cfg)
}

func disableMemoryServer(configFile string, cfg *config.ComposeConfig, namespace string) error {
	fmt.Println("Disabling memory server...")

	// Create Kubernetes client
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Stop the Kubernetes resources
	memoryManager := memory.NewK8sManager(cfg, k8sClient, namespace)
	if err := memoryManager.Stop(); err != nil {
		fmt.Printf("Warning: %v\n", err)
	}

	// Disable in config
	cfg.Memory.Enabled = false

	fmt.Println("Memory server disabled.")

	return config.SaveConfig(configFile, cfg)
}


// startK8sMemoryServer starts the memory server using Kubernetes
func startK8sMemoryServer(cfg *config.ComposeConfig, namespace string) error {
	fmt.Println("Creating MCP memory server...")
	fmt.Printf("Namespace: %s\n", namespace)
	
	// Create Kubernetes client
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create memory manager and start (non-blocking)
	memoryManager := memory.NewK8sManager(cfg, k8sClient, namespace)
	if err := memoryManager.Start(); err != nil {
		return fmt.Errorf("failed to create MCPMemory resource: %w", err)
	}

	fmt.Println("MCPMemory resource created successfully")
	fmt.Println("The controller will deploy the memory service automatically")
	fmt.Printf("Check deployment status with: kubectl get mcpmemory -n %s\n", namespace)
	
	return nil
}

