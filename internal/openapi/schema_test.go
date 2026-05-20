package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateOpenAPISchema_InfoAndVersion(t *testing.T) {
	schema, err := GenerateOpenAPISchema("filesystem", nil)
	if err != nil {
		t.Fatalf("GenerateOpenAPISchema returned error: %v", err)
	}

	if schema.OpenAPI != "3.1.0" {
		t.Errorf("OpenAPI version = %q, want %q", schema.OpenAPI, "3.1.0")
	}
	if schema.Info.Title != "filesystem MCP Server" {
		t.Errorf("Info.Title = %q, want %q", schema.Info.Title, "filesystem MCP Server")
	}
	if schema.Info.Version != "1.0.0" {
		t.Errorf("Info.Version = %q, want %q", schema.Info.Version, "1.0.0")
	}
	if !strings.Contains(schema.Info.Description, "filesystem") {
		t.Errorf("Info.Description = %q, want it to mention server name", schema.Info.Description)
	}
	if len(schema.Servers) != 1 || schema.Servers[0].URL != "/" {
		t.Errorf("Servers = %+v, want single server with URL %q", schema.Servers, "/")
	}
}

func TestGenerateOpenAPISchema_NoTools(t *testing.T) {
	schema, err := GenerateOpenAPISchema("empty", []Tool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(schema.Paths) != 0 {
		t.Errorf("Paths = %v, want empty", schema.Paths)
	}
	if len(schema.Specs) != 0 {
		t.Errorf("Specs = %v, want empty", schema.Specs)
	}
	// Standard MCP schemas must still be present.
	for _, name := range []string{"MCPContent", "MCPError", "MCPMetadata", "MCPAnnotations"} {
		if _, ok := schema.Components.Schemas[name]; !ok {
			t.Errorf("Components.Schemas missing standard schema %q", name)
		}
	}
	// Security scheme must always be present.
	if sc, ok := schema.Components.SecuritySchemes["MCPBearerAuth"]; !ok {
		t.Error("Components.SecuritySchemes missing MCPBearerAuth")
	} else if sc.Type != "http" || sc.Scheme != "bearer" {
		t.Errorf("MCPBearerAuth = %+v, want http/bearer", sc)
	}
}

func TestGenerateOpenAPISchema_SingleToolPathAndOperation(t *testing.T) {
	tools := []Tool{
		{
			Name:        "read_file",
			Description: "Reads a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "the file path",
					},
				},
				"required": []interface{}{"path"},
			},
		},
	}

	schema, err := GenerateOpenAPISchema("fs", tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pathItem, ok := schema.Paths["/read_file"]
	if !ok {
		t.Fatalf("Paths missing %q; got keys %v", "/read_file", keysOf(schema.Paths))
	}

	op := pathItem.Post
	if op.OperationID != "read_file" {
		t.Errorf("OperationID = %q, want %q", op.OperationID, "read_file")
	}
	if op.Summary != "read_file" {
		t.Errorf("Summary = %q, want %q", op.Summary, "read_file")
	}
	if op.MCPMethod != "tools/call" {
		t.Errorf("MCPMethod = %q, want %q", op.MCPMethod, "tools/call")
	}
	if !strings.Contains(op.Description, "Reads a file") {
		t.Errorf("Description = %q, want it to contain tool description", op.Description)
	}
	wantTags := []string{"fs", "mcp-tools"}
	if len(op.Tags) != 2 || op.Tags[0] != wantTags[0] || op.Tags[1] != wantTags[1] {
		t.Errorf("Tags = %v, want %v", op.Tags, wantTags)
	}

	// Request body ref.
	if !op.RequestBody.Required {
		t.Error("RequestBody.Required = false, want true")
	}
	reqMedia, ok := op.RequestBody.Content["application/json"]
	if !ok {
		t.Fatalf("RequestBody missing application/json content")
	}
	if reqMedia.Schema.Ref != "#/components/schemas/read_fileRequest" {
		t.Errorf("request schema ref = %q, want %q", reqMedia.Schema.Ref, "#/components/schemas/read_fileRequest")
	}

	// Responses.
	for _, code := range []string{"200", "400", "401", "500"} {
		if _, ok := op.Responses[code]; !ok {
			t.Errorf("Responses missing status %q", code)
		}
	}
	if ref := op.Responses["200"].Content["application/json"].Schema.Ref; ref != "#/components/schemas/read_fileResponse" {
		t.Errorf("200 response ref = %q, want %q", ref, "#/components/schemas/read_fileResponse")
	}
	if ref := op.Responses["400"].Content["application/json"].Schema.Ref; ref != "#/components/schemas/MCPError" {
		t.Errorf("400 response ref = %q, want %q", ref, "#/components/schemas/MCPError")
	}

	// Security on the operation.
	if len(op.Security) != 1 {
		t.Fatalf("op.Security = %v, want one entry", op.Security)
	}
	if _, ok := op.Security[0]["MCPBearerAuth"]; !ok {
		t.Errorf("op.Security missing MCPBearerAuth, got %v", op.Security[0])
	}
}

func TestGenerateOpenAPISchema_RequestSchemaConversion(t *testing.T) {
	tools := []Tool{
		{
			Name:        "search",
			Description: "Search",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string", "description": "query text"},
					"tags": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"limit": map[string]interface{}{"type": "integer"},
				},
				"required": []interface{}{"query"},
			},
		},
	}

	schema, err := GenerateOpenAPISchema("svc", tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reqSchema, ok := schema.Components.Schemas["searchRequest"]
	if !ok {
		t.Fatalf("Components.Schemas missing searchRequest")
	}
	if reqSchema.Type != "object" {
		t.Errorf("searchRequest.Type = %q, want object", reqSchema.Type)
	}
	if len(reqSchema.Required) != 1 || reqSchema.Required[0] != "query" {
		t.Errorf("searchRequest.Required = %v, want [query]", reqSchema.Required)
	}
	q, ok := reqSchema.Properties["query"]
	if !ok {
		t.Fatalf("searchRequest.Properties missing query")
	}
	if q.Type != "string" || q.Description != "query text" {
		t.Errorf("query property = %+v, want string/query text", q)
	}
	tagsProp, ok := reqSchema.Properties["tags"]
	if !ok {
		t.Fatalf("searchRequest.Properties missing tags")
	}
	if tagsProp.Type != "array" {
		t.Errorf("tags.Type = %q, want array", tagsProp.Type)
	}
	if tagsProp.Items == nil || tagsProp.Items.Type != "string" {
		t.Errorf("tags.Items = %+v, want string items", tagsProp.Items)
	}
}

func TestGenerateOpenAPISchema_NestedSchema(t *testing.T) {
	tools := []Tool{
		{
			Name:        "configure",
			Description: "Configure",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"settings": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"nested": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"leaf": map[string]interface{}{"type": "string"},
								},
							},
						},
					},
				},
			},
		},
	}

	schema, err := GenerateOpenAPISchema("svc", tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reqSchema := schema.Components.Schemas["configureRequest"]
	settings := reqSchema.Properties["settings"]
	if settings.Type != "object" {
		t.Fatalf("settings.Type = %q, want object", settings.Type)
	}
	nested := settings.Properties["nested"]
	if nested.Type != "object" {
		t.Fatalf("nested.Type = %q, want object", nested.Type)
	}
	leaf := nested.Properties["leaf"]
	if leaf.Type != "string" {
		t.Errorf("leaf.Type = %q, want string", leaf.Type)
	}
}

func TestGenerateOpenAPISchema_ArrayWithoutItemsAutoFix(t *testing.T) {
	tools := []Tool{
		{
			Name:        "bad_array",
			Description: "tool with array missing items",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"vals": map[string]interface{}{
						"type": "array",
					},
				},
			},
		},
	}

	schema, err := GenerateOpenAPISchema("svc", tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vals := schema.Components.Schemas["bad_arrayRequest"].Properties["vals"]
	if vals.Type != "array" {
		t.Fatalf("vals.Type = %q, want array", vals.Type)
	}
	if vals.Items == nil {
		t.Fatal("vals.Items = nil, want auto-fixed non-nil items schema")
	}
	if vals.Items.Type != "object" {
		t.Errorf("vals.Items.Type = %q, want object (auto-fix default)", vals.Items.Type)
	}
}

func TestGenerateOpenAPISchema_NoInputSchema(t *testing.T) {
	tools := []Tool{
		{
			Name:        "ping",
			Description: "Ping",
			InputSchema: nil,
		},
	}

	schema, err := GenerateOpenAPISchema("svc", tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Request schema should exist but be effectively empty.
	reqSchema, ok := schema.Components.Schemas["pingRequest"]
	if !ok {
		t.Fatalf("Components.Schemas missing pingRequest")
	}
	if reqSchema.Type != "" || len(reqSchema.Properties) != 0 {
		t.Errorf("pingRequest = %+v, want empty schema", reqSchema)
	}

	// Spec parameters should fall back to an empty object schema.
	if len(schema.Specs) != 1 {
		t.Fatalf("Specs len = %d, want 1", len(schema.Specs))
	}
	params := schema.Specs[0].Parameters
	if params["type"] != "object" {
		t.Errorf("spec parameters type = %v, want object", params["type"])
	}
	if _, ok := params["properties"]; !ok {
		t.Errorf("spec parameters missing properties key: %v", params)
	}
}

func TestGenerateOpenAPISchema_OperationIDSanitization(t *testing.T) {
	tools := []Tool{
		{Name: "get-user info", Description: "x", InputSchema: nil},
	}

	schema, err := GenerateOpenAPISchema("svc", tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Path key keeps the original name.
	if _, ok := schema.Paths["/get-user info"]; !ok {
		t.Fatalf("Paths missing %q; got %v", "/get-user info", keysOf(schema.Paths))
	}
	op := schema.Paths["/get-user info"].Post
	if op.OperationID != "get_user_info" {
		t.Errorf("OperationID = %q, want %q (dashes and spaces replaced)", op.OperationID, "get_user_info")
	}
}

func TestGenerateOpenAPISchema_DuplicateToolNames(t *testing.T) {
	tools := []Tool{
		{
			Name:        "dup",
			Description: "first",
			InputSchema: map[string]interface{}{"type": "object"},
		},
		{
			Name:        "dup",
			Description: "second",
			InputSchema: map[string]interface{}{"type": "object"},
		},
	}

	schema, err := GenerateOpenAPISchema("svc", tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Map-keyed paths/schemas collapse duplicates: last write wins.
	if len(schema.Paths) != 1 {
		t.Errorf("Paths len = %d, want 1 (duplicate collapsed)", len(schema.Paths))
	}
	if got := schema.Paths["/dup"].Post.Description; !strings.Contains(got, "second") {
		t.Errorf("collapsed path description = %q, want last tool (second)", got)
	}
	// Specs is a slice, so both duplicates are appended.
	if len(schema.Specs) != 2 {
		t.Errorf("Specs len = %d, want 2 (slice keeps both)", len(schema.Specs))
	}
}

func TestGenerateOpenAPISchema_Annotations(t *testing.T) {
	tools := []Tool{
		{
			Name:        "delete_all",
			Description: "Deletes everything",
			InputSchema: map[string]interface{}{"type": "object"},
			Annotations: &ToolAnnotations{
				ReadOnlyHint:    false,
				DestructiveHint: true,
				IdempotentHint:  true,
			},
		},
	}

	schema, err := GenerateOpenAPISchema("svc", tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	op := schema.Paths["/delete_all"].Post
	if op.MCPHints == nil {
		t.Fatal("op.MCPHints = nil, want annotations passed through")
	}
	if !op.MCPHints.DestructiveHint || !op.MCPHints.IdempotentHint {
		t.Errorf("op.MCPHints = %+v, want destructive+idempotent", op.MCPHints)
	}
	if !strings.Contains(op.Description, "potentially destructive") {
		t.Errorf("op.Description = %q, want destructive hint text", op.Description)
	}
	if !strings.Contains(op.Description, "idempotent") {
		t.Errorf("op.Description = %q, want idempotent hint text", op.Description)
	}

	reqSchema := schema.Components.Schemas["delete_allRequest"]
	if !strings.Contains(reqSchema.Description, "MCP Annotations:") {
		t.Errorf("request schema description = %q, want MCP annotation block", reqSchema.Description)
	}
	if !strings.Contains(reqSchema.Description, "destructive operations") {
		t.Errorf("request schema description = %q, want destructive warning", reqSchema.Description)
	}

	if schema.Specs[0].Annotations == nil || !schema.Specs[0].Annotations.DestructiveHint {
		t.Errorf("spec annotations = %+v, want destructive hint", schema.Specs[0].Annotations)
	}
}

func TestGenerateOpenAPISchema_SpecialCharactersInName(t *testing.T) {
	tools := []Tool{
		{Name: "weird/name:v2", Description: "x", InputSchema: nil},
	}

	schema, err := GenerateOpenAPISchema("svc", tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := schema.Paths["/weird/name:v2"]; !ok {
		t.Errorf("Paths missing %q; got %v", "/weird/name:v2", keysOf(schema.Paths))
	}
	if _, ok := schema.Components.Schemas["weird/name:v2Request"]; !ok {
		t.Error("Components.Schemas missing request schema for special-char name")
	}
	// operationId only sanitizes dashes and spaces, slashes/colons remain.
	op := schema.Paths["/weird/name:v2"].Post
	if op.OperationID != "weird/name:v2" {
		t.Errorf("OperationID = %q, want %q", op.OperationID, "weird/name:v2")
	}
}

func TestGenerateOpenAPISchema_JSONRoundTrip(t *testing.T) {
	tools := []Tool{
		{
			Name:        "echo",
			Description: "Echoes input",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"msg": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"msg"},
			},
		},
	}

	schema, err := GenerateOpenAPISchema("svc", tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var round OpenAPISchema
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if round.OpenAPI != "3.1.0" {
		t.Errorf("round-trip OpenAPI = %q, want 3.1.0", round.OpenAPI)
	}
	if round.Info.Title != "svc MCP Server" {
		t.Errorf("round-trip Info.Title = %q, want %q", round.Info.Title, "svc MCP Server")
	}
	if _, ok := round.Paths["/echo"]; !ok {
		t.Errorf("round-trip Paths missing /echo: %v", keysOf(round.Paths))
	}
	if round.Paths["/echo"].Post.OperationID != "echo" {
		t.Errorf("round-trip OperationID = %q, want echo", round.Paths["/echo"].Post.OperationID)
	}

	// Verify the raw JSON uses the documented field name.
	if !strings.Contains(string(data), `"openapi":"3.1.0"`) {
		t.Errorf("marshalled JSON missing openapi version field; got %s", truncate(string(data)))
	}
}

func TestBuildToolDescription_NoAnnotations(t *testing.T) {
	got := buildToolDescription(Tool{Description: "plain"})
	if got != "plain" {
		t.Errorf("buildToolDescription = %q, want %q", got, "plain")
	}
}

func TestBuildToolDescription_AllHints(t *testing.T) {
	got := buildToolDescription(Tool{
		Description: "base",
		Annotations: &ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: true,
			IdempotentHint:  true,
			OpenWorldHint:   true,
		},
	})
	for _, want := range []string{"base", "MCP Hints:", "read-only", "potentially destructive", "idempotent", "accepts additional parameters"} {
		if !strings.Contains(got, want) {
			t.Errorf("buildToolDescription = %q, want substring %q", got, want)
		}
	}
}

func TestBuildAnnotationDescription_Empty(t *testing.T) {
	got := buildAnnotationDescription(&ToolAnnotations{})
	if got != "" {
		t.Errorf("buildAnnotationDescription with no hints = %q, want empty", got)
	}
}

func TestConvertJSONSchemaToOpenAPI_Primitive(t *testing.T) {
	got := convertJSONSchemaToOpenAPI(map[string]interface{}{
		"type":        "string",
		"description": "a string",
	})
	if got.Type != "string" {
		t.Errorf("Type = %q, want string", got.Type)
	}
	if got.Description != "a string" {
		t.Errorf("Description = %q, want %q", got.Description, "a string")
	}
}

func TestConvertJSONSchemaToOpenAPI_Empty(t *testing.T) {
	got := convertJSONSchemaToOpenAPI(map[string]interface{}{})
	if got.Type != "" || got.Properties != nil || got.Required != nil || got.Items != nil {
		t.Errorf("convertJSONSchemaToOpenAPI(empty) = %+v, want zero-value Schema", got)
	}
}

func keysOf(m map[string]PathItem) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200]
	}

	return s
}
