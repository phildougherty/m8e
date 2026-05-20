package protocol

import (
	"encoding/json"
	"testing"

	"github.com/phildougherty/m8e/internal/logging"
)

// newTestHandler returns a freshly-initialized StandardMethodHandler with a
// noisy-but-discarded logger so the warning paths can be exercised without
// polluting test output.
func newTestHandler() *StandardMethodHandler {
	return NewStandardMethodHandler(
		ServerInfo{Name: "m8e-test", Version: "0.0.0-test"},
		CapabilitiesOpts{
			Tools: &ToolsOpts{ListChanged: true},
		},
		logging.NewLogger("WARNING"),
	)
}

// initializeWithVersion drives handleInitialize and returns the parsed result.
func initializeWithVersion(t *testing.T, h *StandardMethodHandler, version string) *InitializeResult {
	t.Helper()

	params := InitializeParams{
		ProtocolVersion: version,
		Capabilities:    CapabilitiesOpts{},
		ClientInfo:      ClientInfo{Name: "test-client", Version: "1.0.0"},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal initialize params: %v", err)
	}

	resp, err := h.HandleStandardMethod(MethodInitialize, raw, "req-1")
	if err != nil {
		t.Fatalf("handle initialize (%q): unexpected error %v", version, err)
	}
	if resp == nil {
		t.Fatalf("handle initialize (%q): nil response", version)
	}
	if resp.Error != nil {
		t.Fatalf("handle initialize (%q): error in response %v", version, resp.Error)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}

	return &result
}

func TestInitialize_Accepts_2024_11_05(t *testing.T) {
	h := newTestHandler()
	result := initializeWithVersion(t, h, "2024-11-05")
	if result.ProtocolVersion != MCPVersion {
		t.Errorf("expected server to respond with %q, got %q", MCPVersion, result.ProtocolVersion)
	}
	if !h.IsInitialized() {
		t.Errorf("expected handler to be marked initialized after accepting 2024-11-05")
	}
}

func TestInitialize_Accepts_2025_03_26(t *testing.T) {
	h := newTestHandler()
	result := initializeWithVersion(t, h, "2025-03-26")
	if result.ProtocolVersion != MCPVersion {
		t.Errorf("expected server to respond with %q, got %q", MCPVersion, result.ProtocolVersion)
	}
}

func TestInitialize_Accepts_2025_06_18(t *testing.T) {
	h := newTestHandler()
	result := initializeWithVersion(t, h, "2025-06-18")
	if result.ProtocolVersion != "2025-06-18" {
		t.Errorf("expected server to advertise 2025-06-18, got %q", result.ProtocolVersion)
	}
	if result.ProtocolVersion != MCPVersion {
		t.Errorf("MCPVersion drifted from 2025-06-18: got %q", result.ProtocolVersion)
	}
	// Also sanity-check the round-trip: server info echoes what we configured.
	if result.ServerInfo.Name != "m8e-test" {
		t.Errorf("expected ServerInfo.Name 'm8e-test', got %q", result.ServerInfo.Name)
	}
}

func TestInitialize_UnknownVersion_StillSucceeds(t *testing.T) {
	h := newTestHandler()
	// A made-up future revision: the server should not error, it should just
	// tell us what it speaks and log a warning.
	result := initializeWithVersion(t, h, "2099-12-31")
	if result.ProtocolVersion != MCPVersion {
		t.Errorf("expected server to fall back to %q, got %q", MCPVersion, result.ProtocolVersion)
	}
}

func TestInitialize_EmptyVersion_StillSucceeds(t *testing.T) {
	h := newTestHandler()
	result := initializeWithVersion(t, h, "")
	if result.ProtocolVersion != MCPVersion {
		t.Errorf("expected server to fall back to %q, got %q", MCPVersion, result.ProtocolVersion)
	}
}

func TestIsAcceptedMCPVersion(t *testing.T) {
	for _, v := range []string{"2024-11-05", "2025-03-26", "2025-06-18"} {
		if !IsAcceptedMCPVersion(v) {
			t.Errorf("expected %q to be an accepted version", v)
		}
	}
	for _, v := range []string{"", "2023-01-01", "garbage"} {
		if IsAcceptedMCPVersion(v) {
			t.Errorf("expected %q to NOT be accepted", v)
		}
	}
}

func TestMCPVersionIsCurrent(t *testing.T) {
	if MCPVersion != "2025-06-18" {
		t.Errorf("MCPVersion should be 2025-06-18, got %q", MCPVersion)
	}
}

func TestValidateInitializeRequest_AcceptsAllKnownVersions(t *testing.T) {
	for _, v := range []string{"2024-11-05", "2025-03-26", "2025-06-18"} {
		err := ValidateInitializeRequest(InitializeParams{
			ProtocolVersion: v,
			ClientInfo:      ClientInfo{Name: "x", Version: "y"},
		})
		if err != nil {
			t.Errorf("ValidateInitializeRequest(%q) unexpected error: %v", v, err)
		}
	}
}

func TestValidateInitializeRequest_AcceptsUnknownVersion(t *testing.T) {
	// Soft negotiation: unknown versions must NOT fail validation -- they go
	// to the warn-and-respond path in handleInitialize.
	err := ValidateInitializeRequest(InitializeParams{
		ProtocolVersion: "2099-01-01",
		ClientInfo:      ClientInfo{Name: "x", Version: "y"},
	})
	if err != nil {
		t.Errorf("ValidateInitializeRequest should not reject unknown versions; got %v", err)
	}
}

func TestValidateInitializeRequest_RejectsEmptyVersion(t *testing.T) {
	err := ValidateInitializeRequest(InitializeParams{
		ProtocolVersion: "",
		ClientInfo:      ClientInfo{Name: "x", Version: "y"},
	})
	if err == nil {
		t.Errorf("ValidateInitializeRequest should require a non-empty protocolVersion")
	}
}
