// internal/audit/global.go
package audit

import (
	"sync/atomic"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/logging"
)

// DefaultEvents is the set of audit event names the matey processes care
// about by default when an operator does not explicitly configure
// AuditConfig.Events. Keeping the default broad means a fresh install that
// just enables audit logging actually captures the events we wired
// throughout the codebase.
var DefaultEvents = []string{
	// OAuth / authentication lifecycle.
	"oauth.token.issued",
	"oauth.token.revoked",
	"oauth.user.login",
	"oauth.user.logout",

	// Proxy authentication outcomes.
	"proxy.auth.success",
	"proxy.auth.failure",

	// Per-tool privileged operations.
	"tool.execute_bash",
	"tool.apply_config",
	"tool.reload_proxy",
	"tool.matey_up",
	"tool.matey_down",

	// Server access decisions.
	"server.access.granted",
	"server.access.denied",

	// Controller reconciliation outcomes.
	"controller.reconcile.create",
	"controller.reconcile.update",
	"controller.reconcile.delete",
	"controller.reconcile.error",

	// Process / startup markers — useful smoke signals that audit logging
	// is actually wired in a running pod.
	"process.startup",
	"process.shutdown",
}

// globalLogger holds the audit logger registered by a long-running process
// (controller-manager, serve-proxy). Other packages reach for it via Global()
// so they can emit audit events without taking a direct constructor dep.
//
// The pointer is stored atomically. Reads return nil when no logger has been
// registered; callers must check for nil (or use SafeLog / SafeLogFromContext
// which already do).
var globalLogger atomic.Pointer[AuditLogger]

// SetGlobal registers logger as the process-wide audit logger. Passing nil
// clears the registration (useful in tests). It is safe to call from any
// goroutine; subsequent Global() observations are sequentially consistent
// thanks to the atomic store.
func SetGlobal(logger *AuditLogger) {
	globalLogger.Store(logger)
}

// Global returns the process-wide audit logger, or nil if SetGlobal has not
// been called. Callers must handle the nil case (or use SafeLog).
func Global() *AuditLogger {
	return globalLogger.Load()
}

// SafeLog emits an audit event via the global logger if one is registered.
// It is a no-op when no global logger exists, so call-sites can be wired
// unconditionally without breaking processes that never construct an audit
// logger (e.g. one-shot CLI commands).
func SafeLog(event, userID, clientID, ip, userAgent string, success bool, details map[string]interface{}, err error) {
	if al := Global(); al != nil {
		al.Log(event, userID, clientID, ip, userAgent, success, details, err)
	}
}

// WithDefaultEvents returns a copy of cfg with Events populated from
// DefaultEvents if the caller left it empty. The original config is not
// mutated. This lets long-running processes opt into the full default event
// list without forcing operators to enumerate it in YAML.
func WithDefaultEvents(cfg *config.AuditConfig) *config.AuditConfig {
	if cfg == nil {
		return nil
	}
	if len(cfg.Events) > 0 {
		return cfg
	}

	out := *cfg
	out.Events = append([]string{}, DefaultEvents...)

	return &out
}

// NewLoggerForProcess constructs an audit logger appropriate for a
// long-running matey process (controller-manager, serve-proxy). It centralizes
// the "what backend should we use?" decision so the two callers stay aligned:
//
//   - If cfg is non-nil and cfg.Enabled is true, the configured backend is used
//     with default events filled in when omitted.
//   - If cfg is nil or disabled, audit logging defaults to ENABLED with the
//     file backend at /var/log/matey/audit.log (configurable via
//     M8E_AUDIT_FILE_PATH). If that backend cannot initialize (no perms, etc.),
//     a memory backend is used instead and the failure is logged loudly.
//
// The returned logger is also registered as the process-wide global so that
// helper packages reach it via audit.Global().
func NewLoggerForProcess(cfg *config.AuditConfig, logger *logging.Logger) *AuditLogger {
	resolved := resolveProcessAuditConfig(cfg)

	al, err := NewAuditLoggerWithError(resolved, logger)
	if err != nil {
		if logger != nil {
			logger.Warning("AUDIT: configured backend %q failed to init: %v; falling back to memory backend so events are not silently dropped",
				resolved.Storage, err)
		}

		fallback := *resolved
		fallback.Storage = "memory"
		if fallback.Retention.MaxEntries <= 0 {
			fallback.Retention.MaxEntries = 10000
		}

		al, err = NewAuditLoggerWithError(&fallback, logger)
		if err != nil && logger != nil {
			logger.Error("AUDIT: memory fallback also failed: %v; audit logging will be disabled", err)
		}
	}

	if al != nil {
		SetGlobal(al)
	}

	return al
}

// resolveProcessAuditConfig fills in defaults for a long-running process.
// Splitting this out keeps NewLoggerForProcess testable without touching disk.
func resolveProcessAuditConfig(cfg *config.AuditConfig) *config.AuditConfig {
	if cfg != nil && cfg.Enabled {
		return WithDefaultEvents(cfg)
	}

	defaults := &config.AuditConfig{
		Enabled: true,
		Storage: "file",
		Retention: config.RetentionConfig{
			MaxEntries: 10000,
			MaxAge:     "168h", // 7 days
		},
		Events: append([]string{}, DefaultEvents...),
	}

	return defaults
}
