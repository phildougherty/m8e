package context

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/phildougherty/m8e/internal/ai"
)

// ContextType represents different types of context content
type ContextType string

const (
	ContextTypeFile       ContextType = "file"
	ContextTypeEdit       ContextType = "edit"
	ContextTypeMention    ContextType = "mention"
	ContextTypeLog        ContextType = "log"
	ContextTypeDiagnostic ContextType = "diagnostic"
	ContextTypeGit        ContextType = "git"
	ContextTypeDefinition ContextType = "definition"
)

// ContextItem represents a single piece of context information
type ContextItem struct {
	ID          string      `json:"id"`
	Type        ContextType `json:"type"`
	FilePath    string      `json:"file_path,omitempty"`
	Content     string      `json:"content"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	TokenCount  int         `json:"token_count"`
	Timestamp   time.Time   `json:"timestamp"`
	AccessCount int         `json:"access_count"`
	LastAccess  time.Time   `json:"last_access"`
	Priority    float64     `json:"priority"`
	Hash        string      `json:"hash"`
}

// ContextWindow represents the current context state
type ContextWindow struct {
	Items       []ContextItem `json:"items"`
	TotalTokens int           `json:"total_tokens"`
	MaxTokens   int           `json:"max_tokens"`
	Truncated   bool          `json:"truncated"`
	Strategy    string        `json:"strategy"`
}

// TruncationStrategy defines how context should be truncated when limits are exceeded
type TruncationStrategy string

const (
	TruncateOldest    TruncationStrategy = "oldest"
	TruncateLRU       TruncationStrategy = "lru"
	TruncateByType    TruncationStrategy = "by_type"
	TruncateByPriority TruncationStrategy = "by_priority"
	TruncateIntelligent TruncationStrategy = "intelligent"
)

// ContextConfig configures the context manager behavior
type ContextConfig struct {
	MaxTokens          int                `json:"max_tokens"`
	TruncationStrategy TruncationStrategy `json:"truncation_strategy"`
	TypePriorities     map[ContextType]float64 `json:"type_priorities"`
	RetentionDays      int                `json:"retention_days"`
	TokenEstimator     func(string) int   `json:"-"`
	PersistToK8s       bool               `json:"persist_to_k8s"`
	Namespace          string             `json:"namespace"`
}

// ContextManager manages the context window and provides intelligent truncation
type ContextManager struct {
	config     ContextConfig
	items      map[string]*ContextItem
	window     *ContextWindow
	aiProvider ai.Provider
	mutex      sync.RWMutex
	
	// For Kubernetes persistence
	configMapName string
	secretName    string
}

// NewContextManager creates a new context manager with the given configuration
func NewContextManager(config ContextConfig, aiProvider ai.Provider) *ContextManager {
	if config.MaxTokens == 0 {
		config.MaxTokens = 32768 // Default context window
	}
	if config.TruncationStrategy == "" {
		config.TruncationStrategy = TruncateIntelligent
	}
	if config.TypePriorities == nil {
		config.TypePriorities = defaultTypePriorities()
	}
	if config.RetentionDays == 0 {
		config.RetentionDays = 7
	}
	if config.TokenEstimator == nil {
		config.TokenEstimator = defaultTokenEstimator
	}
	if config.Namespace == "" {
		config.Namespace = "matey-system"
	}

	return &ContextManager{
		config:        config,
		items:         make(map[string]*ContextItem),
		window:        &ContextWindow{MaxTokens: config.MaxTokens},
		aiProvider:    aiProvider,
		configMapName: "matey-context-items",
		secretName:    "matey-context-sensitive",
	}
}

// AddContext adds a new context item to the manager
func (cm *ContextManager) AddContext(contextType ContextType, filePath, content string, metadata map[string]interface{}) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// Create context item
	item := &ContextItem{
		ID:          cm.generateID(contextType, filePath, content),
		Type:        contextType,
		FilePath:    filePath,
		Content:     content,
		Metadata:    metadata,
		TokenCount:  cm.config.TokenEstimator(content),
		Timestamp:   time.Now(),
		LastAccess:  time.Now(),
		AccessCount: 1,
		Priority:    cm.calculatePriority(contextType, filePath, content, metadata),
		Hash:        cm.calculateHash(content),
	}

	// Check if item already exists (by ID)
	if existing, exists := cm.items[item.ID]; exists {
		// Update existing item
		existing.Content = content
		existing.TokenCount = item.TokenCount
		existing.LastAccess = time.Now()
		existing.AccessCount++
		existing.Priority = item.Priority
		existing.Hash = item.Hash
		if metadata != nil {
			existing.Metadata = metadata
		}
	} else {
		// Add new item
		cm.items[item.ID] = item
	}

	// Update context window
	return cm.updateWindow()
}

// GetContext retrieves a context item by ID and updates access statistics
func (cm *ContextManager) GetContext(id string) (*ContextItem, error) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	item, exists := cm.items[id]
	if !exists {
		return nil, fmt.Errorf("context item not found: %s", id)
	}

	// Update access statistics
	item.LastAccess = time.Now()
	item.AccessCount++

	return item, nil
}

// GetContextByFile retrieves all context items for a specific file
func (cm *ContextManager) GetContextByFile(filePath string) ([]*ContextItem, error) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	var items []*ContextItem
	for _, item := range cm.items {
		if item.FilePath == filePath {
			items = append(items, item)
		}
	}

	// Sort by priority and recency
	sort.Slice(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority > items[j].Priority
		}
		return items[i].LastAccess.After(items[j].LastAccess)
	})

	return items, nil
}

// GetCurrentWindow returns the current context window
func (cm *ContextManager) GetCurrentWindow() *ContextWindow {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	// Create a copy to avoid race conditions
	window := &ContextWindow{
		Items:       make([]ContextItem, len(cm.window.Items)),
		TotalTokens: cm.window.TotalTokens,
		MaxTokens:   cm.window.MaxTokens,
		Truncated:   cm.window.Truncated,
		Strategy:    cm.window.Strategy,
	}
	copy(window.Items, cm.window.Items)

	return window
}

// UpdateFilePath updates the file path for all context items (for file renames)
func (cm *ContextManager) UpdateFilePath(oldPath, newPath string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	for _, item := range cm.items {
		if item.FilePath == oldPath {
			item.FilePath = newPath
		}
	}

	return cm.persistToK8s()
}

// RemoveContext removes a context item by ID
func (cm *ContextManager) RemoveContext(id string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	delete(cm.items, id)
	return cm.updateWindow()
}

// RemoveContextByFile removes all context items for a specific file
func (cm *ContextManager) RemoveContextByFile(filePath string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	for id, item := range cm.items {
		if item.FilePath == filePath {
			delete(cm.items, id)
		}
	}

	return cm.updateWindow()
}

// CleanupExpired removes context items older than the retention period
func (cm *ContextManager) CleanupExpired() error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	cutoff := time.Now().AddDate(0, 0, -cm.config.RetentionDays)
	
	for id, item := range cm.items {
		if item.Timestamp.Before(cutoff) && item.LastAccess.Before(cutoff) {
			delete(cm.items, id)
		}
	}

	return cm.updateWindow()
}

// GetStats returns statistics about the context manager
func (cm *ContextManager) GetStats() map[string]interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	typeCount := make(map[ContextType]int)
	totalTokens := 0
	
	for _, item := range cm.items {
		typeCount[item.Type]++
		totalTokens += item.TokenCount
	}

	return map[string]interface{}{
		"total_items":    len(cm.items),
		"total_tokens":   totalTokens,
		"window_tokens":  cm.window.TotalTokens,
		"window_items":   len(cm.window.Items),
		"truncated":      cm.window.Truncated,
		"type_breakdown": typeCount,
		"max_tokens":     cm.config.MaxTokens,
		"utilization":    float64(cm.window.TotalTokens) / float64(cm.config.MaxTokens),
	}
}

// Private methods

func (cm *ContextManager) updateWindow() error {
	// Collect all items
	var allItems []*ContextItem
	for _, item := range cm.items {
		allItems = append(allItems, item)
	}

	// Apply truncation strategy
	selectedItems, strategy := cm.applyTruncationStrategy(allItems)

	// Update window
	cm.window.Items = make([]ContextItem, len(selectedItems))
	cm.window.TotalTokens = 0
	
	for i, item := range selectedItems {
		cm.window.Items[i] = *item // Copy value
		cm.window.TotalTokens += item.TokenCount
	}
	
	cm.window.Truncated = len(selectedItems) < len(allItems)
	cm.window.Strategy = strategy

	// Persist to Kubernetes if enabled
	if cm.config.PersistToK8s {
		return cm.persistToK8s()
	}

	return nil
}

func (cm *ContextManager) applyTruncationStrategy(items []*ContextItem) ([]*ContextItem, string) {
	switch cm.config.TruncationStrategy {
	case TruncateOldest:
		return cm.truncateOldest(items)
	case TruncateLRU:
		return cm.truncateLRU(items)
	case TruncateByType:
		return cm.truncateByType(items)
	case TruncateByPriority:
		return cm.truncateByPriority(items)
	case TruncateIntelligent:
		return cm.truncateIntelligent(items)
	default:
		return cm.truncateIntelligent(items)
	}
}

func (cm *ContextManager) truncateOldest(items []*ContextItem) ([]*ContextItem, string) {
	// Sort by timestamp (newest first)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Timestamp.After(items[j].Timestamp)
	})

	return cm.selectItemsWithinTokenLimit(items), "oldest"
}

func (cm *ContextManager) truncateLRU(items []*ContextItem) ([]*ContextItem, string) {
	// Sort by last access (most recent first)
	sort.Slice(items, func(i, j int) bool {
		return items[i].LastAccess.After(items[j].LastAccess)
	})

	return cm.selectItemsWithinTokenLimit(items), "lru"
}

func (cm *ContextManager) truncateByType(items []*ContextItem) ([]*ContextItem, string) {
	// Group by type and apply type priorities
	typeGroups := make(map[ContextType][]*ContextItem)
	for _, item := range items {
		typeGroups[item.Type] = append(typeGroups[item.Type], item)
	}

	var selected []*ContextItem
	tokens := 0

	// Process types in priority order
	types := cm.getTypesByPriority()
	for _, contextType := range types {
		group := typeGroups[contextType]
		if len(group) == 0 {
			continue
		}

		// Sort group by priority and recency
		sort.Slice(group, func(i, j int) bool {
			if group[i].Priority != group[j].Priority {
				return group[i].Priority > group[j].Priority
			}
			return group[i].LastAccess.After(group[j].LastAccess)
		})

		// Add items from this type until token limit
		for _, item := range group {
			if tokens+item.TokenCount <= cm.config.MaxTokens {
				selected = append(selected, item)
				tokens += item.TokenCount
			} else {
				break
			}
		}

		if tokens >= cm.config.MaxTokens {
			break
		}
	}

	return selected, "by_type"
}

func (cm *ContextManager) truncateByPriority(items []*ContextItem) ([]*ContextItem, string) {
	// Sort by priority (highest first)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority > items[j].Priority
		}
		return items[i].LastAccess.After(items[j].LastAccess)
	})

	return cm.selectItemsWithinTokenLimit(items), "by_priority"
}

func (cm *ContextManager) truncateIntelligent(items []*ContextItem) ([]*ContextItem, string) {
	// Intelligent truncation combines multiple factors:
	// 1. Type priority
	// 2. Recency (last access)
	// 3. Usage frequency (access count)
	// 4. Content relevance

	for _, item := range items {
		item.Priority = cm.calculateIntelligentPriority(item)
	}

	// Sort by calculated priority
	sort.Slice(items, func(i, j int) bool {
		return items[i].Priority > items[j].Priority
	})

	return cm.selectItemsWithinTokenLimit(items), "intelligent"
}

func (cm *ContextManager) selectItemsWithinTokenLimit(items []*ContextItem) []*ContextItem {
	var selected []*ContextItem
	tokens := 0

	for _, item := range items {
		if tokens+item.TokenCount <= cm.config.MaxTokens {
			selected = append(selected, item)
			tokens += item.TokenCount
		}
	}

	return selected
}

func (cm *ContextManager) calculatePriority(contextType ContextType, filePath, content string, metadata map[string]interface{}) float64 {
	// Base priority from type
	priority := cm.config.TypePriorities[contextType]

	// Boost priority for recently edited files
	if contextType == ContextTypeEdit {
		priority += 0.3
	}

	// Boost priority for smaller files (more likely to be relevant)
	if len(content) < 1000 {
		priority += 0.1
	}

	// Boost priority for Go files in Go project
	if strings.HasSuffix(filePath, ".go") {
		priority += 0.2
	}

	// Consider metadata priorities
	if metadata != nil {
		if metaPriority, exists := metadata["priority"]; exists {
			if p, ok := metaPriority.(float64); ok {
				priority += p
			}
		}
	}

	return priority
}

func (cm *ContextManager) calculateIntelligentPriority(item *ContextItem) float64 {
	priority := cm.config.TypePriorities[item.Type]

	// Recency factor (decay over time)
	age := time.Since(item.LastAccess)
	recencyFactor := 1.0 / (1.0 + age.Hours()/24.0) // Decay over days
	priority += recencyFactor * 0.3

	// Usage frequency factor
	usageScore := float64(item.AccessCount) / 10.0 // Normalize
	if usageScore > 1.0 {
		usageScore = 1.0
	}
	priority += usageScore * 0.2

	// Size penalty for very large items
	if item.TokenCount > 1000 {
		sizePenalty := float64(item.TokenCount) / 10000.0
		if sizePenalty > 0.3 {
			sizePenalty = 0.3
		}
		priority -= sizePenalty
	}

	return priority
}

func (cm *ContextManager) getTypesByPriority() []ContextType {
	var types []ContextType
	for contextType := range cm.config.TypePriorities {
		types = append(types, contextType)
	}

	sort.Slice(types, func(i, j int) bool {
		return cm.config.TypePriorities[types[i]] > cm.config.TypePriorities[types[j]]
	})

	return types
}

func (cm *ContextManager) generateID(contextType ContextType, filePath, content string) string {
	data := fmt.Sprintf("%s:%s:%s", contextType, filePath, cm.calculateHash(content))
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash)[:16]
}

func (cm *ContextManager) calculateHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", hash)[:8]
}

func (cm *ContextManager) persistToK8s() error {
	// TODO: Implement Kubernetes ConfigMaps persistence
	// This would store non-sensitive context items in ConfigMaps
	// and sensitive items in Secrets
	return nil
}

// Helper functions

func defaultTypePriorities() map[ContextType]float64 {
	return map[ContextType]float64{
		ContextTypeEdit:       1.0, // Highest priority for recent edits
		ContextTypeFile:       0.8, // High priority for file content
		ContextTypeDefinition: 0.7, // Code definitions are important
		ContextTypeMention:    0.6, // Explicitly mentioned content
		ContextTypeDiagnostic: 0.5, // Diagnostic information
		ContextTypeGit:        0.4, // Git status information
		ContextTypeLog:        0.3, // Lowest priority for logs
	}
}

func defaultTokenEstimator(content string) int {
	// Simple token estimation: roughly 4 characters per token
	// In a real implementation, you'd use the actual tokenizer
	return len(content) / 4
}

// LoadFromK8s loads context from Kubernetes ConfigMaps and Secrets
func (cm *ContextManager) LoadFromK8s(ctx context.Context) error {
	// TODO: Implement Kubernetes loading
	return nil
}

// SaveToK8s saves context to Kubernetes ConfigMaps and Secrets
func (cm *ContextManager) SaveToK8s(ctx context.Context) error {
	// TODO: Implement Kubernetes saving
	return nil
}