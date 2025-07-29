// internal/scheduler/templates.go
package scheduler

import (
	"fmt"
	"strings"

	"github.com/phildougherty/m8e/internal/crd"
)

type WorkflowTemplate struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Category    string                `json:"category"`
	Tags        []string              `json:"tags"`
	Parameters  []TemplateParameter   `json:"parameters"`
	Spec        crd.WorkflowDefinition `json:"spec"`
}

type TemplateParameter struct {
	Name         string      `json:"name"`
	Description  string      `json:"description"`
	Type         string      `json:"type"`
	Required     bool        `json:"required"`
	Default      interface{} `json:"default,omitempty"`
	Options      []string    `json:"options,omitempty"`
	Validation   string      `json:"validation,omitempty"`
}

type TemplateRegistry struct {
	templates map[string]*WorkflowTemplate
}

func NewTemplateRegistry() *TemplateRegistry {
	registry := &TemplateRegistry{
		templates: make(map[string]*WorkflowTemplate),
	}
	
	// Register built-in templates
	registry.registerBuiltinTemplates()
	
	return registry
}

func (tr *TemplateRegistry) registerBuiltinTemplates() {
	// Health monitoring template
	tr.RegisterTemplate(createHealthMonitoringTemplate())
	
	// Data backup template
	tr.RegisterTemplate(createDataBackupTemplate())
	
	// Report generation template
	tr.RegisterTemplate(createReportGenerationTemplate())
	
	// System maintenance template
	tr.RegisterTemplate(createSystemMaintenanceTemplate())

	// Code quality checks template
	tr.RegisterTemplate(createCodeQualityTemplate())

	// Database maintenance template
	tr.RegisterTemplate(createDatabaseMaintenanceTemplate())
}

func (tr *TemplateRegistry) RegisterTemplate(template *WorkflowTemplate) {
	tr.templates[template.Name] = template
}

func (tr *TemplateRegistry) GetTemplate(name string) (*WorkflowTemplate, bool) {
	template, exists := tr.templates[name]
	return template, exists
}

func (tr *TemplateRegistry) ListTemplates() []*WorkflowTemplate {
	templates := make([]*WorkflowTemplate, 0, len(tr.templates))
	for _, template := range tr.templates {
		templates = append(templates, template)
	}
	return templates
}

func (tr *TemplateRegistry) ListTemplatesByCategory(category string) []*WorkflowTemplate {
	templates := make([]*WorkflowTemplate, 0)
	for _, template := range tr.templates {
		if template.Category == category {
			templates = append(templates, template)
		}
	}
	return templates
}

func (tr *TemplateRegistry) CreateWorkflowFromTemplate(templateName string, workflowName string, parameters map[string]interface{}) (*crd.WorkflowDefinition, error) {
	template, exists := tr.GetTemplate(templateName)
	if !exists {
		return nil, fmt.Errorf("template %q not found", templateName)
	}

	// Validate required parameters
	for _, param := range template.Parameters {
		if param.Required {
			if _, exists := parameters[param.Name]; !exists {
				return nil, fmt.Errorf("required parameter %q missing", param.Name)
			}
		}
	}

	// Create a copy of the template spec
	spec := &crd.WorkflowDefinition{
		Schedule:                   template.Spec.Schedule,
		Timezone:                   template.Spec.Timezone,
		Enabled:                    template.Spec.Enabled,
		Steps:                      make([]crd.WorkflowStep, len(template.Spec.Steps)),
		RetryPolicy:                template.Spec.RetryPolicy,
		Timeout:                    template.Spec.Timeout,
		ConcurrencyPolicy:          template.Spec.ConcurrencyPolicy,
		SuccessfulJobsHistoryLimit: template.Spec.SuccessfulJobsHistoryLimit,
		FailedJobsHistoryLimit:     template.Spec.FailedJobsHistoryLimit,
	}
	
	// Copy steps manually to ensure proper deep copy
	for i, step := range template.Spec.Steps {
		spec.Steps[i] = crd.WorkflowStep{
			Name:            step.Name,
			Tool:            step.Tool,
			Parameters:      copyMap(step.Parameters),
			Condition:       step.Condition,
			Timeout:         step.Timeout,
			RetryPolicy:     step.RetryPolicy,
			DependsOn:       copyStringSlice(step.DependsOn),
			ContinueOnError: step.ContinueOnError,
			RunPolicy:       step.RunPolicy,
		}
	}

	// Apply parameter substitutions
	if err := tr.applyParameterSubstitutions(spec, template.Parameters, parameters); err != nil {
		return nil, fmt.Errorf("failed to apply parameters: %w", err)
	}

	return spec, nil
}

func (tr *TemplateRegistry) applyParameterSubstitutions(spec *crd.WorkflowDefinition, templateParams []TemplateParameter, userParams map[string]interface{}) error {
	// Create parameter map with defaults
	paramMap := make(map[string]interface{})
	
	for _, param := range templateParams {
		if param.Default != nil {
			paramMap[param.Name] = param.Default
		}
	}
	
	// Override with user parameters
	for key, value := range userParams {
		paramMap[key] = value
	}

	// Apply substitutions to schedule
	if schedule, ok := paramMap["schedule"].(string); ok {
		spec.Schedule = schedule
	}

	// Apply substitutions to timezone
	if timezone, ok := paramMap["timezone"].(string); ok {
		spec.Timezone = timezone
	}

	// Apply substitutions to steps - this was the bug, steps need to be copied properly
	// The template spec already has the steps defined, we just need to substitute parameters
	for i := range spec.Steps {
		step := &spec.Steps[i]
		
		// Substitute in parameters
		if step.Parameters != nil {
			newParams := make(map[string]interface{})
			for key, value := range step.Parameters {
				if substituted, err := tr.substituteValue(value, paramMap); err == nil {
					newParams[key] = substituted
				} else {
					newParams[key] = value
				}
			}
			step.Parameters = newParams
		}
		
		// Substitute in condition
		if step.Condition != "" {
			if substituted, err := tr.substituteValue(step.Condition, paramMap); err == nil {
				if str, ok := substituted.(string); ok {
					step.Condition = str
				}
			}
		}
	}

	return nil
}

func (tr *TemplateRegistry) substituteValue(value interface{}, params map[string]interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// Simple template substitution - look for {{.param_name}} patterns
		result := v
		for paramName, paramValue := range params {
			placeholder := fmt.Sprintf("{{.%s}}", paramName)
			if strings.Contains(result, placeholder) {
				if str, ok := paramValue.(string); ok {
					result = strings.ReplaceAll(result, placeholder, str)
				} else {
					result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", paramValue))
				}
			}
		}
		return result, nil
	case map[string]interface{}:
		newMap := make(map[string]interface{})
		for k, val := range v {
			if substituted, err := tr.substituteValue(val, params); err == nil {
				newMap[k] = substituted
			} else {
				newMap[k] = val
			}
		}
		return newMap, nil
	case []interface{}:
		newSlice := make([]interface{}, len(v))
		for i, val := range v {
			if substituted, err := tr.substituteValue(val, params); err == nil {
				newSlice[i] = substituted
			} else {
				newSlice[i] = val
			}
		}
		return newSlice, nil
	default:
		return value, nil
	}
}


// Helper functions for deep copying
func copyMap(original map[string]interface{}) map[string]interface{} {
	if original == nil {
		return nil
	}
	copy := make(map[string]interface{})
	for key, value := range original {
		copy[key] = value
	}
	return copy
}

func copyStringSlice(original []string) []string {
	if original == nil {
		return nil
	}
	copy := make([]string, len(original))
	for i, v := range original {
		copy[i] = v
	}
	return copy
}

// Built-in template creators

func createHealthMonitoringTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Name:        "health-monitoring",
		Description: "Monitor health metrics and send alerts when thresholds are exceeded",
		Category:    "monitoring",
		Tags:        []string{"health", "alerts", "monitoring"},
		Parameters: []TemplateParameter{
			{
				Name:        "schedule",
				Description: "Cron expression for monitoring schedule",
				Type:        "string",
				Required:    false,
				Default:     "0 */6 * * *",
			},
			{
				Name:        "timezone",
				Description: "Timezone for schedule",
				Type:        "string",
				Required:    false,
				Default:     "UTC",
			},
			{
				Name:        "alert_channel",
				Description: "Channel for sending alerts",
				Type:        "string",
				Required:    true,
			},
			{
				Name:        "glucose_threshold_high",
				Description: "High glucose threshold for alerts",
				Type:        "number",
				Required:    false,
				Default:     180,
			},
			{
				Name:        "glucose_threshold_low",
				Description: "Low glucose threshold for alerts",
				Type:        "number",
				Required:    false,
				Default:     80,
			},
		},
		Spec: crd.WorkflowDefinition{
			Schedule: "0 */6 * * *",
			Timezone: "UTC",
			Enabled:  true,
			Steps: []crd.WorkflowStep{
				{
					Name: "check-glucose",
					Tool: "get_current_glucose",
					Timeout: "30s",
				},
				{
					Name: "analyze-trend",
					Tool: "analyze_health_data",
					Parameters: map[string]interface{}{
						"current_reading": "{{ steps.check-glucose.output.glucose }}",
						"history_hours":   24,
						"high_threshold":  "{{.glucose_threshold_high}}",
						"low_threshold":   "{{.glucose_threshold_low}}",
					},
					DependsOn: []string{"check-glucose"},
					Timeout: "1m",
				},
				{
					Name: "send-alert",
					Tool: "send_notification",
					Parameters: map[string]interface{}{
						"message": "{{ steps.analyze-trend.output.summary }}",
						"channel": "{{.alert_channel}}",
						"urgency": "{{ steps.analyze-trend.output.urgency }}",
					},
					Condition:   "{{ steps.analyze-trend.output.alert_needed }}",
					DependsOn:   []string{"analyze-trend"},
					Timeout: "30s",
					RunPolicy:   crd.WorkflowRunOnCondition,
				},
			},
			// TODO: Update RetryPolicy to use WorkflowRetryPolicy  
			// RetryPolicy: &crd.WorkflowRetryPolicy{
			//	MaxRetries: 3,
			//	RetryDelay: "30s",
			//	BackoffStrategy: crd.WorkflowBackoffLinear,
			// },
		},
	}
}

func createDataBackupTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Name:        "data-backup",
		Description: "Automated data backup with compression and cloud upload",
		Category:    "backup",
		Tags:        []string{"backup", "data", "cloud", "compression"},
		Parameters: []TemplateParameter{
			{
				Name:        "schedule",
				Description: "Backup schedule (cron expression)",
				Type:        "string",
				Required:    false,
				Default:     "0 2 * * *",
			},
			{
				Name:        "source_path",
				Description: "Path to backup",
				Type:        "string",
				Required:    true,
			},
			{
				Name:        "backup_name",
				Description: "Name for the backup",
				Type:        "string",
				Required:    true,
			},
			{
				Name:        "cloud_provider",
				Description: "Cloud storage provider",
				Type:        "string",
				Required:    true,
				Options:     []string{"s3", "gcs", "azure"},
			},
			{
				Name:        "retention_days",
				Description: "Number of days to retain backups",
				Type:        "number",
				Required:    false,
				Default:     30,
			},
		},
		Spec: crd.WorkflowDefinition{
			Schedule: "0 2 * * *",
			Enabled:  true,
			Steps: []crd.WorkflowStep{
				{
					Name: "create-backup",
					Tool: "create_file_archive",
					Parameters: map[string]interface{}{
						"source_path":   "{{.source_path}}",
						"archive_name":  "{{.backup_name}}",
						"compression":   "gzip",
						"exclude_patterns": []string{".git", "node_modules", "*.log"},
					},
					Timeout: "30m",
				},
				{
					Name: "upload-backup",
					Tool: "upload_to_cloud",
					Parameters: map[string]interface{}{
						"file_path": "{{ steps.create-backup.output.archive_path }}",
						"provider":  "{{.cloud_provider}}",
						"bucket":    "backups",
						"key":       "{{ steps.create-backup.output.archive_name }}",
					},
					DependsOn: []string{"create-backup"},
					Timeout: "15m",
				},
				{
					Name: "cleanup-old-backups",
					Tool: "cleanup_cloud_files",
					Parameters: map[string]interface{}{
						"provider":      "{{.cloud_provider}}",
						"bucket":        "backups",
						"prefix":        "{{.backup_name}}",
						"retention_days": "{{.retention_days}}",
					},
					DependsOn: []string{"upload-backup"},
					Timeout: "5m",
				},
				{
					Name: "send-report",
					Tool: "send_notification",
					Parameters: map[string]interface{}{
						"message": "Backup completed: {{ steps.create-backup.output.archive_name }} ({{ steps.create-backup.output.size }})",
						"channel": "backup-reports",
					},
					DependsOn:       []string{"cleanup-old-backups"},
					ContinueOnError: true,
				},
			},
		},
	}
}

func createReportGenerationTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Name:        "report-generation",
		Description: "Generate and distribute periodic reports from data sources",
		Category:    "reporting",
		Tags:        []string{"reports", "data", "analytics", "distribution"},
		Parameters: []TemplateParameter{
			{
				Name:        "schedule",
				Description: "Report generation schedule",
				Type:        "string",
				Required:    false,
				Default:     "0 8 * * 1",
			},
			{
				Name:        "report_type",
				Description: "Type of report to generate",
				Type:        "string",
				Required:    true,
				Options:     []string{"weekly", "monthly", "quarterly", "custom"},
			},
			{
				Name:        "data_source",
				Description: "Data source for the report",
				Type:        "string",
				Required:    true,
			},
			{
				Name:        "recipients",
				Description: "Report recipients (comma-separated emails)",
				Type:        "string",
				Required:    true,
			},
		},
		Spec: crd.WorkflowDefinition{
			Schedule: "0 8 * * 1",
			Enabled:  true,
			Steps: []crd.WorkflowStep{
				{
					Name: "collect-data",
					Tool: "query_data_source",
					Parameters: map[string]interface{}{
						"source":     "{{.data_source}}",
						"report_type": "{{.report_type}}",
						"date_range": "{{ now | dateModify \"-7d\" | date \"2006-01-02\" }} to {{ now | date \"2006-01-02\" }}",
					},
					Timeout: "10m",
				},
				{
					Name: "generate-report",
					Tool: "create_report",
					Parameters: map[string]interface{}{
						"data":        "{{ steps.collect-data.output.data }}",
						"template":    "{{.report_type}}",
						"format":      "pdf",
						"title":       "{{.report_type | title}} Report - {{ now | date \"2006-01-02\" }}",
					},
					DependsOn: []string{"collect-data"},
					Timeout: "5m",
				},
				{
					Name: "distribute-report",
					Tool: "send_email",
					Parameters: map[string]interface{}{
						"recipients":  "{{.recipients}}",
						"subject":     "{{.report_type | title}} Report - {{ now | date \"January 2, 2006\" }}",
						"body":        "Please find the attached {{.report_type}} report.",
						"attachments": []string{"{{ steps.generate-report.output.file_path }}"},
					},
					DependsOn: []string{"generate-report"},
					Timeout: "2m",
				},
			},
		},
	}
}

func createSystemMaintenanceTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Name:        "system-maintenance",
		Description: "Automated system maintenance with health checks and cleanup",
		Category:    "maintenance",
		Tags:        []string{"maintenance", "cleanup", "health", "system"},
		Parameters: []TemplateParameter{
			{
				Name:        "schedule",
				Description: "Maintenance schedule",
				Type:        "string",
				Required:    false,
				Default:     "0 3 * * 0",
			},
			{
				Name:        "cleanup_threshold_days",
				Description: "Age threshold for cleanup (days)",
				Type:        "number",
				Required:    false,
				Default:     7,
			},
			{
				Name:        "disk_usage_threshold",
				Description: "Disk usage threshold for alerts (%)",
				Type:        "number",
				Required:    false,
				Default:     80,
			},
		},
		Spec: crd.WorkflowDefinition{
			Schedule: "0 3 * * 0",
			Enabled:  true,
			Steps: []crd.WorkflowStep{
				{
					Name: "system-health-check",
					Tool: "check_system_health",
					Parameters: map[string]interface{}{
						"checks": []string{"disk", "memory", "cpu", "network"},
					},
					Timeout: "2m",
				},
				{
					Name: "cleanup-temp-files",
					Tool: "cleanup_files",
					Parameters: map[string]interface{}{
						"paths": []string{"/tmp", "/var/tmp"},
						"max_age_days": "{{.cleanup_threshold_days}}",
						"pattern":      "*",
					},
					DependsOn: []string{"system-health-check"},
					Timeout: "5m",
				},
				{
					Name: "cleanup-logs",
					Tool: "rotate_logs",
					Parameters: map[string]interface{}{
						"log_dirs":     []string{"/var/log"},
						"max_age_days": "{{.cleanup_threshold_days}}",
						"compress":     true,
					},
					DependsOn: []string{"cleanup-temp-files"},
					Timeout: "3m",
				},
				{
					Name: "check-disk-usage",
					Tool: "check_disk_usage",
					Parameters: map[string]interface{}{
						"threshold": "{{.disk_usage_threshold}}",
					},
					DependsOn: []string{"cleanup-logs"},
					Timeout: "1m",
				},
				{
					Name: "send-maintenance-report",
					Tool: "send_notification",
					Parameters: map[string]interface{}{
						"message": "System maintenance completed. Health: {{ steps.system-health-check.output.status }}, Disk usage: {{ steps.check-disk-usage.output.usage }}%",
						"channel": "system-maintenance",
					},
					DependsOn:       []string{"check-disk-usage"},
					ContinueOnError: true,
				},
			},
		},
	}
}

func createCodeQualityTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Name:        "code-quality-checks",
		Description: "Automated code quality checks including linting, testing, and security scans",
		Category:    "development",
		Tags:        []string{"code", "quality", "testing", "security", "ci"},
		Parameters: []TemplateParameter{
			{
				Name:        "schedule",
				Description: "Quality check schedule",
				Type:        "string",
				Required:    false,
				Default:     "0 1 * * *",
			},
			{
				Name:        "repository_url",
				Description: "Git repository URL",
				Type:        "string",
				Required:    true,
			},
			{
				Name:        "branch",
				Description: "Git branch to check",
				Type:        "string",
				Required:    false,
				Default:     "main",
			},
			{
				Name:        "notification_channel",
				Description: "Channel for quality reports",
				Type:        "string",
				Required:    true,
			},
		},
		Spec: crd.WorkflowDefinition{
			Schedule: "0 1 * * *",
			Enabled:  true,
			Steps: []crd.WorkflowStep{
				{
					Name: "checkout-code",
					Tool: "git_checkout",
					Parameters: map[string]interface{}{
						"repository": "{{.repository_url}}",
						"branch":     "{{.branch}}",
						"depth":      1,
					},
					Timeout: "5m",
				},
				{
					Name: "run-linter",
					Tool: "run_linter",
					Parameters: map[string]interface{}{
						"path":   "{{ steps.checkout-code.output.path }}",
						"config": ".lint.yml",
					},
					DependsOn:       []string{"checkout-code"},
					ContinueOnError: true,
					Timeout: "3m",
				},
				{
					Name: "run-tests",
					Tool: "run_tests",
					Parameters: map[string]interface{}{
						"path":     "{{ steps.checkout-code.output.path }}",
						"coverage": true,
					},
					DependsOn:       []string{"checkout-code"},
					ContinueOnError: true,
					Timeout: "10m",
				},
				{
					Name: "security-scan",
					Tool: "security_scan",
					Parameters: map[string]interface{}{
						"path": "{{ steps.checkout-code.output.path }}",
						"type": "sast",
					},
					DependsOn:       []string{"checkout-code"},
					ContinueOnError: true,
					Timeout: "5m",
				},
				{
					Name: "generate-quality-report",
					Tool: "generate_report",
					Parameters: map[string]interface{}{
						"lint_results":     "{{ steps.run-linter.output }}",
						"test_results":     "{{ steps.run-tests.output }}",
						"security_results": "{{ steps.security-scan.output }}",
						"format":           "markdown",
					},
					DependsOn: []string{"run-linter", "run-tests", "security-scan"},
					Timeout: "2m",
				},
				{
					Name: "send-quality-report",
					Tool: "send_notification",
					Parameters: map[string]interface{}{
						"channel": "{{.notification_channel}}",
						"message": "{{ steps.generate-quality-report.output.summary }}",
						"details": "{{ steps.generate-quality-report.output.report }}",
					},
					DependsOn: []string{"generate-quality-report"},
				},
			},
		},
	}
}

func createDatabaseMaintenanceTemplate() *WorkflowTemplate {
	return &WorkflowTemplate{
		Name:        "database-maintenance",
		Description: "Database maintenance including backup, optimization, and health checks",
		Category:    "database",
		Tags:        []string{"database", "maintenance", "backup", "optimization"},
		Parameters: []TemplateParameter{
			{
				Name:        "schedule",
				Description: "Maintenance schedule",
				Type:        "string",
				Required:    false,
				Default:     "0 2 * * 0",
			},
			{
				Name:        "database_url",
				Description: "Database connection URL",
				Type:        "string",
				Required:    true,
			},
			{
				Name:        "backup_retention_days",
				Description: "Number of days to retain backups",
				Type:        "number",
				Required:    false,
				Default:     30,
			},
		},
		Spec: crd.WorkflowDefinition{
			Schedule: "0 2 * * 0",
			Enabled:  true,
			Steps: []crd.WorkflowStep{
				{
					Name: "database-health-check",
					Tool: "check_database_health",
					Parameters: map[string]interface{}{
						"connection_string": "{{.database_url}}",
						"checks":            []string{"connectivity", "disk_space", "connections", "slow_queries"},
					},
					Timeout: "2m",
				},
				{
					Name: "create-backup",
					Tool: "create_database_backup",
					Parameters: map[string]interface{}{
						"connection_string": "{{.database_url}}",
						"backup_type":       "full",
						"compression":       true,
					},
					DependsOn: []string{"database-health-check"},
					Timeout: "30m",
				},
				{
					Name: "optimize-tables",
					Tool: "optimize_database",
					Parameters: map[string]interface{}{
						"connection_string": "{{.database_url}}",
						"operations":        []string{"analyze", "optimize", "repair"},
					},
					DependsOn: []string{"create-backup"},
					Timeout: "15m",
				},
				{
					Name: "cleanup-old-backups",
					Tool: "cleanup_database_backups",
					Parameters: map[string]interface{}{
						"retention_days": "{{.backup_retention_days}}",
						"backup_path":    "{{ steps.create-backup.output.backup_path }}",
					},
					DependsOn: []string{"optimize-tables"},
					Timeout: "5m",
				},
				{
					Name: "send-maintenance-report",
					Tool: "send_notification",
					Parameters: map[string]interface{}{
						"message": "Database maintenance completed. Health: {{ steps.database-health-check.output.status }}, Backup: {{ steps.create-backup.output.size }}",
						"channel": "database-maintenance",
					},
					DependsOn:       []string{"cleanup-old-backups"},
					ContinueOnError: true,
				},
			},
		},
	}
}