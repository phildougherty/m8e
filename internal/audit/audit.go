package audit

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/logging"
)

const (
	// Default audit retention period in days
	DefaultAuditRetentionDays = 7
	// Default audit statistics timeout in seconds
	DefaultAuditStatsTimeout = 5
	// Percentage multiplier for success rate calculation
	PercentageMultiplier = 100
)

var (
	// ErrAuditShutdownTimeout is returned when audit logger shutdown times out.
	ErrAuditShutdownTimeout = errors.New("audit logger shutdown timeout")
)

type AuditLogger struct {
	enabled    bool
	storage    string
	maxEntries int
	maxAge     time.Duration
	events     map[string]bool
	backend    storageBackend
	logger     *logging.Logger
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

type AuditEntry struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Event     string                 `json:"event"`
	UserID    string                 `json:"user_id,omitempty"`
	ClientID  string                 `json:"client_id,omitempty"`
	IP        string                 `json:"ip_address,omitempty"`
	UserAgent string                 `json:"user_agent,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Success   bool                   `json:"success"`
	Error     string                 `json:"error,omitempty"`
}

// ErrUnknownStorageBackend is returned when AuditConfig.Storage names a
// backend the audit package does not implement.
var ErrUnknownStorageBackend = errors.New("audit: unknown storage backend")

// NewAuditLogger constructs an AuditLogger. It preserves the original
// signature for existing callers: if the configured backend fails to
// initialize, the failure is logged loudly at error level and a disabled
// logger is returned — the audit subsystem is NEVER silently downgraded to
// in-memory storage. Callers that need to react to initialization failure
// should use NewAuditLoggerWithError instead.
func NewAuditLogger(auditConfig *config.AuditConfig, logger *logging.Logger) *AuditLogger {
	al, err := NewAuditLoggerWithError(auditConfig, logger)
	if err != nil {
		if logger != nil {
			logger.Error("AUDIT: failed to initialize '%s' storage backend: %v; "+
				"audit logging is DISABLED (no silent fallback)", auditConfig.Storage, err)
		}

		// Return a disabled logger so callers do not panic on a nil pointer,
		// but make absolutely sure nothing is recorded under a false pretense.
		return &AuditLogger{
			enabled: false,
			storage: auditConfig.Storage,
			logger:  logger,
			stopCh:  make(chan struct{}),
		}
	}

	return al
}

// NewAuditLoggerWithError constructs an AuditLogger and returns an error if
// the configured storage backend cannot be initialized. Backend init failure
// is fatal for the audit subsystem: an audit log that silently degrades is
// worse than none at all.
func NewAuditLoggerWithError(auditConfig *config.AuditConfig, logger *logging.Logger) (*AuditLogger, error) {
	maxAge, _ := time.ParseDuration(auditConfig.Retention.MaxAge)
	if maxAge == 0 {
		maxAge = DefaultAuditRetentionDays * constants.HoursInDay * time.Hour // Default 7 days
	}

	events := make(map[string]bool)
	for _, event := range auditConfig.Events {
		events[event] = true
	}

	maxEntries := auditConfig.Retention.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 1
	}

	al := &AuditLogger{
		enabled:    auditConfig.Enabled,
		storage:    auditConfig.Storage,
		maxEntries: maxEntries,
		maxAge:     maxAge,
		events:     events,
		logger:     logger,
		stopCh:     make(chan struct{}),
	}

	backend, err := newBackend(auditConfig.Storage, resolveBackendConfig(maxEntries), logger)
	if err != nil {
		return nil, err
	}
	al.backend = backend

	// Start cleanup routine with proper resource management
	al.wg.Add(1)
	go al.cleanupOldEntries()

	return al, nil
}

// newBackend builds the storage backend named by storage. An empty string
// defaults to memory for backward compatibility. Unknown names are a hard
// error rather than a silent memory fallback.
func newBackend(storage string, cfg backendConfig, logger *logging.Logger) (storageBackend, error) {
	switch storage {
	case "", "memory":
		return newMemoryBackend(cfg.maxEntries), nil
	case "file":
		return newFileBackend(cfg)
	case "database":
		return newDatabaseBackend(cfg, logger)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownStorageBackend, storage)
	}
}

func (al *AuditLogger) Log(event, userID, clientID, ip, userAgent string, success bool, details map[string]interface{}, err error) {
	if !al.enabled {

		return
	}

	// Check if this event type should be logged
	if !al.events[event] {

		return
	}

	entry := AuditEntry{
		ID:        generateAuditID(),
		Timestamp: time.Now(),
		Event:     event,
		UserID:    userID,
		ClientID:  clientID,
		IP:        ip,
		UserAgent: userAgent,
		Success:   success,
		Details:   details,
	}

	if err != nil {
		entry.Error = err.Error()
	}

	al.storeEntry(&entry)

	// Also log to standard logger
	level := "info"
	if !success {
		level = "warn"
	}

	// Fix: Use the correct method name
	if level == "info" {
		al.logger.Info("AUDIT: %s - User: %s, Client: %s, Success: %v", event, userID, clientID, success)
	} else {
		al.logger.Warning("AUDIT: %s - User: %s, Client: %s, Success: %v", event, userID, clientID, success)
	}
}

func (al *AuditLogger) storeEntry(entry *AuditEntry) {
	if al.backend == nil {
		// Should not happen for an enabled logger, but never pretend success.
		if al.logger != nil {
			al.logger.Error("AUDIT: no storage backend configured, entry %s dropped", entry.ID)
		}

		return
	}

	if err := al.backend.Store(entry); err != nil {
		// A failed durable write is loud: silently dropping audit events
		// turns "we have audit logs" into a lie.
		if al.logger != nil {
			al.logger.Error("AUDIT: failed to persist entry %s (event=%s): %v",
				entry.ID, entry.Event, err)
		}
	}
}

// GetEntries returns audit entries matching filter, newest-first, paginated.
// The query is delegated to the configured storage backend.
func (al *AuditLogger) GetEntries(limit int, offset int, filter *AuditFilter) ([]AuditEntry, int, error) {
	if al.backend == nil {
		return nil, 0, nil
	}

	return al.backend.Query(limit, offset, filter)
}

type AuditFilter struct {
	Event     string    `json:"event,omitempty"`
	UserID    string    `json:"user_id,omitempty"`
	ClientID  string    `json:"client_id,omitempty"`
	Success   *bool     `json:"success,omitempty"`
	StartTime time.Time `json:"start_time,omitempty"`
	EndTime   time.Time `json:"end_time,omitempty"`
}

func (al *AuditLogger) cleanupOldEntries() {
	defer al.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-al.stopCh:
			al.logger.Debug("Audit logger cleanup goroutine stopping")

			return
		case <-ticker.C:
			cutoff := time.Now().Add(-al.maxAge)
			if al.backend != nil {
				if err := al.backend.Cleanup(cutoff); err != nil {
					al.logger.Error("AUDIT: failed to clean up entries older than %s: %v", cutoff, err)
				}
			}
		}
	}
}

// Shutdown gracefully stops the audit logger
func (al *AuditLogger) Shutdown() error {
	if al.stopCh != nil {
		close(al.stopCh)
	}

	// Wait for cleanup goroutine to finish with timeout
	done := make(chan struct{})
	go func() {
		al.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		al.logger.Debug("Audit logger shutdown completed")
	case <-time.After(DefaultAuditStatsTimeout * time.Second):
		al.logger.Warning("Audit logger shutdown timeout")

		return ErrAuditShutdownTimeout
	}

	// Flush and release backend resources (file handles, DB connections,
	// retry buffers) only after the cleanup goroutine has stopped.
	if al.backend != nil {
		if err := al.backend.Close(); err != nil {
			al.logger.Error("AUDIT: error closing storage backend: %v", err)

			return err
		}
	}

	return nil
}

// GetStats returns aggregate statistics from the configured storage backend.
func (al *AuditLogger) GetStats() AuditStats {
	if al.backend == nil {
		return AuditStats{EventCounts: make(map[string]int)}
	}

	stats, err := al.backend.Stats()
	if err != nil {
		if al.logger != nil {
			al.logger.Error("AUDIT: failed to compute stats: %v", err)
		}

		return AuditStats{EventCounts: make(map[string]int)}
	}

	return stats
}

type AuditStats struct {
	TotalEntries int            `json:"total_entries"`
	EventCounts  map[string]int `json:"event_counts"`
	SuccessRate  float64        `json:"success_rate"`
}

func generateAuditID() string {

	return fmt.Sprintf("audit_%d", time.Now().UnixNano())
}

// Helper methods for common audit events
func (al *AuditLogger) LogOAuthTokenIssued(userID, clientID, ip, userAgent string, tokenType string, success bool, err error) {
	details := map[string]interface{}{
		"token_type": tokenType,
	}
	al.Log("oauth.token.issued", userID, clientID, ip, userAgent, success, details, err)
}

func (al *AuditLogger) LogOAuthTokenRevoked(userID, clientID, ip, userAgent string, tokenType string, success bool, err error) {
	details := map[string]interface{}{
		"token_type": tokenType,
	}
	al.Log("oauth.token.revoked", userID, clientID, ip, userAgent, success, details, err)
}

func (al *AuditLogger) LogServerAccess(userID, clientID, ip, userAgent string, serverName, scope string, success bool, err error) {
	details := map[string]interface{}{
		"server_name": serverName,
		"scope":       scope,
	}
	event := "server.access.granted"
	if !success {
		event = "server.access.denied"
	}
	al.Log(event, userID, clientID, ip, userAgent, success, details, err)
}

func (al *AuditLogger) LogUserLogin(userID, ip, userAgent string, success bool, err error) {
	al.Log("oauth.user.login", userID, "", ip, userAgent, success, nil, err)
}
