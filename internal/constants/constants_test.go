package constants

import (
	"testing"
	"time"
)

func TestTimeoutConstants(t *testing.T) {
	// Test that timeout constants are positive durations
	timeouts := map[string]time.Duration{
		"DefaultConnectTimeout":     DefaultConnectTimeout,
		"DefaultReadTimeout":        DefaultReadTimeout,
		"DefaultWriteTimeout":       DefaultWriteTimeout,
		"DefaultShutdownTimeout":    DefaultShutdownTimeout,
		"DefaultHealthTimeout":      DefaultHealthTimeout,
		"DefaultStatsTimeout":       DefaultStatsTimeout,
		"DefaultLogStreamTimeout":   DefaultLogStreamTimeout,
		"DefaultCleanupInterval":    DefaultCleanupInterval,
		"DefaultSessionCleanupTime": DefaultSessionCleanupTime,
		"DefaultWebSocketTimeout":   DefaultWebSocketTimeout,
		"DefaultConnectionTimeout":  DefaultConnectionTimeout,
		"DailyCleanupInterval":      DailyCleanupInterval,
		"WebSocketPingInterval":     WebSocketPingInterval,
		"DefaultIdleTimeout":        DefaultIdleTimeout,
		"ShortTimeout":              ShortTimeout,
		"FileOperationTimeout":      FileOperationTimeout,
		"ConnectionKeepAlive":       ConnectionKeepAlive,
		"DefaultRetryDelay":         DefaultRetryDelay,
	}

	for name, timeout := range timeouts {
		if timeout <= 0 {
			t.Errorf("Timeout constant %s should be positive, got %v", name, timeout)
		}
	}
}

func TestBufferSizeConstants(t *testing.T) {
	// Test that buffer size constants are positive integers
	bufferSizes := map[string]int{
		"DefaultBufferSize":    DefaultBufferSize,
		"DefaultChannelBuffer": DefaultChannelBuffer,
		"DefaultIOBufferSize":  DefaultIOBufferSize,
		"WebSocketBufferSize":  WebSocketBufferSize,
		"WebSocketChannelSize": WebSocketChannelSize,
		"ActivityChannelSize":  ActivityChannelSize,
	}

	for name, size := range bufferSizes {
		if size <= 0 {
			t.Errorf("Buffer size constant %s should be positive, got %d", name, size)
		}
	}
}

func TestFilePermissionConstants(t *testing.T) {
	// Test that file permission constants are valid
	permissions := map[string]int{
		"DefaultFileMode":    int(DefaultFileMode),
		"DefaultDirMode":     int(DefaultDirMode),
		"ExecutableFileMode": int(ExecutableFileMode),
	}

	for name, perm := range permissions {
		if perm < 0 || perm > 0777 {
			t.Errorf("File permission constant %s should be valid octal, got %o", name, perm)
		}
	}
}

func TestRetryConstants(t *testing.T) {
	// Test that retry constants are positive
	retryValues := map[string]int{
		"DefaultRetryAttempts": DefaultRetryAttempts,
		"DefaultRetryLimit":    DefaultRetryLimit,
		"DefaultRetryCount":    DefaultRetryCount,
		"RetryMaxAttempts":     RetryMaxAttempts,
	}

	for name, value := range retryValues {
		if value <= 0 {
			t.Errorf("Retry constant %s should be positive, got %d", name, value)
		}
	}
}

func TestPortConstants(t *testing.T) {
	// Test that port constants are in valid range
	ports := map[string]int{
		"DefaultProxyPort":      DefaultProxyPort,
		"DefaultMemoryHTTPPort": DefaultMemoryHTTPPort,
		"TaskSchedulerDefaultPort": TaskSchedulerDefaultPort,
	}

	for name, port := range ports {
		if port < 1 || port > 65535 {
			t.Errorf("Port constant %s should be in valid range (1-65535), got %d", name, port)
		}
	}
}

func TestHTTPStatusConstants(t *testing.T) {
	// Test that HTTP status constants are valid
	if HTTPStatusNotFound != 404 {
		t.Errorf("HTTPStatusNotFound should be 404, got %d", HTTPStatusNotFound)
	}
	if HTTPStatusOK != 200 {
		t.Errorf("HTTPStatusOK should be 200, got %d", HTTPStatusOK)
	}
	if HTTPStatusSuccess != 200 {
		t.Errorf("HTTPStatusSuccess should be 200, got %d", HTTPStatusSuccess)
	}
}

func TestStringConstants(t *testing.T) {
	// Test that string constants are not empty when they should contain values
	stringConstants := map[string]string{
		"DefaultWorkspacePath":     DefaultWorkspacePath,
		"DefaultDatabasePath":      DefaultDatabasePath,
		"DefaultHostInterface":     DefaultHostInterface,
		"DefaultLogLevel":          DefaultLogLevel,
		"DefaultGoModulesProxy":    DefaultGoModulesProxy,
		"DefaultDockerProgressMode": DefaultDockerProgressMode,
		"DefaultBuildCacheOption":  DefaultBuildCacheOption,
		"ResourceLimitCPUs":        ResourceLimitCPUs,
		"ResourceLimitMemory":      ResourceLimitMemory,
	}

	for name, value := range stringConstants {
		if value == "" {
			t.Errorf("String constant %s should not be empty", name)
		}
	}
}

func TestNumericConstants(t *testing.T) {
	// Test specific numeric constant values
	if HoursInDay != 24 {
		t.Errorf("HoursInDay should be 24, got %d", HoursInDay)
	}
	if SecondsInMinute != 60 {
		t.Errorf("SecondsInMinute should be 60, got %d", SecondsInMinute)
	}
	if PercentageMultiplier != 100 {
		t.Errorf("PercentageMultiplier should be 100, got %d", PercentageMultiplier)
	}
	if PercentageMultiplierFloat != 100.0 {
		t.Errorf("PercentageMultiplierFloat should be 100.0, got %f", PercentageMultiplierFloat)
	}
}

func TestConversionConstants(t *testing.T) {
	// Test conversion constants are reasonable
	if NanosecondsToMilliseconds != 1e6 {
		t.Errorf("NanosecondsToMilliseconds should be 1e6, got %g", NanosecondsToMilliseconds)
	}
	if NanoTimeBase != 1e9 {
		t.Errorf("NanoTimeBase should be 1e9, got %g", NanoTimeBase)
	}
	if IDGenerationBase != 10 {
		t.Errorf("IDGenerationBase should be 10, got %d", IDGenerationBase)
	}
}

func TestSleepDurationConstants(t *testing.T) {
	// Test that sleep durations are positive and in logical order
	if ShortSleepDuration <= 0 {
		t.Errorf("ShortSleepDuration should be positive, got %v", ShortSleepDuration)
	}
	if MediumSleepDuration <= ShortSleepDuration {
		t.Errorf("MediumSleepDuration should be greater than ShortSleepDuration")
	}
	if LongSleepDuration <= MediumSleepDuration {
		t.Errorf("LongSleepDuration should be greater than MediumSleepDuration")
	}
}

func TestTransportConstants(t *testing.T) {
	// Test HTTP transport constants are reasonable
	transportConstants := map[string]int{
		"HTTPTransportMaxIdleConns":        HTTPTransportMaxIdleConns,
		"HTTPTransportMaxIdleConnsPerHost": HTTPTransportMaxIdleConnsPerHost,
		"HTTPTransportMaxConnsPerHost":     HTTPTransportMaxConnsPerHost,
		"HTTP2TransportMaxIdleConns":       HTTP2TransportMaxIdleConns,
		"HTTP2TransportMaxIdleConnsPerHost": HTTP2TransportMaxIdleConnsPerHost,
		"HTTP2TransportMaxConnsPerHost":    HTTP2TransportMaxConnsPerHost,
	}

	for name, value := range transportConstants {
		if value <= 0 {
			t.Errorf("Transport constant %s should be positive, got %d", name, value)
		}
	}
}