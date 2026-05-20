// internal/audit/global_test.go
package audit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/phildougherty/m8e/internal/config"
)

// TestSetGlobalAndSafeLog covers the global accessor used by the
// controller-manager / serve-proxy commands to register a process-wide audit
// logger so that other packages can emit events via audit.SafeLog without a
// direct constructor dep.
func TestSetGlobalAndSafeLog(t *testing.T) {
	// Reset global at test exit so other tests aren't poisoned.
	prev := Global()
	t.Cleanup(func() { SetGlobal(prev) })

	if Global() != nil {
		SetGlobal(nil)
	}

	// SafeLog is a no-op before SetGlobal: it must not panic.
	SafeLog("oauth.user.login", "u", "c", "ip", "ua", true, nil, nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	t.Setenv("M8E_AUDIT_FILE_PATH", path)

	cfg := &config.AuditConfig{
		Enabled:   true,
		Storage:   "file",
		Events:    []string{"proxy.auth.failure"},
		Retention: config.RetentionConfig{MaxEntries: 100, MaxAge: "24h"},
	}
	al, err := NewAuditLoggerWithError(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewAuditLoggerWithError: %v", err)
	}
	defer func() { _ = al.Shutdown() }()

	SetGlobal(al)
	if Global() != al {
		t.Fatalf("Global() did not return the registered logger")
	}

	// Synthetic event hits the file backend.
	SafeLog("proxy.auth.failure", "", "", "10.0.0.1", "curl", false, map[string]interface{}{
		"path": "/api/discovery",
	}, nil)

	got, total, err := al.GetEntries(10, 0, nil)
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if got[0].Event != "proxy.auth.failure" || got[0].IP != "10.0.0.1" {
		t.Errorf("unexpected entry: %+v", got[0])
	}
	if got[0].Success {
		t.Error("auth.failure entry should be marked unsuccessful")
	}
}

// TestNewLoggerForProcess_DefaultsToFileBackend mirrors the wiring the
// controller-manager / serve-proxy commands use: if no AuditConfig is
// supplied, a file backend at M8E_AUDIT_FILE_PATH is constructed and
// registered as the global. A synthetic event must land in that file.
func TestNewLoggerForProcess_DefaultsToFileBackend(t *testing.T) {
	prev := Global()
	t.Cleanup(func() { SetGlobal(prev) })

	dir := t.TempDir()
	path := filepath.Join(dir, "process.audit.log")
	t.Setenv("M8E_AUDIT_FILE_PATH", path)

	al := NewLoggerForProcess(nil, testLogger())
	if al == nil {
		t.Fatal("NewLoggerForProcess returned nil")
	}
	defer func() { _ = al.Shutdown() }()

	if Global() != al {
		t.Fatal("NewLoggerForProcess must register the logger as the global")
	}

	// process.startup is one of the DefaultEvents, so this event is recorded.
	al.Log("process.startup", "", "", "", "", true, map[string]interface{}{
		"process": "controller-manager",
	}, nil)

	got, total, err := al.GetEntries(10, 0, nil)
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if got[0].Event != "process.startup" {
		t.Errorf("got event %q, want process.startup", got[0].Event)
	}
	if got[0].Details["process"] != "controller-manager" {
		t.Errorf("missing details: %+v", got[0].Details)
	}
}

// TestNewLoggerForProcess_HonorsExplicitConfig ensures an operator-supplied
// AuditConfig is used as-is (events list, storage choice) when present.
func TestNewLoggerForProcess_HonorsExplicitConfig(t *testing.T) {
	prev := Global()
	t.Cleanup(func() { SetGlobal(prev) })

	cfg := &config.AuditConfig{
		Enabled:   true,
		Storage:   "memory",
		Events:    []string{"oauth.user.login"},
		Retention: config.RetentionConfig{MaxEntries: 10, MaxAge: "1h"},
	}
	al := NewLoggerForProcess(cfg, testLogger())
	if al == nil {
		t.Fatal("NewLoggerForProcess returned nil")
	}
	defer func() { _ = al.Shutdown() }()

	// process.startup is NOT in the explicit events list, so it must be
	// filtered out.
	al.Log("process.startup", "", "", "", "", true, nil, nil)
	al.Log("oauth.user.login", "alice", "", "", "", true, nil, nil)

	_, total, err := al.GetEntries(10, 0, nil)
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if total != 1 {
		t.Fatalf("explicit events list ignored: total = %d, want 1", total)
	}
}

// TestWithDefaultEvents_FillsWhenEmpty validates the helper that opts a
// nil/empty Events list into the package's curated DefaultEvents.
func TestWithDefaultEvents_FillsWhenEmpty(t *testing.T) {
	cfg := &config.AuditConfig{Enabled: true}
	out := WithDefaultEvents(cfg)
	if len(out.Events) == 0 {
		t.Fatal("WithDefaultEvents should populate Events when empty")
	}
	found := false
	for _, e := range out.Events {
		if e == "proxy.auth.failure" {
			found = true
			break
		}
	}
	if !found {
		t.Error("DefaultEvents must include proxy.auth.failure")
	}
	// Original must not be mutated.
	if len(cfg.Events) != 0 {
		t.Error("WithDefaultEvents mutated the input config")
	}

	// Non-empty Events are preserved verbatim.
	cfg2 := &config.AuditConfig{Events: []string{"custom.event"}}
	out2 := WithDefaultEvents(cfg2)
	if strings.Join(out2.Events, ",") != "custom.event" {
		t.Errorf("explicit events list mutated: %v", out2.Events)
	}
}
