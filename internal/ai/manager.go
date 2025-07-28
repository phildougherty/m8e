package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Manager manages multiple AI providers
type Manager struct {
	providers       map[string]Provider
	config          Config
	currentProvider string
	mu              sync.RWMutex
	healthCheck     *time.Ticker
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewManager creates a new AI provider manager
func NewManager(config Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	
	m := &Manager{
		providers: make(map[string]Provider),
		config:    config,
		ctx:       ctx,
		cancel:    cancel,
	}
	
	// Initialize providers
	m.initializeProviders()
	
	// Set current provider
	m.setCurrentProvider(config.DefaultProvider)
	
	// Start health checking
	m.startHealthCheck()
	
	return m
}

// initializeProviders initializes all configured providers
func (m *Manager) initializeProviders() {
	for name, config := range m.config.Providers {
		var provider Provider
		var err error
		
		switch name {
		case string(ProviderTypeOpenAI):
			provider, err = NewOpenAIProvider(config)
		case string(ProviderTypeClaude):
			provider, err = NewClaudeProvider(config)
		case string(ProviderTypeOllama):
			provider, err = NewOllamaProvider(config)
		case string(ProviderTypeOpenRouter):
			provider, err = NewOpenRouterProvider(config)
		default:
			err = fmt.Errorf("unknown provider type: %s", name)
		}
		
		if err != nil {
			// Create placeholder provider to show why it's not available
			m.providers[name] = NewPlaceholderProvider(name, err.Error())
		} else {
			m.providers[name] = provider
		}
	}
}

// setCurrentProvider sets the current provider
func (m *Manager) setCurrentProvider(providerName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if provider, exists := m.providers[providerName]; exists && provider.IsAvailable() {
		m.currentProvider = providerName
		return
	}
	
	// Try fallback providers
	for _, fallback := range m.config.FallbackProviders {
		if provider, exists := m.providers[fallback]; exists && provider.IsAvailable() {
			m.currentProvider = fallback
			return
		}
	}
	
	// If no providers available, set to empty
	m.currentProvider = ""
}

// GetCurrentProvider returns the current provider
func (m *Manager) GetCurrentProvider() (Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if m.currentProvider == "" {
		return nil, fmt.Errorf("no available AI provider")
	}
	
	provider, exists := m.providers[m.currentProvider]
	if !exists {
		return nil, fmt.Errorf("current provider %s not found", m.currentProvider)
	}
	
	return provider, nil
}

// GetProvider returns a specific provider by name
func (m *Manager) GetProvider(name string) (Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	provider, exists := m.providers[name]
	if !exists {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	
	return provider, nil
}

// SwitchProvider switches to a different provider
func (m *Manager) SwitchProvider(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	provider, exists := m.providers[name]
	if !exists {
		return fmt.Errorf("provider %s not found", name)
	}
	
	if !provider.IsAvailable() {
		return fmt.Errorf("provider %s is not available", name)
	}
	
	m.currentProvider = name
	return nil
}

// StreamChat streams a chat completion using the current provider
func (m *Manager) StreamChat(ctx context.Context, messages []Message, options StreamOptions) (<-chan StreamResponse, error) {
	provider, err := m.GetCurrentProvider()
	if err != nil {
		return nil, err
	}
	
	return provider.StreamChat(ctx, messages, options)
}

// StreamChatWithProvider streams a chat completion using a specific provider
func (m *Manager) StreamChatWithProvider(ctx context.Context, providerName string, messages []Message, options StreamOptions) (<-chan StreamResponse, error) {
	provider, err := m.GetProvider(providerName)
	if err != nil {
		return nil, err
	}
	
	return provider.StreamChat(ctx, messages, options)
}

// StreamChatWithFallback streams a chat completion with automatic fallback
func (m *Manager) StreamChatWithFallback(ctx context.Context, messages []Message, options StreamOptions) (<-chan StreamResponse, error) {
	// Try current provider first
	provider, err := m.GetCurrentProvider()
	if err == nil {
		ch, err := provider.StreamChat(ctx, messages, options)
		if err == nil {
			return ch, nil
		}
	}
	
	// Try fallback providers
	for _, fallbackName := range m.config.FallbackProviders {
		if fallbackName == m.currentProvider {
			continue // Skip current provider
		}
		
		provider, err := m.GetProvider(fallbackName)
		if err != nil {
			continue
		}
		
		if !provider.IsAvailable() {
			continue
		}
		
		ch, err := provider.StreamChat(ctx, messages, options)
		if err == nil {
			// Update current provider to successful fallback
			m.mu.Lock()
			m.currentProvider = fallbackName
			m.mu.Unlock()
			return ch, nil
		}
	}
	
	return nil, fmt.Errorf("no available providers for chat completion")
}

// GetProviderStatus returns the status of all providers
func (m *Manager) GetProviderStatus() map[string]ProviderStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	status := make(map[string]ProviderStatus)
	
	for name, provider := range m.providers {
		providerStatus := ProviderStatus{
			Name:      name,
			Available: provider.IsAvailable(),
			LastCheck: time.Now(),
			Models:    provider.SupportedModels(),
		}
		
		if err := provider.ValidateConfig(); err != nil {
			providerStatus.Error = err.Error()
		}
		
		status[name] = providerStatus
	}
	
	return status
}

// GetAvailableProviders returns a list of available providers
func (m *Manager) GetAvailableProviders() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var available []string
	for name, provider := range m.providers {
		if provider.IsAvailable() {
			available = append(available, name)
		}
	}
	
	return available
}

// GetSupportedModels returns all supported models across all providers
func (m *Manager) GetSupportedModels() map[string][]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	models := make(map[string][]string)
	for name, provider := range m.providers {
		models[name] = provider.SupportedModels()
	}
	
	return models
}

// SwitchModel switches the current provider to use a different model
func (m *Manager) SwitchModel(modelName string) error {
	provider, err := m.GetCurrentProvider()
	if err != nil {
		return fmt.Errorf("no current provider available")
	}
	
	// Check if the model is supported by the current provider
	supportedModels := provider.SupportedModels()
	modelSupported := false
	for _, model := range supportedModels {
		if model == modelName {
			modelSupported = true
			break
		}
	}
	
	if !modelSupported {
		return fmt.Errorf("model '%s' is not supported by provider '%s'. Supported models: %s", 
			modelName, provider.Name(), strings.Join(supportedModels, ", "))
	}
	
	// Update the provider configuration
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if providerConfig, exists := m.config.Providers[provider.Name()]; exists {
		providerConfig.DefaultModel = modelName
		m.config.Providers[provider.Name()] = providerConfig
	}
	
	return nil
}

// GetCurrentModel returns the current model for the active provider
func (m *Manager) GetCurrentModel() string {
	provider, err := m.GetCurrentProvider()
	if err != nil {
		return ""
	}
	
	return provider.DefaultModel()
}

// startHealthCheck starts the health checking routine
func (m *Manager) startHealthCheck() {
	m.healthCheck = time.NewTicker(30 * time.Second)
	
	go func() {
		for {
			select {
			case <-m.healthCheck.C:
				m.checkHealth()
			case <-m.ctx.Done():
				return
			}
		}
	}()
}

// checkHealth checks the health of all providers
func (m *Manager) checkHealth() {
	m.mu.RLock()
	currentProvider := m.currentProvider
	m.mu.RUnlock()
	
	// Check if current provider is still available
	if currentProvider != "" {
		if provider, exists := m.providers[currentProvider]; exists {
			if !provider.IsAvailable() {
				// Current provider is no longer available, try to find a fallback
				m.setCurrentProvider(currentProvider)
			}
		}
	} else {
		// No current provider, try to find one
		m.setCurrentProvider(m.config.DefaultProvider)
	}
}

// Close closes the manager and stops health checking
func (m *Manager) Close() {
	if m.healthCheck != nil {
		m.healthCheck.Stop()
	}
	m.cancel()
}