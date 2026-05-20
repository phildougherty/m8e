package audit

import (
	"sync"
	"time"
)

// memoryBackend keeps audit entries in a bounded in-memory ring. It is the
// only backend that is acceptable when explicitly configured; it is never
// used as a silent fallback for a failed file/database backend.
type memoryBackend struct {
	mu         sync.RWMutex
	entries    []AuditEntry
	maxEntries int
}

func newMemoryBackend(maxEntries int) *memoryBackend {
	if maxEntries <= 0 {
		maxEntries = 1
	}

	return &memoryBackend{
		entries:    make([]AuditEntry, 0),
		maxEntries: maxEntries,
	}
}

func (b *memoryBackend) Store(entry *AuditEntry) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.entries = append(b.entries, *entry)
	if len(b.entries) > b.maxEntries {
		b.entries = b.entries[len(b.entries)-b.maxEntries:]
	}

	return nil
}

func (b *memoryBackend) Query(limit, offset int, filter *AuditFilter) ([]AuditEntry, int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	filtered := make([]AuditEntry, 0, len(b.entries))
	// Iterate in reverse so results are newest-first.
	for i := len(b.entries) - 1; i >= 0; i-- {
		if matchesFilter(b.entries[i], filter) {
			filtered = append(filtered, b.entries[i])
		}
	}

	return paginate(filtered, limit, offset)
}

func (b *memoryBackend) Stats() (AuditStats, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return computeStats(b.entries), nil
}

func (b *memoryBackend) Cleanup(cutoff time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	kept := b.entries[:0]
	for _, e := range b.entries {
		if e.Timestamp.After(cutoff) {
			kept = append(kept, e)
		}
	}
	b.entries = kept

	return nil
}

func (b *memoryBackend) Close() error {
	return nil
}
