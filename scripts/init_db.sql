-- Database initialization script for Matey workflow system
-- This script creates all necessary tables for workflows, executions, and scheduling

-- Enable extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Workflow executions table (from workflow_storage.go)
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

-- Workflows table (from workflow_store.go)
CREATE TABLE IF NOT EXISTS workflows (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT,
    schedule VARCHAR(255) NOT NULL,
    timezone VARCHAR(100) DEFAULT 'UTC',
    enabled BOOLEAN DEFAULT true,
    status VARCHAR(50) DEFAULT 'active',
    concurrency_policy VARCHAR(50) DEFAULT 'Allow',
    timeout_seconds INTEGER,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Workflow steps table
CREATE TABLE IF NOT EXISTS workflow_steps (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    step_order INTEGER NOT NULL,
    tool VARCHAR(255) NOT NULL,
    parameters JSONB DEFAULT '{}',
    condition_expr TEXT,
    continue_on_error BOOLEAN DEFAULT false,
    timeout_seconds INTEGER,
    depends_on TEXT[],
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(workflow_id, step_order)
);

-- Workflow retry policies table
CREATE TABLE IF NOT EXISTS workflow_retry_policies (
    workflow_id UUID PRIMARY KEY REFERENCES workflows(id) ON DELETE CASCADE,
    max_retries INTEGER DEFAULT 3,
    retry_delay_seconds INTEGER DEFAULT 30,
    backoff_strategy VARCHAR(50) DEFAULT 'exponential',
    max_retry_delay_seconds INTEGER DEFAULT 3600,
    backoff_multiplier DECIMAL(4,2) DEFAULT 2.0
);

-- Workflow execution records table
CREATE TABLE IF NOT EXISTS workflow_execution_records (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    run_id VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    trigger_type VARCHAR(50) NOT NULL,
    started_at TIMESTAMP WITH TIME ZONE NOT NULL,
    completed_at TIMESTAMP WITH TIME ZONE,
    error_message TEXT,
    result JSONB DEFAULT '{}'
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

CREATE INDEX IF NOT EXISTS idx_workflows_name ON workflows(name);
CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows(status);
CREATE INDEX IF NOT EXISTS idx_workflows_enabled ON workflows(enabled);

CREATE INDEX IF NOT EXISTS idx_workflow_steps_workflow_id ON workflow_steps(workflow_id);
CREATE INDEX IF NOT EXISTS idx_workflow_steps_order ON workflow_steps(workflow_id, step_order);

CREATE INDEX IF NOT EXISTS idx_workflow_execution_records_workflow_id ON workflow_execution_records(workflow_id);
CREATE INDEX IF NOT EXISTS idx_workflow_execution_records_status ON workflow_execution_records(status);
CREATE INDEX IF NOT EXISTS idx_workflow_execution_records_started_at ON workflow_execution_records(started_at);

-- Full-text search index on workflow executions
CREATE INDEX IF NOT EXISTS idx_workflow_executions_search ON workflow_executions USING gin(
    to_tsvector('english', workflow_name || ' ' || workflow_namespace || ' ' || COALESCE(error, ''))
);

-- Update trigger function
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Update triggers
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

DROP TRIGGER IF EXISTS update_workflows_updated_at ON workflows;
CREATE TRIGGER update_workflows_updated_at
    BEFORE UPDATE ON workflows
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();