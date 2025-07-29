package config

import "time"

// Auto-scaling constants
const (
	// DefaultAutoScalingMultiplier is the default multiplier for scaling up/down (25%)
	DefaultAutoScalingMultiplier float32 = 0.25
	
	// DefaultTargetCPUUtilization is the default target CPU utilization for auto-scaling
	DefaultTargetCPUUtilization int32 = 70
	
	// DefaultMinConcurrentTasks is the default minimum number of concurrent tasks
	DefaultMinConcurrentTasks int32 = 1
	
	// DefaultMaxConcurrentTasks is the default maximum number of concurrent tasks
	DefaultMaxConcurrentTasks int32 = 10
	
	// DefaultScaleUpCooldown is the default cooldown period for scaling up
	DefaultScaleUpCooldown = 30 * time.Second
	
	// DefaultScaleDownCooldown is the default cooldown period for scaling down
	DefaultScaleDownCooldown = 5 * time.Minute
	
	// DefaultMetricsInterval is the default interval for collecting metrics
	DefaultMetricsInterval = 15 * time.Second
)

// Task scheduler constants
const (
	// DefaultTaskTimeout is the default timeout for tasks
	DefaultTaskTimeout = 30 * time.Minute
	
	// DefaultRetryAttempts is the default number of retry attempts
	DefaultRetryAttempts int32 = 3
	
	// DefaultBackoffMultiplier is the default backoff multiplier for retries
	DefaultBackoffMultiplier float32 = 2.0
	
	// DefaultInitialBackoff is the default initial backoff duration
	DefaultInitialBackoff = 1 * time.Second
	
	// DefaultMaxBackoff is the default maximum backoff duration
	DefaultMaxBackoff = 60 * time.Second
)

// Connection and health check constants
const (
	// DefaultHealthCheckInterval is the default interval for health checks
	DefaultHealthCheckInterval = 30 * time.Second
	
	// DefaultHealthCheckTimeout is the default timeout for health checks
	DefaultHealthCheckTimeout = 5 * time.Second
	
	// DefaultReconnectInterval is the default interval for reconnection attempts
	DefaultReconnectInterval = 10 * time.Second
	
	// DefaultMaxReconnectAttempts is the default maximum number of reconnection attempts
	DefaultMaxReconnectAttempts int32 = 5
)

// Resource limits constants
const (
	// DefaultMemoryLimit is the default memory limit for containers
	DefaultMemoryLimit = "512Mi"
	
	// DefaultCPULimit is the default CPU limit for containers
	DefaultCPULimit = "500m"
	
	// DefaultMemoryRequest is the default memory request for containers
	DefaultMemoryRequest = "256Mi"
	
	// DefaultCPURequest is the default CPU request for containers
	DefaultCPURequest = "100m"
)

// Reconciliation constants
const (
	// DefaultReconcileInterval is the default reconciliation interval
	DefaultReconcileInterval = 2 * time.Minute
	
	// DefaultReconcileTimeout is the default reconciliation timeout
	DefaultReconcileTimeout = 5 * time.Minute
	
	// DefaultRequeueAfterError is the default requeue duration after an error
	DefaultRequeueAfterError = 30 * time.Second
)

// Utilization thresholds
const (
	// LowUtilizationThreshold is the threshold below which we consider scaling down
	// This is calculated as TargetCPUUtilization / 2
	LowUtilizationDivisor float32 = 2.0
	
	// ScaleUpThreshold is the threshold above which we consider scaling up
	// This uses the TargetCPUUtilization directly
	ScaleUpThreshold = "target_cpu_utilization"
)