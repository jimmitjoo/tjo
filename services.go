package gemquick

import (
	"database/sql"
	"log"
	"sync"

	"github.com/CloudyKit/jet/v6"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jimmitjoo/gemquick/cache"
	"github.com/jimmitjoo/gemquick/email"
	"github.com/jimmitjoo/gemquick/filesystems"
	"github.com/jimmitjoo/gemquick/jobs"
	"github.com/jimmitjoo/gemquick/logging"
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
}

// NewLoggingService creates a new logging service with default loggers
func NewLoggingService() *LoggingService {
	return &LoggingService{}
}

// HTTPService handles HTTP routing, sessions, and rendering
type HTTPService struct {
	Router   *chi.Mux
	Session  *scs.SessionManager
	Render   *render.Render
	JetViews *jet.Set
}

// NewHTTPService creates a new HTTP service
func NewHTTPService() *HTTPService {
	return &HTTPService{}
}

// DataService handles database, caching, and file storage
type DataService struct {
	DB    Database
	Cache cache.Cache
	Files *FileSystemRegistry
}

// NewDataService creates a new data service
func NewDataService() *DataService {
	return &DataService{
		Files: NewFileSystemRegistry(),
	}
}

// BackgroundService handles background jobs, scheduling, mail, and SMS
type BackgroundService struct {
	Jobs      *jobs.JobManager
	Scheduler *cron.Cron
	Mail      email.Mail
	SMS       sms.SMSProvider
}

// NewBackgroundService creates a new background service
func NewBackgroundService() *BackgroundService {
	return &BackgroundService{}
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
