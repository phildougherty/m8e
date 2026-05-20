// internal/compose/mcpserver_test.go
package compose

import (
	"os"
	"testing"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/logging"
)

// baseComposer returns a composer with no k8s client (these tests only exercise
// the pure convertServerConfigToMCPServer transform, which never touches it).
func baseComposer(cfg *config.ComposeConfig, namespace string) *K8sComposer {
	if namespace == "" {
		namespace = constants.MateyNamespace
	}
	return &K8sComposer{config: cfg, namespace: namespace, logger: logging.NewLogger("error")}
}

func TestConvertServerConfigToMCPServer_ImageHandling(t *testing.T) {
	tests := []struct {
		name      string
		registry  string
		server    config.ServerConfig
		wantImage string
		wantNil   bool
	}{
		{
			name:      "standard docker hub image with tag is used verbatim",
			registry:  "registry.example.com",
			server:    config.ServerConfig{Image: "postgres:15-alpine"},
			wantImage: "postgres:15-alpine",
		},
		{
			name:      "custom image gets registry prefix",
			registry:  "registry.example.com",
			server:    config.ServerConfig{Image: "my-mcp-server"},
			wantImage: "registry.example.com/my-mcp-server",
		},
		{
			name:      "custom image without registry configured stays bare",
			registry:  "",
			server:    config.ServerConfig{Image: "my-mcp-server"},
			wantImage: "my-mcp-server",
		},
		{
			name:      "build context derives image from registry url and name",
			registry:  "registry.example.com",
			server:    config.ServerConfig{Build: config.BuildConfig{Context: "./svc"}},
			wantImage: "registry.example.com/build-server:latest",
		},
		{
			name:    "no image and no build context yields nil (skipped)",
			server:  config.ServerConfig{Command: "echo"},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ComposeConfig{}
			cfg.Registry.URL = tt.registry
			c := baseComposer(cfg, "matey")

			name := "test-server"
			if tt.server.Build.Context != "" {
				name = "build-server"
			}
			got := c.convertServerConfigToMCPServer(name, tt.server)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil MCPServer, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil MCPServer, got nil")
			}
			if got.Spec.Image != tt.wantImage {
				t.Errorf("Spec.Image = %q, want %q", got.Spec.Image, tt.wantImage)
			}
		})
	}
}

func TestConvertServerConfigToMCPServer_MetadataAndLabels(t *testing.T) {
	c := baseComposer(&config.ComposeConfig{}, "custom-ns")
	got := c.convertServerConfigToMCPServer("my-server", config.ServerConfig{Image: "nginx:latest"})
	if got == nil {
		t.Fatal("expected non-nil MCPServer")
	}
	if got.Name != "my-server" {
		t.Errorf("Name = %q, want my-server", got.Name)
	}
	if got.Namespace != "custom-ns" {
		t.Errorf("Namespace = %q, want custom-ns", got.Namespace)
	}
	wantLabels := map[string]string{
		"app.kubernetes.io/name":       "mcp-server",
		"app.kubernetes.io/instance":   "my-server",
		"app.kubernetes.io/component":  "mcp-server",
		"app.kubernetes.io/managed-by": "matey",
		"mcp.matey.ai/role":            "server",
	}
	for k, v := range wantLabels {
		if got.Labels[k] != v {
			t.Errorf("label %q = %q, want %q", k, got.Labels[k], v)
		}
	}
}

func TestConvertServerConfigToMCPServer_PortAndProtocol(t *testing.T) {
	tests := []struct {
		name         string
		server       config.ServerConfig
		wantHTTPPort int32
		wantProtocol string
	}{
		{
			name:         "explicit http_port sets port and keeps protocol",
			server:       config.ServerConfig{Image: "x", HttpPort: 8007, Protocol: "http"},
			wantHTTPPort: 8007,
			wantProtocol: "http",
		},
		{
			name:         "stdio_hoster_port forces http protocol",
			server:       config.ServerConfig{Image: "x", StdioHosterPort: 9001},
			wantHTTPPort: 9001,
			wantProtocol: "http",
		},
		{
			name:         "http protocol with no port defaults to 8080",
			server:       config.ServerConfig{Image: "x", Protocol: "http"},
			wantHTTPPort: 8080,
			wantProtocol: "http",
		},
		{
			name:         "sse protocol with no port defaults to 8080",
			server:       config.ServerConfig{Image: "x", Protocol: "sse"},
			wantHTTPPort: 8080,
			wantProtocol: "sse",
		},
		{
			name:         "port extracted from ports array host:container form",
			server:       config.ServerConfig{Image: "x", Ports: []string{"8007:8007"}},
			wantHTTPPort: 8007,
			wantProtocol: "http",
		},
		{
			name:         "no port and no protocol defaults to stdio",
			server:       config.ServerConfig{Image: "x"},
			wantHTTPPort: 0,
			wantProtocol: "stdio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := baseComposer(&config.ComposeConfig{}, "matey")
			got := c.convertServerConfigToMCPServer("s", tt.server)
			if got == nil {
				t.Fatal("expected non-nil MCPServer")
			}
			if got.Spec.HttpPort != tt.wantHTTPPort {
				t.Errorf("Spec.HttpPort = %d, want %d", got.Spec.HttpPort, tt.wantHTTPPort)
			}
			if got.Spec.Protocol != tt.wantProtocol {
				t.Errorf("Spec.Protocol = %q, want %q", got.Spec.Protocol, tt.wantProtocol)
			}
		})
	}
}

func TestConvertServerConfigToMCPServer_CommandArgsEnvCapabilities(t *testing.T) {
	c := baseComposer(&config.ComposeConfig{}, "matey")
	server := config.ServerConfig{
		Image:        "x",
		Command:      "/bin/server",
		Args:         []string{"--flag", "value"},
		Env:          map[string]string{"FOO": "bar"},
		Capabilities: []string{"tools", "resources"},
		Protocol:     "http",
	}
	got := c.convertServerConfigToMCPServer("s", server)
	if got == nil {
		t.Fatal("expected non-nil MCPServer")
	}
	if len(got.Spec.Command) != 1 || got.Spec.Command[0] != "/bin/server" {
		t.Errorf("Spec.Command = %v, want [/bin/server]", got.Spec.Command)
	}
	if len(got.Spec.Args) != 2 || got.Spec.Args[1] != "value" {
		t.Errorf("Spec.Args = %v, want [--flag value]", got.Spec.Args)
	}
	if got.Spec.Env["FOO"] != "bar" {
		t.Errorf("Spec.Env[FOO] = %q, want bar", got.Spec.Env["FOO"])
	}
	if len(got.Spec.Capabilities) != 2 {
		t.Errorf("Spec.Capabilities = %v, want 2 entries", got.Spec.Capabilities)
	}

	// Empty command should not produce a Command slice.
	gotEmpty := c.convertServerConfigToMCPServer("s", config.ServerConfig{Image: "x"})
	if gotEmpty.Spec.Command != nil {
		t.Errorf("Spec.Command = %v, want nil for empty command", gotEmpty.Spec.Command)
	}
}

func TestConvertServerConfigToMCPServer_Security(t *testing.T) {
	c := baseComposer(&config.ComposeConfig{}, "matey")

	server := config.ServerConfig{
		Image:      "x",
		Privileged: false,
		ReadOnly:   true,
		CapAdd:     []string{"NET_ADMIN"},
		CapDrop:    []string{"ALL"},
	}
	server.Security.AllowHostMounts = []string{"/data"}
	server.Security.TrustedImage = true
	server.Security.NoNewPrivileges = true

	got := c.convertServerConfigToMCPServer("s", server)
	if got == nil || got.Spec.Security == nil {
		t.Fatal("expected non-nil MCPServer with Security")
	}
	sec := got.Spec.Security
	if sec.AllowPrivilegedOps {
		t.Error("AllowPrivilegedOps should be false")
	}
	if !sec.ReadOnlyRootFS {
		t.Error("ReadOnlyRootFS should be true")
	}
	if !sec.NoNewPrivileges {
		t.Error("NoNewPrivileges should be true")
	}
	if !sec.TrustedImage {
		t.Error("TrustedImage should be true")
	}
	if len(sec.CapDrop) != 1 || sec.CapDrop[0] != "ALL" {
		t.Errorf("CapDrop = %v, want [ALL]", sec.CapDrop)
	}
	if len(sec.CapAdd) != 1 || sec.CapAdd[0] != "NET_ADMIN" {
		t.Errorf("CapAdd = %v, want [NET_ADMIN]", sec.CapAdd)
	}
	if len(sec.AllowHostMounts) != 1 || sec.AllowHostMounts[0] != "/data" {
		t.Errorf("AllowHostMounts = %v, want [/data]", sec.AllowHostMounts)
	}
}

func TestConvertServerConfigToMCPServer_PrivilegedOverride(t *testing.T) {
	c := baseComposer(&config.ComposeConfig{}, "matey")
	// Privileged false at top level, but Security.AllowPrivilegedOps true must win.
	server := config.ServerConfig{Image: "x", Privileged: false}
	server.Security.AllowPrivilegedOps = true
	got := c.convertServerConfigToMCPServer("s", server)
	if !got.Spec.Security.AllowPrivilegedOps {
		t.Error("Security.AllowPrivilegedOps override should set AllowPrivilegedOps true")
	}
}

func TestConvertServerConfigToMCPServer_UserParsing(t *testing.T) {
	tests := []struct {
		name      string
		user      string
		wantUID   *int64
		wantGID   *int64
		wantUnset bool
	}{
		{name: "root maps to uid/gid 0", user: "root", wantUID: ptrInt64(0), wantGID: ptrInt64(0)},
		{name: "uid:gid form parses both", user: "1000:2000", wantUID: ptrInt64(1000), wantGID: ptrInt64(2000)},
		{name: "uid only parses uid, leaves gid nil", user: "1500", wantUID: ptrInt64(1500), wantGID: nil},
		{name: "empty user leaves both nil", user: "", wantUnset: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := baseComposer(&config.ComposeConfig{}, "matey")
			got := c.convertServerConfigToMCPServer("s", config.ServerConfig{Image: "x", User: tt.user})
			sec := got.Spec.Security
			if tt.wantUnset {
				if sec.RunAsUser != nil || sec.RunAsGroup != nil {
					t.Errorf("expected RunAsUser/RunAsGroup nil, got %v/%v", sec.RunAsUser, sec.RunAsGroup)
				}
				return
			}
			if !int64PtrEqual(sec.RunAsUser, tt.wantUID) {
				t.Errorf("RunAsUser = %v, want %v", derefInt64(sec.RunAsUser), derefInt64(tt.wantUID))
			}
			if !int64PtrEqual(sec.RunAsGroup, tt.wantGID) {
				t.Errorf("RunAsGroup = %v, want %v", derefInt64(sec.RunAsGroup), derefInt64(tt.wantGID))
			}
		})
	}
}

func TestConvertServerConfigToMCPServer_ResourceLimits(t *testing.T) {
	c := baseComposer(&config.ComposeConfig{}, "matey")

	server := config.ServerConfig{Image: "x"}
	server.Deploy.Resources.Limits.CPUs = "500m"
	server.Deploy.Resources.Limits.Memory = "256Mi"
	got := c.convertServerConfigToMCPServer("s", server)
	if got.Spec.Resources.Limits["cpu"] != "500m" {
		t.Errorf("Limits[cpu] = %q, want 500m", got.Spec.Resources.Limits["cpu"])
	}
	if got.Spec.Resources.Limits["memory"] != "256Mi" {
		t.Errorf("Limits[memory] = %q, want 256Mi", got.Spec.Resources.Limits["memory"])
	}

	// No limits configured -> no limits map populated.
	gotEmpty := c.convertServerConfigToMCPServer("s", config.ServerConfig{Image: "x"})
	if gotEmpty.Spec.Resources.Limits != nil {
		t.Errorf("expected nil Limits when none configured, got %v", gotEmpty.Spec.Resources.Limits)
	}
}

func TestConvertServerConfigToMCPServer_Volumes(t *testing.T) {
	c := baseComposer(&config.ComposeConfig{}, "matey")

	// Absolute host path is preserved verbatim.
	got := c.convertServerConfigToMCPServer("s", config.ServerConfig{
		Image:   "x",
		Volumes: []string{"/host/data:/container/data:rw", "/etc/conf:/app/conf"},
	})
	if len(got.Spec.Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(got.Spec.Volumes))
	}
	if got.Spec.Volumes[0].HostPath != "/host/data" {
		t.Errorf("Volumes[0].HostPath = %q, want /host/data", got.Spec.Volumes[0].HostPath)
	}
	if got.Spec.Volumes[0].MountPath != "/container/data" {
		t.Errorf("Volumes[0].MountPath = %q, want /container/data", got.Spec.Volumes[0].MountPath)
	}
	if got.Spec.Volumes[0].Name != "volume-0" || got.Spec.Volumes[1].Name != "volume-1" {
		t.Errorf("volume names = %q,%q, want volume-0,volume-1",
			got.Spec.Volumes[0].Name, got.Spec.Volumes[1].Name)
	}

	// Malformed volume entry (no colon) is skipped.
	gotBad := c.convertServerConfigToMCPServer("s", config.ServerConfig{
		Image:   "x",
		Volumes: []string{"justaname"},
	})
	if len(gotBad.Spec.Volumes) != 0 {
		t.Errorf("expected malformed volume to be skipped, got %v", gotBad.Spec.Volumes)
	}
}

func TestConvertServerConfigToMCPServer_NamedVolumeResolution(t *testing.T) {
	// A named volume (not starting with /) is resolved under MATEY_DATA_DIR.
	t.Setenv("MATEY_DATA_DIR", "/var/lib/matey-data")
	c := baseComposer(&config.ComposeConfig{}, "matey")
	got := c.convertServerConfigToMCPServer("s", config.ServerConfig{
		Image:   "x",
		Volumes: []string{"mydata:/app/data"},
	})
	if len(got.Spec.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(got.Spec.Volumes))
	}
	want := "/var/lib/matey-data/mydata"
	if got.Spec.Volumes[0].HostPath != want {
		t.Errorf("HostPath = %q, want %q", got.Spec.Volumes[0].HostPath, want)
	}
	_ = os.Unsetenv("MATEY_DATA_DIR")
}

func TestConvertServerConfigToMCPServer_Authentication(t *testing.T) {
	c := baseComposer(&config.ComposeConfig{}, "matey")
	allowAPIKey := true
	server := config.ServerConfig{
		Image: "x",
		Authentication: &config.ServerAuthConfig{
			Enabled:       true,
			RequiredScope: "mcp:read",
			OptionalAuth:  true,
			AllowAPIKey:   &allowAPIKey,
		},
	}
	got := c.convertServerConfigToMCPServer("s", server)
	if got.Spec.Authentication == nil {
		t.Fatal("expected non-nil Authentication")
	}
	if !got.Spec.Authentication.Enabled {
		t.Error("Authentication.Enabled should be true")
	}
	if got.Spec.Authentication.RequiredScope != "mcp:read" {
		t.Errorf("RequiredScope = %q, want mcp:read", got.Spec.Authentication.RequiredScope)
	}
	if !got.Spec.Authentication.OptionalAuth {
		t.Error("OptionalAuth should be true")
	}

	// No authentication config -> nil Authentication.
	gotNone := c.convertServerConfigToMCPServer("s", config.ServerConfig{Image: "x"})
	if gotNone.Spec.Authentication != nil {
		t.Errorf("expected nil Authentication when unconfigured, got %+v", gotNone.Spec.Authentication)
	}
}

// helpers ---------------------------------------------------------------

func ptrInt64(v int64) *int64 { return &v }

func int64PtrEqual(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func derefInt64(p *int64) interface{} {
	if p == nil {
		return nil
	}
	return *p
}
