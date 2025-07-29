// internal/scheduler/workflow_storage.go
package scheduler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	_ "github.com/lib/pq"
)

// WorkflowStorage provides PostgreSQL-backed workflow execution storage
type WorkflowStorage struct {
	db     *sql.DB
	logger logr.Logger
}

// WorkflowExecutionRecord represents a stored workflow execution
type WorkflowExecutionRecord struct {
	ID                string    `json:"id"`
	WorkflowName      string    `json:"workflowName"`
	WorkflowNamespace string    `json:"workflowNamespace"`
	StartTime         time.Time `json:"startTime"`
	EndTime           *time.Time `json:"endTime,omitempty"`
	Status            string    `json:"status"`
	Steps             string    `json:"steps"` // JSON-encoded steps
	Error             string    `json:"error,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// NewWorkflowStorage creates a new workflow storage instance
func NewWorkflowStorage(databaseURL string, logger logr.Logger) (*WorkflowStorage, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	storage := &WorkflowStorage{
		db:     db,
		logger: logger,
	}

	// Initialize the database schema
	if err := storage.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return storage, nil
}

func (ws *WorkflowStorage) Close() error {
	return ws.db.Close()
}

func (ws *WorkflowStorage) initSchema() error {
	// Create tables if they don't exist
	schema := `
	-- Enable extensions
	CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

	-- Workflow executions table
	CREATE TABLE IF NOT EXISTS workflow_executions (
		id VARCHAR(255) PRIMARY KEY,
		workflow_name VARCHAR(255) NOT NULL,
		workflow_namespace VARCHAR(255) NOT NULL,
		start_time TIMESTAMP WITH TIME ZONE NOT NULL,
		end_time TIMESTAMP WITH TIME ZONE,
		status VARCHAR(50) NOT NULL,
		steps JSONB NOT NULL DEFAULT '[]',
		error TEXT,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);

	-- Workflow execution stats table
	CREATE TABLE IF NOT EXISTS workflow_execution_stats (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		workflow_name VARCHAR(255) NOT NULL,
		workflow_namespace VARCHAR(255) NOT NULL,
		execution_date DATE NOT NULL,
		total_executions INTEGER DEFAULT 0,
		successful_executions INTEGER DEFAULT 0,
		failed_executions INTEGER DEFAULT 0,
		avg_duration_seconds DECIMAL(10,2),
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(workflow_name, workflow_namespace, execution_date)
	);

	-- Cron jobs table
	CREATE TABLE IF NOT EXISTS cron_jobs (
		id VARCHAR(255) PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		schedule VARCHAR(255) NOT NULL,
		timezone VARCHAR(100) DEFAULT 'UTC',
		enabled BOOLEAN DEFAULT true,
		last_run TIMESTAMP WITH TIME ZONE,
		next_run TIMESTAMP WITH TIME ZONE,
		max_retries INTEGER DEFAULT 3,
		retry_delay_seconds INTEGER DEFAULT 30,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);

	-- Indexes for performance
	CREATE INDEX IF NOT EXISTS idx_workflow_executions_name ON workflow_executions(workflow_name);
	CREATE INDEX IF NOT EXISTS idx_workflow_executions_namespace ON workflow_executions(workflow_namespace);
	CREATE INDEX IF NOT EXISTS idx_workflow_executions_status ON workflow_executions(status);
	CREATE INDEX IF NOT EXISTS idx_workflow_executions_start_time ON workflow_executions(start_time);
	CREATE INDEX IF NOT EXISTS idx_workflow_executions_name_namespace ON workflow_executions(workflow_name, workflow_namespace);
	
	CREATE INDEX IF NOT EXISTS idx_workflow_stats_name_namespace ON workflow_execution_stats(workflow_name, workflow_namespace);
	CREATE INDEX IF NOT EXISTS idx_workflow_stats_date ON workflow_execution_stats(execution_date);
	
	CREATE INDEX IF NOT EXISTS idx_cron_jobs_enabled ON cron_jobs(enabled);
	CREATE INDEX IF NOT EXISTS idx_cron_jobs_next_run ON cron_jobs(next_run);

	-- Full-text search index on workflow executions
	CREATE INDEX IF NOT EXISTS idx_workflow_executions_search ON workflow_executions USING gin(
		to_tsvector('english', workflow_name || ' ' || workflow_namespace || ' ' || COALESCE(error, ''))
	);

	-- Update trigger for workflow_executions
	CREATE OR REPLACE FUNCTION update_updated_at_column()
	RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = CURRENT_TIMESTAMP;
		RETURN NEW;
	END;
	$$ language 'plpgsql';

	DROP TRIGGER IF EXISTS update_workflow_executions_updated_at ON workflow_executions;
	CREATE TRIGGER update_workflow_executions_updated_at
		BEFORE UPDATE ON workflow_executions
		FOR EACH ROW
		EXECUTE FUNCTION update_updated_at_column();

	DROP TRIGGER IF EXISTS update_workflow_stats_updated_at ON workflow_execution_stats;
	CREATE TRIGGER update_workflow_stats_updated_at
		BEFORE UPDATE ON workflow_execution_stats
		FOR EACH ROW
		EXECUTE FUNCTION update_updated_at_column();

	DROP TRIGGER IF EXISTS update_cron_jobs_updated_at ON cron_jobs;
	CREATE TRIGGER update_cron_jobs_updated_at
		BEFORE UPDATE ON cron_jobs
		FOR EACH ROW
		EXECUTE FUNCTION update_updated_at_column();
	`

	_, err := ws.db.Exec(schema)
	return err
}

// StoreWorkflowExecution stores a workflow execution record
func (ws *WorkflowStorage) StoreWorkflowExecution(execution *WorkflowExecution) error {
	// Convert steps to JSON
	stepsJSON, err := json.Marshal(execution.Steps)
	if err != nil {
		return fmt.Errorf("failed to marshal steps: %w", err)
	}

	query := `
		INSERT INTO workflow_executions (
			id, workflow_name, workflow_namespace, start_time, end_time, 
			status, steps, error, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			end_time = EXCLUDED.end_time,
			status = EXCLUDED.status,
			steps = EXCLUDED.steps,
			error = EXCLUDED.error,
			updated_at = CURRENT_TIMESTAMP
	`

	now := time.Now()
	_, err = ws.db.Exec(query,
		execution.ID,
		execution.WorkflowName,
		execution.WorkflowNamespace, 
		execution.StartTime,
		execution.EndTime,
		string(execution.Status),
		string(stepsJSON),
		execution.Error,
		now,
		now,
	)

	if err != nil {
		return fmt.Errorf("failed to store workflow execution: %w", err)
	}

	// Update statistics if execution is complete
	if execution.EndTime != nil {
		if err := ws.updateWorkflowStats(execution); err != nil {
			ws.logger.Error(err, "Failed to update workflow statistics", "executionID", execution.ID)
		}
	}

	return nil
}

// GetWorkflowExecution retrieves a workflow execution by ID
func (ws *WorkflowStorage) GetWorkflowExecution(executionID string) (*WorkflowExecutionRecord, error) {
	query := `
		SELECT id, workflow_name, workflow_namespace, start_time, end_time,
			   status, steps, error, created_at, updated_at
		FROM workflow_executions
		WHERE id = $1
	`

	row := ws.db.QueryRow(query, executionID)
	
	var record WorkflowExecutionRecord
	var stepsJSON string

	err := row.Scan(
		&record.ID,
		&record.WorkflowName,
		&record.WorkflowNamespace,
		&record.StartTime,
		&record.EndTime,
		&record.Status,
		&stepsJSON,
		&record.Error,
		&record.CreatedAt,
		&record.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("workflow execution not found: %s", executionID)
		}
		return nil, fmt.Errorf("failed to get workflow execution: %w", err)
	}

	record.Steps = stepsJSON
	return &record, nil
}

// ListWorkflowExecutions lists workflow executions with pagination
func (ws *WorkflowStorage) ListWorkflowExecutions(workflowName, workflowNamespace string, limit, offset int) ([]*WorkflowExecutionRecord, error) {
	query := `
		SELECT id, workflow_name, workflow_namespace, start_time, end_time,
			   status, steps, error, created_at, updated_at
		FROM workflow_executions
		WHERE workflow_name = $1 AND workflow_namespace = $2
		ORDER BY start_time DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := ws.db.Query(query, workflowName, workflowNamespace, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflow executions: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Printf("Warning: Failed to close rows: %v\n", err)
		}
	}()

	var records []*WorkflowExecutionRecord
	for rows.Next() {
		var record WorkflowExecutionRecord
		var stepsJSON string

		err := rows.Scan(
			&record.ID,
			&record.WorkflowName,
			&record.WorkflowNamespace,
			&record.StartTime,
			&record.EndTime,
			&record.Status,
			&stepsJSON,
			&record.Error,
			&record.CreatedAt,
			&record.UpdatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan workflow execution: %w", err)
		}

		record.Steps = stepsJSON
		records = append(records, &record)
	}

	return records, nil
}

// StoreCronJob stores a cron job configuration
func (ws *WorkflowStorage) StoreCronJob(jobSpec *JobSpec) error {
	query := `
		INSERT INTO cron_jobs (
			id, name, schedule, timezone, enabled, last_run, next_run,
			max_retries, retry_delay_seconds, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			schedule = EXCLUDED.schedule,
			timezone = EXCLUDED.timezone,
			enabled = EXCLUDED.enabled,
			last_run = EXCLUDED.last_run,
			next_run = EXCLUDED.next_run,
			max_retries = EXCLUDED.max_retries,
			retry_delay_seconds = EXCLUDED.retry_delay_seconds,
			updated_at = CURRENT_TIMESTAMP
	`

	timezone := "UTC"
	if jobSpec.Timezone != nil {
		timezone = jobSpec.Timezone.String()
	}

	retryDelaySeconds := int(jobSpec.RetryDelay.Seconds())
	now := time.Now()

	_, err := ws.db.Exec(query,
		jobSpec.ID,
		jobSpec.Name,
		jobSpec.Schedule,
		timezone,
		jobSpec.Enabled,
		jobSpec.LastRun,
		jobSpec.NextRun,
		jobSpec.MaxRetries,
		retryDelaySeconds,
		now,
		now,
	)

	return err
}

// GetCronJobs retrieves all cron jobs
func (ws *WorkflowStorage) GetCronJobs() ([]*JobSpec, error) {
	query := `
		SELECT id, name, schedule, timezone, enabled, last_run, next_run,
			   max_retries, retry_delay_seconds
		FROM cron_jobs
		ORDER BY name
	`

	rows, err := ws.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get cron jobs: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Printf("Warning: Failed to close rows: %v\n", err)
		}
	}()

	var jobs []*JobSpec
	for rows.Next() {
		var job JobSpec
		var timezoneStr string
		var retryDelaySeconds int

		err := rows.Scan(
			&job.ID,
			&job.Name,
			&job.Schedule,
			&timezoneStr,
			&job.Enabled,
			&job.LastRun,
			&job.NextRun,
			&job.MaxRetries,
			&retryDelaySeconds,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan cron job: %w", err)
		}

		// Parse timezone
		if timezone, err := time.LoadLocation(timezoneStr); err == nil {
			job.Timezone = timezone
		} else {
			job.Timezone = time.UTC
		}

		job.RetryDelay = time.Duration(retryDelaySeconds) * time.Second
		jobs = append(jobs, &job)
	}

	return jobs, nil
}

// updateWorkflowStats updates daily workflow execution statistics
func (ws *WorkflowStorage) updateWorkflowStats(execution *WorkflowExecution) error {
	executionDate := execution.StartTime.Format("2006-01-02")
	duration := float64(0)
	
	if execution.EndTime != nil {
		duration = execution.EndTime.Sub(execution.StartTime).Seconds()
	}

	successful := 0
	failed := 0
	switch execution.Status {
	case WorkflowExecutionStatusSucceeded:
		successful = 1
	case WorkflowExecutionStatusFailed:
		failed = 1
	}

	query := `
		INSERT INTO workflow_execution_stats (
			workflow_name, workflow_namespace, execution_date,
			total_executions, successful_executions, failed_executions, avg_duration_seconds
		) VALUES ($1, $2, $3, 1, $4, $5, $6)
		ON CONFLICT (workflow_name, workflow_namespace, execution_date) DO UPDATE SET
			total_executions = workflow_execution_stats.total_executions + 1,
			successful_executions = workflow_execution_stats.successful_executions + $4,
			failed_executions = workflow_execution_stats.failed_executions + $5,
			avg_duration_seconds = (
				(workflow_execution_stats.avg_duration_seconds * workflow_execution_stats.total_executions + $6) /
				(workflow_execution_stats.total_executions + 1)
			),
			updated_at = CURRENT_TIMESTAMP
	`

	_, err := ws.db.Exec(query,
		execution.WorkflowName,
		execution.WorkflowNamespace,
		executionDate,
		successful,
		failed,
		duration,
	)

	return err
}

// GetWorkflowStats retrieves workflow execution statistics
func (ws *WorkflowStorage) GetWorkflowStats(workflowName, workflowNamespace string, days int) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get total executions
	var totalExecutions int
	err := ws.db.QueryRow(`
		SELECT COUNT(*) FROM workflow_executions 
		WHERE workflow_name = $1 AND workflow_namespace = $2
	`, workflowName, workflowNamespace).Scan(&totalExecutions)
	if err != nil {
		return nil, fmt.Errorf("failed to get total executions: %w", err)
	}
	stats["totalExecutions"] = totalExecutions

	// Get recent stats
	query := `
		SELECT 
			SUM(total_executions) as total,
			SUM(successful_executions) as successful,
			SUM(failed_executions) as failed,
			AVG(avg_duration_seconds) as avg_duration
		FROM workflow_execution_stats
		WHERE workflow_name = $1 AND workflow_namespace = $2
		AND execution_date >= CURRENT_DATE - INTERVAL '%d days'
	`

	var total, successful, failed int
	var avgDuration sql.NullFloat64

	err = ws.db.QueryRow(fmt.Sprintf(query, days), workflowName, workflowNamespace).Scan(
		&total, &successful, &failed, &avgDuration,
	)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get workflow stats: %w", err)
	}

	stats["recentExecutions"] = total
	stats["successfulExecutions"] = successful
	stats["failedExecutions"] = failed
	if avgDuration.Valid {
		stats["avgDurationSeconds"] = avgDuration.Float64
	} else {
		stats["avgDurationSeconds"] = 0
	}

	// Calculate success rate
	if total > 0 {
		stats["successRate"] = float64(successful) / float64(total) * 100
	} else {
		stats["successRate"] = 0
	}

	return stats, nil
}

// HealthCheck verifies database connectivity
func (ws *WorkflowStorage) HealthCheck() error {
	return ws.db.Ping()
}