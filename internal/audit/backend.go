package audit

import (
	"os"
	"strconv"
	"time"
)

// storageBackend abstracts the persistence layer for audit entries. Every
// backend must be safe for concurrent use: AuditLogger.Log is invoked from
// many goroutines without external synchronization around the backend.
type storageBackend interface {
	// Store persists a single audit entry. A returned error means the entry
	// was NOT durably recorded and must be surfaced loudly by the caller.
	Store(entry *AuditEntry) error
	// Query returns entries matching filter, ordered newest-first, with
	// pagination applied. total is the count before pagination.
	Query(limit, offset int, filter *AuditFilter) (entries []AuditEntry, total int, err error)
	// Stats returns aggregate statistics across all retained entries.
	Stats() (AuditStats, error)
	// Cleanup removes entries older than cutoff. Backends that rotate or
	// expire data by other means may treat this as a no-op.
	Cleanup(cutoff time.Time) error
	// Close releases any resources (file handles, DB connections).
	Close() error
}

// backendConfig carries the resolved settings a backend needs. Because the
// audit package may not modify internal/config, file path / rotation / DSN
// settings are sourced from environment variables with sane defaults. This
// keeps the public config.AuditConfig schema untouched while still making
// the file and database backends fully configurable.
type backendConfig struct {
	// File backend
	filePath    string // path to the active audit log file
	maxFileSize int64  // rotate when the active file exceeds this many bytes
	maxBackups  int    // number of rotated files to retain

	// Database backend
	databaseURL string // PostgreSQL connection string
	tableName   string // audit table name

	// Shared
	maxEntries int
}

const (
	defaultAuditFilePath    = "/var/log/m8e/audit.log"
	defaultAuditMaxFileSize = 50 * 1024 * 1024 // 50 MiB
	defaultAuditMaxBackups  = 5
	defaultAuditTableName   = "audit_log"
)

// resolveBackendConfig builds a backendConfig from the audit config plus
// environment overrides. Environment variables are used because the shared
// config schema cannot be extended from this package.
func resolveBackendConfig(maxEntries int) backendConfig {
	bc := backendConfig{
		filePath:    envOr("M8E_AUDIT_FILE_PATH", defaultAuditFilePath),
		maxFileSize: defaultAuditMaxFileSize,
		maxBackups:  defaultAuditMaxBackups,
		databaseURL: os.Getenv("M8E_AUDIT_DATABASE_URL"),
		tableName:   envOr("M8E_AUDIT_TABLE_NAME", defaultAuditTableName),
		maxEntries:  maxEntries,
	}

	if v := os.Getenv("M8E_AUDIT_MAX_FILE_SIZE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			bc.maxFileSize = n
		}
	}
	if v := os.Getenv("M8E_AUDIT_MAX_BACKUPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			bc.maxBackups = n
		}
	}
	// The memory service exposes its DSN as DATABASE_URL; reuse it as a
	// fallback so the audit DB backend follows the same convention.
	if bc.databaseURL == "" {
		bc.databaseURL = os.Getenv("DATABASE_URL")
	}

	return bc
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}
