package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGenerateJobID(t *testing.T) {
	id1 := generateJobID()
	id2 := generateJobID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Equal(t, 24, len(id1)) // 12 bytes = 24 hex chars
}

func TestCalculateBackoffDelay(t *testing.T) {
	baseDelay := time.Second
	maxDelay := time.Minute

	delay1 := calculateBackoffDelay(1, baseDelay, maxDelay)
	delay2 := calculateBackoffDelay(2, baseDelay, maxDelay)
	delay3 := calculateBackoffDelay(3, baseDelay, maxDelay)

	assert.True(t, delay1 >= baseDelay && delay1 < 3*baseDelay)
	assert.True(t, delay2 >= 2*baseDelay && delay2 < 5*baseDelay)
	assert.True(t, delay3 >= 4*baseDelay && delay3 < 9*baseDelay)

	largeAttempt := calculateBackoffDelay(20, baseDelay, maxDelay)
	assert.True(t, largeAttempt <= maxDelay*11/10) // max + 10% jitter
}

func TestCalculateNextRetryAt(t *testing.T) {
	baseDelay := time.Second
	maxDelay := time.Minute

	now := time.Now()
	nextRetry := calculateNextRetryAt(1, baseDelay, maxDelay)

	assert.True(t, nextRetry.After(now))
	assert.True(t, nextRetry.Before(now.Add(3*baseDelay)))
}

func TestGetQueuePriority(t *testing.T) {
	assert.Equal(t, 1000, getQueuePriority(PriorityCritical))
	assert.Equal(t, 100, getQueuePriority(PriorityHigh))
	assert.Equal(t, 10, getQueuePriority(PriorityNormal))
	assert.Equal(t, 1, getQueuePriority(PriorityLow))
	assert.Equal(t, 10, getQueuePriority(Priority(999))) // unknown priority
}

func TestJSONMarshal(t *testing.T) {
	data, err := jsonMarshal(nil)
	assert.NoError(t, err)
	assert.Equal(t, []byte("null"), data)

	data, err = jsonMarshal(map[string]string{"key": "value"})
	assert.NoError(t, err)
	assert.Contains(t, string(data), "key")
	assert.Contains(t, string(data), "value")

	data, err = jsonMarshal("simple string")
	assert.NoError(t, err)
	assert.Equal(t, []byte(`"simple string"`), data)
}
