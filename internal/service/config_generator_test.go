// internal/service/config_generator_test.go
package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/phildougherty/m8e/internal/config"
)

func sampleConfig() *config.ComposeConfig {
	return &config.ComposeConfig{
		ProxyAuth: config.ProxyAuthConfig{APIKey: "test-key"},
		Servers: map[string]config.ServerConfig{
			"filesystem": {
				Protocol:     "http",
				HttpPort:     8080,
				Capabilities: []string{"tools", "resources"},
				Command:      "mcp-filesystem",
				Args:         []string{"--root", "/data"},
			},
		},
	}
}

func TestConfigGenerator_ClaudeCode(t *testing.T) {
	dir := t.TempDir()
	gen := NewConfigGenerator(sampleConfig(), dir, nil)

	if err := gen.Generate("claude-code", nil); err != nil {
		t.Fatalf("Generate(claude-code): %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}

	var parsed struct {
		McpServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal generated config: %v", err)
	}

	srv, ok := parsed.McpServers["filesystem"]
	if !ok {
		t.Fatal("expected filesystem server in generated config")
	}
	if srv.Type != "http" {
		t.Errorf("expected type http, got %q", srv.Type)
	}
	if srv.Headers["Authorization"] != "Bearer test-key" {
		t.Errorf("expected Bearer test-key auth header, got %q", srv.Headers["Authorization"])
	}
}

func TestConfigGenerator_Gemini(t *testing.T) {
	dir := t.TempDir()
	gen := NewConfigGenerator(sampleConfig(), dir, nil)

	if err := gen.Generate("gemini", nil); err != nil {
		t.Fatalf("Generate(gemini): %v", err)
	}

	path := filepath.Join(dir, ".gemini", "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gemini settings: %v", err)
	}

	var parsed struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal gemini config: %v", err)
	}
	if _, ok := parsed.McpServers["filesystem"]; !ok {
		t.Error("expected filesystem server in gemini config")
	}
	// Gemini config always appends the internal matey server.
	if _, ok := parsed.McpServers["matey"]; !ok {
		t.Error("expected matey server appended to gemini config")
	}
}

func TestConfigGenerator_All(t *testing.T) {
	dir := t.TempDir()
	gen := NewConfigGenerator(sampleConfig(), dir, nil)

	if err := gen.Generate("all", nil); err != nil {
		t.Fatalf("Generate(all): %v", err)
	}

	for _, f := range []string{
		"claude-desktop-servers.json",
		".mcp.json",
		filepath.Join(".gemini", "settings.json"),
		"anthropic_mcp_example.py",
		"openai_mcp_example.js",
		"package.json",
		".opencode.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("expected %s to be generated: %v", f, err)
		}
	}
}

func TestConfigGenerator_UnknownType(t *testing.T) {
	gen := NewConfigGenerator(sampleConfig(), t.TempDir(), nil)
	if err := gen.Generate("nonsense", nil); err == nil {
		t.Fatal("expected error for unknown client type")
	}
}

func TestConfigGenerator_ResolveAPIKeyWarnsOnPlaceholder(t *testing.T) {
	cfg := sampleConfig()
	cfg.ProxyAuth.APIKey = ""
	t.Setenv("MCP_API_KEY", "")

	var warned string
	gen := NewConfigGenerator(cfg, t.TempDir(), func(msg string) { warned = msg })

	key := gen.resolveProxyAPIKey()
	if key != ProxyAPIKeyPlaceholder {
		t.Errorf("expected placeholder key, got %q", key)
	}
	if warned == "" {
		t.Error("expected a warning when no API key is configured")
	}
}

func TestConfigGenerator_ResolveAPIKeyFromEnv(t *testing.T) {
	cfg := sampleConfig()
	cfg.ProxyAuth.APIKey = ""
	t.Setenv("MCP_API_KEY", "env-key")

	gen := NewConfigGenerator(cfg, t.TempDir(), func(string) {
		t.Error("warn should not be called when env key is present")
	})
	if got := gen.resolveProxyAPIKey(); got != "env-key" {
		t.Errorf("expected env-key, got %q", got)
	}
}
