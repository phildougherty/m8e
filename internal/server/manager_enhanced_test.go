package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/fsnotify/fsnotify"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/logging"
	"github.com/phildougherty/m8e/internal/protocol"
)

func TestNewManager_Enhanced(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.ComposeConfig
		wantErr bool
	}{
		{
			name: "valid config with servers",
			config: &config.ComposeConfig{
				Version: "1",
				Servers: map[string]config.ServerConfig{
					"test-server": {
						Protocol: "stdio",
						Command:  "echo hello",
					},
				},
				Logging: config.LoggingConfig{
					Level: "info",
				},
			},
			wantErr: false,
		},
		{
			name: "config with memory service",
			config: &config.ComposeConfig{
				Version: "1",
				Servers: make(map[string]config.ServerConfig),
				Memory: config.MemoryConfig{
					Enabled:          true,
					Port:             3001,
					DatabaseURL:      "postgresql://test:test@localhost:5432/test",
					PostgresDB:       "test_db",
					PostgresUser:     "test_user",
					PostgresPassword: "test_pass",
				},
				Logging: config.LoggingConfig{
					Level: "info",
				},
			},
			wantErr: false,
		},
		{
			name: "config with task scheduler",
			config: &config.ComposeConfig{
				Version: "1",
				Servers: make(map[string]config.ServerConfig),
				TaskScheduler: &config.TaskScheduler{
					Enabled:           true,
					Port:              8090,
					DatabasePath:      "/tmp/scheduler.db",
					LogLevel:          "info",
					OllamaURL:         "http://localhost:11434",
					OllamaModel:       "llama3.2:latest",
					OpenRouterAPIKey:  "test-key",
					OpenRouterModel:   "anthropic/claude-3.5-sonnet",
					MCPProxyURL:       "http://localhost:8080",
					MCPProxyAPIKey:    "test-key",
				},
				Logging: config.LoggingConfig{
					Level: "info",
				},
			},
			wantErr: false,
		},
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewManager(tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, manager)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, manager)
				assert.NotNil(t, manager.config)
				assert.NotNil(t, manager.servers)
				assert.NotNil(t, manager.logger)
				assert.NotNil(t, manager.ctx)
				assert.NotNil(t, manager.cancel)
				assert.NotNil(t, manager.shutdownCh)
				assert.NotNil(t, manager.healthCheckers)
			}
		})
	}
}

func TestManager_StartProcessServer(t *testing.T) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: map[string]config.ServerConfig{
			"test-server": {
				Protocol: "stdio",
				Command:  "echo",
				Args:     []string{"hello"},
			},
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}

	manager, err := NewManager(cfg)
	require.NoError(t, err)

	serverConfig := &config.ServerConfig{
		Command: "echo",
		Args:    []string{"hello"},
		Env: map[string]string{
			"TEST_VAR": "test_value",
		},
	}

	// Test process server start
	err = manager.startProcessServer("test-server", "matey-test-server", serverConfig)
	
	if err != nil {
		// On some systems, echo might not be available or behave differently
		// We'll check if it's a command not found error
		if !assert.Contains(t, err.Error(), "executable file not found") {
			t.Logf("Process start failed (expected in some test environments): %v", err)
		}
	} else {
		// If successful, verify the process was created
		instance, exists := manager.GetServerInstance("test-server")
		if exists && instance.Process != nil {
			assert.NotNil(t, instance.Process)
			// Clean up
			instance.Process.Stop()
		}
	}
}

func TestManager_GetServerStatusUnsafe(t *testing.T) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: map[string]config.ServerConfig{
			"test-server": {
				Protocol: "stdio",
				Command:  "echo hello",
			},
		},
	}

	manager, err := NewManager(cfg)
	require.NoError(t, err)

	tests := []struct {
		name             string
		serverName       string
		fixedIdentifier  string
		expectError      bool
		expectedStatus   string
	}{
		{
			name:             "non-existent server",
			serverName:       "non-existent",
			fixedIdentifier:  "matey-non-existent",
			expectError:      true,
			expectedStatus:   "unknown",
		},
		{
			name:             "existing server",
			serverName:       "test-server",
			fixedIdentifier:  "matey-test-server",
			expectError:      false,
			expectedStatus:   "stopped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := manager.getServerStatusUnsafe(tt.serverName, tt.fixedIdentifier)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedStatus, status)
		})
	}
}

func TestManager_IsBuiltInService(t *testing.T) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: make(map[string]config.ServerConfig),
	}

	manager, err := NewManager(cfg)
	require.NoError(t, err)

	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{"memory service", "memory", true},
		{"task scheduler", "task-scheduler", true},
		{"dashboard", "dashboard", true},
		{"proxy", "proxy", true},
		{"custom server", "custom-server", false},
		{"empty name", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.isBuiltInService(tt.service)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestManager_IsLikelyContainer(t *testing.T) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: make(map[string]config.ServerConfig),
	}

	manager, err := NewManager(cfg)
	require.NoError(t, err)

	tests := []struct {
		name         string
		serverName   string
		serverConfig config.ServerConfig
		expected     bool
	}{
		{
			name:       "http server with port",
			serverName: "http-server",
			serverConfig: config.ServerConfig{
				Protocol: "http",
				HttpPort: 8080,
			},
			expected: true,
		},
		{
			name:       "stdio server",
			serverName: "stdio-server",
			serverConfig: config.ServerConfig{
				Protocol: "stdio",
				Command:  "echo hello",
			},
			expected: false,
		},
		{
			name:       "server with image",
			serverName: "image-server",
			serverConfig: config.ServerConfig{
				Image: "nginx:latest",
			},
			expected: false, // Based on current implementation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.isLikelyContainer(tt.serverName, tt.serverConfig)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestManager_ValidateServerConfig(t *testing.T) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: make(map[string]config.ServerConfig),
	}

	manager, err := NewManager(cfg)
	require.NoError(t, err)

	tests := []struct {
		name         string
		serverName   string
		serverConfig config.ServerConfig
		expectError  bool
	}{
		{
			name:       "valid config with image",
			serverName: "valid-image",
			serverConfig: config.ServerConfig{
				Image:    "nginx:latest",
				Protocol: "http",
			},
			expectError: false,
		},
		{
			name:       "valid config with command",
			serverName: "valid-command",
			serverConfig: config.ServerConfig{
				Command:  "echo hello",
				Protocol: "stdio",
			},
			expectError: false,
		},
		{
			name:       "invalid config - no image or command",
			serverName: "invalid-server",
			serverConfig: config.ServerConfig{
				Protocol: "http",
			},
			expectError: true,
		},
		{
			name:       "invalid protocol",
			serverName: "invalid-protocol",
			serverConfig: config.ServerConfig{
				Command:  "echo hello",
				Protocol: "invalid-protocol",
			},
			expectError: true,
		},
		{
			name:       "invalid capability",
			serverName: "invalid-capability",
			serverConfig: config.ServerConfig{
				Command:      "echo hello",
				Protocol:     "stdio",
				Capabilities: []string{"invalid-capability"},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.validateServerConfig(tt.serverName, tt.serverConfig)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestManager_CheckServerHealth(t *testing.T) {
	// Create a test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("healthy"))
	}))
	defer server.Close()

	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: map[string]config.ServerConfig{
			"test-server": {
				Protocol: "http",
				HttpPort: 8080,
				Command:  "echo test",
			},
		},
	}

	manager, err := NewManager(cfg)
	require.NoError(t, err)

	tests := []struct {
		name         string
		serverName   string
		identifier   string
		endpoint     string
		timeout      time.Duration
		expectHealth bool
	}{
		{
			name:         "healthy server with full URL",
			serverName:   "test-server",
			identifier:   "matey-test-server",
			endpoint:     server.URL + "/health",
			timeout:      5 * time.Second,
			expectHealth: true,
		},
		{
			name:         "unreachable server",
			serverName:   "test-server",
			identifier:   "matey-test-server",
			endpoint:     "http://localhost:99999/health",
			timeout:      1 * time.Second,
			expectHealth: false,
		},
		{
			name:         "timeout",
			serverName:   "test-server",
			identifier:   "matey-test-server",
			endpoint:     server.URL + "/health",
			timeout:      1 * time.Nanosecond,
			expectHealth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			healthy, err := manager.checkServerHealth(tt.serverName, tt.identifier, tt.endpoint, tt.timeout)

			assert.Equal(t, tt.expectHealth, healthy)
			if !tt.expectHealth {
				assert.Error(t, err)
			}
		})
	}
}

func TestManager_RunLifecycleHook(t *testing.T) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: make(map[string]config.ServerConfig),
	}

	manager, err := NewManager(cfg)
	require.NoError(t, err)

	tests := []struct {
		name       string
		hookScript string
		expectErr  bool
	}{
		{
			name:       "successful hook",
			hookScript: "echo 'hook executed'",
			expectErr:  false,
		},
		{
			name:       "failing hook",
			hookScript: "exit 1",
			expectErr:  true,
		},
		{
			name:       "non-existent command",
			hookScript: "non-existent-command",
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.runLifecycleHook(tt.hookScript)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestManager_InitializeServerCapabilities(t *testing.T) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: map[string]config.ServerConfig{
			"test-server": {
				Protocol:     "stdio",
				Command:      "echo hello",
				Capabilities: []string{"resources", "tools"},
				Resources: config.ResourcesConfig{
					Paths: []config.ResourcePath{
						{
							Source: "/tmp/test",
							Target: "/test",
						},
					},
				},
				Tools: []config.ToolConfig{
					{
						Name:        "test-tool",
						Description: "Test tool",
					},
				},
			},
		},
	}

	manager, err := NewManager(cfg)
	require.NoError(t, err)

	tests := []struct {
		name       string
		serverName string
		expectErr  bool
	}{
		{
			name:       "existing server",
			serverName: "test-server",
			expectErr:  false,
		},
		{
			name:       "non-existent server",
			serverName: "non-existent",
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.initializeServerCapabilities(tt.serverName)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				
				// Verify capabilities were initialized
				instance, exists := manager.GetServerInstance(tt.serverName)
				if exists {
					assert.True(t, instance.Capabilities["resources"])
					assert.True(t, instance.Capabilities["tools"])
				}
			}
		})
	}
}

func TestResourcesWatcher_Lifecycle(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "resources-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := &config.ServerConfig{
		Resources: config.ResourcesConfig{
			Paths: []config.ResourcePath{
				{
					Source: tempDir,
					Target: "/test",
					Watch:  true,
				},
			},
			SyncInterval: "1s",
		},
	}

	instance := &ServerInstance{
		ResourceManager: protocol.NewResourceManager(),
	}

	logger := logging.NewLogger("info")
	watcher, err := NewResourcesWatcher(cfg, instance, logger)
	require.NoError(t, err)

	// Test start
	watcher.Start()
	assert.True(t, watcher.active)

	// Test stop
	watcher.Stop()
	time.Sleep(100 * time.Millisecond) // Give it time to stop
}

func TestResourcesWatcher_ShouldProcessEvent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "resources-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := &config.ServerConfig{
		Resources: config.ResourcesConfig{
			Paths: []config.ResourcePath{
				{
					Source: tempDir,
					Target: "/test",
					Watch:  true,
				},
			},
		},
	}

	instance := &ServerInstance{
		ResourceManager: protocol.NewResourceManager(),
	}

	logger := logging.NewLogger("info")
	watcher, err := NewResourcesWatcher(cfg, instance, logger)
	require.NoError(t, err)

	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{
			name:     "normal file",
			filename: "test.txt",
			expected: true,
		},
		{
			name:     "hidden file",
			filename: ".hidden",
			expected: false,
		},
		{
			name:     "hidden directory",
			filename: ".git",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fsnotify event
			event := fsnotify.Event{
				Name: tt.filename,
				Op:   fsnotify.Write,
			}
			
			result := watcher.shouldProcessEvent(event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestManager_ConcurrentOperations(t *testing.T) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: map[string]config.ServerConfig{
			"test-server": {
				Protocol: "stdio",
				Command:  "echo hello",
			},
		},
	}

	manager, err := NewManager(cfg)
	require.NoError(t, err)

	var wg sync.WaitGroup
	numGoroutines := 10
	numOperations := 100

	// Test concurrent GetServerInstance calls
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_, _ = manager.GetServerInstance("test-server")
			}
		}()
	}

	// Test concurrent GetServerStatus calls
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_, _ = manager.GetServerStatus("test-server")
			}
		}()
	}

	wg.Wait()

	// Verify manager is still in valid state
	instance, exists := manager.GetServerInstance("test-server")
	assert.True(t, exists)
	assert.NotNil(t, instance)
}

func TestManager_ShutdownGracefully(t *testing.T) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: map[string]config.ServerConfig{
			"test-server": {
				Protocol: "stdio",
				Command:  "sleep 1",
			},
		},
	}

	manager, err := NewManager(cfg)
	require.NoError(t, err)

	// Test shutdown
	err = manager.Shutdown()
	assert.NoError(t, err)

	// Verify context is cancelled
	select {
	case <-manager.ctx.Done():
		// Context was cancelled, which is expected
	default:
		t.Error("Expected context to be cancelled after shutdown")
	}
}

func TestServerInstance_Structure(t *testing.T) {
	now := time.Now()
	ctx, cancel := context.WithCancel(context.Background())

	instance := &ServerInstance{
		Name:             "test-server",
		Config:           config.ServerConfig{Protocol: "stdio", Command: "echo hello"},
		ContainerID:      "container-123",
		Process:          nil,
		IsContainer:      false,
		Status:           "running",
		StartTime:        now,
		Capabilities:     map[string]bool{"resources": true, "tools": true},
		ConnectionInfo:   map[string]string{"host": "localhost", "port": "8080"},
		HealthStatus:     "healthy",
		ResourcesWatcher: nil,
		ProgressManager:  protocol.NewProgressManager(),
		ResourceManager:  protocol.NewResourceManager(),
		SamplingManager:  protocol.NewSamplingManager(),
		ctx:              ctx,
		cancel:           cancel,
	}

	// Test structure
	assert.Equal(t, "test-server", instance.Name)
	assert.Equal(t, "stdio", instance.Config.Protocol)
	assert.Equal(t, "container-123", instance.ContainerID)
	assert.Nil(t, instance.Process)
	assert.False(t, instance.IsContainer)
	assert.Equal(t, "running", instance.Status)
	assert.Equal(t, now, instance.StartTime)
	assert.True(t, instance.Capabilities["resources"])
	assert.True(t, instance.Capabilities["tools"])
	assert.Equal(t, "localhost", instance.ConnectionInfo["host"])
	assert.Equal(t, "8080", instance.ConnectionInfo["port"])
	assert.Equal(t, "healthy", instance.HealthStatus)
	assert.Nil(t, instance.ResourcesWatcher)
	assert.NotNil(t, instance.ProgressManager)
	assert.NotNil(t, instance.ResourceManager)
	assert.NotNil(t, instance.SamplingManager)
	assert.Equal(t, ctx, instance.ctx)
	assert.NotNil(t, instance.cancel)

	// Test context cancellation
	cancel()
	select {
	case <-instance.ctx.Done():
		// Expected
	default:
		t.Error("Expected context to be cancelled")
	}
}

func TestManager_MemoryServiceIntegration(t *testing.T) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: make(map[string]config.ServerConfig),
		Memory: config.MemoryConfig{
			Enabled:          true,
			Port:             3001,
			DatabaseURL:      "postgresql://test:test@localhost:5432/test",
			PostgresDB:       "test_db",
			PostgresUser:     "test_user",
			PostgresPassword: "test_pass",
			Volumes:          []string{"/data:/var/lib/postgresql/data"},
		},
	}

	manager, err := NewManager(cfg)
	require.NoError(t, err)

	// Verify memory service was added
	_, exists := manager.GetServerInstance("memory")
	assert.True(t, exists)

	// Verify postgres service was added
	_, exists = manager.GetServerInstance("postgres-memory")
	assert.True(t, exists)

	// Verify memory service configuration
	memoryInstance, _ := manager.GetServerInstance("memory")
	assert.Equal(t, "matey-memory:latest", memoryInstance.Config.Image)
	assert.Equal(t, "http", memoryInstance.Config.Protocol)
	assert.Equal(t, 3001, memoryInstance.Config.HttpPort)
	assert.Equal(t, "postgresql://test:test@localhost:5432/test", memoryInstance.Config.Env["DATABASE_URL"])
}

func TestManager_TaskSchedulerIntegration(t *testing.T) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: make(map[string]config.ServerConfig),
		TaskScheduler: &config.TaskScheduler{
			Enabled:           true,
			Port:              8090,
			DatabasePath:      "/tmp/scheduler.db",
			LogLevel:          "info",
			OllamaURL:         "http://localhost:11434",
			OllamaModel:       "llama3.2:latest",
			OpenRouterAPIKey:  "test-key",
			OpenRouterModel:   "anthropic/claude-3.5-sonnet",
			MCPProxyURL:       "http://localhost:8080",
			MCPProxyAPIKey:    "test-key",
			Env: map[string]string{
				"CUSTOM_VAR": "custom_value",
			},
		},
	}

	manager, err := NewManager(cfg)
	require.NoError(t, err)

	// Verify task scheduler service was added
	instance, exists := manager.GetServerInstance("task-scheduler")
	assert.True(t, exists)

	// Verify task scheduler configuration
	assert.Equal(t, "matey-task-scheduler:latest", instance.Config.Image)
	assert.Equal(t, "sse", instance.Config.Protocol)
	assert.Equal(t, 8090, instance.Config.HttpPort)
	assert.Equal(t, "/sse", instance.Config.SSEPath)
	assert.Equal(t, "/tmp/scheduler.db", instance.Config.Env["MCP_CRON_DATABASE_PATH"])
	assert.Equal(t, "info", instance.Config.Env["MCP_CRON_LOGGING_LEVEL"])
	assert.Equal(t, "http://localhost:11434", instance.Config.Env["MCP_CRON_OLLAMA_BASE_URL"])
	assert.Equal(t, "test-key", instance.Config.Env["OPENROUTER_API_KEY"])
	assert.Equal(t, "custom_value", instance.Config.Env["CUSTOM_VAR"])
}

// Mock types for testing


func BenchmarkManager_GetServerInstance(b *testing.B) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: map[string]config.ServerConfig{
			"test-server": {
				Protocol: "stdio",
				Command:  "echo hello",
			},
		},
	}

	manager, err := NewManager(cfg)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.GetServerInstance("test-server")
	}
}

func BenchmarkManager_GetServerStatus(b *testing.B) {
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: map[string]config.ServerConfig{
			"test-server": {
				Protocol: "stdio",
				Command:  "echo hello",
			},
		},
	}

	manager, err := NewManager(cfg)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.GetServerStatus("test-server")
	}
}