// internal/server/id_generator.go
package server

import (
	"strconv"
	"sync"
)

// idGenerator hands out monotonically increasing JSON-RPC request IDs. It is
// safe for concurrent use; the previous god object exposed the counter and its
// mutex as separate public fields.
type idGenerator struct {
	mu      sync.Mutex
	counter int
}

// nextInt returns the next request ID as an int.
func (g *idGenerator) nextInt() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.counter++

	return g.counter
}

// nextString returns the next request ID as a string, for MCP servers that
// require string IDs.
func (g *idGenerator) nextString() string {
	return strconv.Itoa(g.nextInt())
}
