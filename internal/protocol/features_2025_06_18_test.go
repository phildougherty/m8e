package protocol

import (
	"encoding/json"
	"testing"
)

func TestTool_OutputSchemaRoundTrip(t *testing.T) {
	original := Tool{
		Name:         "do_math",
		Title:        "Do Math",
		Description:  "Adds two numbers",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"sum":{"type":"number"}}}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal Tool: %v", err)
	}

	// The wire form must contain both inputSchema and outputSchema.
	if !jsonContainsKey(t, data, "inputSchema") {
		t.Errorf("expected wire form to include inputSchema, got %s", data)
	}
	if !jsonContainsKey(t, data, "outputSchema") {
		t.Errorf("expected wire form to include outputSchema, got %s", data)
	}

	var decoded Tool
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal Tool: %v", err)
	}
	if decoded.Name != original.Name || decoded.Title != original.Title {
		t.Errorf("Tool round-trip mismatch: %+v vs %+v", decoded, original)
	}
	if string(decoded.OutputSchema) != string(original.OutputSchema) {
		t.Errorf("OutputSchema mismatch:\n got  %s\n want %s", decoded.OutputSchema, original.OutputSchema)
	}
}

func TestTool_OmitsOutputSchemaWhenAbsent(t *testing.T) {
	// Older tools without outputSchema must still serialize cleanly.
	tool := Tool{
		Name:        "legacy",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if jsonContainsKey(t, data, "outputSchema") {
		t.Errorf("legacy tool wire form should omit outputSchema, got %s", data)
	}
}

func TestToolResult_StructuredContentRoundTrip(t *testing.T) {
	original := ToolResult{
		Content: []Content{
			{Type: ContentTypeText, Text: "Result: 42"},
		},
		StructuredContent: json.RawMessage(`{"sum":42}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal ToolResult: %v", err)
	}
	if !jsonContainsKey(t, data, "structuredContent") {
		t.Errorf("expected wire form to include structuredContent, got %s", data)
	}

	var decoded ToolResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}
	if len(decoded.Content) != 1 || decoded.Content[0].Text != "Result: 42" {
		t.Errorf("Content mismatch: %+v", decoded.Content)
	}
	if string(decoded.StructuredContent) != `{"sum":42}` {
		t.Errorf("StructuredContent mismatch: %s", decoded.StructuredContent)
	}
}

func TestContent_ResourceLinkRoundTrip(t *testing.T) {
	original := Content{
		Type:        ContentTypeResourceLink,
		URI:         "file:///tmp/log.txt",
		Name:        "log",
		Description: "Today's log file",
		MimeType:    "text/plain",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal Content: %v", err)
	}

	var decoded Content
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal Content: %v", err)
	}
	if decoded.Type != ContentTypeResourceLink {
		t.Errorf("expected type %q, got %q", ContentTypeResourceLink, decoded.Type)
	}
	if decoded.URI != original.URI {
		t.Errorf("URI mismatch: %q vs %q", decoded.URI, original.URI)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name mismatch")
	}
	if decoded.Text != "" {
		t.Errorf("resource_link should not carry Text; got %q", decoded.Text)
	}
}

func TestElicitationCreateRequest_RoundTrip(t *testing.T) {
	req := ElicitationCreateRequest{
		Message:         "Please enter your name:",
		RequestedSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`),
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal ElicitationCreateRequest: %v", err)
	}
	if !jsonContainsKey(t, data, "message") || !jsonContainsKey(t, data, "requestedSchema") {
		t.Errorf("wire form missing expected fields: %s", data)
	}

	var decoded ElicitationCreateRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Message != req.Message {
		t.Errorf("Message mismatch: %q vs %q", decoded.Message, req.Message)
	}
}

func TestElicitationCreateResult_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		res  ElicitationCreateResult
	}{
		{"accept", ElicitationCreateResult{Action: ElicitationActionAccept, Content: json.RawMessage(`{"name":"Phil"}`)}},
		{"decline", ElicitationCreateResult{Action: ElicitationActionDecline}},
		{"cancel", ElicitationCreateResult{Action: ElicitationActionCancel}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.res)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded ElicitationCreateResult
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if decoded.Action != tc.res.Action {
				t.Errorf("action mismatch: %q vs %q", decoded.Action, tc.res.Action)
			}
			if string(decoded.Content) != string(tc.res.Content) {
				t.Errorf("content mismatch: %s vs %s", decoded.Content, tc.res.Content)
			}
		})
	}
}

func TestCompleteRequestResult_RoundTrip(t *testing.T) {
	req := CompleteRequest{
		Ref:      CompletionRef{Type: CompletionRefTypePrompt, Name: "code-review"},
		Argument: CompletionArgument{Name: "language", Value: "py"},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var decodedReq CompleteRequest
	if err := json.Unmarshal(data, &decodedReq); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if decodedReq.Ref.Type != CompletionRefTypePrompt || decodedReq.Ref.Name != "code-review" {
		t.Errorf("ref mismatch: %+v", decodedReq.Ref)
	}
	if decodedReq.Argument.Value != "py" {
		t.Errorf("argument value mismatch: %q", decodedReq.Argument.Value)
	}

	res := CompleteResult{
		Completion: CompletionValues{
			Values:  []string{"python", "pytest"},
			Total:   2,
			HasMore: false,
		},
	}
	rdata, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	// Spec requires the key name "completion".
	if !jsonContainsKey(t, rdata, "completion") {
		t.Errorf("CompleteResult must contain key 'completion'; got %s", rdata)
	}
	var decodedRes CompleteResult
	if err := json.Unmarshal(rdata, &decodedRes); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(decodedRes.Completion.Values) != 2 || decodedRes.Completion.Values[0] != "python" {
		t.Errorf("values mismatch: %+v", decodedRes.Completion.Values)
	}
	if decodedRes.Completion.Total != 2 {
		t.Errorf("total mismatch: %d", decodedRes.Completion.Total)
	}
}

func TestLogMessage_RoundTrip(t *testing.T) {
	msg := LogMessage{
		Level:  LogLevelWarning,
		Logger: "m8e.proxy",
		Data:   json.RawMessage(`{"event":"slow_call","ms":1234}`),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded LogMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Level != LogLevelWarning {
		t.Errorf("level mismatch: %q", decoded.Level)
	}
	if decoded.Logger != "m8e.proxy" {
		t.Errorf("logger mismatch: %q", decoded.Logger)
	}
	if string(decoded.Data) != `{"event":"slow_call","ms":1234}` {
		t.Errorf("data mismatch: %s", decoded.Data)
	}
}

func TestSetLevelRequest_RoundTrip(t *testing.T) {
	req := SetLevelRequest{Level: LogLevelDebug}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded SetLevelRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Level != LogLevelDebug {
		t.Errorf("level mismatch: %q", decoded.Level)
	}
}

func TestCapabilities_NewMarkerFields(t *testing.T) {
	// Both Elicitation and Completions are presence-only marker objects.
	caps := CapabilitiesOpts{
		Elicitation: &ElicitationOpts{},
		Completions: &CompletionsOpts{},
	}
	data, err := json.Marshal(caps)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !jsonContainsKey(t, data, "elicitation") {
		t.Errorf("expected wire form to include elicitation marker: %s", data)
	}
	if !jsonContainsKey(t, data, "completions") {
		t.Errorf("expected wire form to include completions marker: %s", data)
	}

	var decoded CapabilitiesOpts
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Elicitation == nil {
		t.Errorf("expected Elicitation to be non-nil after round-trip")
	}
	if decoded.Completions == nil {
		t.Errorf("expected Completions to be non-nil after round-trip")
	}
}

func TestStandardMethods_NewConstants(t *testing.T) {
	// Sanity: the new method constants must be recognised as standard.
	for _, m := range []string{
		MethodElicitationCreate,
		MethodCompletionComplete,
		MethodLoggingSetLevel,
		NotificationMessage,
	} {
		if !IsStandardMethod(m) {
			t.Errorf("expected %q to be a standard MCP method", m)
		}
	}
}

// jsonContainsKey is a tiny helper that parses data as a generic map and
// reports whether the given top-level key is present.
func jsonContainsKey(t *testing.T, data []byte, key string) bool {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("jsonContainsKey: invalid JSON: %v (%s)", err, data)
	}
	_, ok := m[key]

	return ok
}
