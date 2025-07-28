// internal/scheduler/workflow_store.go
package scheduler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/phildougherty/m8e/internal/crd"
)

// WorkflowStore provides PostgreSQL-backed workflow persistence
type WorkflowStore struct {
	db     *sql.DB
	logger logr.Logger
}

// WorkflowRecord represents a workflow in the database
type WorkflowRecord struct {
	ID                uuid.UUID                      `json:"id"`
	Name              string                         `json:"name"`
	Description       string                         `json:"description"`
	Schedule          string                         `json:"schedule"`
	Timezone          string                         `json:"timezone"`
	Enabled           bool                           `json:"enabled"`
	Status            string                         `json:"status"` // active, disabled, deleted
	ConcurrencyPolicy string                         `json:"concurrency_policy"`
	TimeoutSeconds    *int                           `json:"timeout_seconds"`
	Steps             []WorkflowStepRecord           `json:"steps"`
	RetryPolicy       *WorkflowRetryPolicyRecord     `json:"retry_policy"`
	CreatedAt         time.Time                      `json:"created_at"`
	UpdatedAt         time.Time                      `json:"updated_at"`
}

// WorkflowStepRecord represents a workflow step in the database
type WorkflowStepRecord struct {
	ID               uuid.UUID              `json:"id"`
	WorkflowID       uuid.UUID              `json:"workflow_id"`
	Name             string                 `json:"name"`
	StepOrder        int                    `json:"step_order"`
	Tool             string                 `json:"tool"`
	Parameters       map[string]interface{} `json:"parameters"`
	ConditionExpr    string                 `json:"condition_expr"`
	ContinueOnError  bool                   `json:"continue_on_error"`
	TimeoutSeconds   *int                   `json:"timeout_seconds"`
	DependsOn        []string               `json:"depends_on"`
	CreatedAt        time.Time              `json:"created_at"`
}

// WorkflowRetryPolicyRecord represents retry policy in the database
type WorkflowRetryPolicyRecord struct {
	WorkflowID            uuid.UUID `json:"workflow_id"`
	MaxRetries            int       `json:"max_retries"`
	RetryDelaySeconds     int       `json:"retry_delay_seconds"`
	BackoffStrategy       string    `json:"backoff_strategy"`
	MaxRetryDelaySeconds  int       `json:"max_retry_delay_seconds"`
	BackoffMultiplier     float64   `json:"backoff_multiplier"`
}

// WorkflowExecutionDBRecord represents a workflow execution in the database
type WorkflowExecutionDBRecord struct {
	ID          uuid.UUID              `json:"id"`
	WorkflowID  uuid.UUID              `json:"workflow_id"`
	RunID       string                 `json:"run_id"`
	Status      string                 `json:"status"`
	TriggerType string                 `json:"trigger_type"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt *time.Time             `json:"completed_at"`
	ErrorMessage *string               `json:"error_message"`
	Result      map[string]interface{} `json:"result"`
}

// NewWorkflowStore creates a new workflow store connected to PostgreSQL
func NewWorkflowStore(databaseURL string, logger logr.Logger) (*WorkflowStore, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := &WorkflowStore{
		db:     db,
		logger: logger,
	}

	return store, nil
}

// Close closes the database connection
func (ws *WorkflowStore) Close() error {
	return ws.db.Close()
}

// CreateWorkflow creates a new workflow in the database
func (ws *WorkflowStore) CreateWorkflow(workflow crd.WorkflowDefinition) (*WorkflowRecord, error) {
	tx, err := ws.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert workflow
	workflowID := uuid.New()
	var timeoutSeconds *int
	if workflow.Timeout != "" {
		if duration, err := time.ParseDuration(workflow.Timeout); err == nil {
			seconds := int(duration.Seconds())
			timeoutSeconds = &seconds
		}
	}

	_, err = tx.Exec(`
		INSERT INTO workflows (id, name, description, schedule, timezone, enabled, status, concurrency_policy, timeout_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, workflowID, workflow.Name, workflow.Description, workflow.Schedule, workflow.Timezone, 
	   workflow.Enabled, "active", string(workflow.ConcurrencyPolicy), timeoutSeconds)
	
	if err != nil {
		return nil, fmt.Errorf("failed to insert workflow: %w", err)
	}

	// Insert steps
	var stepRecords []WorkflowStepRecord
	for i, step := range workflow.Steps {
		stepID := uuid.New()
		
		// Convert parameters to JSONB
		parametersJSON, err := json.Marshal(step.Parameters)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal step parameters: %w", err)
		}

		var stepTimeoutSeconds *int
		if step.Timeout != "" {
			if duration, err := time.ParseDuration(step.Timeout); err == nil {
				seconds := int(duration.Seconds())
				stepTimeoutSeconds = &seconds
			}
		}

		// Convert depends_on to PostgreSQL array
		dependsOnArray := "{}"
		if len(step.DependsOn) > 0 {
			dependsOnJSON, _ := json.Marshal(step.DependsOn)
			dependsOnArray = string(dependsOnJSON)
		}

		_, err = tx.Exec(`
			INSERT INTO workflow_steps (id, workflow_id, name, step_order, tool, parameters, condition_expr, continue_on_error, timeout_seconds, depends_on)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, stepID, workflowID, step.Name, i+1, step.Tool, string(parametersJSON), 
		   step.Condition, step.ContinueOnError, stepTimeoutSeconds, dependsOnArray)
		
		if err != nil {
			return nil, fmt.Errorf("failed to insert workflow step %s: %w", step.Name, err)
		}

		stepRecords = append(stepRecords, WorkflowStepRecord{
			ID:              stepID,
			WorkflowID:      workflowID,
			Name:            step.Name,
			StepOrder:       i + 1,
			Tool:            step.Tool,
			Parameters:      step.Parameters,
			ConditionExpr:   step.Condition,
			ContinueOnError: step.ContinueOnError,
			TimeoutSeconds:  stepTimeoutSeconds,
			DependsOn:       step.DependsOn,
			CreatedAt:       time.Now(),
		})
	}

	// Insert retry policy if exists
	var retryPolicyRecord *WorkflowRetryPolicyRecord
	if workflow.RetryPolicy != nil {
		retryDelaySeconds := 30
		if workflow.RetryPolicy.RetryDelay != "" {
			if duration, err := time.ParseDuration(workflow.RetryPolicy.RetryDelay); err == nil {
				retryDelaySeconds = int(duration.Seconds())
			}
		}

		maxRetryDelaySeconds := 300
		if workflow.RetryPolicy.MaxRetryDelay != "" {
			if duration, err := time.ParseDuration(workflow.RetryPolicy.MaxRetryDelay); err == nil {
				maxRetryDelaySeconds = int(duration.Seconds())
			}
		}

		_, err = tx.Exec(`
			INSERT INTO workflow_retry_policies (workflow_id, max_retries, retry_delay_seconds, backoff_strategy, max_retry_delay_seconds, backoff_multiplier)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, workflowID, workflow.RetryPolicy.MaxRetries, retryDelaySeconds, 
		   string(workflow.RetryPolicy.BackoffStrategy), maxRetryDelaySeconds, workflow.RetryPolicy.BackoffMultiplier)
		
		if err != nil {
			return nil, fmt.Errorf("failed to insert retry policy: %w", err)
		}

		retryPolicyRecord = &WorkflowRetryPolicyRecord{
			WorkflowID:           workflowID,
			MaxRetries:           int(workflow.RetryPolicy.MaxRetries),
			RetryDelaySeconds:    retryDelaySeconds,
			BackoffStrategy:      string(workflow.RetryPolicy.BackoffStrategy),
			MaxRetryDelaySeconds: maxRetryDelaySeconds,
			BackoffMultiplier:    workflow.RetryPolicy.BackoffMultiplier,
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &WorkflowRecord{
		ID:                workflowID,
		Name:              workflow.Name,
		Description:       workflow.Description,
		Schedule:          workflow.Schedule,
		Timezone:          workflow.Timezone,
		Enabled:           workflow.Enabled,
		ConcurrencyPolicy: string(workflow.ConcurrencyPolicy),
		TimeoutSeconds:    timeoutSeconds,
		Steps:             stepRecords,
		RetryPolicy:       retryPolicyRecord,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}, nil
}

// GetWorkflow retrieves a workflow by name
func (ws *WorkflowStore) GetWorkflow(name string) (*WorkflowRecord, error) {
	// Get workflow details
	var workflow WorkflowRecord
	var timeoutSeconds sql.NullInt32
	
	err := ws.db.QueryRow(`
		SELECT id, name, description, schedule, timezone, enabled, status, concurrency_policy, timeout_seconds, created_at, updated_at
		FROM workflows WHERE name = $1 AND status = 'active'
	`, name).Scan(&workflow.ID, &workflow.Name, &workflow.Description, &workflow.Schedule, 
		&workflow.Timezone, &workflow.Enabled, &workflow.Status, &workflow.ConcurrencyPolicy, &timeoutSeconds,
		&workflow.CreatedAt, &workflow.UpdatedAt)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("workflow %s not found", name)
		}
		return nil, fmt.Errorf("failed to get workflow: %w", err)
	}

	if timeoutSeconds.Valid {
		seconds := int(timeoutSeconds.Int32)
		workflow.TimeoutSeconds = &seconds
	}

	// Get workflow steps
	steps, err := ws.getWorkflowSteps(workflow.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow steps: %w", err)
	}
	workflow.Steps = steps

	// Get retry policy
	retryPolicy, err := ws.getWorkflowRetryPolicy(workflow.ID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get retry policy: %w", err)
	}
	workflow.RetryPolicy = retryPolicy

	return &workflow, nil
}

// ListWorkflows retrieves all active workflows
func (ws *WorkflowStore) ListWorkflows() ([]WorkflowRecord, error) {
	rows, err := ws.db.Query(`
		SELECT id, name, description, schedule, timezone, enabled, status, concurrency_policy, timeout_seconds, created_at, updated_at
		FROM workflows WHERE status = 'active' ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}
	defer rows.Close()

	var workflows []WorkflowRecord
	for rows.Next() {
		var workflow WorkflowRecord
		var timeoutSeconds sql.NullInt32
		
		err := rows.Scan(&workflow.ID, &workflow.Name, &workflow.Description, 
			&workflow.Schedule, &workflow.Timezone, &workflow.Enabled, &workflow.Status,
			&workflow.ConcurrencyPolicy, &timeoutSeconds, &workflow.CreatedAt, &workflow.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan workflow: %w", err)
		}

		if timeoutSeconds.Valid {
			seconds := int(timeoutSeconds.Int32)
			workflow.TimeoutSeconds = &seconds
		}

		// Get steps for each workflow
		steps, err := ws.getWorkflowSteps(workflow.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get steps for workflow %s: %w", workflow.Name, err)
		}
		workflow.Steps = steps

		// Get retry policy
		retryPolicy, err := ws.getWorkflowRetryPolicy(workflow.ID)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("failed to get retry policy for workflow %s: %w", workflow.Name, err)
		}
		workflow.RetryPolicy = retryPolicy

		workflows = append(workflows, workflow)
	}

	return workflows, nil
}

// DeleteWorkflow soft deletes a workflow by marking it as deleted
func (ws *WorkflowStore) DeleteWorkflow(name string) error {
	result, err := ws.db.Exec("UPDATE workflows SET status = 'deleted', updated_at = NOW() WHERE name = $1 AND status = 'active'", name)
	if err != nil {
		return fmt.Errorf("failed to soft delete workflow: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("workflow %s not found", name)
	}

	ws.logger.Info("Soft deleted workflow", "name", name)
	return nil
}

// ToCRDWorkflowDefinition converts a WorkflowRecord to CRD WorkflowDefinition
func (wr *WorkflowRecord) ToCRDWorkflowDefinition() crd.WorkflowDefinition {
	workflow := crd.WorkflowDefinition{
		Name:              wr.Name,
		Description:       wr.Description,
		Schedule:          wr.Schedule,
		Timezone:          wr.Timezone,
		Enabled:           wr.Enabled,
		ConcurrencyPolicy: crd.WorkflowConcurrencyPolicy(wr.ConcurrencyPolicy),
	}
	
	if wr.TimeoutSeconds != nil {
		workflow.Timeout = fmt.Sprintf("%ds", *wr.TimeoutSeconds)
	}
	
	// Convert steps
	workflow.Steps = make([]crd.WorkflowStep, len(wr.Steps))
	for i, step := range wr.Steps {
		workflow.Steps[i] = crd.WorkflowStep{
			Name:            step.Name,
			Tool:            step.Tool,
			Parameters:      step.Parameters,
			Condition:       step.ConditionExpr,
			ContinueOnError: step.ContinueOnError,
			DependsOn:       step.DependsOn,
		}
		
		if step.TimeoutSeconds != nil {
			workflow.Steps[i].Timeout = fmt.Sprintf("%ds", *step.TimeoutSeconds)
		}
	}
	
	// Convert retry policy
	if wr.RetryPolicy != nil {
		workflow.RetryPolicy = &crd.WorkflowRetryPolicy{
			MaxRetries:            int32(wr.RetryPolicy.MaxRetries),
			RetryDelay:            fmt.Sprintf("%ds", wr.RetryPolicy.RetryDelaySeconds),
			BackoffStrategy:       crd.WorkflowBackoffStrategy(wr.RetryPolicy.BackoffStrategy),
			MaxRetryDelay:         fmt.Sprintf("%ds", wr.RetryPolicy.MaxRetryDelaySeconds),
			BackoffMultiplier:     wr.RetryPolicy.BackoffMultiplier,
		}
	}
	
	return workflow
}

// UpdateWorkflowEnabled updates the enabled status of a workflow
func (ws *WorkflowStore) UpdateWorkflowEnabled(name string, enabled bool) error {
	result, err := ws.db.Exec("UPDATE workflows SET enabled = $1, updated_at = CURRENT_TIMESTAMP WHERE name = $2", enabled, name)
	if err != nil {
		return fmt.Errorf("failed to update workflow enabled status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("workflow %s not found", name)
	}

	return nil
}

// Helper methods

func (ws *WorkflowStore) getWorkflowSteps(workflowID uuid.UUID) ([]WorkflowStepRecord, error) {
	rows, err := ws.db.Query(`
		SELECT id, name, step_order, tool, parameters, condition_expr, continue_on_error, timeout_seconds, depends_on, created_at
		FROM workflow_steps WHERE workflow_id = $1 ORDER BY step_order
	`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []WorkflowStepRecord
	for rows.Next() {
		var step WorkflowStepRecord
		var parametersJSON string
		var dependsOnJSON string
		var timeoutSeconds sql.NullInt32
		
		err := rows.Scan(&step.ID, &step.Name, &step.StepOrder, &step.Tool, 
			&parametersJSON, &step.ConditionExpr, &step.ContinueOnError, 
			&timeoutSeconds, &dependsOnJSON, &step.CreatedAt)
		if err != nil {
			return nil, err
		}

		step.WorkflowID = workflowID

		if timeoutSeconds.Valid {
			seconds := int(timeoutSeconds.Int32)
			step.TimeoutSeconds = &seconds
		}

		// Parse parameters JSON
		if err := json.Unmarshal([]byte(parametersJSON), &step.Parameters); err != nil {
			return nil, fmt.Errorf("failed to parse step parameters: %w", err)
		}

		// Parse depends_on array
		if dependsOnJSON != "{}" && dependsOnJSON != "" {
			if err := json.Unmarshal([]byte(dependsOnJSON), &step.DependsOn); err != nil {
				return nil, fmt.Errorf("failed to parse depends_on: %w", err)
			}
		}

		steps = append(steps, step)
	}

	return steps, nil
}

func (ws *WorkflowStore) getWorkflowRetryPolicy(workflowID uuid.UUID) (*WorkflowRetryPolicyRecord, error) {
	var policy WorkflowRetryPolicyRecord
	
	err := ws.db.QueryRow(`
		SELECT workflow_id, max_retries, retry_delay_seconds, backoff_strategy, max_retry_delay_seconds, backoff_multiplier
		FROM workflow_retry_policies WHERE workflow_id = $1
	`, workflowID).Scan(&policy.WorkflowID, &policy.MaxRetries, &policy.RetryDelaySeconds,
		&policy.BackoffStrategy, &policy.MaxRetryDelaySeconds, &policy.BackoffMultiplier)
	
	if err != nil {
		return nil, err
	}

	return &policy, nil
}

// ConvertToWorkflowDefinition converts a WorkflowRecord to a crd.WorkflowDefinition
func (wr *WorkflowRecord) ToWorkflowDefinition() crd.WorkflowDefinition {
	workflow := crd.WorkflowDefinition{
		Name:              wr.Name,
		Description:       wr.Description,
		Schedule:          wr.Schedule,
		Timezone:          wr.Timezone,
		Enabled:           wr.Enabled,
		ConcurrencyPolicy: crd.WorkflowConcurrencyPolicy(wr.ConcurrencyPolicy),
	}

	if wr.TimeoutSeconds != nil {
		workflow.Timeout = fmt.Sprintf("%ds", *wr.TimeoutSeconds)
	}

	// Convert steps
	for _, stepRecord := range wr.Steps {
		step := crd.WorkflowStep{
			Name:            stepRecord.Name,
			Tool:            stepRecord.Tool,
			Parameters:      stepRecord.Parameters,
			Condition:       stepRecord.ConditionExpr,
			ContinueOnError: stepRecord.ContinueOnError,
			DependsOn:       stepRecord.DependsOn,
		}

		if stepRecord.TimeoutSeconds != nil {
			step.Timeout = fmt.Sprintf("%ds", *stepRecord.TimeoutSeconds)
		}

		workflow.Steps = append(workflow.Steps, step)
	}

	// Convert retry policy
	if wr.RetryPolicy != nil {
		workflow.RetryPolicy = &crd.WorkflowRetryPolicy{
			MaxRetries:        int32(wr.RetryPolicy.MaxRetries),
			RetryDelay:        fmt.Sprintf("%ds", wr.RetryPolicy.RetryDelaySeconds),
			BackoffStrategy:   crd.WorkflowBackoffStrategy(wr.RetryPolicy.BackoffStrategy),
			MaxRetryDelay:     fmt.Sprintf("%ds", wr.RetryPolicy.MaxRetryDelaySeconds),
			BackoffMultiplier: wr.RetryPolicy.BackoffMultiplier,
		}
	}

	return workflow
}