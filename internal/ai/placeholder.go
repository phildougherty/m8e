package ai

import (
	"context"
	"fmt"
)

// PlaceholderProvider represents a provider that couldn't be initialized
type PlaceholderProvider struct {
	name  string
	error string
}

// NewPlaceholderProvider creates a new placeholder provider
func NewPlaceholderProvider(name, errorMsg string) *PlaceholderProvider {
	return &PlaceholderProvider{
		name:  name,
		error: errorMsg,
	}
}

// Name returns the provider name
func (p *PlaceholderProvider) Name() string {
	return p.name
}

// StreamChat returns an error explaining why the provider is unavailable
func (p *PlaceholderProvider) StreamChat(ctx context.Context, messages []Message, options StreamOptions) (<-chan StreamResponse, error) {
	return nil, fmt.Errorf("provider %s is not available: %s", p.name, p.error)
}

// SupportedModels returns an empty list
func (p *PlaceholderProvider) SupportedModels() []string {
	return []string{}
}

// ValidateConfig returns the initialization error
func (p *PlaceholderProvider) ValidateConfig() error {
	return fmt.Errorf("%s", p.error)
}

// IsAvailable always returns false
func (p *PlaceholderProvider) IsAvailable() bool {
	return false
}

// DefaultModel returns empty string
func (p *PlaceholderProvider) DefaultModel() string {
	return ""
}