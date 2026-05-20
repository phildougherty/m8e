// internal/server/tool_cache.go
package server

import (
	"sync"
	"time"
)

// ToolCache maps tool names to the server that exposes them. It owns its own
// mutex and TTL so callers never reach into proxy internals to synchronize.
type ToolCache struct {
	mu     sync.RWMutex
	tools  map[string]string
	expiry time.Time
}

// newToolCache creates an empty tool cache.
func newToolCache() *ToolCache {
	return &ToolCache{
		tools: make(map[string]string),
	}
}

// lookup returns the server name for a tool, if cached.
func (c *ToolCache) lookup(toolName string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	server, found := c.tools[toolName]

	return server, found
}

// state reports whether the cache is empty and/or expired plus its size, in a
// single locked read so callers see a consistent snapshot.
func (c *ToolCache) state() (empty, expired bool, size int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.tools) == 0, time.Now().After(c.expiry), len(c.tools)
}

// size returns the number of cached tools.
func (c *ToolCache) size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.tools)
}

// snapshot returns a copy of the cache contents for debug logging.
func (c *ToolCache) snapshot() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]string, len(c.tools))
	for k, v := range c.tools {
		out[k] = v
	}

	return out
}

// replace swaps in a freshly discovered tool set and sets the new expiry.
func (c *ToolCache) replace(tools map[string]string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools = tools
	c.expiry = time.Now().Add(ttl)
}

// invalidate clears the cache and forces the next lookup to refresh.
func (c *ToolCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools = make(map[string]string)
	c.expiry = time.Now()
}
