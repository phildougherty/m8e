// internal/config/validate_test.go
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempConfig writes content to a temp matey.yaml and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "matey.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	return path
}

func validationErrs(t *testing.T, err error) ValidationErrors {
	t.Helper()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T: %v", err, err)
	}

	return verrs
}

func TestValidateFile_Valid(t *testing.T) {
	path := writeTempConfig(t, `version: "1"
servers:
  echo:
    protocol: stdio
    command: "echo hello"
memory:
  enabled: false
`)
	cfg, err := ValidateFile(path)
	if err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
	if len(cfg.Servers) != 1 {
		t.Errorf("expected 1 server, got %d", len(cfg.Servers))
	}
}

func TestValidateFile_SyntaxError_HasLine(t *testing.T) {
	// Unterminated quoted scalar on line 4.
	path := writeTempConfig(t, `version: "1"
servers:
  echo:
    command: "echo hello
`)
	verrs := validationErrs(t, mustErr(ValidateFile(path)))
	if len(verrs) == 0 {
		t.Fatal("expected at least one error")
	}
	found := false
	for _, ve := range verrs {
		if ve.Line > 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a syntax error with a line number, got: %v", verrs)
	}
}

func TestValidateFile_WrongType_HasLine(t *testing.T) {
	// http_port wants an int; a string triggers a *yaml.TypeError with a line.
	path := writeTempConfig(t, `version: "1"
servers:
  web:
    protocol: http
    image: "nginx:latest"
    http_port: "not-a-number"
`)
	verrs := validationErrs(t, mustErr(ValidateFile(path)))
	got := verrs.Error()
	if !strings.Contains(got, "matey.yaml:6") {
		t.Errorf("expected error to point at line 6, got: %s", got)
	}
}

func TestValidateFile_MissingRequiredField_HasLine(t *testing.T) {
	// Server with neither command, image, nor build context.
	path := writeTempConfig(t, `version: "1"
servers:
  broken:
    protocol: stdio
`)
	verrs := validationErrs(t, mustErr(ValidateFile(path)))
	matched := false
	for _, ve := range verrs {
		if ve.Path == "servers.broken" && ve.Line == 3 &&
			strings.Contains(ve.Message, "must specify either command, image, or build context") {
			matched = true
		}
	}
	if !matched {
		t.Errorf("expected located error for servers.broken at line 3, got: %v", verrs)
	}
}

func TestValidateFile_BadEnum_HasLine(t *testing.T) {
	path := writeTempConfig(t, `version: "1"
servers:
  web:
    protocol: gopher
    command: "serve"
`)
	verrs := validationErrs(t, mustErr(ValidateFile(path)))
	matched := false
	for _, ve := range verrs {
		if ve.Path == "servers.web" && ve.Line > 0 && strings.Contains(ve.Message, "invalid protocol") {
			matched = true
		}
	}
	if !matched {
		t.Errorf("expected located invalid-protocol error, got: %v", verrs)
	}
}

func TestValidateFile_CircularDependency(t *testing.T) {
	path := writeTempConfig(t, `version: "1"
servers:
  a:
    command: "run-a"
    depends_on: ["b"]
  b:
    command: "run-b"
    depends_on: ["a"]
`)
	verrs := validationErrs(t, mustErr(ValidateFile(path)))
	matched := false
	for _, ve := range verrs {
		if strings.Contains(ve.Message, "circular dependency") && ve.Line > 0 {
			matched = true
		}
	}
	if !matched {
		t.Errorf("expected circular dependency error with line, got: %v", verrs)
	}
}

func TestValidateFile_UndefinedDependency(t *testing.T) {
	path := writeTempConfig(t, `version: "1"
servers:
  a:
    command: "run-a"
    depends_on: ["ghost"]
`)
	verrs := validationErrs(t, mustErr(ValidateFile(path)))
	matched := false
	for _, ve := range verrs {
		if strings.Contains(ve.Message, `depends on undefined server "ghost"`) {
			matched = true
		}
	}
	if !matched {
		t.Errorf("expected undefined-dependency error, got: %v", verrs)
	}
}

func TestValidateFile_MultipleErrorsReported(t *testing.T) {
	// Three independent problems: bad version, bad protocol, bad capability.
	path := writeTempConfig(t, `version: "2"
servers:
  one:
    protocol: telnet
    command: "run-one"
  two:
    command: "run-two"
    capabilities: ["bogus"]
`)
	verrs := validationErrs(t, mustErr(ValidateFile(path)))
	if len(verrs) < 3 {
		t.Fatalf("expected at least 3 errors collected at once, got %d: %v", len(verrs), verrs)
	}
	joined := verrs.Error()
	for _, want := range []string{"unsupported version", "invalid protocol", "invalid capability"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected combined errors to contain %q, got: %s", want, joined)
		}
	}
}

func TestGenerateSchema_StructureAndJSON(t *testing.T) {
	data, err := GenerateSchemaJSON()
	if err != nil {
		t.Fatalf("GenerateSchemaJSON: %v", err)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	if doc["$schema"] != jsonSchemaURI {
		t.Errorf("expected $schema %q, got %v", jsonSchemaURI, doc["$schema"])
	}
	if doc["type"] != "object" {
		t.Errorf("expected top-level type object, got %v", doc["type"])
	}

	props, ok := doc["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties object")
	}
	for _, key := range []string{"version", "servers", "proxy", "memory", "oauth"} {
		if _, exists := props[key]; !exists {
			t.Errorf("expected schema to define top-level property %q", key)
		}
	}

	// `servers` is a map -> object with additionalProperties describing ServerConfig.
	servers, ok := props["servers"].(map[string]interface{})
	if !ok {
		t.Fatal("expected servers property to be an object")
	}
	if servers["type"] != "object" {
		t.Errorf("expected servers type object, got %v", servers["type"])
	}
	serverItem, ok := servers["additionalProperties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected servers.additionalProperties to describe a server")
	}
	serverProps, ok := serverItem["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected server properties")
	}
	for _, key := range []string{"image", "command", "protocol", "depends_on"} {
		if _, exists := serverProps[key]; !exists {
			t.Errorf("expected server schema to define %q", key)
		}
	}
}

func TestGenerateSchema_ValidConfigRoundTrips(t *testing.T) {
	// A valid config should validate against its own schema, checked
	// structurally: every key present in the config exists in the schema.
	path := writeTempConfig(t, `version: "1"
servers:
  echo:
    protocol: stdio
    command: "echo hello"
    depends_on: []
memory:
  enabled: false
`)
	cfg, err := ValidateFile(path)
	if err != nil {
		t.Fatalf("config should be valid: %v", err)
	}

	schema := GenerateSchema()
	if schema.Properties["version"] == nil {
		t.Fatal("schema missing version property")
	}
	if schema.Properties["servers"] == nil || schema.Properties["servers"].AdditionalProperties == nil {
		t.Fatal("schema missing servers definition")
	}
	serverSchema := schema.Properties["servers"].AdditionalProperties.(*schemaNode)
	if _, ok := cfg.Servers["echo"]; !ok {
		t.Fatal("expected echo server in parsed config")
	}
	for _, key := range []string{"protocol", "command", "depends_on"} {
		if serverSchema.Properties[key] == nil {
			t.Errorf("schema server definition missing %q used by valid config", key)
		}
	}
}

// mustErr is a tiny helper to thread the (cfg, err) pair from ValidateFile into
// the error-only assertion helpers.
func mustErr(_ *ComposeConfig, err error) error {

	return err
}
