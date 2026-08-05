package jobs

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
)

type Worker struct {
	id            string
	queue         Queue
	processor     *JobProcessor
	ctx           context.Context
	cancel        context.CancelFunc
	status        WorkerStatus
	currentJob    *Job
	startedAt     time.Time
	completedJobs int
	failedJobs    int
	mutex         sync.RWMutex

	// done is closed when run returns, so StopAll can tell the difference
	// between "cancelled" and "actually finished".
	done chan struct{}
}

type WorkerStatus string

const (
	WorkerStatusIdle    WorkerStatus = "idle"
	WorkerStatusBusy    WorkerStatus = "busy"
	WorkerStatusStopped WorkerStatus = "stopped"
)

func NewWorker(id string, queue Queue, processor *JobProcessor) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Worker{
		id:        id,
		queue:     queue,
		processor: processor,
		ctx:       ctx,
		cancel:    cancel,
		status:    WorkerStatusIdle,
		startedAt: time.Now(),
		done:      make(chan struct{}),
	}
}

func (w *Worker) Start() {
	go func() {
		defer close(w.done)
		w.run()
	}()
}

// Wait blocks until the worker's run loop has returned, or ctx expires.
func (w *Worker) Wait(ctx context.Context) error {
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) Stop() {
	w.cancel()
	w.setStatus(WorkerStatusStopped)
}

func (w *Worker) run() {
	for {
		select {
		case <-w.ctx.Done():
			return
		default:
			w.processNextJob()
		}
	}
}

func (w *Worker) processNextJob() {
	job, err := w.queue.Pop(w.ctx)
	if err != nil {
		if err == context.Canceled {
			return
		}
		log.Printf("Worker %s: Error popping job from queue: %v", w.id, err)
		time.Sleep(time.Second)
		return
	}

	w.setCurrentJob(job)
	w.setStatus(WorkerStatusBusy)

	// As claimed. ProcessJob's in-memory retry bookkeeping increments this, and
	// the queue already incremented the row when it handed the job over, so
	// settling with the post-processing value would burn two attempts per run.
	claimedAttempts := job.Attempts

	err = w.processor.ProcessJob(w.ctx, job)
	w.settle(job, claimedAttempts, err)

	w.mutex.Lock()
	if err != nil {
		w.failedJobs++
	} else {
		w.completedJobs++
	}
	w.mutex.Unlock()

	w.setCurrentJob(nil)
	w.setStatus(WorkerStatusIdle)
}

// settleTimeout bounds how long recording an outcome may take.
//
// It is deliberately generous relative to a single UPDATE: this runs during
// shutdown too, and a settle that gives up leaves the job looking abandoned.
const settleTimeout = 5 * time.Second

// settle records how the job ended, for queues that need telling.
func (w *Worker) settle(job *Job, claimedAttempts int, err error) {
	if parked, ok := IsParked(err); ok {
		w.park(job, parked)
		return
	}

	settler, ok := w.queue.(Settler)
	if !ok {
		return
	}

	// Not w.ctx. Stop() cancels it, and shutdown is exactly the moment the
	// outcome must still be written -- otherwise every job in flight when the
	// process stopped looks abandoned and runs a second time on the next boot.
	ctx, cancel := context.WithTimeout(context.Background(), settleTimeout)
	defer cancel()

	if job.Status == JobStatusCompleted {
		if err := settler.Complete(ctx, job.ID); err != nil {
			log.Printf("Worker %s: could not mark job %s completed: %v", w.id, job.ID, err)
		}
		return
	}

	// ProcessJob returns nil when it has scheduled an in-memory retry, so the
	// job's own status is the authority on what happened, not the error.
	cause := err
	if cause == nil {
		cause = fmt.Errorf("%s", job.Error)
	}

	claimed := *job
	claimed.Attempts = claimedAttempts

	if err := settler.Fail(ctx, &claimed, cause); err != nil {
		log.Printf("Worker %s: could not record failure of job %s: %v", w.id, job.ID, err)
	}
}

// park suspends a workflow that is waiting on time or on an event.
func (w *Worker) park(job *Job, parked *Parked) {
	parker, ok := w.queue.(Parker)
	if !ok {
		// Loudly. On a MemoryQueue the job was removed by Pop and there is no
		// row to reschedule, so the workflow is gone -- and a workflow that
		// vanishes at its first Sleep is worth more than a silent return.
		log.Printf("Worker %s: job %s parked until %s (%s) but queue %q cannot park jobs -- the workflow is lost; use a durable queue such as SQLQueue",
			w.id, job.ID, parked.Until.Format(time.RFC3339), parked.Why, w.queue.Name())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), settleTimeout)
	defer cancel()

	if err := parker.Park(ctx, job.ID, parked.Until); err != nil {
		log.Printf("Worker %s: could not park job %s: %v", w.id, job.ID, err)
	}
}

func (w *Worker) setStatus(status WorkerStatus) {
	w.mutex.Lock()
	w.status = status
	w.mutex.Unlock()
}

func (w *Worker) setCurrentJob(job *Job) {
	w.mutex.Lock()
	w.currentJob = job
	w.mutex.Unlock()
}

func (w *Worker) GetStatus() WorkerStatus {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.status
}

func (w *Worker) GetCurrentJob() *Job {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.currentJob
}

func (w *Worker) GetStats() WorkerStats {
	w.mutex.RLock()
	defer w.mutex.RUnlock()

	return WorkerStats{
		ID:            w.id,
		Status:        w.status,
		Queue:         w.queue.Name(),
		StartedAt:     w.startedAt,
		CompletedJobs: w.completedJobs,
		FailedJobs:    w.failedJobs,
		CurrentJob:    w.currentJob,
		Uptime:        time.Since(w.startedAt),
	}
}

type WorkerStats struct {
	ID            string        `json:"id"`
	Status        WorkerStatus  `json:"status"`
	Queue         string        `json:"queue"`
	StartedAt     time.Time     `json:"started_at"`
	CompletedJobs int           `json:"completed_jobs"`
	FailedJobs    int           `json:"failed_jobs"`
	CurrentJob    *Job          `json:"current_job,omitempty"`
	Uptime        time.Duration `json:"uptime"`
}

type WorkerPool struct {
	workers   map[string]*Worker
	processor *JobProcessor
	mutex     sync.RWMutex

	// nextID is monotonic and never reused. IDs used to be derived from
	// len(workers)+1 or a loop index, which collided with live workers during
	// scale-down/scale-up and overwrote them in the map without stopping them.
	nextID int
}

// workerStopTimeout bounds how long StopAll waits for a worker that is inside
// a user handler. The handler's context is already cancelled by then.
const workerStopTimeout = 10 * time.Second

// nextWorkerID returns an unused worker ID. The caller must hold wp.mutex.
func (wp *WorkerPool) nextWorkerID(queueName string) string {
	wp.nextID++
	return fmt.Sprintf("%s-worker-%d", queueName, wp.nextID)
}

func NewWorkerPool(processor *JobProcessor) *WorkerPool {
	return &WorkerPool{
		workers:   make(map[string]*Worker),
		processor: processor,
	}
}

func (wp *WorkerPool) AddWorker(queueName string, queue Queue) string {
	wp.mutex.Lock()
	defer wp.mutex.Unlock()

	workerID := wp.nextWorkerID(queueName)
	worker := NewWorker(workerID, queue, wp.processor)
	wp.workers[workerID] = worker
	worker.Start()

	return workerID
}

func (wp *WorkerPool) RemoveWorker(workerID string) error {
	wp.mutex.Lock()
	defer wp.mutex.Unlock()

	worker, exists := wp.workers[workerID]
	if !exists {
		return fmt.Errorf("worker %s not found", workerID)
	}

	worker.Stop()
	delete(wp.workers, workerID)
	return nil
}

func (wp *WorkerPool) GetWorker(workerID string) (*Worker, error) {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()

	worker, exists := wp.workers[workerID]
	if !exists {
		return nil, fmt.Errorf("worker %s not found", workerID)
	}

	return worker, nil
}

func (wp *WorkerPool) ListWorkers() []string {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()

	workerIDs := make([]string, 0, len(wp.workers))
	for id := range wp.workers {
		workerIDs = append(workerIDs, id)
	}

	return workerIDs
}

func (wp *WorkerPool) GetAllWorkerStats() []WorkerStats {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()

	stats := make([]WorkerStats, 0, len(wp.workers))
	for _, worker := range wp.workers {
		stats = append(stats, worker.GetStats())
	}

	return stats
}

// StopAll cancels every worker and waits for them to return.
//
// It used to cancel and return immediately, so a worker mid-handler kept
// running after "Job manager stopped" was logged -- and Tjo.Shutdown then
// closed the database pool underneath it.
func (wp *WorkerPool) StopAll() {
	wp.mutex.Lock()
	stopping := make([]*Worker, 0, len(wp.workers))
	for _, worker := range wp.workers {
		worker.Stop()
		stopping = append(stopping, worker)
	}
	wp.workers = make(map[string]*Worker)
	wp.mutex.Unlock()

	// Wait outside the lock: a worker finishing a job must not have to
	// contend for wp.mutex in order to exit.
	ctx, cancel := context.WithTimeout(context.Background(), workerStopTimeout)
	defer cancel()

	for _, worker := range stopping {
		if err := worker.Wait(ctx); err != nil {
			log.Printf("Worker %s did not stop within %s", worker.id, workerStopTimeout)
		}
	}
}

func (wp *WorkerPool) GetActiveWorkers() int {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()

	count := 0
	for _, worker := range wp.workers {
		if worker.GetStatus() != WorkerStatusStopped {
			count++
		}
	}

	return count
}

func (wp *WorkerPool) GetBusyWorkers() int {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()

	count := 0
	for _, worker := range wp.workers {
		if worker.GetStatus() == WorkerStatusBusy {
			count++
		}
	}

	return count
}

func (wp *WorkerPool) ScaleQueue(queueName string, queue Queue, targetWorkers int) error {
	wp.mutex.Lock()
	defer wp.mutex.Unlock()

	currentWorkers := 0
	var queueWorkers []string

	for id, worker := range wp.workers {
		if worker.queue.Name() == queueName {
			queueWorkers = append(queueWorkers, id)
			if worker.GetStatus() != WorkerStatusStopped {
				currentWorkers++
			}
		}
	}

	// Sort so scale-down picks the same victims every time. queueWorkers is
	// built by ranging a map, so without this it stopped an arbitrary subset
	// and the surviving IDs were unpredictable.
	sort.Strings(queueWorkers)

	if currentWorkers < targetWorkers {
		for i := currentWorkers; i < targetWorkers; i++ {
			workerID := wp.nextWorkerID(queueName)
			worker := NewWorker(workerID, queue, wp.processor)
			wp.workers[workerID] = worker
			worker.Start()
		}
	} else if currentWorkers > targetWorkers {
		stopCount := currentWorkers - targetWorkers
		for i := 0; i < stopCount && i < len(queueWorkers); i++ {
			worker := wp.workers[queueWorkers[i]]
			worker.Stop()
			delete(wp.workers, queueWorkers[i])
		}
	}

	return nil
}
