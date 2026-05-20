// internal/server/connection_stats.go
package server

import (
	"sync"
	"time"
)

// ConnectionStats tracks connection performance for a single upstream server.
type ConnectionStats struct {
	TotalRequests  int64
	FailedRequests int64
	TimeoutErrors  int64
	LastError      time.Time
	LastSuccess    time.Time
}

// connStatsTracker aggregates per-server connection statistics behind its own
// mutex, so request handlers record outcomes without sharing proxy locks.
type connStatsTracker struct {
	mu    sync.RWMutex
	stats map[string]*ConnectionStats
}

// newConnStatsTracker creates an empty statistics tracker.
func newConnStatsTracker() *connStatsTracker {
	return &connStatsTracker{
		stats: make(map[string]*ConnectionStats),
	}
}

// record updates the counters for a server after a request completes.
func (t *connStatsTracker) record(serverName string, success bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stats[serverName] == nil {
		t.stats[serverName] = &ConnectionStats{}
	}

	stats := t.stats[serverName]
	stats.TotalRequests++

	if success {
		stats.LastSuccess = time.Now()
	} else {
		stats.FailedRequests++
		stats.LastError = time.Now()
	}
}
