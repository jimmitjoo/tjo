package tjo

import (
	"database/sql"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/CloudyKit/jet/v6"
	"github.com/alexedwards/scs/v2"
	"github.com/dgraph-io/badger/v4"
	"github.com/go-chi/chi/v5"
	"github.com/gomodule/redigo/redis"
	"github.com/jimmitjoo/tjo/cache"
	"github.com/jimmitjoo/tjo/email"
	"github.com/jimmitjoo/tjo/filesystems"
	"github.com/jimmitjoo/tjo/jobs"
	"github.com/jimmitjoo/tjo/logging"
	"github.com/jimmitjoo/tjo/otel"
	"github.com/jimmitjoo/tjo/render"
	"github.com/jimmitjoo/tjo/sms"
	"github.com/robfig/cron/v3"
)

// LoggingService handles all logging, metrics, and health monitoring
type LoggingService struct {
	Error   *log.Logger
	Info    *log.Logger
	Logger  *logging.Logger
	Metrics *logging.MetricRegistry
	Health  *logging.HealthMonitor
	App     *logging.ApplicationMetrics
	OTel    *otel.Provider // OpenTelemetry provider for distributed tracing
}

// HTTPService handles HTTP routing, sessions, and rendering
type HTTPService struct {
	Router   *chi.Mux
	Session  *scs.SessionManager
	Render   *render.Render
	JetViews *jet.Set
}

// DataService handles database, caching, and file storage
type DataService struct {
	DB          Database
	Cache       cache.Cache
	Files       *FileSystemRegistry
	redisCache  *cache.RedisCache
	badgerCache *cache.BadgerCache
	redisPool   *redis.Pool
	badgerConn  *badger.DB
}

// NewDataService creates a new data service
func NewDataService() *DataService {
	return &DataService{
		Files: NewFileSystemRegistry(),
	}
}

// BackgroundService handles background jobs, scheduling, mail, and SMS
type BackgroundService struct {
	Jobs        *jobs.JobManager
	Scheduler   *cron.Cron
	Mail        email.Mail
	SMS         sms.SMSProvider
	cronEntries map[string]cron.EntryID
	cronRuns    map[string]*CronRun
	cronMu      sync.RWMutex
}

// CronRun is what happened the last time a scheduled job ran.
//
// Nothing recorded this before, which is why a cron entry that had silently
// stopped firing was invisible: the scheduler knew the job existed and nothing
// knew whether it had ever done anything.
type CronRun struct {
	// Name is the scheduled job.
	Name string

	// LastRun is when it last started. Zero means it has not run since the
	// process started -- which for a nightly job is normal for most of the day,
	// and is why the dashboard shows the schedule next to it.
	LastRun time.Time

	// Duration is how long the last run took.
	Duration time.Duration

	// Runs and Failures count since the process started.
	Runs     int
	Failures int

	// LastError is the panic recovered from the last failed run.
	LastError string
}

// ScheduleCron adds a named cron job that can be unscheduled later.
// Returns the entry ID and any error from adding the job.
func (b *BackgroundService) ScheduleCron(name, expr string, fn func()) (cron.EntryID, error) {
	b.cronMu.Lock()
	defer b.cronMu.Unlock()

	if b.cronEntries == nil {
		b.cronEntries = make(map[string]cron.EntryID)
	}
	if b.cronRuns == nil {
		b.cronRuns = make(map[string]*CronRun)
	}

	// Wrapped so that the run is recorded and a panic in a scheduled job is
	// contained. robfig/cron's default logger prints a recovered panic and
	// keeps the entry, but nothing else could tell that it had happened; a job
	// panicking every night looked exactly like a job doing its work.
	id, err := b.Scheduler.AddFunc(expr, func() { b.runCron(name, fn) })
	if err != nil {
		return 0, err
	}

	b.cronEntries[name] = id
	b.cronRuns[name] = &CronRun{Name: name}
	return id, nil
}

// runCron records one run.
func (b *BackgroundService) runCron(name string, fn func()) {
	started := time.Now()

	defer func() {
		recovered := recover()

		b.cronMu.Lock()
		defer b.cronMu.Unlock()

		run := b.cronRuns[name]
		if run == nil {
			run = &CronRun{Name: name}
			b.cronRuns[name] = run
		}

		run.LastRun = started
		run.Duration = time.Since(started)
		run.Runs++

		if recovered != nil {
			run.Failures++
			run.LastError = fmt.Sprint(recovered)
		}
	}()

	fn()
}

// CronStatus reports the last run of every scheduled job, sorted by name.
func (b *BackgroundService) CronStatus() []CronRun {
	b.cronMu.RLock()
	defer b.cronMu.RUnlock()

	out := make([]CronRun, 0, len(b.cronRuns))
	for _, run := range b.cronRuns {
		out = append(out, *run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// UnscheduleCron removes a named cron job. Returns true if the job was found and removed.
func (b *BackgroundService) UnscheduleCron(name string) bool {
	b.cronMu.Lock()
	defer b.cronMu.Unlock()

	if id, ok := b.cronEntries[name]; ok {
		b.Scheduler.Remove(id)
		delete(b.cronEntries, name)
		delete(b.cronRuns, name)
		return true
	}
	return false
}

// GetCronEntry returns the entry ID for a named cron job.
func (b *BackgroundService) GetCronEntry(name string) (cron.EntryID, bool) {
	b.cronMu.RLock()
	defer b.cronMu.RUnlock()

	id, ok := b.cronEntries[name]
	return id, ok
}

// ListCronJobs returns all named cron job names.
func (b *BackgroundService) ListCronJobs() []string {
	b.cronMu.RLock()
	defer b.cronMu.RUnlock()

	names := make([]string, 0, len(b.cronEntries))
	for name := range b.cronEntries {
		names = append(names, name)
	}
	return names
}

// FileSystemRegistry provides thread-safe access to registered file systems.
// It uses the filesystems.FS interface for type safety instead of map[string]interface{}.
type FileSystemRegistry struct {
	systems map[string]filesystems.FS
	mu      sync.RWMutex
}

// NewFileSystemRegistry creates a new file system registry
func NewFileSystemRegistry() *FileSystemRegistry {
	return &FileSystemRegistry{
		systems: make(map[string]filesystems.FS),
	}
}

// Register adds a file system to the registry
func (r *FileSystemRegistry) Register(name string, fs filesystems.FS) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.systems[name] = fs
}

// Get retrieves a file system by name
func (r *FileSystemRegistry) Get(name string) (filesystems.FS, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fs, ok := r.systems[name]
	return fs, ok
}

// Has checks if a file system is registered
func (r *FileSystemRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.systems[name]
	return ok
}

// Names returns all registered file system names
func (r *FileSystemRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.systems))
	for name := range r.systems {
		names = append(names, name)
	}
	return names
}

// Database represents a database connection with metadata
type Database struct {
	DataType    string
	Pool        *sql.DB
	TablePrefix string
}
