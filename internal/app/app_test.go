package app

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStderr swaps os.Stderr for a pipe, runs fn, and returns whatever was
// written to stderr during fn's execution.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stderr = orig

	return <-done
}

func TestDetectClusterMCPProxy_ReturnsFirstLocalhostEndpoint(t *testing.T) {
	got := detectClusterMCPProxy()
	want := "http://localhost:9876"
	if got != want {
		t.Errorf("detectClusterMCPProxy() = %q, want %q", got, want)
	}
}

func TestDetectClusterMCPProxy_Deterministic(t *testing.T) {
	// The helper has no inputs; it must be stable across calls so callers can
	// rely on a predictable default proxy endpoint.
	first := detectClusterMCPProxy()
	for i := 0; i < 5; i++ {
		if got := detectClusterMCPProxy(); got != first {
			t.Fatalf("detectClusterMCPProxy() call %d = %q, want %q", i, got, first)
		}
	}
}

func TestNew_WarnsWhenMCPAPIKeyUnset(t *testing.T) {
	t.Setenv("MCP_API_KEY", "")

	var app *App
	var err error
	stderr := captureStderr(t, func() {
		app, err = New()
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if app == nil {
		t.Fatal("New() returned nil app")
	}

	if !strings.Contains(stderr, "MCP_API_KEY is not set") {
		t.Errorf("stderr = %q, want warning about unset MCP_API_KEY", stderr)
	}
	if !strings.Contains(stderr, "unauthenticated") {
		t.Errorf("stderr = %q, want it to mention unauthenticated proxy calls", stderr)
	}
}

func TestNew_NoWarningWhenMCPAPIKeySet(t *testing.T) {
	t.Setenv("MCP_API_KEY", "real-secret-key")

	var app *App
	var err error
	stderr := captureStderr(t, func() {
		app, err = New()
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if app == nil {
		t.Fatal("New() returned nil app")
	}

	if strings.Contains(stderr, "MCP_API_KEY") {
		t.Errorf("stderr = %q, want no MCP_API_KEY warning when key is set", stderr)
	}
}

func TestNew_WiresAllComponents(t *testing.T) {
	t.Setenv("MCP_API_KEY", "k")

	app, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if app.AI == nil {
		t.Error("app.AI = nil, want initialized AI manager")
	}
	if app.MCP == nil {
		t.Error("app.MCP = nil, want initialized MCP client")
	}
	if app.Context == nil {
		t.Error("app.Context = nil, want initialized context manager")
	}
	if app.FileDiscovery == nil {
		t.Error("app.FileDiscovery = nil, want initialized file discovery")
	}
	if app.WorkingDir == "" {
		t.Error("app.WorkingDir = empty, want current working directory")
	}
}

func TestNew_WorkingDirMatchesGetwd(t *testing.T) {
	t.Setenv("MCP_API_KEY", "k")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	app, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if app.WorkingDir != cwd {
		t.Errorf("app.WorkingDir = %q, want %q", app.WorkingDir, cwd)
	}
}

func TestNew_AIManagerSelectsOpenRouterWhenKeyPresent(t *testing.T) {
	t.Setenv("MCP_API_KEY", "k")
	// New() defaults to the openrouter provider; it only becomes the "current"
	// provider when it reports itself available, which requires an API key.
	t.Setenv("OPENROUTER_API_KEY", "test-openrouter-key")

	app, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	provider, err := app.AI.GetCurrentProvider()
	if err != nil {
		t.Fatalf("GetCurrentProvider() error = %v", err)
	}
	if provider == nil {
		t.Fatal("GetCurrentProvider() returned nil provider")
	}
	if provider.Name() != "openrouter" {
		t.Errorf("current provider = %q, want %q", provider.Name(), "openrouter")
	}
}

func TestNew_AIManagerNoCurrentProviderWithoutKeys(t *testing.T) {
	t.Setenv("MCP_API_KEY", "k")
	// With no provider API keys set, no provider is available, so there is no
	// current provider and GetCurrentProvider reports an error.
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	app, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// The manager itself must still be wired even with no usable provider.
	if app.AI == nil {
		t.Fatal("app.AI = nil, want initialized manager even without keys")
	}
	if _, err := app.AI.GetCurrentProvider(); err == nil {
		t.Error("GetCurrentProvider() error = nil, want error when no provider keys set")
	}
}
