// internal/scheduler/cron_engine.go
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/go-logr/logr"
)

type CronEngine struct {
	cron   *cron.Cron
	jobs   map[string]*JobSpec
	logger logr.Logger
	mu     sync.RWMutex
}

type JobSpec struct {
	ID          string
	Name        string
	Schedule    string
	Timezone    *time.Location
	Enabled     bool
	LastRun     *time.Time
	NextRun     *time.Time
	Handler     JobHandler
	MaxRetries  int
	RetryDelay  time.Duration
	cronEntryID cron.EntryID
}

type JobHandler func(ctx context.Context, jobID string) error

type JobExecution struct {
	JobID     string
	StartTime time.Time
	EndTime   *time.Time
	Success   bool
	Error     error
	Attempt   int
}

func NewCronEngine(logger logr.Logger) *CronEngine {
	c := cron.New(cron.WithLocation(time.UTC), cron.WithChain(
		cron.SkipIfStillRunning(cron.DefaultLogger),
		cron.Recover(cron.DefaultLogger),
	))

	return &CronEngine{
		cron:   c,
		jobs:   make(map[string]*JobSpec),
		logger: logger,
	}
}

func (e *CronEngine) Start() {
	e.logger.Info("Starting cron engine")
	e.cron.Start()
}

func (e *CronEngine) Stop() {
	e.logger.Info("Stopping cron engine")
	ctx := e.cron.Stop()
	<-ctx.Done()
}

func (e *CronEngine) AddJob(jobSpec *JobSpec) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if jobSpec.Timezone == nil {
		jobSpec.Timezone = time.UTC
	}

	if jobSpec.MaxRetries == 0 {
		jobSpec.MaxRetries = 3
	}

	if jobSpec.RetryDelay == 0 {
		jobSpec.RetryDelay = 30 * time.Second
	}

	// Validate cron expression
	schedule, err := cron.ParseStandard(jobSpec.Schedule)
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", jobSpec.Schedule, err)
	}

	// Calculate next run time
	now := time.Now().In(jobSpec.Timezone)
	nextRun := schedule.Next(now)
	jobSpec.NextRun = &nextRun

	// Create wrapped handler with retry logic
	wrappedHandler := e.createWrappedHandler(jobSpec)

	// Add to cron scheduler
	entryID, err := e.cron.AddFunc(jobSpec.Schedule, wrappedHandler)
	if err != nil {
		return fmt.Errorf("failed to add job to cron: %w", err)
	}

	jobSpec.cronEntryID = entryID
	e.jobs[jobSpec.ID] = jobSpec

	e.logger.Info("Added cron job", "jobID", jobSpec.ID, "schedule", jobSpec.Schedule, "nextRun", nextRun)
	return nil
}

func (e *CronEngine) RemoveJob(jobID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	job, exists := e.jobs[jobID]
	if !exists {
		return fmt.Errorf("job %q not found", jobID)
	}

	e.cron.Remove(job.cronEntryID)
	delete(e.jobs, jobID)

	e.logger.Info("Removed cron job", "jobID", jobID)
	return nil
}

func (e *CronEngine) GetJob(jobID string) (*JobSpec, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	job, exists := e.jobs[jobID]
	return job, exists
}

func (e *CronEngine) ListJobs() []*JobSpec {
	e.mu.RLock()
	defer e.mu.RUnlock()

	jobs := make([]*JobSpec, 0, len(e.jobs))
	for _, job := range e.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (e *CronEngine) EnableJob(jobID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	job, exists := e.jobs[jobID]
	if !exists {
		return fmt.Errorf("job %q not found", jobID)
	}

	if job.Enabled {
		return nil // Already enabled
	}

	// Re-add the job to cron
	wrappedHandler := e.createWrappedHandler(job)
	entryID, err := e.cron.AddFunc(job.Schedule, wrappedHandler)
	if err != nil {
		return fmt.Errorf("failed to re-enable job: %w", err)
	}

	job.cronEntryID = entryID
	job.Enabled = true

	e.logger.Info("Enabled cron job", "jobID", jobID)
	return nil
}

func (e *CronEngine) DisableJob(jobID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	job, exists := e.jobs[jobID]
	if !exists {
		return fmt.Errorf("job %q not found", jobID)
	}

	if !job.Enabled {
		return nil // Already disabled
	}

	e.cron.Remove(job.cronEntryID)
	job.Enabled = false

	e.logger.Info("Disabled cron job", "jobID", jobID)
	return nil
}

func (e *CronEngine) createWrappedHandler(jobSpec *JobSpec) func() {
	return func() {
		if !jobSpec.Enabled {
			return
		}

		ctx := context.Background()
		execution := &JobExecution{
			JobID:     jobSpec.ID,
			StartTime: time.Now(),
		}

		e.logger.Info("Starting job execution", "jobID", jobSpec.ID)

		// Update last run time
		now := time.Now()
		jobSpec.LastRun = &now

		// Execute with retry logic
		var lastErr error
		for attempt := 1; attempt <= jobSpec.MaxRetries; attempt++ {
			execution.Attempt = attempt

			err := jobSpec.Handler(ctx, jobSpec.ID)
			if err == nil {
				execution.Success = true
				execution.EndTime = &[]time.Time{time.Now()}[0]
				e.logger.Info("Job execution succeeded", "jobID", jobSpec.ID, "attempt", attempt)
				break
			}

			lastErr = err
			e.logger.Error(err, "Job execution failed", "jobID", jobSpec.ID, "attempt", attempt)

			// Don't retry on last attempt
			if attempt < jobSpec.MaxRetries {
				e.logger.Info("Retrying job", "jobID", jobSpec.ID, "retryDelay", jobSpec.RetryDelay)
				time.Sleep(jobSpec.RetryDelay)
			}
		}

		if !execution.Success {
			execution.Error = lastErr
			execution.EndTime = &[]time.Time{time.Now()}[0]
			e.logger.Error(lastErr, "Job execution failed after all retries", "jobID", jobSpec.ID, "maxRetries", jobSpec.MaxRetries)
		}

		// Update next run time
		schedule, _ := cron.ParseStandard(jobSpec.Schedule)
		nextRun := schedule.Next(time.Now().In(jobSpec.Timezone))
		jobSpec.NextRun = &nextRun
	}
}

// ParseCronExpression validates and parses a cron expression
func ParseCronExpression(expr string) error {
	_, err := cron.ParseStandard(expr)
	return err
}

// NextScheduledTime calculates the next time a cron expression will trigger
func NextScheduledTime(expr string, timezone *time.Location) (time.Time, error) {
	schedule, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, err
	}

	if timezone == nil {
		timezone = time.UTC
	}

	now := time.Now().In(timezone)
	return schedule.Next(now), nil
}

// ConvertToCronExpression provides helper functions to convert natural language to cron
func ConvertToCronExpression(natural string) (string, error) {
	expressions := map[string]string{
		"every minute":      "* * * * *",
		"every 5 minutes":   "*/5 * * * *",
		"every 15 minutes":  "*/15 * * * *",
		"every 30 minutes":  "*/30 * * * *",
		"every hour":        "0 * * * *",
		"every 2 hours":     "0 */2 * * *",
		"every 6 hours":     "0 */6 * * *",
		"every 12 hours":    "0 */12 * * *",
		"daily":             "0 0 * * *",
		"every day":         "0 0 * * *",
		"daily at noon":     "0 12 * * *",
		"daily at midnight": "0 0 * * *",
		"weekly":            "0 0 * * 0",
		"every week":        "0 0 * * 0",
		"monthly":           "0 0 1 * *",
		"every month":       "0 0 1 * *",
		"yearly":            "0 0 1 1 *",
		"every year":        "0 0 1 1 *",
	}

	if expr, exists := expressions[natural]; exists {
		return expr, nil
	}

	return "", fmt.Errorf("unknown natural language expression: %q", natural)
}