package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type JobManager struct {
	queueManager *QueueManager
	processor    *JobProcessor
	workerPool   *WorkerPool
	scheduler    *cron.Cron
	persistence  *JobPersistence
	config       *ManagerConfig
	ctx          context.Context
	cancel       context.CancelFunc
	mutex        sync.RWMutex
	running      bool
}

type ManagerConfig struct {
	DefaultQueue          string
	DefaultWorkers        int
	EnablePersistence     bool
	PersistenceInterval   time.Duration
	SchedulerPollInterval time.Duration
	RetryConfig           RetryConfig
	MaxQueueSize          int
}

type JobPersistence struct {
	db       *sql.DB
	interval time.Duration
	mutex    sync.RWMutex
}

func DefaultManagerConfig() *ManagerConfig {
	return &ManagerConfig{
		DefaultQueue:          "default",
		DefaultWorkers:        5,
		EnablePersistence:     false,
		PersistenceInterval:   time.Minute * 5,
		SchedulerPollInterval: time.Second * 30,
		RetryConfig:           DefaultRetryConfig(),
		MaxQueueSize:          1000,
	}
}

func NewJobManager(config *ManagerConfig) *JobManager {
	if config == nil {
		config = DefaultManagerConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	queueManager := NewQueueManager()
	processor := NewJobProcessor(config.RetryConfig)
	workerPool := NewWorkerPool(processor)

	scheduler := cron.New(cron.WithSeconds())

	jm := &JobManager{
		queueManager: queueManager,
		processor:    processor,
		workerPool:   workerPool,
		scheduler:    scheduler,
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
	}

	jm.startDefaultWorkers()

	deadLetterQueue := queueManager.GetOrCreateQueue("dead_letter")
	processor.SetDeadLetterQueue(deadLetterQueue)

	return jm
}

// startDefaultWorkers staffs the default queue. Called from NewJobManager and
// again from Start when a previous Stop emptied the pool.
func (jm *JobManager) startDefaultWorkers() {
	defaultQueue := jm.queueManager.GetOrCreateQueue(jm.config.DefaultQueue)
	for i := 0; i < jm.config.DefaultWorkers; i++ {
		jm.workerPool.AddWorker(jm.config.DefaultQueue, defaultQueue)
	}
}

func (jm *JobManager) Start() error {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()

	if jm.running {
		return fmt.Errorf("job manager is already running")
	}

	// Rebuild the context. Stop cancels the one built in NewJobManager and
	// nothing recreated it, so a Stop/Start cycle spawned goroutines that
	// returned immediately on the already-cancelled context while running was
	// set to true -- a manager that reported healthy and scheduled nothing.
	jm.ctx, jm.cancel = context.WithCancel(context.Background())

	// Stop drains the worker pool, so a restart needs its workers back.
	if jm.workerPool.GetActiveWorkers() == 0 {
		jm.startDefaultWorkers()
	}

	jm.scheduler.Start()

	if jm.config.EnablePersistence && jm.persistence != nil {
		go jm.runPersistence(jm.ctx)
	}

	go jm.runScheduledJobProcessor(jm.ctx)

	jm.running = true
	log.Println("Job manager started")
	return nil
}

func (jm *JobManager) Stop() error {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()

	if !jm.running {
		return fmt.Errorf("job manager is not running")
	}

	jm.cancel()
	jm.scheduler.Stop()
	jm.workerPool.StopAll()

	if jm.persistence != nil {
		jm.savePersistentJobs()
	}

	jm.running = false
	log.Println("Job manager stopped")
	return nil
}

func (jm *JobManager) SetPersistence(db *sql.DB) error {
	jm.persistence = &JobPersistence{
		db:       db,
		interval: jm.config.PersistenceInterval,
	}

	return jm.initializePersistenceSchema()
}

func (jm *JobManager) initializePersistenceSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS background_jobs (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		queue TEXT NOT NULL,
		priority INTEGER NOT NULL,
		payload TEXT NOT NULL,
		status TEXT NOT NULL,
		attempts INTEGER NOT NULL,
		max_attempts INTEGER NOT NULL,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		scheduled_at DATETIME,
		started_at DATETIME,
		completed_at DATETIME,
		failed_at DATETIME,
		error TEXT,
		result TEXT,
		metadata TEXT
	);
	
	CREATE INDEX IF NOT EXISTS idx_jobs_status ON background_jobs(status);
	CREATE INDEX IF NOT EXISTS idx_jobs_queue ON background_jobs(queue);
	CREATE INDEX IF NOT EXISTS idx_jobs_scheduled ON background_jobs(scheduled_at);
	`

	_, err := jm.persistence.db.Exec(schema)
	return err
}

func (jm *JobManager) Enqueue(job *Job) error {
	if job.Queue == "" {
		job.Queue = jm.config.DefaultQueue
	}

	queue := jm.queueManager.GetOrCreateQueue(job.Queue)

	if jm.config.MaxQueueSize > 0 && queue.Size() >= jm.config.MaxQueueSize {
		return fmt.Errorf("queue %s is full (max size: %d)", job.Queue, jm.config.MaxQueueSize)
	}

	if err := queue.Push(job); err != nil {
		return err
	}

	jm.processor.emitEvent(EventJobQueued, job, nil, nil)

	if jm.persistence != nil {
		jm.saveJobToDB(job)
	}

	return nil
}

func (jm *JobManager) EnqueueIn(job *Job, delay time.Duration) error {
	scheduledAt := time.Now().Add(delay)
	job.WithScheduledAt(scheduledAt)
	return jm.Enqueue(job)
}

func (jm *JobManager) EnqueueAt(job *Job, at time.Time) error {
	job.WithScheduledAt(at)
	return jm.Enqueue(job)
}

func (jm *JobManager) ScheduleCron(cronExpr string, jobType string, queue string, payload map[string]interface{}) (cron.EntryID, error) {
	return jm.scheduler.AddFunc(cronExpr, func() {
		job := NewJob(jobType, queue, payload)
		if err := jm.Enqueue(job); err != nil {
			log.Printf("Failed to enqueue scheduled job: %v", err)
		}
	})
}

func (jm *JobManager) UnscheduleCron(entryID cron.EntryID) {
	jm.scheduler.Remove(entryID)
}

func (jm *JobManager) RegisterHandler(jobType string, handler JobHandler) {
	jm.processor.RegisterHandler(jobType, handler)
}

func (jm *JobManager) RegisterHandlerFunc(jobType string, handler JobHandlerFunc) {
	jm.processor.RegisterHandlerFunc(jobType, handler)
}

func (jm *JobManager) AddEventListener(listener EventListener) {
	jm.processor.AddEventListener(listener)
}

func (jm *JobManager) AddEventListenerFunc(listener EventListenerFunc) {
	jm.processor.AddEventListenerFunc(listener)
}

func (jm *JobManager) ScaleQueue(queueName string, workerCount int) error {
	queue := jm.queueManager.GetOrCreateQueue(queueName)
	return jm.workerPool.ScaleQueue(queueName, queue, workerCount)
}

func (jm *JobManager) GetQueueStats() map[string]QueueStats {
	return jm.queueManager.GetQueueStats()
}

func (jm *JobManager) GetWorkerStats() []WorkerStats {
	return jm.workerPool.GetAllWorkerStats()
}

func (jm *JobManager) GetProcessorMetrics() ProcessorMetrics {
	return jm.processor.GetMetrics()
}

func (jm *JobManager) GetManagerStats() ManagerStats {
	queueStats := jm.GetQueueStats()
	workerStats := jm.GetWorkerStats()
	metrics := jm.GetProcessorMetrics()

	totalJobs := 0
	for _, stats := range queueStats {
		totalJobs += stats.Size
	}

	return ManagerStats{
		TotalQueues:      len(queueStats),
		TotalWorkers:     len(workerStats),
		ActiveWorkers:    jm.workerPool.GetActiveWorkers(),
		BusyWorkers:      jm.workerPool.GetBusyWorkers(),
		TotalJobs:        totalJobs,
		QueueStats:       queueStats,
		WorkerStats:      workerStats,
		ProcessorMetrics: metrics,
		IsRunning:        jm.running,
	}
}

func (jm *JobManager) ClearQueue(queueName string) error {
	return jm.queueManager.ClearQueue(queueName)
}

func (jm *JobManager) PauseQueue(queueName string) error {
	return jm.workerPool.ScaleQueue(queueName, jm.queueManager.GetOrCreateQueue(queueName), 0)
}

func (jm *JobManager) ResumeQueue(queueName string, workerCount int) error {
	if workerCount == 0 {
		workerCount = jm.config.DefaultWorkers
	}
	return jm.ScaleQueue(queueName, workerCount)
}

// runScheduledJobProcessor takes ctx as a parameter rather than reading
// jm.ctx: Start reassigns that field on restart, which raced with the
// goroutines a previous Start had left reading it.
func (jm *JobManager) runScheduledJobProcessor(ctx context.Context) {
	ticker := time.NewTicker(jm.config.SchedulerPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			jm.processScheduledJobs()
		}
	}
}

func (jm *JobManager) processScheduledJobs() {
	for _, queueName := range jm.queueManager.ListQueues() {
		queue, err := jm.queueManager.GetQueue(queueName)
		if err != nil {
			continue
		}

		if memQueue, ok := queue.(*MemoryQueue); ok {
			memQueue.PromoteScheduled()
		}
	}
}

// runPersistence takes ctx for the same reason as runScheduledJobProcessor.
func (jm *JobManager) runPersistence(ctx context.Context) {
	ticker := time.NewTicker(jm.persistence.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			jm.savePersistentJobs()
		}
	}
}

func (jm *JobManager) savePersistentJobs() {
	if jm.persistence == nil {
		return
	}

	for _, queueName := range jm.queueManager.ListQueues() {
		queue, err := jm.queueManager.GetQueue(queueName)
		if err != nil {
			continue
		}

		if memQueue, ok := queue.(*MemoryQueue); ok {
			jobs := memQueue.GetJobs()
			for _, job := range jobs {
				jm.saveJobToDB(job)
			}
		}
	}
}

func (jm *JobManager) saveJobToDB(job *Job) {
	if jm.persistence == nil {
		return
	}

	payloadJSON, _ := jsonMarshal(job.Payload)
	metadataJSON, _ := jsonMarshal(job.Metadata)
	resultJSON, _ := jsonMarshal(job.Result)

	query := `
		INSERT OR REPLACE INTO background_jobs (
			id, type, queue, priority, payload, status, attempts, max_attempts,
			created_at, updated_at, scheduled_at, started_at, completed_at, failed_at,
			error, result, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := jm.persistence.db.Exec(query,
		job.ID, job.Type, job.Queue, job.Priority, string(payloadJSON), job.Status,
		job.Attempts, job.MaxAttempts, job.CreatedAt, job.UpdatedAt,
		job.ScheduledAt, job.StartedAt, job.CompletedAt, job.FailedAt,
		job.Error, string(resultJSON), string(metadataJSON),
	)

	if err != nil {
		log.Printf("Failed to save job to database: %v", err)
	}
}

type ManagerStats struct {
	TotalQueues      int                   `json:"total_queues"`
	TotalWorkers     int                   `json:"total_workers"`
	ActiveWorkers    int                   `json:"active_workers"`
	BusyWorkers      int                   `json:"busy_workers"`
	TotalJobs        int                   `json:"total_jobs"`
	QueueStats       map[string]QueueStats `json:"queue_stats"`
	WorkerStats      []WorkerStats         `json:"worker_stats"`
	ProcessorMetrics ProcessorMetrics      `json:"processor_metrics"`
	IsRunning        bool                  `json:"is_running"`
}
