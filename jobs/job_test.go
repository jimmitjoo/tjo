package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJob(t *testing.T) {
	payload := map[string]interface{}{
		"message": "test message",
		"count":   42,
	}
	
	job := NewJob("email", "notifications", payload)
	
	assert.NotEmpty(t, job.ID)
	assert.Equal(t, "email", job.Type)
	assert.Equal(t, "notifications", job.Queue)
	assert.Equal(t, PriorityNormal, job.Priority)
	assert.Equal(t, payload, job.Payload)
	assert.Equal(t, JobStatusPending, job.Status)
	assert.Equal(t, 3, job.MaxAttempts)
	assert.Equal(t, 0, job.Attempts)
	assert.NotZero(t, job.CreatedAt)
	assert.NotZero(t, job.UpdatedAt)
	assert.NotNil(t, job.Metadata)
}

func TestJobWithMethods(t *testing.T) {
	job := NewJob("test", "default", nil)
	
	job.WithPriority(PriorityHigh)
	assert.Equal(t, PriorityHigh, job.Priority)
	
	job.WithMaxAttempts(5)
	assert.Equal(t, 5, job.MaxAttempts)
	
	scheduledTime := time.Now().Add(time.Hour)
	job.WithScheduledAt(scheduledTime)
	assert.Equal(t, &scheduledTime, job.ScheduledAt)
	assert.Equal(t, JobStatusScheduled, job.Status)
	
	job.WithMetadata("source", "test")
	assert.Equal(t, "test", job.Metadata["source"])
}

func TestJobStatusMethods(t *testing.T) {
	job := NewJob("test", "default", nil)
	
	job.MarkRunning()
	assert.Equal(t, JobStatusRunning, job.Status)
	assert.NotNil(t, job.StartedAt)
	
	result := map[string]interface{}{"success": true}
	job.MarkCompleted(result)
	assert.Equal(t, JobStatusCompleted, job.Status)
	assert.Equal(t, result, job.Result)
	assert.NotNil(t, job.CompletedAt)
	
	job2 := NewJob("test", "default", nil)
	err := assert.AnError
	job2.MarkFailed(err)
	assert.Equal(t, JobStatusFailed, job2.Status)
	assert.Equal(t, err.Error(), job2.Error)
	assert.NotNil(t, job2.FailedAt)
	
	job3 := NewJob("test", "default", nil)
	job3.MarkRetrying(err)
	assert.Equal(t, JobStatusRetrying, job3.Status)
	assert.Equal(t, err.Error(), job3.Error)
	assert.Equal(t, 1, job3.Attempts)
	
	job4 := NewJob("test", "default", nil)
	job4.MarkCancelled()
	assert.Equal(t, JobStatusCancelled, job4.Status)
}

func TestJobShouldRetry(t *testing.T) {
	job := NewJob("test", "default", nil)
	job.MaxAttempts = 3
	
	job.Attempts = 1
	job.Status = JobStatusFailed
	assert.True(t, job.ShouldRetry())
	
	job.Attempts = 3
	assert.False(t, job.ShouldRetry())
	
	job.Attempts = 1
	job.Status = JobStatusCompleted
	assert.False(t, job.ShouldRetry())
}

func TestJobIsReady(t *testing.T) {
	job := NewJob("test", "default", nil)
	assert.True(t, job.IsReady())
	
	job.Status = JobStatusRunning
	assert.False(t, job.IsReady())
	
	job.Status = JobStatusScheduled
	future := time.Now().Add(time.Hour)
	job.ScheduledAt = &future
	assert.False(t, job.IsReady())
	
	past := time.Now().Add(-time.Hour)
	job.ScheduledAt = &past
	assert.True(t, job.IsReady())
}

func TestJobPayloadMethods(t *testing.T) {
	payload := map[string]interface{}{
		"message": "hello",
		"count":   42,
		"active":  true,
		"invalid": []int{1, 2, 3},
	}
	
	job := NewJob("test", "default", payload)
	
	message, err := job.GetPayloadString("message")
	require.NoError(t, err)
	assert.Equal(t, "hello", message)
	
	count, err := job.GetPayloadInt("count")
	require.NoError(t, err)
	assert.Equal(t, 42, count)
	
	active, err := job.GetPayloadBool("active")
	require.NoError(t, err)
	assert.True(t, active)
	
	_, err = job.GetPayloadString("nonexistent")
	assert.Error(t, err)
	
	_, err = job.GetPayloadString("count")
	assert.Error(t, err)
	
	_, err = job.GetPayloadInt("invalid")
	assert.Error(t, err)
}

func TestJobDuration(t *testing.T) {
	job := NewJob("test", "default", nil)
	
	assert.Zero(t, job.Duration())
	
	startTime := time.Now().Add(-time.Minute)
	job.StartedAt = &startTime
	
	duration := job.Duration()
	assert.True(t, duration > 50*time.Second)
	assert.True(t, duration < 70*time.Second)
	
	endTime := startTime.Add(30 * time.Second)
	job.CompletedAt = &endTime
	
	duration = job.Duration()
	assert.Equal(t, 30*time.Second, duration)
}

func TestJobSerialization(t *testing.T) {
	payload := map[string]interface{}{
		"message": "test",
		"count":   42,
	}
	
	job := NewJob("email", "notifications", payload)
	job.WithPriority(PriorityHigh)
	job.WithMetadata("source", "test")
	
	jsonData, err := job.ToJSON()
	require.NoError(t, err)
	assert.NotEmpty(t, jsonData)
	
	newJob := &Job{}
	err = newJob.FromJSON(jsonData)
	require.NoError(t, err)
	
	assert.Equal(t, job.ID, newJob.ID)
	assert.Equal(t, job.Type, newJob.Type)
	assert.Equal(t, job.Queue, newJob.Queue)
	assert.Equal(t, job.Priority, newJob.Priority)
	
	// Handle JSON unmarshaling converting int to float64
	expectedPayload := map[string]interface{}{
		"message": "test",
		"count":   float64(42), // JSON unmarshaling converts numbers to float64
	}
	assert.Equal(t, expectedPayload, newJob.Payload)
	assert.Equal(t, job.Metadata, newJob.Metadata)
}

func TestJobClone(t *testing.T) {
	payload := map[string]interface{}{
		"message": "test",
	}
	
	job := NewJob("test", "default", payload)
	job.WithMetadata("key", "value")
	
	now := time.Now()
	job.StartedAt = &now
	
	clone := job.Clone()
	
	assert.Equal(t, job.ID, clone.ID)
	assert.Equal(t, job.Type, clone.Type)
	assert.Equal(t, job.Payload, clone.Payload)
	assert.Equal(t, job.Metadata, clone.Metadata)
	assert.Equal(t, job.StartedAt, clone.StartedAt)
	
	clone.Payload["message"] = "modified"
	assert.NotEqual(t, job.Payload["message"], clone.Payload["message"])
	
	clone.Metadata["key"] = "modified"
	assert.NotEqual(t, job.Metadata["key"], clone.Metadata["key"])
}

func TestGetPayloadGeneric(t *testing.T) {
	payload := map[string]interface{}{
		"email":   "test@example.com",
		"count":   42.0, // JSON unmarshals numbers as float64
		"enabled": true,
	}

	job := NewJob("test", "default", payload)

	// Test successful retrieval
	email, err := GetPayload[string](job, "email")
	require.NoError(t, err)
	assert.Equal(t, "test@example.com", email)

	count, err := GetPayload[float64](job, "count")
	require.NoError(t, err)
	assert.Equal(t, 42.0, count)

	enabled, err := GetPayload[bool](job, "enabled")
	require.NoError(t, err)
	assert.True(t, enabled)

	// Test missing key
	_, err = GetPayload[string](job, "missing")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Test wrong type
	_, err = GetPayload[int](job, "email")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not the expected type")
}

func TestGetMetadataGeneric(t *testing.T) {
	job := NewJob("test", "default", nil)
	job.WithMetadata("retry_count", 3)
	job.WithMetadata("source", "api")

	// Test successful retrieval
	count, err := GetMetadata[int](job, "retry_count")
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	source, err := GetMetadata[string](job, "source")
	require.NoError(t, err)
	assert.Equal(t, "api", source)

	// Test missing key
	_, err = GetMetadata[string](job, "missing")
	assert.Error(t, err)

	// Test nil metadata
	emptyJob := &Job{}
	_, err = GetMetadata[string](emptyJob, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "metadata is nil")
}