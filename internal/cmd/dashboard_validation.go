package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
)

// ValidationResult represents the result of configuration validation
type ValidationResult struct {
	Valid    bool              `json:"valid"`
	Errors   []ValidationError `json:"errors"`
	Warnings []ValidationError `json:"warnings"`
}

// ValidationError represents a validation error or warning
type ValidationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	Type    string `json:"type"` // "error" or "warning"
}

// ConfigValidator validates matey configurations
type ConfigValidator struct {
	knownCRDs map[string]schema.GroupVersionKind
}

// NewConfigValidator creates a new configuration validator
func NewConfigValidator() *ConfigValidator {
	return &ConfigValidator{
		knownCRDs: map[string]schema.GroupVersionKind{
			"MCPServer": {
				Group:   "mcp.dev",
				Version: "v1",
				Kind:    "MCPServer",
			},
			"MCPMemory": {
				Group:   "mcp.dev",
				Version: "v1",
				Kind:    "MCPMemory",
			},
			"MCPTaskScheduler": {
				Group:   "mcp.dev",
				Version: "v1",
				Kind:    "MCPTaskScheduler",
			},
			"MCPProxy": {
				Group:   "mcp.dev",
				Version: "v1",
				Kind:    "MCPProxy",
			},
			"MCPWorkflow": {
				Group:   "mcp.dev",
				Version: "v1",
				Kind:    "MCPWorkflow",
			},
		},
	}
}

// ValidateYAML validates a YAML configuration string
func (v *ConfigValidator) ValidateYAML(yamlContent string) ValidationResult {
	result := ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationError{},
	}

	// Parse YAML
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &config); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Path:    "root",
			Message: fmt.Sprintf("Invalid YAML: %s", err.Error()),
			Type:    "error",
		})
		return result
	}

	// Validate Kubernetes resource structure
	if err := v.validateKubernetesResource(config, &result); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Path:    "root",
			Message: err.Error(),
			Type:    "error",
		})
	}

	// Validate MCP-specific fields
	if kind, ok := config["kind"].(string); ok {
		switch kind {
		case "MCPServer":
			v.validateMCPServer(config, &result)
		case "MCPMemory":
			v.validateMCPMemory(config, &result)
		case "MCPTaskScheduler":
			v.validateMCPTaskScheduler(config, &result)
		case "MCPProxy":
			v.validateMCPProxy(config, &result)
		case "MCPWorkflow":
			v.validateMCPWorkflow(config, &result)
		}
	}

	return result
}

// validateKubernetesResource validates basic Kubernetes resource structure
func (v *ConfigValidator) validateKubernetesResource(config map[string]interface{}, result *ValidationResult) error {
	// Check required fields
	requiredFields := []string{"apiVersion", "kind", "metadata", "spec"}
	for _, field := range requiredFields {
		if _, ok := config[field]; !ok {
			result.Errors = append(result.Errors, ValidationError{
				Path:    field,
				Message: fmt.Sprintf("Required field '%s' is missing", field),
				Type:    "error",
			})
		}
	}

	// Validate apiVersion
	if apiVersion, ok := config["apiVersion"].(string); ok {
		if !strings.Contains(apiVersion, "/") {
			result.Warnings = append(result.Warnings, ValidationError{
				Path:    "apiVersion",
				Message: "apiVersion should include group (e.g., 'mcp.dev/v1')",
				Type:    "warning",
			})
		}
	}

	// Validate kind
	if kind, ok := config["kind"].(string); ok {
		if _, known := v.knownCRDs[kind]; !known {
			result.Warnings = append(result.Warnings, ValidationError{
				Path:    "kind",
				Message: fmt.Sprintf("Unknown kind '%s' - may not be a matey CRD", kind),
				Type:    "warning",
			})
		}
	}

	// Validate metadata
	if metadata, ok := config["metadata"].(map[string]interface{}); ok {
		if name, ok := metadata["name"].(string); ok {
			if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
				result.Errors = append(result.Errors, ValidationError{
					Path:    "metadata.name",
					Message: fmt.Sprintf("Invalid name: %s", strings.Join(errs, ", ")),
					Type:    "error",
				})
			}
		} else {
			result.Errors = append(result.Errors, ValidationError{
				Path:    "metadata.name",
				Message: "metadata.name is required",
				Type:    "error",
			})
		}
	}

	return nil
}

// validateMCPServer validates MCPServer-specific fields
func (v *ConfigValidator) validateMCPServer(config map[string]interface{}, result *ValidationResult) {
	spec, ok := config["spec"].(map[string]interface{})
	if !ok {
		return
	}

	// Validate image
	if image, ok := spec["image"].(string); ok {
		if !v.isValidImageName(image) {
			result.Errors = append(result.Errors, ValidationError{
				Path:    "spec.image",
				Message: "Invalid container image name",
				Type:    "error",
			})
		}
	} else {
		result.Errors = append(result.Errors, ValidationError{
			Path:    "spec.image",
			Message: "spec.image is required for MCPServer",
			Type:    "error",
		})
	}

	// Validate port
	if port, ok := spec["port"].(int); ok {
		if port < 1 || port > 65535 {
			result.Errors = append(result.Errors, ValidationError{
				Path:    "spec.port",
				Message: "Port must be between 1 and 65535",
				Type:    "error",
			})
		}
	}

	// Validate protocol
	if protocol, ok := spec["protocol"].(string); ok {
		validProtocols := []string{"http", "websocket", "sse", "stdio"}
		if !v.contains(validProtocols, protocol) {
			result.Errors = append(result.Errors, ValidationError{
				Path:    "spec.protocol",
				Message: fmt.Sprintf("Invalid protocol '%s'. Must be one of: %s", protocol, strings.Join(validProtocols, ", ")),
				Type:    "error",
			})
		}
	}

	// Validate replicas
	if replicas, ok := spec["replicas"].(int); ok {
		if replicas < 0 {
			result.Errors = append(result.Errors, ValidationError{
				Path:    "spec.replicas",
				Message: "Replicas cannot be negative",
				Type:    "error",
			})
		}
	}

	// Validate resources
	if resources, ok := spec["resources"].(map[string]interface{}); ok {
		v.validateResources(resources, "spec.resources", result)
	}
}

// validateMCPMemory validates MCPMemory-specific fields
func (v *ConfigValidator) validateMCPMemory(config map[string]interface{}, result *ValidationResult) {
	spec, ok := config["spec"].(map[string]interface{})
	if !ok {
		return
	}

	// Validate storage size
	if storage, ok := spec["storage"].(string); ok {
		if !v.isValidStorageSize(storage) {
			result.Errors = append(result.Errors, ValidationError{
				Path:    "spec.storage",
				Message: "Invalid storage size format (e.g., '1Gi', '500Mi')",
				Type:    "error",
			})
		}
	}

	// Validate database type
	if dbType, ok := spec["type"].(string); ok {
		validTypes := []string{"postgres", "redis", "memory"}
		if !v.contains(validTypes, dbType) {
			result.Errors = append(result.Errors, ValidationError{
				Path:    "spec.type",
				Message: fmt.Sprintf("Invalid database type '%s'. Must be one of: %s", dbType, strings.Join(validTypes, ", ")),
				Type:    "error",
			})
		}
	}
}

// validateMCPTaskScheduler validates MCPTaskScheduler-specific fields
func (v *ConfigValidator) validateMCPTaskScheduler(config map[string]interface{}, result *ValidationResult) {
	spec, ok := config["spec"].(map[string]interface{})
	if !ok {
		return
	}

	// Validate schedule if present
	if schedule, ok := spec["schedule"].(string); ok {
		if !v.isValidCronExpression(schedule) {
			result.Errors = append(result.Errors, ValidationError{
				Path:    "spec.schedule",
				Message: "Invalid cron expression format",
				Type:    "error",
			})
		}
	}

	// Validate concurrency policy
	if policy, ok := spec["concurrencyPolicy"].(string); ok {
		validPolicies := []string{"Allow", "Forbid", "Replace"}
		if !v.contains(validPolicies, policy) {
			result.Errors = append(result.Errors, ValidationError{
				Path:    "spec.concurrencyPolicy",
				Message: fmt.Sprintf("Invalid concurrency policy '%s'. Must be one of: %s", policy, strings.Join(validPolicies, ", ")),
				Type:    "error",
			})
		}
	}
}

// validateMCPProxy validates MCPProxy-specific fields
func (v *ConfigValidator) validateMCPProxy(config map[string]interface{}, result *ValidationResult) {
	spec, ok := config["spec"].(map[string]interface{})
	if !ok {
		return
	}

	// Validate target servers
	if targets, ok := spec["targets"].([]interface{}); ok {
		if len(targets) == 0 {
			result.Errors = append(result.Errors, ValidationError{
				Path:    "spec.targets",
				Message: "At least one target server is required",
				Type:    "error",
			})
		}
	}
}

// validateMCPWorkflow validates MCPWorkflow-specific fields
func (v *ConfigValidator) validateMCPWorkflow(config map[string]interface{}, result *ValidationResult) {
	spec, ok := config["spec"].(map[string]interface{})
	if !ok {
		return
	}

	// Validate steps
	if steps, ok := spec["steps"].([]interface{}); ok {
		if len(steps) == 0 {
			result.Errors = append(result.Errors, ValidationError{
				Path:    "spec.steps",
				Message: "At least one step is required in workflow",
				Type:    "error",
			})
		}
	}
}

// validateResources validates resource specifications
func (v *ConfigValidator) validateResources(resources map[string]interface{}, path string, result *ValidationResult) {
	if requests, ok := resources["requests"].(map[string]interface{}); ok {
		if cpu, ok := requests["cpu"].(string); ok {
			if !v.isValidCPUResource(cpu) {
				result.Errors = append(result.Errors, ValidationError{
					Path:    path + ".requests.cpu",
					Message: "Invalid CPU resource format (e.g., '100m', '1')",
					Type:    "error",
				})
			}
		}
		
		if memory, ok := requests["memory"].(string); ok {
			if !v.isValidMemoryResource(memory) {
				result.Errors = append(result.Errors, ValidationError{
					Path:    path + ".requests.memory",
					Message: "Invalid memory resource format (e.g., '128Mi', '1Gi')",
					Type:    "error",
				})
			}
		}
	}
}

// Helper validation functions

func (v *ConfigValidator) isValidImageName(image string) bool {
	// Simple image name validation
	return strings.Contains(image, "/") || !strings.Contains(image, " ")
}

func (v *ConfigValidator) isValidStorageSize(size string) bool {
	matched, _ := regexp.MatchString(`^\d+[KMGT]i?$`, size)
	return matched
}

func (v *ConfigValidator) isValidCronExpression(expr string) bool {
	// Basic cron validation - 5 or 6 fields
	fields := strings.Fields(expr)
	return len(fields) == 5 || len(fields) == 6
}

func (v *ConfigValidator) isValidCPUResource(cpu string) bool {
	// CPU can be millicore (100m) or whole numbers (1)
	matched, _ := regexp.MatchString(`^\d+m?$`, cpu)
	return matched
}

func (v *ConfigValidator) isValidMemoryResource(memory string) bool {
	// Memory format validation
	matched, _ := regexp.MatchString(`^\d+[KMGT]i?$`, memory)
	return matched
}

func (v *ConfigValidator) contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// FormatValidationResult formats validation results for display
func (v *ConfigValidator) FormatValidationResult(result ValidationResult) string {
	var output strings.Builder
	
	if result.Valid {
		output.WriteString("✅ Configuration is valid\n")
	} else {
		output.WriteString("❌ Configuration has errors\n")
	}
	
	if len(result.Errors) > 0 {
		output.WriteString("\nErrors:\n")
		for _, err := range result.Errors {
			output.WriteString(fmt.Sprintf("  🔴 %s: %s\n", err.Path, err.Message))
		}
	}
	
	if len(result.Warnings) > 0 {
		output.WriteString("\nWarnings:\n")
		for _, warning := range result.Warnings {
			output.WriteString(fmt.Sprintf("  🟡 %s: %s\n", warning.Path, warning.Message))
		}
	}
	
	return output.String()
}