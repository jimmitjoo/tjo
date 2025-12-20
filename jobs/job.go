package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusRunning    JobStatus = "running"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
	JobStatusRetrying   JobStatus = "retrying"
	JobStatusCancelled  JobStatus = "cancelled"
	JobStatusScheduled  JobStatus = "scheduled"
)

type Priority int

const (
	PriorityLow Priority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

type Job struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Queue       string                 `json:"queue"`
	Priority    Priority               `json:"priority"`
	Payload     map[string]interface{} `json:"payload"`
	Status      JobStatus              `json:"status"`
	Attempts    int                    `json:"attempts"`
	MaxAttempts int                    `json:"max_attempts"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	ScheduledAt *time.Time             `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	FailedAt    *time.Time             `json:"failed_at,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Result      interface{}            `json:"result,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type JobHandler interface {
	Handle(ctx context.Context, job *Job) error
}

type JobHandlerFunc func(ctx context.Context, job *Job) error

func (f JobHandlerFunc) Handle(ctx context.Context, job *Job) error {
	return f(ctx, job)
}

type JobEvent struct {
	Type      string      `json:"type"`
	Job       *Job        `json:"job"`
	Error     error       `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
	Metadata  interface{} `json:"metadata,omitempty"`
}

const (
	EventJobQueued     = "job.queued"
	EventJobStarted    = "job.started"
	EventJobCompleted  = "job.completed"
	EventJobFailed     = "job.failed"
	EventJobRetrying   = "job.retrying"
	EventJobCancelled  = "job.cancelled"
	EventJobDeadLetter = "job.dead_letter"
)

func NewJob(jobType, queue string, payload map[string]interface{}) *Job {
	return &Job{
		ID:          generateJobID(),
		Type:        jobType,
		Queue:       queue,
		Priority:    PriorityNormal,
		Payload:     payload,
		Status:      JobStatusPending,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Metadata:    make(map[string]interface{}),
	}
}

func (j *Job) WithPriority(priority Priority) *Job {
	j.Priority = priority
	j.UpdatedAt = time.Now()
	return j
}

func (j *Job) WithMaxAttempts(attempts int) *Job {
	j.MaxAttempts = attempts
	j.UpdatedAt = time.Now()
	return j
}

func (j *Job) WithScheduledAt(scheduledAt time.Time) *Job {
	j.ScheduledAt = &scheduledAt
	j.Status = JobStatusScheduled
	j.UpdatedAt = time.Now()
	return j
}

func (j *Job) WithMetadata(key string, value interface{}) *Job {
	if j.Metadata == nil {
		j.Metadata = make(map[string]interface{})
	}
	j.Metadata[key] = value
	j.UpdatedAt = time.Now()
	return j
}

func (j *Job) MarkRunning() {
	j.Status = JobStatusRunning
	now := time.Now()
	j.StartedAt = &now
	j.UpdatedAt = now
}

func (j *Job) MarkCompleted(result interface{}) {
	j.Status = JobStatusCompleted
	j.Result = result
	now := time.Now()
	j.CompletedAt = &now
	j.UpdatedAt = now
}

func (j *Job) MarkFailed(err error) {
	j.Status = JobStatusFailed
	j.Error = err.Error()
	now := time.Now()
	j.FailedAt = &now
	j.UpdatedAt = now
}

func (j *Job) MarkRetrying(err error) {
	j.Status = JobStatusRetrying
	j.Error = err.Error()
	j.Attempts++
	j.UpdatedAt = time.Now()
}

func (j *Job) MarkCancelled() {
	j.Status = JobStatusCancelled
	j.UpdatedAt = time.Now()
}

func (j *Job) ShouldRetry() bool {
	return j.Attempts < j.MaxAttempts && j.Status == JobStatusFailed
}

func (j *Job) IsReady() bool {
	if j.Status != JobStatusPending && j.Status != JobStatusScheduled {
		return false
	}
	
	if j.ScheduledAt != nil {
		return time.Now().After(*j.ScheduledAt)
	}
	
	return j.Status == JobStatusPending
}

func (j *Job) Duration() time.Duration {
	if j.StartedAt == nil {
		return 0
	}
	
	endTime := time.Now()
	if j.CompletedAt != nil {
		endTime = *j.CompletedAt
	} else if j.FailedAt != nil {
		endTime = *j.FailedAt
	}
	
	return endTime.Sub(*j.StartedAt)
}

func (j *Job) ToJSON() ([]byte, error) {
	return json.Marshal(j)
}

func (j *Job) FromJSON(data []byte) error {
	return json.Unmarshal(data, j)
}

func (j *Job) GetPayloadValue(key string) (interface{}, bool) {
	value, exists := j.Payload[key]
	return value, exists
}

func (j *Job) GetPayloadString(key string) (string, error) {
	value, exists := j.GetPayloadValue(key)
	if !exists {
		return "", fmt.Errorf("key %s not found in payload", key)
	}
	
	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("key %s is not a string", key)
	}
	
	return str, nil
}

func (j *Job) GetPayloadInt(key string) (int, error) {
	value, exists := j.GetPayloadValue(key)
	if !exists {
		return 0, fmt.Errorf("key %s not found in payload", key)
	}
	
	switch v := value.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("key %s is not a number", key)
	}
}

func (j *Job) GetPayloadBool(key string) (bool, error) {
	value, exists := j.GetPayloadValue(key)
	if !exists {
		return false, fmt.Errorf("key %s not found in payload", key)
	}

	b, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("key %s is not a boolean", key)
	}

	return b, nil
}

// GetPayload is a generic helper for type-safe payload retrieval.
// Example: email, err := GetPayload[string](job, "email")
func GetPayload[T any](j *Job, key string) (T, error) {
	var zero T
	value, exists := j.GetPayloadValue(key)
	if !exists {
		return zero, fmt.Errorf("key %s not found in payload", key)
	}

	typed, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("key %s is not the expected type", key)
	}

	return typed, nil
}

// GetMetadata is a generic helper for type-safe metadata retrieval.
// Example: count, err := GetMetadata[int](job, "retry_count")
func GetMetadata[T any](j *Job, key string) (T, error) {
	var zero T
	if j.Metadata == nil {
		return zero, fmt.Errorf("metadata is nil")
	}

	value, exists := j.Metadata[key]
	if !exists {
		return zero, fmt.Errorf("key %s not found in metadata", key)
	}

	typed, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("key %s is not the expected type", key)
	}

	return typed, nil
}

func (j *Job) Clone() *Job {
	clone := *j
	
	if j.Payload != nil {
		clone.Payload = make(map[string]interface{})
		for k, v := range j.Payload {
			clone.Payload[k] = v
		}
	}
	
	if j.Metadata != nil {
		clone.Metadata = make(map[string]interface{})
		for k, v := range j.Metadata {
			clone.Metadata[k] = v
		}
	}
	
	if j.ScheduledAt != nil {
		scheduledAt := *j.ScheduledAt
		clone.ScheduledAt = &scheduledAt
	}
	
	if j.StartedAt != nil {
		startedAt := *j.StartedAt
		clone.StartedAt = &startedAt
	}
	
	if j.CompletedAt != nil {
		completedAt := *j.CompletedAt
		clone.CompletedAt = &completedAt
	}
	
	if j.FailedAt != nil {
		failedAt := *j.FailedAt
		clone.FailedAt = &failedAt
	}
	
	return &clone
}