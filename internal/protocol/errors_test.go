package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *MCPError
		expected string
	}{
		{
			name: "error without data",
			err: &MCPError{
				Code:    InvalidRequest,
				Message: "Invalid request format",
			},
			expected: "MCP Error -32600: Invalid request format",
		},
		{
			name: "error with data",
			err: &MCPError{
				Code:    InvalidParams,
				Message: "Missing required parameter",
				Data: map[string]interface{}{
					"parameter": "name",
					"received":  nil,
				},
			},
			expected: "MCP Error -32602: Missing required parameter (data:",
		},
		{
			name: "error with empty data map",
			err: &MCPError{
				Code:    InternalError,
				Message: "Server error",
				Data:    map[string]interface{}{},
			},
			expected: "MCP Error -32603: Server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.Error()
			if !strings.Contains(result, tt.expected) {
				t.Errorf("MCPError.Error() = %q, want to contain %q", result, tt.expected)
			}
		})
	}
}

func TestNewMCPError(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		message  string
		data     map[string]interface{}
		expected *MCPError
	}{
		{
			name:    "simple error",
			code:    ParseError,
			message: "Parse error",
			data:    nil,
			expected: &MCPError{
				Code:    ParseError,
				Message: "Parse error",
				Data:    nil,
			},
		},
		{
			name:    "error with data",
			code:    ValidationError,
			message: "Validation failed",
			data:    map[string]interface{}{"field": "email"},
			expected: &MCPError{
				Code:    ValidationError,
				Message: "Validation failed",
				Data:    map[string]interface{}{"field": "email"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewMCPError(tt.code, tt.message, tt.data)
			
			if result.Code != tt.expected.Code {
				t.Errorf("NewMCPError().Code = %d, want %d", result.Code, tt.expected.Code)
			}
			if result.Message != tt.expected.Message {
				t.Errorf("NewMCPError().Message = %q, want %q", result.Message, tt.expected.Message)
			}
			if !mapsEqual(result.Data, tt.expected.Data) {
				t.Errorf("NewMCPError().Data = %v, want %v", result.Data, tt.expected.Data)
			}
		})
	}
}

func TestMCPError_JSONSerialization(t *testing.T) {
	err := &MCPError{
		Code:    RequestFailed,
		Message: "Operation failed",
		Data: map[string]interface{}{
			"reason": "timeout",
			"retry":  true,
		},
	}

	// Test marshaling
	data, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("Failed to marshal MCPError: %v", marshalErr)
	}

	// Test unmarshaling
	var unmarshaled MCPError
	if unmarshalErr := json.Unmarshal(data, &unmarshaled); unmarshalErr != nil {
		t.Fatalf("Failed to unmarshal MCPError: %v", unmarshalErr)
	}

	// Verify the round-trip
	if unmarshaled.Code != err.Code {
		t.Errorf("Code mismatch after JSON round-trip: got %d, want %d", unmarshaled.Code, err.Code)
	}
	if unmarshaled.Message != err.Message {
		t.Errorf("Message mismatch after JSON round-trip: got %q, want %q", unmarshaled.Message, err.Message)
	}
	if !mapsEqual(unmarshaled.Data, err.Data) {
		t.Errorf("Data mismatch after JSON round-trip: got %v, want %v", unmarshaled.Data, err.Data)
	}
}

func TestErrorConstants(t *testing.T) {
	// Test that error codes are in expected ranges
	jsonRPCErrors := []int{ParseError, InvalidRequest, MethodNotFound, InvalidParams, InternalError}
	for _, code := range jsonRPCErrors {
		if code <= -32600 && code >= -32700 {
			// JSON-RPC 2.0 reserved range is -32768 to -32000, with specific codes -32700 to -32600
		} else {
			t.Errorf("JSON-RPC error code %d is not in the expected range (-32700 to -32600)", code)
		}
	}

	mcpErrors := []int{
		RequestFailed, RequestCancelled, RequestTimeout,
		TransportError, SessionError, CapabilityError, ProtocolError,
		AuthenticationError, AuthorizationError, RateLimitError,
		ResourceError, ValidationError, ExecutionError, StateError, ConfigurationError,
	}
	for _, code := range mcpErrors {
		if code > -31988 || code < -32002 {
			t.Errorf("MCP error code %d is not in the expected range (-32002 to -31988)", code)
		}
	}
}

func TestStandardErrorConstructors(t *testing.T) {
	tests := []struct {
		name     string
		errorFn  func() *MCPError
		wantCode int
		wantType string
	}{
		{
			name:     "NewParseError",
			errorFn:  func() *MCPError { return NewParseError("invalid JSON") },
			wantCode: ParseError,
			wantType: "parse_error",
		},
		{
			name:     "NewInvalidRequest",
			errorFn:  func() *MCPError { return NewInvalidRequest("missing id field") },
			wantCode: InvalidRequest,
			wantType: "invalid_request",
		},
		{
			name:     "NewMethodNotFound",
			errorFn:  func() *MCPError { return NewMethodNotFound("unknown_method") },
			wantCode: MethodNotFound,
			wantType: "method_not_found",
		},
		{
			name:     "NewInvalidParams",
			errorFn:  func() *MCPError { return NewInvalidParams("missing param", map[string]interface{}{"expected": "name"}) },
			wantCode: InvalidParams,
			wantType: "invalid_params",
		},
		{
			name:     "NewInternalError",
			errorFn:  func() *MCPError { return NewInternalError("database connection failed") },
			wantCode: InternalError,
			wantType: "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.errorFn()
			
			if err.Code != tt.wantCode {
				t.Errorf("%s Code = %d, want %d", tt.name, err.Code, tt.wantCode)
			}
			if err.Data["type"] != tt.wantType {
				t.Errorf("%s Data[\"type\"] = %v, want %v", tt.name, err.Data["type"], tt.wantType)
			}
		})
	}
}

func TestMCPSpecificErrorConstructors(t *testing.T) {
	tests := []struct {
		name     string
		errorFn  func() *MCPError
		wantCode int
		wantType string
	}{
		{
			name:     "NewRequestTimeout",
			errorFn:  func() *MCPError { return NewRequestTimeout("process_request", "30s") },
			wantCode: RequestTimeout,
			wantType: "timeout_error",
		},
		{
			name:     "NewTransportError",
			errorFn:  func() *MCPError { return NewTransportError("websocket", "connection closed") },
			wantCode: TransportError,
			wantType: "transport_error",
		},
		{
			name:     "NewSessionError",
			errorFn:  func() *MCPError { return NewSessionError("session-123", "expired") },
			wantCode: SessionError,
			wantType: "session_error",
		},
		{
			name:     "NewCapabilityError",
			errorFn:  func() *MCPError { return NewCapabilityError("tools", "not supported") },
			wantCode: CapabilityError,
			wantType: "capability_error",
		},
		{
			name:     "NewProtocolError",
			errorFn:  func() *MCPError { return NewProtocolError("2.0", "1.0") },
			wantCode: ProtocolError,
			wantType: "protocol_error",
		},
		{
			name:     "NewAuthenticationError",
			errorFn:  func() *MCPError { return NewAuthenticationError("invalid token") },
			wantCode: AuthenticationError,
			wantType: "authentication_error",
		},
		{
			name:     "NewAuthorizationError",
			errorFn:  func() *MCPError { return NewAuthorizationError("file.txt", "read") },
			wantCode: AuthorizationError,
			wantType: "authorization_error",
		},
		{
			name:     "NewRateLimitError",
			errorFn:  func() *MCPError { return NewRateLimitError("100/hour", "1 hour") },
			wantCode: RateLimitError,
			wantType: "rate_limit_error",
		},
		{
			name:     "NewResourceError",
			errorFn:  func() *MCPError { return NewResourceError("database", "connect", "timeout") },
			wantCode: ResourceError,
			wantType: "resource_error",
		},
		{
			name:     "NewValidationError",
			errorFn:  func() *MCPError { return NewValidationError("email", "invalid", "must be valid email") },
			wantCode: ValidationError,
			wantType: "validation_error",
		},
		{
			name:     "NewExecutionError",
			errorFn:  func() *MCPError { return NewExecutionError("file_reader", "permission denied") },
			wantCode: ExecutionError,
			wantType: "execution_error",
		},
		{
			name:     "NewStateError",
			errorFn:  func() *MCPError { return NewStateError("initialized", "connecting") },
			wantCode: StateError,
			wantType: "state_error",
		},
		{
			name:     "NewConfigurationError",
			errorFn:  func() *MCPError { return NewConfigurationError("server", "missing required field") },
			wantCode: ConfigurationError,
			wantType: "configuration_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.errorFn()
			
			if err.Code != tt.wantCode {
				t.Errorf("%s Code = %d, want %d", tt.name, err.Code, tt.wantCode)
			}
			if err.Data["type"] != tt.wantType {
				t.Errorf("%s Data[\"type\"] = %v, want %v", tt.name, err.Data["type"], tt.wantType)
			}
		})
	}
}

func TestMCPError_IsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      *MCPError
		expected bool
	}{
		{
			name:     "RequestTimeout is retryable",
			err:      NewRequestTimeout("test", "30s"),
			expected: true,
		},
		{
			name:     "TransportError is retryable",
			err:      NewTransportError("websocket", "connection lost"),
			expected: true,
		},
		{
			name:     "SessionError is retryable", 
			err:      NewSessionError("123", "expired"),
			expected: true,
		},
		{
			name:     "RateLimitError is retryable",
			err:      NewRateLimitError("100/hour", "1 hour"),
			expected: true,
		},
		{
			name:     "ValidationError is not retryable",
			err:      NewValidationError("email", "invalid", "must be valid email"),
			expected: false,
		},
		{
			name:     "AuthenticationError is not retryable",
			err:      NewAuthenticationError("invalid token"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.IsRetryable()
			if result != tt.expected {
				t.Errorf("IsRetryable() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMCPError_IsTemporary(t *testing.T) {
	// IsTemporary should return the same as IsRetryable based on the implementation
	err := NewRequestTimeout("test", "30s")
	if err.IsTemporary() != err.IsRetryable() {
		t.Errorf("IsTemporary() != IsRetryable() for same error")
	}
}

func TestMCPError_GetRetryDelay(t *testing.T) {
	tests := []struct {
		name         string
		err          *MCPError
		expectedGT   int // greater than
		expectedLTE  int // less than or equal
	}{
		{
			name:        "RequestTimeout has reasonable delay",
			err:         NewRequestTimeout("test", "30s"),
			expectedGT:  0,
			expectedLTE: 10,
		},
		{
			name:        "TransportError has reasonable delay",
			err:         NewTransportError("websocket", "connection lost"),
			expectedGT:  0,
			expectedLTE: 10,
		},
		{
			name:        "SessionError has longer delay",
			err:         NewSessionError("123", "expired"),
			expectedGT:  10,
			expectedLTE: 60,
		},
		{
			name:        "RateLimitError has longest delay",
			err:         NewRateLimitError("100/hour", "1 hour"),
			expectedGT:  30,
			expectedLTE: 300,
		},
		{
			name:        "Non-retryable error has zero delay",
			err:         NewValidationError("email", "invalid", "must be valid email"),
			expectedGT:  -1,
			expectedLTE: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.GetRetryDelay()
			if result <= tt.expectedGT || result > tt.expectedLTE {
				t.Errorf("GetRetryDelay() = %d, want > %d and <= %d", result, tt.expectedGT, tt.expectedLTE)
			}
		})
	}
}

// Helper function to compare maps
func mapsEqual(a, b map[string]interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}