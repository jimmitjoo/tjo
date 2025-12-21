package gemquick

import (
	"database/sql"
	"log"
	"sync"

	"github.com/CloudyKit/jet/v6"
	"github.com/alexedwards/scs/v2"
	"github.com/dgraph-io/badger/v3"
	"github.com/go-chi/chi/v5"
	"github.com/gomodule/redigo/redis"
	"github.com/jimmitjoo/gemquick/cache"
	"github.com/jimmitjoo/gemquick/email"
	"github.com/jimmitjoo/gemquick/filesystems"
	"github.com/jimmitjoo/gemquick/jobs"
	"github.com/jimmitjoo/gemquick/logging"
	"github.com/jimmitjoo/gemquick/otel"
	"github.com/jimmitjoo/gemquick/render"
	"github.com/jimmitjoo/gemquick/sms"
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
	cronMu      sync.RWMutex
}

// ScheduleCron adds a named cron job that can be unscheduled later.
// Returns the entry ID and any error from adding the job.
func (b *BackgroundService) ScheduleCron(name, expr string, fn func()) (cron.EntryID, error) {
	b.cronMu.Lock()
	defer b.cronMu.Unlock()

	if b.cronEntries == nil {
		b.cronEntries = make(map[string]cron.EntryID)
	}

	id, err := b.Scheduler.AddFunc(expr, fn)
	if err != nil {
		return 0, err
	}

	b.cronEntries[name] = id
	return id, nil
}

// UnscheduleCron removes a named cron job. Returns true if the job was found and removed.
func (b *BackgroundService) UnscheduleCron(name string) bool {
	b.cronMu.Lock()
	defer b.cronMu.Unlock()

	if id, ok := b.cronEntries[name]; ok {
		b.Scheduler.Remove(id)
		delete(b.cronEntries, name)
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
