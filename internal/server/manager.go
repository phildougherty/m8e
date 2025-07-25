package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs" // Keep for filepath.Walk, os.Stat etc.
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/logging"
	"github.com/phildougherty/m8e/internal/protocol"
	"github.com/phildougherty/m8e/internal/runtime"

	"github.com/fsnotify/fsnotify" // Keep if ResourcesWatcher uses it
)

// ServerInstance represents a running server instance
type ServerInstance struct {
	Name             string
	Config           config.ServerConfig
	ContainerID      string
	Process          *runtime.Process
	IsContainer      bool
	Status           string
	StartTime        time.Time
	Capabilities     map[string]bool
	ConnectionInfo   map[string]string
	HealthStatus     string
	ResourcesWatcher *ResourcesWatcher
	ProgressManager  *protocol.ProgressManager
	ResourceManager  *protocol.ResourceManager
	SamplingManager  *protocol.SamplingManager
	mu               sync.RWMutex
	ctx              context.Context
	cancel           context.CancelFunc
}

// Manager handles server lifecycle operations
type Manager struct {
	config           *config.ComposeConfig
	projectDir       string // For running lifecycle hooks and resolving relative paths
	servers          map[string]*ServerInstance
	// networks field removed - networking is handled by Kubernetes
	logger           *logging.Logger
	mu               sync.RWMutex
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	shutdownCh       chan struct{}
	healthCheckers   map[string]context.CancelFunc
	healthCheckMu    sync.Mutex
}

func NewManager(cfg *config.ComposeConfig) (*Manager, error) {
	if cfg == nil {

		return nil, fmt.Errorf("config cannot be nil")
	}

	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}

	logLevel := "info"
	if cfg.Logging.Level != "" {
		logLevel = cfg.Logging.Level
	}

	logger := logging.NewLogger(logLevel)

	// Create a temporary manager with logger for validation
	tempManager := &Manager{logger: logger}

	// Add task-scheduler as a built-in service if enabled
	if cfg.TaskScheduler != nil && cfg.TaskScheduler.Enabled {
		logger.Info("Task scheduler enabled in config, adding as built-in server")

		// Create task-scheduler server config
		taskSchedulerConfig := config.ServerConfig{
			// CRITICAL: Add image so validation passes
			Image:        "matey-task-scheduler:latest",
			Protocol:     "sse",
			HttpPort:     cfg.TaskScheduler.Port,
			SSEPath:      "/sse",
			User:         "root",
			ReadOnly:     false,
			Privileged:   false,
			Capabilities: []string{"tools", "resources"},
			Env: map[string]string{
				"TZ":                                 "America/New_York",
				"MCP_CRON_SERVER_TRANSPORT":          "sse",
				"MCP_CRON_SERVER_ADDRESS":            "0.0.0.0",
				"MCP_CRON_SERVER_PORT":               fmt.Sprintf("%d", cfg.TaskScheduler.Port),
				"MCP_CRON_DATABASE_PATH":             cfg.TaskScheduler.DatabasePath,
				"MCP_CRON_DATABASE_ENABLED":          "true",
				"MCP_CRON_LOGGING_LEVEL":             cfg.TaskScheduler.LogLevel,
				"MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT": "10m",
				"MCP_CRON_OLLAMA_ENABLED":            "true",
				"MCP_CRON_OLLAMA_BASE_URL":           cfg.TaskScheduler.OllamaURL,
				"MCP_CRON_OLLAMA_DEFAULT_MODEL":      cfg.TaskScheduler.OllamaModel,
				"USE_OPENROUTER":                     "true",
				"OPENROUTER_ENABLED":                 "true",
				"OPENROUTER_API_KEY":                 cfg.TaskScheduler.OpenRouterAPIKey,
				"OPENROUTER_MODEL":                   cfg.TaskScheduler.OpenRouterModel,
				"MCP_PROXY_URL":                      cfg.TaskScheduler.MCPProxyURL,
				"MCP_PROXY_API_KEY":                  cfg.TaskScheduler.MCPProxyAPIKey,
				"MCP_MEMORY_SERVER_URL":              "http://matey-memory:3001",
				"MCP_FILESYSTEM_URL":                 "http://matey-filesystem:3000",
				"MCP_OPENROUTER_GATEWAY_URL":         "http://matey-openrouter-gateway:8012",
			},
			// Networks handled by Kubernetes
			Authentication: &config.ServerAuthConfig{
				Enabled:       true,
				RequiredScope: "mcp:tools",
				OptionalAuth:  false,
				AllowAPIKey:   &[]bool{true}[0],
			},
			// Add volumes if specified in task scheduler config
			Volumes: cfg.TaskScheduler.Volumes,
		}

		// Merge any additional env vars from task scheduler config
		if cfg.TaskScheduler.Env != nil {
			for k, v := range cfg.TaskScheduler.Env {
				taskSchedulerConfig.Env[k] = v
			}
		}

		// Add to servers map
		if cfg.Servers == nil {
			cfg.Servers = make(map[string]config.ServerConfig)
		}
		cfg.Servers["task-scheduler"] = taskSchedulerConfig

		logger.Info("Added task-scheduler as built-in server on port %d", cfg.TaskScheduler.Port)
	}

	if cfg.Memory.Enabled {
		logger.Info("Memory server enabled in config, adding as built-in server")

		// Create memory server config
		memoryConfig := config.ServerConfig{
			// Use the built image name that will be created by the memory manager
			Image:        "matey-memory:latest",
			Protocol:     "http",
			HttpPort:     cfg.Memory.Port,
			User:         "root",
			ReadOnly:     false,
			Privileged:   false,
			Capabilities: []string{"tools", "resources"},
			Env: map[string]string{
				"NODE_ENV":     "production",
				"DATABASE_URL": cfg.Memory.DatabaseURL,
			},
			// Networks handled by Kubernetes
			Authentication: cfg.Memory.Authentication,
			// DependsOn removed - postgres should be managed as MCPPostgres resource
		}

		// postgres-memory config removed - postgres should be managed as MCPPostgres resource

		// Add to servers map
		if cfg.Servers == nil {
			cfg.Servers = make(map[string]config.ServerConfig)
		}
		cfg.Servers["memory"] = memoryConfig
		// postgres-memory server removed - postgres should be managed as MCPPostgres resource

		logger.Info("Added memory as built-in server on port %d", cfg.Memory.Port)
	}

	// Validate each server configuration using our method
	for name, serverCfg := range cfg.Servers {
		if err := tempManager.validateServerConfig(name, serverCfg); err != nil {

			return nil, fmt.Errorf("invalid server configuration: %w", err)
		}
	}

	// CREATE CONTEXT AND CANCEL FUNCTION
	ctx, cancel := context.WithCancel(context.Background())

	manager := &Manager{
		config:           cfg,
		projectDir:       wd,
		servers:          make(map[string]*ServerInstance),
		logger:           logger,
		ctx:              ctx,
		cancel:           cancel,
		shutdownCh:       make(chan struct{}),
		healthCheckers:   make(map[string]context.CancelFunc),
	}

	// Initialize server instances
	for name, serverCfg := range cfg.Servers {
		instanceCtx, instanceCancel := context.WithCancel(ctx)

		// INITIALIZE PROTOCOL MANAGERS
		progressManager := protocol.NewProgressManager()
		resourceManager := protocol.NewResourceManager()
		samplingManager := protocol.NewSamplingManager()

		// Register default text transformer
		resourceManager.RegisterTransformer("default", &protocol.DefaultTextTransformer{})

		manager.servers[name] = &ServerInstance{
			Name:            name,
			Config:          serverCfg,
			IsContainer:     serverCfg.Image != "" || serverCfg.Runtime != "" || manager.isLikelyContainer(name, serverCfg),
			Status:          "stopped",
			Capabilities:    make(map[string]bool),
			ConnectionInfo:  make(map[string]string),
			HealthStatus:    "unknown",
			ProgressManager: progressManager,
			ResourceManager: resourceManager,
			SamplingManager: samplingManager,
			ctx:             instanceCtx,
			cancel:          instanceCancel,
		}

		logger.Info("Initialized server instance '%s' (container: %t)", name, manager.servers[name].IsContainer)
	}

	logger.Info("Manager initialized with %d servers", len(manager.servers))

	return manager, nil
}

func (m *Manager) StartServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("MANAGER: StartServer called for '%s'", name)

	instance, ok := m.servers[name]
	if !ok {
		m.logger.Error("MANAGER: Server '%s' not found in configuration during StartServer", name)

		return fmt.Errorf("server '%s' not found in configuration", name)
	}

	srvCfg := instance.Config
	fixedIdentifier := fmt.Sprintf("matey-%s", name)
	m.logger.Info("MANAGER: Determined fixedIdentifier for '%s' as '%s'", name, fixedIdentifier)

	// Check current status
	m.logger.Info("MANAGER: Checking current status for '%s' (identifier: %s)...", name, fixedIdentifier)
	currentStatus, statusErr := m.getServerStatusUnsafe(name, fixedIdentifier)
	if statusErr != nil {
		m.logger.Warning("MANAGER: Error getting status for '%s': %v. Proceeding with start attempt.", name, statusErr)
	}
	m.logger.Info("MANAGER: Current status for '%s' is '%s'", name, currentStatus)

	if currentStatus == "running" {
		m.logger.Info("MANAGER: Server '%s' (identifier: %s) reported as already running by status check.", name, fixedIdentifier)

		return nil
	}

	// Pre-start hooks
	if srvCfg.Lifecycle.PreStart != "" {
		m.logger.Info("MANAGER: Running pre-start hook for server '%s'...", name)
		if hookErr := m.runLifecycleHook(srvCfg.Lifecycle.PreStart); hookErr != nil {
			m.logger.Error("MANAGER: Pre-start hook for server '%s' failed: %v", name, hookErr)

			return fmt.Errorf("pre-start hook for server '%s' failed: %w", name, hookErr)
		}
		m.logger.Info("MANAGER: Pre-start hook for server '%s' completed.", name)
	}

	// Network management handled by system

	var startErr error
	if instance.IsContainer {
		startErr = fmt.Errorf("server '%s' is configured as container - use 'matey up' instead", name)
	} else if srvCfg.Command != "" {
		m.logger.Info("MANAGER: Server '%s' is process. Calling startProcessServer with identifier '%s'.", name, fixedIdentifier)
		startErr = m.startProcessServer(name, fixedIdentifier, &srvCfg)
	} else {
		m.logger.Error("MANAGER: Server '%s' has no command or image specified.", name)
		startErr = fmt.Errorf("server '%s' has no command or image specified in config", name)
	}

	if startErr != nil {
		m.logger.Error("MANAGER: Error starting server '%s' (identifier: %s): %v", name, fixedIdentifier, startErr)

		return fmt.Errorf("failed to start server '%s' (identifier: %s): %w", name, fixedIdentifier, startErr)
	}

	instance.Status = "running"
	instance.StartTime = time.Now()
	m.logger.Info("MANAGER: Server '%s' (identifier: %s) marked as started successfully. ContainerID (if any): %s", name, fixedIdentifier, instance.ContainerID)

	// REMOVE ALL THE BLOCKING POST-START ACTIVITIES
	// Just start them in background goroutines without waiting

	// Post-start hooks (non-blocking)
	if srvCfg.Lifecycle.PostStart != "" {
		go func() {
			m.logger.Info("MANAGER: Running post-start hook for server '%s' (background)...", name)
			if hookErr := m.runLifecycleHook(srvCfg.Lifecycle.PostStart); hookErr != nil {
				m.logger.Warning("MANAGER: Post-start hook for server '%s' failed: %v", name, hookErr)
			} else {
				m.logger.Info("MANAGER: Post-start hook for server '%s' completed.", name)
			}
		}()
	}
	if config.IsCapabilityEnabled(srvCfg, "resources") && len(srvCfg.Resources.Paths) > 0 {
		go func() {
			m.logger.Info("MANAGER: Initializing resource watcher for server '%s' (background)...", name)
			// Fix: Pass the instance as the second parameter
			watcher, watchErr := NewResourcesWatcher(&srvCfg, instance, m.logger)
			if watchErr != nil {
				m.logger.Warning("MANAGER: Failed to initialize resource watcher for server '%s': %v", name, watchErr)

				return
			}

			instance.mu.Lock()
			instance.ResourcesWatcher = watcher
			instance.mu.Unlock()

			watcher.Start()
			m.logger.Info("MANAGER: Resource watcher started for server '%s'", name)
		}()
	}

	// Health check (non-blocking)
	if srvCfg.Lifecycle.HealthCheck.Endpoint != "" {
		go func() {
			m.logger.Info("MANAGER: Starting health check for server '%s' (background)...", name)
			m.startHealthCheck(name, fixedIdentifier)
		}()
	}

	// Capabilities (non-blocking)
	go func() {
		if capErr := m.initializeServerCapabilities(name); capErr != nil {
			m.logger.Warning("MANAGER: Failed to initialize capabilities for server '%s': %v", name, capErr)
		} else {
			m.logger.Info("MANAGER: Capabilities initialized for server '%s'", name)
		}
	}()

	m.logger.Info("MANAGER: StartServer for '%s' completed.", name)

	return nil
}


// startProcessServer uses processIdentifier for log/pid files
func (m *Manager) startProcessServer(serverKeyName, processIdentifier string, srvCfg *config.ServerConfig) error {
	m.logger.Info("Preparing to start process '%s' for server '%s' with command '%s'", processIdentifier, serverKeyName, srvCfg.Command)

	env := make(map[string]string)
	if srvCfg.Env != nil {
		for k, v := range srvCfg.Env {
			env[k] = v
		}
	}
	// Add standard MCP environment variables
	env["MCP_SERVER_NAME"] = serverKeyName
	// Add connection-related environment variables from global config
	for connKey, connCfg := range m.config.Connections {
		prefix := fmt.Sprintf("MCP_CONN_%s_", strings.ToUpper(connKey))
		env[prefix+"TRANSPORT"] = connCfg.Transport
		if connCfg.Port > 0 {
			env[prefix+"PORT"] = fmt.Sprintf("%d", connCfg.Port)
		}
		if connCfg.Host != "" {
			env[prefix+"HOST"] = connCfg.Host
		}
		if connCfg.Path != "" {
			env[prefix+"PATH"] = connCfg.Path
		}
	}

	proc, err := runtime.NewProcess(srvCfg.Command, srvCfg.Args, runtime.ProcessOptions{
		Env:     env,
		WorkDir: srvCfg.WorkDir,
		Name:    processIdentifier, // runtime.Process uses this for its internal tracking (e.g., PID file name)
	})
	if err != nil {

		return fmt.Errorf("failed to create process structure for '%s' (server '%s'): %w", processIdentifier, serverKeyName, err)
	}
	if err := proc.Start(); err != nil {

		return fmt.Errorf("failed to start process '%s' (server '%s'): %w", processIdentifier, serverKeyName, err)
	}

	m.servers[serverKeyName].Process = proc
	m.logger.Info("Process '%s' for server '%s' started", processIdentifier, serverKeyName)

	return nil
}

// StopServer stops a server using its fixed identifier
func (m *Manager) StopServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, ok := m.servers[name]
	if !ok {

		return fmt.Errorf("server '%s' not found in manager", name)
	}
	srvCfg := instance.Config
	fixedIdentifier := fmt.Sprintf("matey-%s", name)

	currentStatus, _ := m.getServerStatusUnsafe(name, fixedIdentifier)
	if currentStatus != "running" {
		m.logger.Info("Server '%s' (identifier: %s) is not running, nothing to stop", name, fixedIdentifier)

		return nil // Or return an error if it was expected to be running
	}

	m.logger.Info("Stopping server '%s' (identifier: %s)...", name, fixedIdentifier)

	if srvCfg.Lifecycle.PreStop != "" {
		m.logger.Info("Running pre-stop hook for server '%s'", name)
		if err := m.runLifecycleHook(srvCfg.Lifecycle.PreStop); err != nil {
			m.logger.Warning("Pre-stop hook for server '%s' failed: %v", name, err) // Log but continue stopping
		}
	}

	if instance.ResourcesWatcher != nil {
		instance.ResourcesWatcher.Stop()
		instance.ResourcesWatcher = nil
		m.logger.Debug("Resource watcher stopped for server '%s'", name)
	}

	var stopErr error
	if instance.IsContainer {
		stopErr = fmt.Errorf("container operations not supported - use 'matey down' instead")
	} else if instance.Process != nil {
		m.logger.Info("Stopping process '%s' for server '%s'", fixedIdentifier, name)
		stopErr = instance.Process.Stop()
		if stopErr != nil {
			m.logger.Error("Failed to stop process '%s' for server '%s': %v", fixedIdentifier, name, stopErr)
		}
		instance.Process = nil
	} else {
		m.logger.Warning("Server '%s' (identifier: %s) was marked to stop but had no active container or process reference", name, fixedIdentifier)
	}

	instance.Status = "stopped"
	instance.HealthStatus = "unknown"
	m.logger.Info("Server '%s' (identifier: %s) has been stopped", name, fixedIdentifier)

	if srvCfg.Lifecycle.PostStop != "" {
		m.logger.Info("Running post-stop hook for server '%s'", name)
		if err := m.runLifecycleHook(srvCfg.Lifecycle.PostStop); err != nil {
			m.logger.Warning("Post-stop hook for server '%s' failed: %v", name, err)
		}
	}

	return stopErr // Return the error from the stop operation, if any
}

// GetServerStatus returns the status of a server, using the fixed identifier.
// This public method ensures locking.
func (m *Manager) GetServerStatus(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fixedIdentifier := fmt.Sprintf("matey-%s", name)

	// Check if this is a built-in service that might have different container handling
	if m.isBuiltInService(name) {
		return m.getBuiltInServiceStatus(name, fixedIdentifier)
	}

	return m.getServerStatusUnsafe(name, fixedIdentifier)
}

// getServerStatusUnsafe is the internal implementation without locking, for use by other locked methods.
func (m *Manager) getServerStatusUnsafe(name string, fixedIdentifier string) (string, error) {
	instance, ok := m.servers[name]
	if !ok {
		m.logger.Debug("Server '%s' not found in manager's server list (have %d servers)", name, len(m.servers))

		return "unknown", fmt.Errorf("server '%s' not found in manager's list", name)
	}

	var currentRuntimeStatus string
	var err error

	if instance.IsContainer {
		currentRuntimeStatus = "unknown"
		err = fmt.Errorf("container operations not supported - use 'matey ps' instead")
	} else { // Process-based server
		proc, findErr := runtime.FindProcess(fixedIdentifier)
		if findErr != nil {
			currentRuntimeStatus = "stopped"
		} else {
			isRunning, runErr := proc.IsRunning()
			if runErr != nil || !isRunning {
				currentRuntimeStatus = "stopped"
			} else {
				currentRuntimeStatus = "running"
			}
		}
	}
	instance.Status = currentRuntimeStatus // Update cached status

	return currentRuntimeStatus, err // Return error from runtime if any
}

// isBuiltInService checks if a server is a built-in service with special handling
func (m *Manager) isBuiltInService(name string) bool {
	builtInServices := []string{"memory", "task-scheduler", "dashboard", "proxy"}
	for _, service := range builtInServices {
		if name == service {
			return true
		}
	}

	return false
}

// getBuiltInServiceStatus handles status checking for built-in services
func (m *Manager) getBuiltInServiceStatus(name string, fixedIdentifier string) (string, error) {
	instance, ok := m.servers[name]
	if !ok {
		// Built-in service not found in server list
		return "unknown", fmt.Errorf("built-in service '%s' not found in manager's list", name)
	}

	var currentRuntimeStatus string
	var err error

	if instance.IsContainer {
		currentRuntimeStatus = "unknown"
		err = fmt.Errorf("container operations not supported - use 'matey ps' instead")
	} else {
		currentRuntimeStatus = "stopped"
	}

	instance.Status = currentRuntimeStatus

	return currentRuntimeStatus, err
}

// isLikelyContainer determines if a server is likely running in a container
// based on protocol and other hints, even if Image field is not set
func (m *Manager) isLikelyContainer(serverName string, serverCfg config.ServerConfig) bool {
	// If it has HTTP protocol and port, it's likely a container
	if serverCfg.Protocol == "http" && serverCfg.HttpPort > 0 {
		return true
	}

	// Container detection removed

	return false
}

// ShowLogs displays logs for a server using the fixed identifier
func (m *Manager) ShowLogs(name string, follow bool) error {
	instance, ok := m.servers[name]
	if !ok {

		return fmt.Errorf("server '%s' not found for showing logs", name)
	}
	fixedIdentifier := fmt.Sprintf("matey-%s", name)
	m.logger.Debug("Requesting logs for server '%s' (identifier: %s)", name, fixedIdentifier)

	if instance.IsContainer {
		return fmt.Errorf("container logs not available - use kubectl logs for pods")
	} else {
		proc, err := runtime.FindProcess(fixedIdentifier)
		if err != nil {
			return fmt.Errorf("process for server '%s' (identifier: %s) not found: %w", name, fixedIdentifier, err)
		}
		return proc.ShowLogs(follow)
	}
}

type ResourcesWatcher struct {
	config          *config.ServerConfig
	fsWatcher       *fsnotify.Watcher // Simplified to one watcher for the example
	stopCh          chan struct{}
	active          bool
	logger          *logging.Logger
	mu              sync.Mutex
	changedFiles    map[string]time.Time
	ticker          *time.Ticker
	resourceManager *protocol.ResourceManager
	serverInstance  *ServerInstance
}

func NewResourcesWatcher(cfg *config.ServerConfig, instance *ServerInstance, loggerInstance ...*logging.Logger) (*ResourcesWatcher, error) {
	var logger *logging.Logger
	if len(loggerInstance) > 0 && loggerInstance[0] != nil {
		logger = loggerInstance[0]
	} else {
		logger = logging.NewLogger("info")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {

		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	return &ResourcesWatcher{
		config:          cfg,
		fsWatcher:       watcher,
		stopCh:          make(chan struct{}),
		logger:          logger,
		changedFiles:    make(map[string]time.Time),
		resourceManager: instance.ResourceManager,
		serverInstance:  instance,
	}, nil
}

func (w *ResourcesWatcher) Start() {
	w.mu.Lock()
	if w.active {
		w.mu.Unlock()
		w.logger.Debug("Resource watcher already active.")

		return
	}
	w.active = true
	w.mu.Unlock()

	w.logger.Info("Starting resource watcher for paths: %v", w.config.Resources.Paths)

	for _, rp := range w.config.Resources.Paths {
		if rp.Watch {
			// Walk the path to add all subdirectories
			err := filepath.WalkDir(rp.Source, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					w.logger.Error("Error walking path %s for watcher: %v", path, err)

					return err // Or return nil to continue walking other parts
				}
				if d.IsDir() {
					w.logger.Debug("Adding path to watcher: %s", path)
					if addErr := w.fsWatcher.Add(path); addErr != nil {
						w.logger.Error("Failed to add path %s to watcher: %v", path, addErr)
						// Potentially continue to try and watch other directories
					}
				}

				return nil
			})
			if err != nil {
				w.logger.Error("Error setting up watch for path %s: %v", rp.Source, err)
				// Potentially stop or handle error
			}
		}
	}

	syncInterval := constants.SyncIntervalDefault // Default sync interval
	if w.config.Resources.SyncInterval != "" {
		parsedInterval, err := time.ParseDuration(w.config.Resources.SyncInterval)
		if err == nil {
			syncInterval = parsedInterval
		} else {
			w.logger.Warning("Invalid resource sync interval '%s', using default %v: %v", w.config.Resources.SyncInterval, syncInterval, err)
		}
	}
	w.ticker = time.NewTicker(syncInterval)

	go func() {
		defer w.cleanupWatcher()
		for {
			select {
			case <-w.stopCh:
				w.logger.Info("Resource watcher stop signal received.")

				return
			case event, ok := <-w.fsWatcher.Events:
				if !ok {
					w.logger.Info("Watcher events channel closed.")

					return
				}
				if w.shouldProcessEvent(event) {
					w.recordChange(event.Name)
				}
			case err, ok := <-w.fsWatcher.Errors:
				if !ok {
					w.logger.Info("Watcher errors channel closed.")

					return
				}
				w.logger.Error("Watcher error: %v", err)
			case <-w.ticker.C:
				w.processChanges()
			}
		}
	}()
}
func (w *ResourcesWatcher) cleanupWatcher() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ticker != nil {
		w.ticker.Stop()
	}
	if w.fsWatcher != nil {
		if err := w.fsWatcher.Close(); err != nil {
			w.logger.Warning("Failed to close filesystem watcher: %v", err)
		}
	}
	w.active = false
	w.logger.Info("Resource watcher cleaned up.")
}

func (w *ResourcesWatcher) shouldProcessEvent(event fsnotify.Event) bool {
	// Basic filtering, can be expanded
	if strings.HasPrefix(filepath.Base(event.Name), ".") { // Ignore hidden files/dirs

		return false
	}
	// Only interested in these operations

	return event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)
}

func (w *ResourcesWatcher) recordChange(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.changedFiles[path] = time.Now()
	w.logger.Debug("Resource change detected: %s", path)
}

func (w *ResourcesWatcher) processChanges() {
	w.mu.Lock()
	if len(w.changedFiles) == 0 {
		w.mu.Unlock()

		return
	}
	// Create a copy to process, then clear the map
	changesToProcess := make(map[string]time.Time, len(w.changedFiles))
	for k, v := range w.changedFiles {
		changesToProcess[k] = v
	}
	w.changedFiles = make(map[string]time.Time) // Clear original map
	w.mu.Unlock()

	if len(changesToProcess) == 0 {

		return
	}

	mappedChanges := make(map[string]string) // Path -> "file" | "directory" | "deleted"
	for changedPath := range changesToProcess {
		// Determine type or if deleted
		info, err := os.Stat(changedPath)
		var changeType string
		if err == nil {
			changeType = "file"
			if info.IsDir() {
				changeType = "directory"
			}
		} else if os.IsNotExist(err) {
			changeType = "deleted"
		} else {
			w.logger.Warning("Error stating changed path %s: %v", changedPath, err)

			continue // Skip if cannot determine state
		}

		// Map this changedPath to the target path in the MCP server's context
		var targetPath string
		foundMapping := false
		for _, rp := range w.config.Resources.Paths {
			if strings.HasPrefix(changedPath, rp.Source) {
				relPath, _ := filepath.Rel(rp.Source, changedPath)
				targetPath = filepath.Join(rp.Target, relPath)
				mappedChanges[targetPath] = changeType
				foundMapping = true

				break
			}
		}
		if !foundMapping {
			w.logger.Debug("No resource mapping found for changed path: %s", changedPath)
		}
	}

	if len(mappedChanges) > 0 {
		w.notifyChanges(mappedChanges)
	}
}

func (w *ResourcesWatcher) notifyChanges(changes map[string]string) {
	// Placeholder for actual notification
	// This would involve constructing an MCP resources/list-changed notification
	// and sending it to the associated MCP server instance.
	changesJSON, _ := json.MarshalIndent(changes, "", "  ")
	w.logger.Info("Server notified of resource changes: %s", string(changesJSON))
}

func (w *ResourcesWatcher) Stop() {
	w.mu.Lock()
	if !w.active {
		w.mu.Unlock()

		return
	}
	// Set active to false first to prevent new operations from starting
	w.active = false
	w.mu.Unlock()

	// Signal the watcher goroutine to stop by closing stopCh
	// Check if stopCh is nil or already closed to prevent panic
	w.mu.Lock()
	if w.stopCh != nil {
		select {
		case <-w.stopCh:
			// Already closed or being closed
		default:
			close(w.stopCh) // Close the channel
			// Don't set w.stopCh = nil to avoid race with goroutine
		}
	}
	w.mu.Unlock() // Unlock before logging

	// Wait for cleanup to complete with timeout
	done := make(chan struct{})
	go func() {
		// Wait a bit for the goroutine to finish cleanly
		time.Sleep(constants.ShortSleepDuration)
		close(done)
	}()

	select {
	case <-done:
		w.logger.Info("Resource watcher stopped successfully")
	case <-time.After(constants.LongSleepDuration):
		w.logger.Warning("Resource watcher stop timeout")
	}
}

func (m *Manager) startHealthCheck(serverName, fixedIdentifier string) {
	instance, ok := m.servers[serverName]
	if !ok {
		m.logger.Error("HealthCheck: Server '%s' not found.", serverName)

		return
	}

	healthCfg := instance.Config.Lifecycle.HealthCheck
	if healthCfg.Endpoint == "" {
		m.logger.Debug("HealthCheck: No endpoint for server '%s'.", serverName)

		return
	}

	interval, err := time.ParseDuration(healthCfg.Interval)
	if err != nil {
		interval = constants.SyncIntervalLong
		m.logger.Warning("HealthCheck: Invalid interval '%s' for '%s', using default %v: %v", healthCfg.Interval, serverName, interval, err)
	}

	// Get configurable timeout for health checks
	timeout := constants.SyncFallbackTimeout // Default fallback
	if healthCfg.Timeout != "" {
		if parsed, parseErr := time.ParseDuration(healthCfg.Timeout); parseErr == nil {
			timeout = parsed
		} else {
			m.logger.Warning("HealthCheck: Invalid timeout '%s' for '%s', using default %v: %v", healthCfg.Timeout, serverName, timeout, parseErr)
		}
	} else if len(m.config.Connections) > 0 {
		// Use global connection timeout config as fallback
		for _, conn := range m.config.Connections {
			timeout = conn.Timeouts.GetHealthCheckTimeout()

			break
		}
	}

	retries := healthCfg.Retries
	if retries <= 0 {
		retries = 3
	}

	// USE fixedIdentifier in the logging here
	m.logger.Info("HealthCheck: Starting for server '%s' (container: %s), endpoint: %s, interval: %v, timeout: %v, retries: %d",
		serverName, fixedIdentifier, healthCfg.Endpoint, interval, timeout, retries)

	go func() {
		healthCheckTicker := time.NewTicker(interval)
		defer healthCheckTicker.Stop()
		failCount := 0

		for {
			select {
			case <-healthCheckTicker.C:
				m.mu.Lock()
				instance, stillExists := m.servers[serverName]
				targetStatus := ""
				if stillExists {
					targetStatus = instance.Status
				}
				m.mu.Unlock()

				if !stillExists || targetStatus != "running" {
					m.logger.Info("HealthCheck: Server '%s' (container: %s) no longer exists or is not running, stopping health checks.", serverName, fixedIdentifier)

					return
				}

				// USE fixedIdentifier in the health check call
				healthy, checkErr := m.checkServerHealth(serverName, fixedIdentifier, healthCfg.Endpoint, timeout)

				m.mu.Lock()
				instance, stillExists = m.servers[serverName]
				if !stillExists {
					m.mu.Unlock()
					m.logger.Info("HealthCheck: Server '%s' (container: %s) removed during health check, stopping checks.", serverName, fixedIdentifier)

					return
				}

				if healthy {
					if instance.HealthStatus != "healthy" {
						m.logger.Info("HealthCheck: Server '%s' (container: %s) is now healthy.", serverName, fixedIdentifier)
					}
					instance.HealthStatus = "healthy"
					failCount = 0
				} else {
					failCount++
					instance.HealthStatus = fmt.Sprintf("failing (%d/%d)", failCount, retries)
					m.logger.Warning("HealthCheck: Server '%s' (container: %s) failed check %d/%d. Error: %v", serverName, fixedIdentifier, failCount, retries, checkErr)

					if failCount >= retries {
						instance.HealthStatus = "unhealthy"
						m.logger.Error("HealthCheck: Server '%s' (container: %s) is now unhealthy after %d retries.", serverName, fixedIdentifier, retries)

						if healthCfg.Action == "restart" {
							m.logger.Info("HealthCheck: Restart action configured for unhealthy server '%s' (container: %s). Attempting restart...", serverName, fixedIdentifier)
							m.mu.Unlock()
							go func(sName, containerName string) {
								m.logger.Info("HealthCheck: Restart goroutine initiated for '%s' (container: %s).", sName, containerName)
								if err := m.StopServer(sName); err != nil {
									m.logger.Error("HealthCheck: Failed to stop unhealthy server '%s': %v", sName, err)
								} else {
									m.logger.Info("HealthCheck: Server '%s' stopped for restart. Waiting briefly...", sName)
									time.Sleep(constants.ManagerRetryDelay)
									if err := m.StartServer(sName); err != nil {
										m.logger.Error("HealthCheck: Failed to restart server '%s': %v", sName, err)
									} else {
										m.logger.Info("HealthCheck: Server '%s' restarted successfully due to health check.", sName)
									}
								}
							}(serverName, fixedIdentifier) // Pass both parameters

							return
						}
					}
				}
				m.mu.Unlock()

			case <-m.ctx.Done():
				m.logger.Info("HealthCheck: Manager shutting down, stopping health checks for '%s'", serverName)

				return
			}
		}
	}()
}

func (m *Manager) checkServerHealth(serverName, fixedIdentifier, endpoint string, timeout time.Duration) (bool, error) {
	instance, ok := m.servers[serverName]
	if !ok {

		return false, fmt.Errorf("server '%s' not found for health check", serverName)
	}

	var url string
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		url = endpoint
	} else {
		// Construct URL based on server configuration
		var hostPort string
		var host string // DECLARE host here, outside the if blocks

		if instance.IsContainer {
			// Use the fixed identifier (container name) for internal health checks
			host = fixedIdentifier

			// Determine port from configuration
			if instance.Config.HttpPort > 0 {
				hostPort = fmt.Sprintf("%d", instance.Config.HttpPort)
			} else if instance.Config.SSEPort > 0 && instance.Config.Protocol == "sse" {
				hostPort = fmt.Sprintf("%d", instance.Config.SSEPort)
			} else if len(instance.Config.Ports) > 0 {
				// Try to extract port from port mappings
				parts := strings.Split(instance.Config.Ports[0], ":")
				if len(parts) >= constants.ServerNameParts {
					hostPort = parts[1] // container port
				} else {
					hostPort = parts[0]
				}
			} else {
				// Default ports based on protocol
				switch instance.Config.Protocol {
				case "http":
					hostPort = "80"
				case "sse":
					hostPort = "8080"
				default:
					hostPort = "80"
				}
			}
		} else {
			// For processes, use localhost
			host = "localhost"

			// For processes, try to determine port from various sources
			if instance.Config.HttpPort > 0 {
				hostPort = fmt.Sprintf("%d", instance.Config.HttpPort)
			} else if len(m.config.Connections) > 0 {
				// Check global connections for port
				for _, conn := range m.config.Connections {
					if (conn.Transport == "http" || conn.Transport == "https") && conn.Port > 0 {
						hostPort = fmt.Sprintf("%d", conn.Port)

						break
					}
				}
			}

			// If still no port found, try to extract from args
			if hostPort == "" {
				for i, arg := range instance.Config.Args {
					if (arg == "--port" || arg == "-p") && i+1 < len(instance.Config.Args) {
						hostPort = instance.Config.Args[i+1]

						break
					} else if strings.HasPrefix(arg, "--port=") {
						hostPort = strings.TrimPrefix(arg, "--port=")

						break
					}
				}
			}

			// Final fallback
			if hostPort == "" {
				hostPort = "80"
			}
		}

		url = fmt.Sprintf("http://%s:%s%s", host, hostPort, endpoint)
	}

	client := http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true, // Don't keep connections alive for health checks
			IdleConnTimeout:   timeout / constants.ManagerIdleConnDivisor,
		},
	}

	// Log with both server name and identifier for better debugging
	m.logger.Debug("HealthCheck: Pinging %s for server '%s' (container: %s)", url, serverName, fixedIdentifier)

	resp, err := client.Get(url)
	if err != nil {
		// Provide more detailed error information
		if strings.Contains(err.Error(), "connection refused") {

			return false, fmt.Errorf("server '%s' (%s) not reachable at %s: connection refused", serverName, fixedIdentifier, url)
		} else if strings.Contains(err.Error(), "timeout") {

			return false, fmt.Errorf("server '%s' (%s) health check timed out at %s", serverName, fixedIdentifier, url)
		} else if strings.Contains(err.Error(), "no such host") {
			// Extract host from url for error message instead of using the variable
			urlParts := strings.Split(strings.TrimPrefix(url, "http://"), ":")
			hostFromURL := urlParts[0]

			return false, fmt.Errorf("server '%s' (%s) hostname not found: %s", serverName, fixedIdentifier, hostFromURL)
		}

		return false, fmt.Errorf("health check request to %s failed for server '%s' (%s): %w", url, serverName, fixedIdentifier, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check for healthy status codes
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		m.logger.Debug("HealthCheck: Server '%s' (%s) is healthy (status: %d)", serverName, fixedIdentifier, resp.StatusCode)

		return true, nil
	}

	// Read response body for error details
	body, _ := io.ReadAll(io.LimitReader(resp.Body, constants.HTTPLogBufferSize))

	return false, fmt.Errorf("server '%s' (%s) health check failed: status %d from %s: %s",
		serverName, fixedIdentifier, resp.StatusCode, url, string(body))
}

// Add this method to validate server configuration
func (m *Manager) validateServerConfig(name string, config config.ServerConfig) error {
	if config.Image == "" && config.Command == "" {

		return fmt.Errorf("server '%s' must specify either 'image' or 'command'", name)
	}

	if config.Protocol != "" {
		validProtocols := []string{"http", "sse", "stdio", "tcp"}
		valid := false
		for _, p := range validProtocols {
			if config.Protocol == p {
				valid = true

				break
			}
		}
		if !valid {

			return fmt.Errorf("server '%s' has invalid protocol '%s', must be one of: %v", name, config.Protocol, validProtocols)
		}
	}

	// Validate capabilities
	validCapabilities := []string{"resources", "tools", "prompts", "sampling", "logging"}
	for _, cap := range config.Capabilities {
		valid := false
		for _, validCap := range validCapabilities {
			if cap == validCap {
				valid = true

				break
			}
		}
		if !valid {

			return fmt.Errorf("server '%s' has invalid capability '%s', must be one of: %v", name, cap, validCapabilities)
		}
	}

	return nil
}

func (m *Manager) runLifecycleHook(hookScript string) error {
	m.logger.Info("Running lifecycle hook: %s", hookScript)

	// Get configurable timeout for lifecycle hooks
	timeout := constants.HTTPRequestTimeout // Default fallback
	if len(m.config.Connections) > 0 {
		for _, conn := range m.config.Connections {
			timeout = conn.Timeouts.GetLifecycleHookTimeout()

			break // Use first connection's timeout config
		}
	}

	// Create a context with configurable timeout for the hook
	ctx, cancel := context.WithTimeout(m.ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", hookScript)
	cmd.Env = append(os.Environ(),
		"MCP_PROJECT_DIR="+m.projectDir,
		"MCP_CONFIG_DIR="+filepath.Dir(m.projectDir),
	)
	cmd.Dir = m.projectDir

	// Capture both stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Log hook output
	if stdout.Len() > 0 {
		m.logger.Debug("Lifecycle hook '%s' stdout: %s", hookScript, stdout.String())
	}
	if stderr.Len() > 0 {
		m.logger.Debug("Lifecycle hook '%s' stderr: %s", hookScript, stderr.String())
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {

			return fmt.Errorf("lifecycle hook '%s' timed out after %v", hookScript, timeout)
		}

		return fmt.Errorf("lifecycle hook '%s' failed: %w. Stderr: %s", hookScript, err, stderr.String())
	}

	m.logger.Info("Lifecycle hook '%s' completed successfully", hookScript)

	return nil
}

// Network management functions removed

func (m *Manager) Shutdown() error {
	m.logger.Info("MANAGER: Starting graceful shutdown process")

	// Cancel all contexts first
	if m.cancel != nil {
		m.cancel()
	}

	// Stop all health checkers
	m.healthCheckMu.Lock()
	for name, cancel := range m.healthCheckers {
		m.logger.Debug("MANAGER: Stopping health checker for %s", name)
		cancel()
	}
	m.healthCheckers = make(map[string]context.CancelFunc)
	m.healthCheckMu.Unlock()

	// Stop all resource watchers
	m.mu.RLock()
	serverNames := make([]string, 0, len(m.servers))
	for name, instance := range m.servers {
		serverNames = append(serverNames, name)
		if instance.ResourcesWatcher != nil {
			go instance.ResourcesWatcher.Stop() // Stop in parallel
		}
	}
	m.mu.RUnlock()

	// Stop all servers in parallel
	stopGroup := sync.WaitGroup{}
	stopErrors := make(chan error, len(serverNames))

	for _, name := range serverNames {
		stopGroup.Add(1)
		go func(serverName string) {
			defer stopGroup.Done()
			if err := m.StopServer(serverName); err != nil {
				stopErrors <- fmt.Errorf("failed to stop server %s: %w", serverName, err)
			} else {
				m.logger.Info("MANAGER: Server %s stopped successfully", serverName)
			}
		}(name)
	}

	// Wait for all stops to complete with timeout
	done := make(chan struct{})
	go func() {
		stopGroup.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.logger.Info("MANAGER: All servers stopped")
	case <-time.After(constants.CleanupIntervalExtended):
		m.logger.Warning("MANAGER: Timeout waiting for servers to stop")
	}

	// Collect any stop errors
	close(stopErrors)
	var stopErr error
	for err := range stopErrors {
		if stopErr == nil {
			stopErr = err
		} else {
			m.logger.Error("MANAGER: Additional stop error: %v", err)
		}
	}

	// Network cleanup handled by system

	// Wait for all background goroutines
	waitDone := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		m.logger.Info("MANAGER: All goroutines finished")
	case <-time.After(constants.ManagerCleanupTimeout):
		m.logger.Warning("MANAGER: Timeout waiting for goroutines to finish")
	}

	// Close shutdown channel
	select {
	case <-m.shutdownCh:
		// Already closed
	default:
		close(m.shutdownCh)
	}

	if stopErr != nil {

		return fmt.Errorf("shutdown completed with errors: %w", stopErr)
	}

	m.logger.Info("MANAGER: Shutdown completed successfully")

	return nil
}

func (m *Manager) initializeServerCapabilities(serverName string) error {
	instance, ok := m.servers[serverName]
	if !ok {

		return fmt.Errorf("server '%s' not found for capability initialization", serverName)
	}

	// Initialize capabilities from config
	for _, capName := range instance.Config.Capabilities {
		instance.Capabilities[capName] = true
	}

	// Initialize resource paths in resource manager
	if instance.ResourceManager != nil && config.IsCapabilityEnabled(instance.Config, "resources") {
		for _, resourcePath := range instance.Config.Resources.Paths {
			// Create resource entries for each configured path
			resource := &protocol.Resource{
				URI:         resourcePath.Target,
				Name:        filepath.Base(resourcePath.Source),
				Description: fmt.Sprintf("Resource from %s", resourcePath.Source),
				Created:     time.Now(),
				Modified:    time.Now(),
			}

			// Read actual content if it's a file
			if info, err := os.Stat(resourcePath.Source); err == nil {
				if !info.IsDir() {
					if content, err := os.ReadFile(resourcePath.Source); err == nil {
						resource.Content = &protocol.ResourceContentData{
							Type:         "text",
							Data:         string(content),
							Encoding:     "utf-8",
							LastModified: info.ModTime(),
						}
						resource.Size = info.Size()
					}
				}
			}

			if err := instance.ResourceManager.AddResource(resource); err != nil {
				m.logger.Warning("Failed to add resource %s: %v", resourcePath.Target, err)
			}
		}
	}

	// Initialize tool capabilities if configured
	if config.IsCapabilityEnabled(instance.Config, "tools") {
		for _, tool := range instance.Config.Tools {
			m.logger.Debug("Tool capability registered: %s", tool.Name)
		}
	}

	// Initialize sampling capabilities with optional human control
	if instance.SamplingManager != nil && config.IsCapabilityEnabled(instance.Config, "sampling") {
		// Set up human control configuration if specified
		if instance.Config.Lifecycle.HumanControl != nil {
			humanConfig := &protocol.HumanControlConfig{
				RequireApproval:     instance.Config.Lifecycle.HumanControl.RequireApproval,
				AutoApprovePatterns: instance.Config.Lifecycle.HumanControl.AutoApprovePatterns,
				BlockPatterns:       instance.Config.Lifecycle.HumanControl.BlockPatterns,
				MaxTokens:           instance.Config.Lifecycle.HumanControl.MaxTokens,
				TimeoutSeconds:      instance.Config.Lifecycle.HumanControl.TimeoutSeconds,
			}
			instance.SamplingManager.SetHumanControls(serverName, humanConfig)
		}
	}

	m.logger.Info("Initialized capabilities for server '%s': %v", serverName, instance.Capabilities)

	return nil
}

func (m *Manager) GetServerInstance(serverName string) (*ServerInstance, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	instance, exists := m.servers[serverName]

	return instance, exists
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {

			return true
		}
	}

	return false
}
