package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCronEngine_NewCronEngine(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	assert.NotNil(t, engine)
	assert.NotNil(t, engine.cron)
	assert.NotNil(t, engine.jobs)
	assert.Equal(t, logger, engine.logger)
}

func TestCronEngine_AddJob(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	executionCount := 0
	jobSpec := &JobSpec{
		ID:       "test-job",
		Name:     "Test Job",
		Schedule: "*/5 * * * *",
		Enabled:  true,
		Handler: func(ctx context.Context, jobID string) error {
			executionCount++
			return nil
		},
	}

	err := engine.AddJob(jobSpec)
	assert.NoError(t, err)

	// Verify job was added
	storedJob, exists := engine.GetJob("test-job")
	assert.True(t, exists)
	assert.Equal(t, "test-job", storedJob.ID)
	assert.Equal(t, "Test Job", storedJob.Name)
	assert.Equal(t, "*/5 * * * *", storedJob.Schedule)
	assert.True(t, storedJob.Enabled)
	assert.NotNil(t, storedJob.NextRun)
	assert.Equal(t, 3, storedJob.MaxRetries)
	assert.Equal(t, 30*time.Second, storedJob.RetryDelay)
	assert.Equal(t, time.UTC, storedJob.Timezone)
}

func TestCronEngine_AddJob_WithCustomSettings(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	timezone, _ := time.LoadLocation("America/New_York")
	jobSpec := &JobSpec{
		ID:         "custom-job",
		Name:       "Custom Job",
		Schedule:   "0 0 * * *",
		Timezone:   timezone,
		Enabled:    true,
		MaxRetries: 5,
		RetryDelay: 1 * time.Minute,
		Handler: func(ctx context.Context, jobID string) error {
			return nil
		},
	}

	err := engine.AddJob(jobSpec)
	assert.NoError(t, err)

	// Verify job was added with custom settings
	storedJob, exists := engine.GetJob("custom-job")
	assert.True(t, exists)
	assert.Equal(t, timezone, storedJob.Timezone)
	assert.Equal(t, 5, storedJob.MaxRetries)
	assert.Equal(t, 1*time.Minute, storedJob.RetryDelay)
}

func TestCronEngine_AddJob_InvalidSchedule(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	jobSpec := &JobSpec{
		ID:       "invalid-job",
		Name:     "Invalid Job",
		Schedule: "invalid cron expression",
		Enabled:  true,
		Handler: func(ctx context.Context, jobID string) error {
			return nil
		},
	}

	err := engine.AddJob(jobSpec)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cron expression")

	// Verify job was not added
	_, exists := engine.GetJob("invalid-job")
	assert.False(t, exists)
}

func TestCronEngine_RemoveJob(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	jobSpec := &JobSpec{
		ID:       "removable-job",
		Name:     "Removable Job",
		Schedule: "0 0 * * *",
		Enabled:  true,
		Handler: func(ctx context.Context, jobID string) error {
			return nil
		},
	}

	// Add job
	err := engine.AddJob(jobSpec)
	require.NoError(t, err)

	// Verify job exists
	_, exists := engine.GetJob("removable-job")
	assert.True(t, exists)

	// Remove job
	err = engine.RemoveJob("removable-job")
	assert.NoError(t, err)

	// Verify job was removed
	_, exists = engine.GetJob("removable-job")
	assert.False(t, exists)
}

func TestCronEngine_RemoveJob_NotFound(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	err := engine.RemoveJob("non-existent-job")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "job \"non-existent-job\" not found")
}

func TestCronEngine_ListJobs(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	// Initially empty
	jobs := engine.ListJobs()
	assert.Empty(t, jobs)

	// Add some jobs
	job1 := &JobSpec{
		ID:       "job1",
		Name:     "Job 1",
		Schedule: "0 0 * * *",
		Enabled:  true,
		Handler: func(ctx context.Context, jobID string) error {
			return nil
		},
	}

	job2 := &JobSpec{
		ID:       "job2",
		Name:     "Job 2",
		Schedule: "0 12 * * *",
		Enabled:  true,
		Handler: func(ctx context.Context, jobID string) error {
			return nil
		},
	}

	err := engine.AddJob(job1)
	require.NoError(t, err)
	err = engine.AddJob(job2)
	require.NoError(t, err)

	// List jobs
	jobs = engine.ListJobs()
	assert.Len(t, jobs, 2)

	jobIDs := make([]string, len(jobs))
	for i, job := range jobs {
		jobIDs[i] = job.ID
	}
	assert.ElementsMatch(t, []string{"job1", "job2"}, jobIDs)
}

func TestCronEngine_EnableJob(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	jobSpec := &JobSpec{
		ID:       "enable-job",
		Name:     "Enable Job",
		Schedule: "0 0 * * *",
		Enabled:  true,
		Handler: func(ctx context.Context, jobID string) error {
			return nil
		},
	}

	// Add job
	err := engine.AddJob(jobSpec)
	require.NoError(t, err)

	// Disable job first
	err = engine.DisableJob("enable-job")
	require.NoError(t, err)

	// Verify job is disabled
	job, exists := engine.GetJob("enable-job")
	assert.True(t, exists)
	assert.False(t, job.Enabled)

	// Enable job
	err = engine.EnableJob("enable-job")
	assert.NoError(t, err)

	// Verify job is enabled
	job, exists = engine.GetJob("enable-job")
	assert.True(t, exists)
	assert.True(t, job.Enabled)
}

func TestCronEngine_EnableJob_AlreadyEnabled(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	jobSpec := &JobSpec{
		ID:       "already-enabled-job",
		Name:     "Already Enabled Job",
		Schedule: "0 0 * * *",
		Enabled:  true,
		Handler: func(ctx context.Context, jobID string) error {
			return nil
		},
	}

	// Add job
	err := engine.AddJob(jobSpec)
	require.NoError(t, err)

	// Enable job (should be no-op)
	err = engine.EnableJob("already-enabled-job")
	assert.NoError(t, err)

	// Verify job is still enabled
	job, exists := engine.GetJob("already-enabled-job")
	assert.True(t, exists)
	assert.True(t, job.Enabled)
}

func TestCronEngine_EnableJob_NotFound(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	err := engine.EnableJob("non-existent-job")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "job \"non-existent-job\" not found")
}

func TestCronEngine_DisableJob(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	jobSpec := &JobSpec{
		ID:       "disable-job",
		Name:     "Disable Job",
		Schedule: "0 0 * * *",
		Enabled:  true,
		Handler: func(ctx context.Context, jobID string) error {
			return nil
		},
	}

	// Add job
	err := engine.AddJob(jobSpec)
	require.NoError(t, err)

	// Verify job is enabled
	job, exists := engine.GetJob("disable-job")
	assert.True(t, exists)
	assert.True(t, job.Enabled)

	// Disable job
	err = engine.DisableJob("disable-job")
	assert.NoError(t, err)

	// Verify job is disabled
	job, exists = engine.GetJob("disable-job")
	assert.True(t, exists)
	assert.False(t, job.Enabled)
}

func TestCronEngine_DisableJob_AlreadyDisabled(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	jobSpec := &JobSpec{
		ID:       "already-disabled-job",
		Name:     "Already Disabled Job",
		Schedule: "0 0 * * *",
		Enabled:  true,
		Handler: func(ctx context.Context, jobID string) error {
			return nil
		},
	}

	// Add job
	err := engine.AddJob(jobSpec)
	require.NoError(t, err)

	// Disable job
	err = engine.DisableJob("already-disabled-job")
	require.NoError(t, err)

	// Disable job again (should be no-op)
	err = engine.DisableJob("already-disabled-job")
	assert.NoError(t, err)

	// Verify job is still disabled
	job, exists := engine.GetJob("already-disabled-job")
	assert.True(t, exists)
	assert.False(t, job.Enabled)
}

func TestCronEngine_DisableJob_NotFound(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	err := engine.DisableJob("non-existent-job")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "job \"non-existent-job\" not found")
}

func TestCronEngine_StartStop(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	// Start engine
	engine.Start()

	// Stop engine
	engine.Stop()

	// This test mainly verifies that start/stop don't panic
	// and complete without hanging
}

func TestCronEngine_JobExecutionWithRetry(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	executionCount := 0
	jobSpec := &JobSpec{
		ID:         "retry-job",
		Name:       "Retry Job",
		Schedule:   "*/1 * * * *", // Every minute for testing
		Enabled:    true,
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
		Handler: func(ctx context.Context, jobID string) error {
			executionCount++
			if executionCount < 3 {
				return assert.AnError // Fail first 2 times
			}
			return nil // Succeed on 3rd try
		},
	}

	err := engine.AddJob(jobSpec)
	require.NoError(t, err)

	// Manually trigger the wrapped handler to test retry logic
	job, exists := engine.GetJob("retry-job")
	require.True(t, exists)

	// Test the wrapped handler
	wrappedHandler := engine.createWrappedHandler(job)
	wrappedHandler()

	// Verify job was retried and eventually succeeded
	assert.Equal(t, 3, executionCount)
	assert.NotNil(t, job.LastRun)
	assert.NotNil(t, job.NextRun)
}

func TestCronEngine_JobExecutionFailure(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	executionCount := 0
	jobSpec := &JobSpec{
		ID:         "failing-job",
		Name:       "Failing Job",
		Schedule:   "*/1 * * * *",
		Enabled:    true,
		MaxRetries: 2,
		RetryDelay: 100 * time.Millisecond,
		Handler: func(ctx context.Context, jobID string) error {
			executionCount++
			return assert.AnError // Always fail
		},
	}

	err := engine.AddJob(jobSpec)
	require.NoError(t, err)

	// Manually trigger the wrapped handler
	job, exists := engine.GetJob("failing-job")
	require.True(t, exists)

	wrappedHandler := engine.createWrappedHandler(job)
	wrappedHandler()

	// Verify job was retried max times
	assert.Equal(t, 2, executionCount)
	assert.NotNil(t, job.LastRun)
	assert.NotNil(t, job.NextRun)
}

func TestCronEngine_JobExecutionSuccess(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	executionCount := 0
	jobSpec := &JobSpec{
		ID:         "success-job",
		Name:       "Success Job",
		Schedule:   "*/1 * * * *",
		Enabled:    true,
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
		Handler: func(ctx context.Context, jobID string) error {
			executionCount++
			return nil // Always succeed
		},
	}

	err := engine.AddJob(jobSpec)
	require.NoError(t, err)

	// Manually trigger the wrapped handler
	job, exists := engine.GetJob("success-job")
	require.True(t, exists)

	wrappedHandler := engine.createWrappedHandler(job)
	wrappedHandler()

	// Verify job executed only once (no retries needed)
	assert.Equal(t, 1, executionCount)
	assert.NotNil(t, job.LastRun)
	assert.NotNil(t, job.NextRun)
}

func TestCronEngine_DisabledJobSkipsExecution(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	executionCount := 0
	jobSpec := &JobSpec{
		ID:         "disabled-job",
		Name:       "Disabled Job",
		Schedule:   "*/1 * * * *",
		Enabled:    false, // Disabled
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
		Handler: func(ctx context.Context, jobID string) error {
			executionCount++
			return nil
		},
	}

	err := engine.AddJob(jobSpec)
	require.NoError(t, err)

	// Manually trigger the wrapped handler
	job, exists := engine.GetJob("disabled-job")
	require.True(t, exists)

	wrappedHandler := engine.createWrappedHandler(job)
	wrappedHandler()

	// Verify job was not executed
	assert.Equal(t, 0, executionCount)
}

func TestParseCronExpression(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		wantErr    bool
	}{
		{
			name:       "valid cron expression",
			expression: "0 0 * * *",
			wantErr:    false,
		},
		{
			name:       "valid cron expression with seconds",
			expression: "0 0 0 * * *",
			wantErr:    true, // Standard cron doesn't support seconds
		},
		{
			name:       "invalid cron expression",
			expression: "invalid",
			wantErr:    true,
		},
		{
			name:       "empty expression",
			expression: "",
			wantErr:    true,
		},
		{
			name:       "every minute",
			expression: "* * * * *",
			wantErr:    false,
		},
		{
			name:       "every hour",
			expression: "0 * * * *",
			wantErr:    false,
		},
		{
			name:       "daily at midnight",
			expression: "0 0 * * *",
			wantErr:    false,
		},
		{
			name:       "weekly on Sunday",
			expression: "0 0 * * 0",
			wantErr:    false,
		},
		{
			name:       "monthly on 1st",
			expression: "0 0 1 * *",
			wantErr:    false,
		},
		{
			name:       "yearly on Jan 1st",
			expression: "0 0 1 1 *",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ParseCronExpression(tt.expression)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNextScheduledTime(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		timezone   *time.Location
		wantErr    bool
	}{
		{
			name:       "valid expression with UTC",
			expression: "0 0 * * *",
			timezone:   time.UTC,
			wantErr:    false,
		},
		{
			name:       "valid expression with nil timezone",
			expression: "0 0 * * *",
			timezone:   nil,
			wantErr:    false,
		},
		{
			name:       "valid expression with custom timezone",
			expression: "0 0 * * *",
			timezone:   func() *time.Location { loc, _ := time.LoadLocation("America/New_York"); return loc }(),
			wantErr:    false,
		},
		{
			name:       "invalid expression",
			expression: "invalid",
			timezone:   time.UTC,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextTime, err := NextScheduledTime(tt.expression, tt.timezone)
			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, nextTime.IsZero())
			} else {
				assert.NoError(t, err)
				assert.False(t, nextTime.IsZero())
				assert.True(t, nextTime.After(time.Now()))
			}
		})
	}
}

func TestConvertToCronExpression(t *testing.T) {
	tests := []struct {
		name       string
		natural    string
		expected   string
		wantErr    bool
	}{
		{
			name:     "every minute",
			natural:  "every minute",
			expected: "* * * * *",
			wantErr:  false,
		},
		{
			name:     "every 5 minutes",
			natural:  "every 5 minutes",
			expected: "*/5 * * * *",
			wantErr:  false,
		},
		{
			name:     "every 15 minutes",
			natural:  "every 15 minutes",
			expected: "*/15 * * * *",
			wantErr:  false,
		},
		{
			name:     "every 30 minutes",
			natural:  "every 30 minutes",
			expected: "*/30 * * * *",
			wantErr:  false,
		},
		{
			name:     "every hour",
			natural:  "every hour",
			expected: "0 * * * *",
			wantErr:  false,
		},
		{
			name:     "every 2 hours",
			natural:  "every 2 hours",
			expected: "0 */2 * * *",
			wantErr:  false,
		},
		{
			name:     "every 6 hours",
			natural:  "every 6 hours",
			expected: "0 */6 * * *",
			wantErr:  false,
		},
		{
			name:     "every 12 hours",
			natural:  "every 12 hours",
			expected: "0 */12 * * *",
			wantErr:  false,
		},
		{
			name:     "daily",
			natural:  "daily",
			expected: "0 0 * * *",
			wantErr:  false,
		},
		{
			name:     "every day",
			natural:  "every day",
			expected: "0 0 * * *",
			wantErr:  false,
		},
		{
			name:     "daily at noon",
			natural:  "daily at noon",
			expected: "0 12 * * *",
			wantErr:  false,
		},
		{
			name:     "daily at midnight",
			natural:  "daily at midnight",
			expected: "0 0 * * *",
			wantErr:  false,
		},
		{
			name:     "weekly",
			natural:  "weekly",
			expected: "0 0 * * 0",
			wantErr:  false,
		},
		{
			name:     "every week",
			natural:  "every week",
			expected: "0 0 * * 0",
			wantErr:  false,
		},
		{
			name:     "monthly",
			natural:  "monthly",
			expected: "0 0 1 * *",
			wantErr:  false,
		},
		{
			name:     "every month",
			natural:  "every month",
			expected: "0 0 1 * *",
			wantErr:  false,
		},
		{
			name:     "yearly",
			natural:  "yearly",
			expected: "0 0 1 1 *",
			wantErr:  false,
		},
		{
			name:     "every year",
			natural:  "every year",
			expected: "0 0 1 1 *",
			wantErr:  false,
		},
		{
			name:     "unknown expression",
			natural:  "every blue moon",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ConvertToCronExpression(tt.natural)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestJobSpec_Structure(t *testing.T) {
	timezone, _ := time.LoadLocation("America/New_York")
	lastRun := time.Now().Add(-1 * time.Hour)
	nextRun := time.Now().Add(1 * time.Hour)

	jobSpec := &JobSpec{
		ID:         "test-job-spec",
		Name:       "Test Job Spec",
		Schedule:   "0 0 * * *",
		Timezone:   timezone,
		Enabled:    true,
		LastRun:    &lastRun,
		NextRun:    &nextRun,
		MaxRetries: 3,
		RetryDelay: 30 * time.Second,
		Handler: func(ctx context.Context, jobID string) error {
			return nil
		},
	}

	assert.Equal(t, "test-job-spec", jobSpec.ID)
	assert.Equal(t, "Test Job Spec", jobSpec.Name)
	assert.Equal(t, "0 0 * * *", jobSpec.Schedule)
	assert.Equal(t, timezone, jobSpec.Timezone)
	assert.True(t, jobSpec.Enabled)
	assert.Equal(t, lastRun, *jobSpec.LastRun)
	assert.Equal(t, nextRun, *jobSpec.NextRun)
	assert.Equal(t, 3, jobSpec.MaxRetries)
	assert.Equal(t, 30*time.Second, jobSpec.RetryDelay)
	assert.NotNil(t, jobSpec.Handler)
}

func TestJobExecution_Structure(t *testing.T) {
	startTime := time.Now()
	endTime := startTime.Add(2 * time.Minute)

	execution := &JobExecution{
		JobID:     "test-job-execution",
		StartTime: startTime,
		EndTime:   &endTime,
		Success:   true,
		Error:     nil,
		Attempt:   3,
	}

	assert.Equal(t, "test-job-execution", execution.JobID)
	assert.Equal(t, startTime, execution.StartTime)
	assert.Equal(t, endTime, *execution.EndTime)
	assert.True(t, execution.Success)
	assert.Nil(t, execution.Error)
	assert.Equal(t, 3, execution.Attempt)
}

func TestCronEngine_ConcurrentJobManagement(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	// Test concurrent addition and removal of jobs
	const numJobs = 10
	
	// Add jobs concurrently
	for i := 0; i < numJobs; i++ {
		go func(index int) {
			jobSpec := &JobSpec{
				ID:       fmt.Sprintf("concurrent-job-%d", index),
				Name:     fmt.Sprintf("Concurrent Job %d", index),
				Schedule: "0 0 * * *",
				Enabled:  true,
				Handler: func(ctx context.Context, jobID string) error {
					return nil
				},
			}
			err := engine.AddJob(jobSpec)
			assert.NoError(t, err)
		}(i)
	}

	// Wait a bit for goroutines to complete
	time.Sleep(100 * time.Millisecond)

	// Verify all jobs were added
	jobs := engine.ListJobs()
	assert.Len(t, jobs, numJobs)

	// Remove jobs concurrently
	for i := 0; i < numJobs; i++ {
		go func(index int) {
			err := engine.RemoveJob(fmt.Sprintf("concurrent-job-%d", index))
			assert.NoError(t, err)
		}(i)
	}

	// Wait a bit for goroutines to complete
	time.Sleep(100 * time.Millisecond)

	// Verify all jobs were removed
	jobs = engine.ListJobs()
	assert.Empty(t, jobs)
}

func TestCronEngine_UpdateNextRunTime(t *testing.T) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	jobSpec := &JobSpec{
		ID:       "next-run-job",
		Name:     "Next Run Job",
		Schedule: "0 0 * * *", // Daily at midnight
		Enabled:  true,
		Handler: func(ctx context.Context, jobID string) error {
			return nil
		},
	}

	err := engine.AddJob(jobSpec)
	require.NoError(t, err)

	// Get initial next run time
	job, exists := engine.GetJob("next-run-job")
	require.True(t, exists)
	initialNextRun := job.NextRun

	// Manually execute to update next run time
	wrappedHandler := engine.createWrappedHandler(job)
	wrappedHandler()

	// Verify next run time was updated
	updatedJob, exists := engine.GetJob("next-run-job")
	require.True(t, exists)
	assert.NotEqual(t, initialNextRun, updatedJob.NextRun)
	assert.NotNil(t, updatedJob.LastRun)
}

func BenchmarkCronEngine_AddRemoveJob(b *testing.B) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jobSpec := &JobSpec{
			ID:       fmt.Sprintf("bench-job-%d", i),
			Name:     fmt.Sprintf("Bench Job %d", i),
			Schedule: "0 0 * * *",
			Enabled:  true,
			Handler: func(ctx context.Context, jobID string) error {
				return nil
			},
		}

		err := engine.AddJob(jobSpec)
		if err != nil {
			b.Fatal(err)
		}

		err = engine.RemoveJob(jobSpec.ID)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCronEngine_ListJobs(b *testing.B) {
	logger := logr.Discard()
	engine := NewCronEngine(logger)

	// Add some jobs
	for i := 0; i < 100; i++ {
		jobSpec := &JobSpec{
			ID:       fmt.Sprintf("list-bench-job-%d", i),
			Name:     fmt.Sprintf("List Bench Job %d", i),
			Schedule: "0 0 * * *",
			Enabled:  true,
			Handler: func(ctx context.Context, jobID string) error {
				return nil
			},
		}
		err := engine.AddJob(jobSpec)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jobs := engine.ListJobs()
		if len(jobs) != 100 {
			b.Fatalf("Expected 100 jobs, got %d", len(jobs))
		}
	}
}