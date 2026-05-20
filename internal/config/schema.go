// internal/config/schema.go
package config

import (
	"encoding/json"
	"reflect"
	"strings"
)

// jsonSchemaURI is the draft this generator targets. Draft 2020-12 is what
// modern editors (VS Code YAML, JetBrains) expect for `$schema`.
const jsonSchemaURI = "https://json-schema.org/draft/2020-12/schema"

// schemaNode is a minimal JSON Schema document. Fields are pointers/omitempty
// so unused keywords are dropped from the emitted JSON.
type schemaNode struct {
	Schema               string                 `json:"$schema,omitempty"`
	Title                string                 `json:"title,omitempty"`
	Description          string                 `json:"description,omitempty"`
	Type                 string                 `json:"type,omitempty"`
	Properties           map[string]*schemaNode `json:"properties,omitempty"`
	Required             []string               `json:"required,omitempty"`
	Items                *schemaNode            `json:"items,omitempty"`
	AdditionalProperties interface{}            `json:"additionalProperties,omitempty"`
}

// GenerateSchema reflects over ComposeConfig and produces a JSON Schema
// document describing matey.yaml. Reflection (rather than a hand-authored
// schema) keeps it from drifting as the config structs evolve.
func GenerateSchema() *schemaNode {
	root := schemaForType(reflect.TypeOf(ComposeConfig{}))
	root.Schema = jsonSchemaURI
	root.Title = "Matey Configuration"
	root.Description = "Schema for matey.yaml MCP orchestrator configuration"

	return root
}

// GenerateSchemaJSON returns the schema as indented JSON bytes.
func GenerateSchemaJSON() ([]byte, error) {

	return json.MarshalIndent(GenerateSchema(), "", "  ")
}

func schemaForType(t reflect.Type) *schemaNode {
	// Unwrap pointers — a *OAuthConfig field is the same shape as OAuthConfig.
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:

		return &schemaNode{Type: "string"}
	case reflect.Bool:

		return &schemaNode{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:

		return &schemaNode{Type: "integer"}
	case reflect.Float32, reflect.Float64:

		return &schemaNode{Type: "number"}
	case reflect.Slice, reflect.Array:

		return &schemaNode{Type: "array", Items: schemaForType(t.Elem())}
	case reflect.Map:
		// YAML maps become objects with value-typed additionalProperties.
		return &schemaNode{Type: "object", AdditionalProperties: schemaForType(t.Elem())}
	case reflect.Struct:

		return schemaForStruct(t)
	case reflect.Interface:
		// `interface{}` (e.g. tool parameter defaults) — accept anything.
		return &schemaNode{}
	default:

		return &schemaNode{}
	}
}

func schemaForStruct(t reflect.Type) *schemaNode {
	node := &schemaNode{
		Type:                 "object",
		Properties:           map[string]*schemaNode{},
		AdditionalProperties: false,
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {

			continue // unexported
		}
		name, opts := yamlFieldName(field)
		if name == "" || name == "-" {

			continue
		}
		node.Properties[name] = schemaForType(field.Type)
		// A field without `omitempty` is treated as required.
		if !opts["omitempty"] {
			node.Required = append(node.Required, name)
		}
	}
	if len(node.Required) == 0 {
		node.Required = nil
	}

	return node
}

// yamlFieldName parses a struct field's `yaml:"..."` tag, returning the wire
// name and the set of tag options (omitempty, etc).
func yamlFieldName(field reflect.StructField) (string, map[string]bool) {
	tag := field.Tag.Get("yaml")
	opts := map[string]bool{}
	if tag == "" {

		return strings.ToLower(field.Name), opts
	}
	parts := strings.Split(tag, ",")
	for _, o := range parts[1:] {
		opts[o] = true
	}
	name := parts[0]
	if name == "" {
		name = strings.ToLower(field.Name)
	}

	return name, opts
}
