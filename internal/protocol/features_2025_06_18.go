// internal/protocol/features_2025_06_18.go
//
// Types introduced (or substantially extended) in the 2025-06-18 revision of
// the Model Context Protocol:
//
//   - Tools may now declare an OutputSchema (JSON Schema describing the shape
//     of their return value) in addition to the existing InputSchema.
//   - A tool call result may carry both human-readable Content AND a
//     StructuredContent payload matching that OutputSchema.
//   - A new content kind "resource_link" lets a tool reference a resource by
//     URI without inlining its bytes.
//   - "elicitation/create" lets servers ask the user for structured input
//     mid-request. Both the request and the typed response are defined here.
//   - "completion/complete" lets clients ask the server to autocomplete
//     prompt arguments or resource URIs.
//   - "logging/setLevel" + "notifications/message" let clients control the
//     server log stream they receive.
//
// All types here are pure additions. Existing protocol structs are not
// renamed or removed; struct fields use ,omitempty so older wire messages
// continue to round-trip.
package protocol

import "encoding/json"

// Content type constants used in tool results, prompt messages, and elsewhere
// the spec talks about "content blocks".
const (
	ContentTypeText         = "text"
	ContentTypeImage        = "image"
	ContentTypeAudio        = "audio"
	ContentTypeResource     = "resource"      // embedded inline resource
	ContentTypeResourceLink = "resource_link" // 2025-06-18: link by URI only
)

// Tool is the protocol-level description of a callable tool. Includes the
// 2025-06-18 OutputSchema field so a server can declare the shape of its
// structured tool output. Title is also new and carries a human-friendly
// label distinct from the machine name.
type Tool struct {
	Name         string           `json:"name"`
	Title        string           `json:"title,omitempty"`
	Description  string           `json:"description,omitempty"`
	InputSchema  json.RawMessage  `json:"inputSchema,omitempty"`
	OutputSchema json.RawMessage  `json:"outputSchema,omitempty"`
	Annotations  *ToolAnnotations `json:"annotations,omitempty"`
}

// ToolAnnotations carries optional behaviour hints (read-only, destructive,
// idempotent, openWorld) introduced alongside the structured-output work.
// All fields are hints, not enforced -- clients may surface them in UI.
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// Content is a single content block returned by a tool, prompt, or sampling
// call. The Type field discriminates the union -- see the ContentType*
// constants. Only the fields relevant to that Type should be populated;
// everything else is omitempty so the wire form stays clean.
//
// Resource-link blocks (2025-06-18) carry URI + optional metadata but never
// inline data; the client follows the URI via resources/read if it wants the
// bytes.
type Content struct {
	Type        string                 `json:"type"`
	Text        string                 `json:"text,omitempty"`
	Data        string                 `json:"data,omitempty"` // base64 for image/audio
	MimeType    string                 `json:"mimeType,omitempty"`
	URI         string                 `json:"uri,omitempty"`  // resource / resource_link
	Name        string                 `json:"name,omitempty"` // resource_link display name
	Description string                 `json:"description,omitempty"`
	Resource    *Resource              `json:"resource,omitempty"` // embedded resource block
	Annotations map[string]interface{} `json:"annotations,omitempty"`
}

// ToolResult is the body of a tools/call response. As of 2025-06-18 a server
// may return both Content (human-readable) and StructuredContent (a JSON
// value matching the tool's declared OutputSchema). Older clients ignore the
// new field and just read Content; newer clients prefer StructuredContent
// when present.
type ToolResult struct {
	Content           []Content       `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// --- elicitation/create -------------------------------------------------

// ElicitationCreateRequest is sent by a server to ask the user for input
// mid-request. RequestedSchema is a JSON Schema (object) describing the
// shape of the data the server wants back.
type ElicitationCreateRequest struct {
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requestedSchema"`
}

// ElicitationAction is the user's response disposition.
type ElicitationAction string

const (
	ElicitationActionAccept  ElicitationAction = "accept"
	ElicitationActionDecline ElicitationAction = "decline"
	ElicitationActionCancel  ElicitationAction = "cancel"
)

// ElicitationCreateResult is the reply to an elicitation/create request.
// When Action == "accept", Content holds the JSON object matching
// RequestedSchema. For decline/cancel, Content is typically empty.
type ElicitationCreateResult struct {
	Action  ElicitationAction `json:"action"`
	Content json.RawMessage   `json:"content,omitempty"`
}

// --- completion/complete -----------------------------------------------

// CompletionRefType discriminates the two kinds of things that can be
// completed: a prompt argument or a resource URI.
const (
	CompletionRefTypePrompt   = "ref/prompt"
	CompletionRefTypeResource = "ref/resource"
)

// CompletionRef identifies what is being completed. For prompt refs use
// Name; for resource refs use URI. Exactly one of the two should be set.
type CompletionRef struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
	URI  string `json:"uri,omitempty"`
}

// CompletionArgument is the (partial) value the user has already typed for
// the named argument.
type CompletionArgument struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CompleteRequest is the body of a completion/complete request.
type CompleteRequest struct {
	Ref      CompletionRef      `json:"ref"`
	Argument CompletionArgument `json:"argument"`
}

// CompletionValues is the inner "completion" object of CompleteResult. It is
// declared as its own type so a JSON consumer can pluck the field with the
// spec-mandated name "completion".
type CompletionValues struct {
	Values  []string `json:"values"`
	Total   int      `json:"total,omitempty"`
	HasMore bool     `json:"hasMore,omitempty"`
}

// CompleteResult is the body of a completion/complete response.
type CompleteResult struct {
	Completion CompletionValues `json:"completion"`
}

// --- logging/setLevel + notifications/message -------------------------

// LogLevel mirrors the syslog-style levels the spec uses for logging.
type LogLevel string

const (
	LogLevelDebug     LogLevel = "debug"
	LogLevelInfo      LogLevel = "info"
	LogLevelNotice    LogLevel = "notice"
	LogLevelWarning   LogLevel = "warning"
	LogLevelError     LogLevel = "error"
	LogLevelCritical  LogLevel = "critical"
	LogLevelAlert     LogLevel = "alert"
	LogLevelEmergency LogLevel = "emergency"
)

// SetLevelRequest is the body of a logging/setLevel request.
type SetLevelRequest struct {
	Level LogLevel `json:"level"`
}

// LogMessage is the payload of a notifications/message notification.
// Data is left as raw JSON because the spec lets servers ship arbitrary
// structured payloads with each log line.
type LogMessage struct {
	Level  LogLevel        `json:"level"`
	Logger string          `json:"logger,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}
