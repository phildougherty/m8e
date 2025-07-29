package protocol

import (
	"encoding/json"
	"testing"
)

func TestMCPCapabilities_Serialization(t *testing.T) {
	caps := MCPCapabilities{
		Resources: &ResourcesCapability{
			ListChanged: true,
			Subscribe:   true,
		},
		Tools: &ToolsCapability{
			ListChanged: false,
		},
		Prompts: &PromptsCapability{
			ListChanged: true,
		},
		Sampling: &SamplingCapability{},
		Logging:  &LoggingCapability{},
	}
	
	// Test JSON marshaling
	data, err := json.Marshal(caps)
	if err != nil {
		t.Fatalf("Failed to marshal MCPCapabilities: %v", err)
	}
	
	// Test JSON unmarshaling
	var unmarshaled MCPCapabilities
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal MCPCapabilities: %v", err)
	}
	
	// Verify resources capability
	if unmarshaled.Resources == nil {
		t.Error("Resources capability should not be nil")
	} else {
		if unmarshaled.Resources.ListChanged != true {
			t.Error("Resources ListChanged should be true")
		}
		if unmarshaled.Resources.Subscribe != true {
			t.Error("Resources Subscribe should be true")
		}
	}
	
	// Verify tools capability
	if unmarshaled.Tools == nil {
		t.Error("Tools capability should not be nil")
	} else {
		if unmarshaled.Tools.ListChanged != false {
			t.Error("Tools ListChanged should be false")
		}
	}
	
	// Verify prompts capability
	if unmarshaled.Prompts == nil {
		t.Error("Prompts capability should not be nil")
	} else {
		if unmarshaled.Prompts.ListChanged != true {
			t.Error("Prompts ListChanged should be true")
		}
	}
	
	// Verify sampling and logging capabilities exist
	if unmarshaled.Sampling == nil {
		t.Error("Sampling capability should not be nil")
	}
	if unmarshaled.Logging == nil {
		t.Error("Logging capability should not be nil")
	}
}

func TestMCPRequest_Serialization(t *testing.T) {
	params := json.RawMessage(`{"key": "value"}`)
	req := MCPRequest{
		JSONRPC: "2.0",
		ID:      123,
		Method:  "test/method",
		Params:  params,
	}
	
	// Test JSON marshaling
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal MCPRequest: %v", err)
	}
	
	// Test JSON unmarshaling
	var unmarshaled MCPRequest
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal MCPRequest: %v", err)
	}
	
	if unmarshaled.JSONRPC != "2.0" {
		t.Errorf("Expected JSONRPC '2.0', got %s", unmarshaled.JSONRPC)
	}
	if unmarshaled.Method != "test/method" {
		t.Errorf("Expected method 'test/method', got %s", unmarshaled.Method)
	}
	if string(unmarshaled.Params) != `{"key":"value"}` {
		t.Errorf("Expected params '{\"key\":\"value\"}', got %s", string(unmarshaled.Params))
	}
}

func TestMCPResponse_Serialization(t *testing.T) {
	result := json.RawMessage(`{"success": true}`)
	resp := MCPResponse{
		JSONRPC: "2.0",
		ID:      456,
		Result:  result,
	}
	
	// Test JSON marshaling
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal MCPResponse: %v", err)
	}
	
	// Test JSON unmarshaling
	var unmarshaled MCPResponse
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal MCPResponse: %v", err)
	}
	
	if unmarshaled.JSONRPC != "2.0" {
		t.Errorf("Expected JSONRPC '2.0', got %s", unmarshaled.JSONRPC)
	}
	if string(unmarshaled.Result) != `{"success":true}` {
		t.Errorf("Expected result '{\"success\":true}', got %s", string(unmarshaled.Result))
	}
}

func TestMCPResponse_WithError(t *testing.T) {
	errorData := json.RawMessage(`{"details": "error details"}`)
	resp := MCPResponse{
		JSONRPC: "2.0",
		ID:      789,
		Error: &MCPError{
			Code:    -1,
			Message: "Test error",
			Data:    errorData,
		},
	}
	
	// Test JSON marshaling
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal MCPResponse with error: %v", err)
	}
	
	// Test JSON unmarshaling
	var unmarshaled MCPResponse
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal MCPResponse with error: %v", err)
	}
	
	if unmarshaled.Error == nil {
		t.Fatal("Error should not be nil")
	}
	if unmarshaled.Error.Code != -1 {
		t.Errorf("Expected error code -1, got %d", unmarshaled.Error.Code)
	}
	if unmarshaled.Error.Message != "Test error" {
		t.Errorf("Expected error message 'Test error', got %s", unmarshaled.Error.Message)
	}
	if string(unmarshaled.Error.Data) != `{"details":"error details"}` {
		t.Errorf("Expected error data '{\"details\":\"error details\"}', got %s", string(unmarshaled.Error.Data))
	}
}

func TestMCPNotification_Serialization(t *testing.T) {
	params := json.RawMessage(`{"event": "notification"}`)
	notif := MCPNotification{
		JSONRPC: "2.0",
		Method:  "notification/test",
		Params:  params,
	}
	
	// Test JSON marshaling
	data, err := json.Marshal(notif)
	if err != nil {
		t.Fatalf("Failed to marshal MCPNotification: %v", err)
	}
	
	// Test JSON unmarshaling
	var unmarshaled MCPNotification
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal MCPNotification: %v", err)
	}
	
	if unmarshaled.JSONRPC != "2.0" {
		t.Errorf("Expected JSONRPC '2.0', got %s", unmarshaled.JSONRPC)
	}
	if unmarshaled.Method != "notification/test" {
		t.Errorf("Expected method 'notification/test', got %s", unmarshaled.Method)
	}
	if string(unmarshaled.Params) != `{"event":"notification"}` {
		t.Errorf("Expected params '{\"event\":\"notification\"}', got %s", string(unmarshaled.Params))
	}
}

func TestInitializeRequest_Serialization(t *testing.T) {
	req := InitializeRequest{
		ProtocolVersion: "1.0",
		Capabilities: MCPCapabilities{
			Resources: &ResourcesCapability{ListChanged: true},
			Tools:     &ToolsCapability{ListChanged: false},
		},
	}
	req.ClientInfo.Name = "test-client"
	req.ClientInfo.Version = "1.0.0"
	
	// Test JSON marshaling
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal InitializeRequest: %v", err)
	}
	
	// Test JSON unmarshaling
	var unmarshaled InitializeRequest
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal InitializeRequest: %v", err)
	}
	
	if unmarshaled.ProtocolVersion != "1.0" {
		t.Errorf("Expected protocol version '1.0', got %s", unmarshaled.ProtocolVersion)
	}
	if unmarshaled.ClientInfo.Name != "test-client" {
		t.Errorf("Expected client name 'test-client', got %s", unmarshaled.ClientInfo.Name)
	}
	if unmarshaled.ClientInfo.Version != "1.0.0" {
		t.Errorf("Expected client version '1.0.0', got %s", unmarshaled.ClientInfo.Version)
	}
}

func TestInitializeResponse_Serialization(t *testing.T) {
	resp := InitializeResponse{
		ProtocolVersion: "1.0",
		Capabilities: MCPCapabilities{
			Resources: &ResourcesCapability{ListChanged: true},
		},
		Instructions: "Welcome to the server",
	}
	resp.ServerInfo.Name = "test-server"
	resp.ServerInfo.Version = "2.0.0"
	
	// Test JSON marshaling
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal InitializeResponse: %v", err)
	}
	
	// Test JSON unmarshaling
	var unmarshaled InitializeResponse
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal InitializeResponse: %v", err)
	}
	
	if unmarshaled.ProtocolVersion != "1.0" {
		t.Errorf("Expected protocol version '1.0', got %s", unmarshaled.ProtocolVersion)
	}
	if unmarshaled.ServerInfo.Name != "test-server" {
		t.Errorf("Expected server name 'test-server', got %s", unmarshaled.ServerInfo.Name)
	}
	if unmarshaled.ServerInfo.Version != "2.0.0" {
		t.Errorf("Expected server version '2.0.0', got %s", unmarshaled.ServerInfo.Version)
	}
	if unmarshaled.Instructions != "Welcome to the server" {
		t.Errorf("Expected instructions 'Welcome to the server', got %s", unmarshaled.Instructions)
	}
}

func TestValidateCapabilities_Success(t *testing.T) {
	serverCaps := MCPCapabilities{
		Resources: &ResourcesCapability{ListChanged: true},
		Tools:     &ToolsCapability{ListChanged: false},
		Prompts:   &PromptsCapability{ListChanged: true},
		Sampling:  &SamplingCapability{},
		Logging:   &LoggingCapability{},
	}
	
	requiredCaps := []string{"resources", "tools", "prompts", "sampling", "logging"}
	
	err := ValidateCapabilities(serverCaps, requiredCaps)
	if err != nil {
		t.Errorf("ValidateCapabilities should succeed, got error: %v", err)
	}
}

func TestValidateCapabilities_MissingResources(t *testing.T) {
	serverCaps := MCPCapabilities{
		Tools: &ToolsCapability{ListChanged: false},
	}
	
	requiredCaps := []string{"resources"}
	
	err := ValidateCapabilities(serverCaps, requiredCaps)
	if err == nil {
		t.Error("ValidateCapabilities should fail for missing resources capability")
	}
	if err.Error() != "server does not support resources capability" {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

func TestValidateCapabilities_MissingTools(t *testing.T) {
	serverCaps := MCPCapabilities{
		Resources: &ResourcesCapability{ListChanged: true},
	}
	
	requiredCaps := []string{"tools"}
	
	err := ValidateCapabilities(serverCaps, requiredCaps)
	if err == nil {
		t.Error("ValidateCapabilities should fail for missing tools capability")
	}
	if err.Error() != "server does not support tools capability" {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

func TestValidateCapabilities_MissingPrompts(t *testing.T) {
	serverCaps := MCPCapabilities{}
	
	requiredCaps := []string{"prompts"}
	
	err := ValidateCapabilities(serverCaps, requiredCaps)
	if err == nil {
		t.Error("ValidateCapabilities should fail for missing prompts capability")
	}
	if err.Error() != "server does not support prompts capability" {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

func TestValidateCapabilities_MissingSampling(t *testing.T) {
	serverCaps := MCPCapabilities{}
	
	requiredCaps := []string{"sampling"}
	
	err := ValidateCapabilities(serverCaps, requiredCaps)
	if err == nil {
		t.Error("ValidateCapabilities should fail for missing sampling capability")
	}
	if err.Error() != "server does not support sampling capability" {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

func TestValidateCapabilities_MissingLogging(t *testing.T) {
	serverCaps := MCPCapabilities{}
	
	requiredCaps := []string{"logging"}
	
	err := ValidateCapabilities(serverCaps, requiredCaps)
	if err == nil {
		t.Error("ValidateCapabilities should fail for missing logging capability")
	}
	if err.Error() != "server does not support logging capability" {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

func TestValidateCapabilities_EmptyRequired(t *testing.T) {
	serverCaps := MCPCapabilities{}
	requiredCaps := []string{}
	
	err := ValidateCapabilities(serverCaps, requiredCaps)
	if err != nil {
		t.Errorf("ValidateCapabilities should succeed with empty required capabilities, got error: %v", err)
	}
}