package jobs

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

type Queue interface {
	Push(job *Job) error
	Pop(ctx context.Context) (*Job, error)
	Peek() (*Job, error)
	Size() int
	Name() string
	Clear() error
}

type MemoryQueue struct {
	name  string
	jobs  []*Job
	mutex sync.RWMutex
}

func NewMemoryQueue(name string) *MemoryQueue {
	return &MemoryQueue{
		name: name,
		jobs: make([]*Job, 0),
	}
}

// Push stores a copy of job. The queue owns its jobs outright: workers mutate
// the job they popped, and letting the caller keep a pointer to the same object
// made Job.Status race between the caller's goroutine and Job.MarkRunning on a
// worker. See issue #24.
func (mq *MemoryQueue) Push(job *Job) error {
	mq.mutex.Lock()
	defer mq.mutex.Unlock()

	job.Queue = mq.name

	queued := job.Clone()
	mq.jobs = append(mq.jobs, queued)

	sort.Slice(mq.jobs, func(i, j int) bool {
		if mq.jobs[i].Priority != mq.jobs[j].Priority {
			return getQueuePriority(mq.jobs[i].Priority) > getQueuePriority(mq.jobs[j].Priority)
		}
		return mq.jobs[i].CreatedAt.Before(mq.jobs[j].CreatedAt)
	})

	return nil
}

func (mq *MemoryQueue) Pop(ctx context.Context) (*Job, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			mq.mutex.Lock()

			for i, job := range mq.jobs {
				if job.IsReady() {
					mq.jobs = append(mq.jobs[:i], mq.jobs[i+1:]...)
					mq.mutex.Unlock()
					return job, nil
				}
			}

			mq.mutex.Unlock()
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// Peek returns a copy of the next ready job. The job stays in the queue, so
// handing out the pointer would let the caller read fields a worker is about
// to write.
func (mq *MemoryQueue) Peek() (*Job, error) {
	mq.mutex.RLock()
	defer mq.mutex.RUnlock()

	for _, job := range mq.jobs {
		if job.IsReady() {
			return job.Clone(), nil
		}
	}

	return nil, fmt.Errorf("no ready jobs in queue")
}

func (mq *MemoryQueue) Size() int {
	mq.mutex.RLock()
	defer mq.mutex.RUnlock()
	return len(mq.jobs)
}

func (mq *MemoryQueue) Name() string {
	return mq.name
}

func (mq *MemoryQueue) Clear() error {
	mq.mutex.Lock()
	defer mq.mutex.Unlock()
	mq.jobs = mq.jobs[:0]
	return nil
}

// GetJobs returns copies of the queued jobs. Copying the slice alone still
// handed out the same underlying jobs.
func (mq *MemoryQueue) GetJobs() []*Job {
	mq.mutex.RLock()
	defer mq.mutex.RUnlock()

	jobs := make([]*Job, len(mq.jobs))
	for i, job := range mq.jobs {
		jobs[i] = job.Clone()
	}
	return jobs
}

func (mq *MemoryQueue) GetJobsByStatus(status JobStatus) []*Job {
	mq.mutex.RLock()
	defer mq.mutex.RUnlock()

	var jobs []*Job
	for _, job := range mq.jobs {
		if job.Status == status {
			jobs = append(jobs, job.Clone())
		}
	}
	return jobs
}

// PromoteScheduled flips scheduled jobs whose time has come to pending, under
// the queue's own lock. The manager used to do this by mutating the pointers
// GetJobsByStatus handed back, from the scheduler goroutine, while workers were
// reading the same jobs.
func (mq *MemoryQueue) PromoteScheduled() int {
	mq.mutex.Lock()
	defer mq.mutex.Unlock()

	promoted := 0
	now := time.Now()
	for _, job := range mq.jobs {
		if job.Status == JobStatusScheduled && job.ScheduledAt != nil && !job.ScheduledAt.After(now) {
			job.Status = JobStatusPending
			job.UpdatedAt = now
			promoted++
		}
	}
	return promoted
}

func (mq *MemoryQueue) RemoveJob(jobID string) error {
	mq.mutex.Lock()
	defer mq.mutex.Unlock()

	for i, job := range mq.jobs {
		if job.ID == jobID {
			mq.jobs = append(mq.jobs[:i], mq.jobs[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("job with ID %s not found", jobID)
}

type PriorityQueue struct {
	*MemoryQueue
}

func NewPriorityQueue(name string) *PriorityQueue {
	return &PriorityQueue{
		MemoryQueue: NewMemoryQueue(name),
	}
}

type DelayedQueue struct {
	*MemoryQueue
}

func NewDelayedQueue(name string) *DelayedQueue {
	return &DelayedQueue{
		MemoryQueue: NewMemoryQueue(name),
	}
}

func (dq *DelayedQueue) Push(job *Job) error {
	if job.ScheduledAt == nil {
		now := time.Now()
		job.ScheduledAt = &now
	}
	job.Status = JobStatusScheduled
	return dq.MemoryQueue.Push(job)
}

type QueueManager struct {
	queues map[string]Queue
	mutex  sync.RWMutex
}

func NewQueueManager() *QueueManager {
	return &QueueManager{
		queues: make(map[string]Queue),
	}
}

func (qm *QueueManager) RegisterQueue(name string, queue Queue) {
	qm.mutex.Lock()
	defer qm.mutex.Unlock()
	qm.queues[name] = queue
}

func (qm *QueueManager) GetQueue(name string) (Queue, error) {
	qm.mutex.RLock()
	defer qm.mutex.RUnlock()

	queue, exists := qm.queues[name]
	if !exists {
		return nil, fmt.Errorf("queue %s not found", name)
	}

	return queue, nil
}

func (qm *QueueManager) GetOrCreateQueue(name string) Queue {
	qm.mutex.Lock()
	defer qm.mutex.Unlock()

	if queue, exists := qm.queues[name]; exists {
		return queue
	}

	queue := NewMemoryQueue(name)
	qm.queues[name] = queue
	return queue
}

func (qm *QueueManager) ListQueues() []string {
	qm.mutex.RLock()
	defer qm.mutex.RUnlock()

	names := make([]string, 0, len(qm.queues))
	for name := range qm.queues {
		names = append(names, name)
	}

	return names
}

func (qm *QueueManager) GetQueueStats() map[string]QueueStats {
	qm.mutex.RLock()
	defer qm.mutex.RUnlock()

	stats := make(map[string]QueueStats)
	for name, queue := range qm.queues {
		if memQueue, ok := queue.(*MemoryQueue); ok {
			stats[name] = QueueStats{
				Name:      name,
				Size:      memQueue.Size(),
				Pending:   len(memQueue.GetJobsByStatus(JobStatusPending)),
				Running:   len(memQueue.GetJobsByStatus(JobStatusRunning)),
				Completed: len(memQueue.GetJobsByStatus(JobStatusCompleted)),
				Failed:    len(memQueue.GetJobsByStatus(JobStatusFailed)),
				Scheduled: len(memQueue.GetJobsByStatus(JobStatusScheduled)),
			}
		} else {
			stats[name] = QueueStats{
				Name: name,
				Size: queue.Size(),
			}
		}
	}

	return stats
}

func (qm *QueueManager) ClearQueue(name string) error {
	qm.mutex.RLock()
	defer qm.mutex.RUnlock()

	queue, exists := qm.queues[name]
	if !exists {
		return fmt.Errorf("queue %s not found", name)
	}

	return queue.Clear()
}

func (qm *QueueManager) RemoveQueue(name string) error {
	qm.mutex.Lock()
	defer qm.mutex.Unlock()

	if _, exists := qm.queues[name]; !exists {
		return fmt.Errorf("queue %s not found", name)
	}

	delete(qm.queues, name)
	return nil
}

type QueueStats struct {
	Name      string `json:"name"`
	Size      int    `json:"size"`
	Pending   int    `json:"pending"`
	Running   int    `json:"running"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
	Scheduled int    `json:"scheduled"`
}
