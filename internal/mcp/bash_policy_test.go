package mcp

import (
	"bytes"
	"strings"
	"testing"
)

// TestLoadBashPolicyTo_UnrestrictedWarns asserts that when an operator sets
// MATEY_BASH_MODE=unrestricted the construction-time log carries a loud,
// unmissable WARNING line. This is the one operational signal that prevents
// "we accidentally shipped with the filter off" from going unnoticed.
func TestLoadBashPolicyTo_UnrestrictedWarns(t *testing.T) {
	t.Setenv("MATEY_BASH_MODE", "unrestricted")
	t.Setenv("MATEY_BASH_ALLOWLIST", "")

	var buf bytes.Buffer
	p := LoadBashPolicyTo(&buf)

	if p.Mode != BashModeUnrestricted {
		t.Fatalf("expected mode unrestricted, got %q", p.Mode)
	}

	out := buf.String()
	if !strings.Contains(out, "WARNING") {
		t.Errorf("expected WARNING-level line in unrestricted mode startup log, got:\n%s", out)
	}
	if !strings.Contains(out, "unrestricted") {
		t.Errorf("expected 'unrestricted' in warning text, got:\n%s", out)
	}
	if !strings.Contains(out, "execute_bash") {
		t.Errorf("expected 'execute_bash' in warning text so operators grepping logs find it, got:\n%s", out)
	}
	// The startup line for mode + allowlist size must always be present.
	if !strings.Contains(out, "mode=unrestricted") {
		t.Errorf("expected info line with mode=unrestricted, got:\n%s", out)
	}
	if !strings.Contains(out, "allowlist_size=") {
		t.Errorf("expected info line with allowlist_size, got:\n%s", out)
	}
}

// TestLoadBashPolicyTo_AllowlistNoWarning asserts the default mode does NOT
// emit the warning (so operators are not desensitised to it).
func TestLoadBashPolicyTo_AllowlistNoWarning(t *testing.T) {
	t.Setenv("MATEY_BASH_MODE", "allowlist")
	t.Setenv("MATEY_BASH_ALLOWLIST", "")

	var buf bytes.Buffer
	p := LoadBashPolicyTo(&buf)

	if p.Mode != BashModeAllowlist {
		t.Fatalf("expected mode allowlist, got %q", p.Mode)
	}

	out := buf.String()
	if strings.Contains(out, "WARNING") {
		t.Errorf("did not expect WARNING line in allowlist mode startup log, got:\n%s", out)
	}
	if !strings.Contains(out, "mode=allowlist") {
		t.Errorf("expected info line with mode=allowlist, got:\n%s", out)
	}
}

// TestLoadBashPolicyTo_DisabledNoWarning: disabled mode is the most-restrictive
// option; an operator who picked it explicitly does not need to be shouted at.
func TestLoadBashPolicyTo_DisabledNoWarning(t *testing.T) {
	t.Setenv("MATEY_BASH_MODE", "disabled")
	t.Setenv("MATEY_BASH_ALLOWLIST", "")

	var buf bytes.Buffer
	p := LoadBashPolicyTo(&buf)

	if p.Mode != BashModeDisabled {
		t.Fatalf("expected mode disabled, got %q", p.Mode)
	}

	out := buf.String()
	if strings.Contains(out, "WARNING") {
		t.Errorf("did not expect WARNING line in disabled mode startup log, got:\n%s", out)
	}
	if !strings.Contains(out, "mode=disabled") {
		t.Errorf("expected info line with mode=disabled, got:\n%s", out)
	}
}

// TestLoadBashPolicyTo_UnknownModeFallsBackToAllowlist proves that a typo'd
// MATEY_BASH_MODE does not accidentally land in unrestricted mode (the safe
// default is allowlist).
func TestLoadBashPolicyTo_UnknownModeFallsBackToAllowlist(t *testing.T) {
	t.Setenv("MATEY_BASH_MODE", "yolo")
	t.Setenv("MATEY_BASH_ALLOWLIST", "")

	var buf bytes.Buffer
	p := LoadBashPolicyTo(&buf)

	if p.Mode != BashModeAllowlist {
		t.Fatalf("expected fallback to allowlist for unknown mode, got %q", p.Mode)
	}
	if strings.Contains(buf.String(), "WARNING") {
		t.Errorf("did not expect WARNING line for fallback to allowlist, got:\n%s", buf.String())
	}
}

// TestLoadBashPolicyTo_AllowlistExtra confirms MATEY_BASH_ALLOWLIST extras are
// merged into the allowlist and counted in the startup log line.
func TestLoadBashPolicyTo_AllowlistExtra(t *testing.T) {
	t.Setenv("MATEY_BASH_MODE", "allowlist")
	t.Setenv("MATEY_BASH_ALLOWLIST", "rsync,go")

	var buf bytes.Buffer
	p := LoadBashPolicyTo(&buf)

	if !p.Allowlist["rsync"] || !p.Allowlist["go"] {
		t.Errorf("expected rsync and go to be added to allowlist, got %v", p.Allowlist)
	}
	if !strings.Contains(buf.String(), "allowlist_size=") {
		t.Errorf("expected allowlist_size= in startup line, got:\n%s", buf.String())
	}
}
