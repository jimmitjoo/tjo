package gemquick

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/CloudyKit/jet/v6"
	"github.com/alexedwards/scs/v2"
	"github.com/dgraph-io/badger/v3"
	"github.com/go-chi/chi/v5"
	"github.com/gomodule/redigo/redis"
	"github.com/jimmitjoo/gemquick/cache"
	"github.com/jimmitjoo/gemquick/config"
	"github.com/jimmitjoo/gemquick/email"
	"github.com/jimmitjoo/gemquick/filesystems/miniofilesystem"
	"github.com/jimmitjoo/gemquick/filesystems/s3filesystem"
	"github.com/jimmitjoo/gemquick/jobs"
	"github.com/jimmitjoo/gemquick/logging"
	"github.com/jimmitjoo/gemquick/render"
	"github.com/jimmitjoo/gemquick/session"
	"github.com/jimmitjoo/gemquick/sms"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
)

const version = "0.0.1"

// Gemquick is the main framework struct that orchestrates all components.
// It uses composition to organize functionality into focused services:
// - Logging: structured logging, metrics, and health monitoring
// - HTTP: routing, sessions, and template rendering
// - Data: database, caching, and file storage
// - Background: job processing, scheduling, mail, and SMS
type Gemquick struct {
	// Core configuration
	AppName       string
	Debug         bool
	Version       string
	RootPath      string
	EncryptionKey string
	Server        Server
	Config        *config.Config

	// Services (composed)
	Logging    *LoggingService
	HTTP       *HTTPService
	Data       *DataService
	Background *BackgroundService

	// Legacy accessors (for backwards compatibility during migration)
	// These will be deprecated in favor of service accessors
	ErrorLog       *log.Logger              // Use Logging.Error instead
	InfoLog        *log.Logger              // Use Logging.Info instead
	Logger         *logging.Logger          // Use Logging.Logger instead
	MetricRegistry *logging.MetricRegistry  // Use Logging.Metrics instead
	HealthMonitor  *logging.HealthMonitor   // Use Logging.Health instead
	AppMetrics     *logging.ApplicationMetrics // Use Logging.App instead
	Routes         *chi.Mux                 // Use HTTP.Router instead
	Render         *render.Render           // Use HTTP.Render instead
	Session        *scs.SessionManager      // Use HTTP.Session instead
	JetViews       *jet.Set                 // Use HTTP.JetViews instead
	DB             Database                 // Use Data.DB instead
	Cache          cache.Cache              // Use Data.Cache instead
	FileSystems    map[string]interface{}   // Use Data.Files instead
	Scheduler      *cron.Cron               // Use Background.Scheduler instead
	JobManager     *jobs.JobManager         // Use Background.Jobs instead
	SMSProvider    sms.SMSProvider          // Use Background.SMS instead
	Mail           email.Mail               // Use Background.Mail instead
}

type Server struct {
	ServerName string
	Port       string
	Secure     bool
	URL        string
}

func (g *Gemquick) New(rootPath string) error {
	pathConfig := initPaths{
		rootPath:    rootPath,
		folderNames: []string{"handlers", "migrations", "views", "email", "data", "public", "tmp", "logs", "middleware"},
	}

	err := g.Init(pathConfig)
	if err != nil {
		return err
	}

	err = g.checkDotEnv(rootPath)
	if err != nil {
		return err
	}

	// read .env
	err = godotenv.Load(rootPath + "/.env")
	if err != nil {
		return err
	}

	// Load and validate configuration
	g.Config, err = config.Load()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	// Initialize services
	g.Logging = NewLoggingService()
	g.HTTP = NewHTTPService()
	g.Data = NewDataService()
	g.Background = NewBackgroundService()

	// create loggers
	infoLog, errorLog := g.startLoggers()
	g.Logging.Error = errorLog
	g.Logging.Info = infoLog

	g.setupStructuredLogging()

	// connect to database
	if g.Config.Database.IsEnabled() {
		db, err := g.OpenDB(g.Config.Database.Type, g.Config.Database.DSN(rootPath))
		if err != nil {
			errorLog.Println(err)
			os.Exit(1)
		}

		dbConfig := Database{
			DataType:    g.Config.Database.Type,
			Pool:        db,
			TablePrefix: g.Config.Database.TablePrefix,
		}
		g.Data.DB = dbConfig
		g.DB = dbConfig // Legacy accessor
	}

	scheduler := cron.New()
	g.Background.Scheduler = scheduler
	g.Scheduler = scheduler // Legacy accessor

	// initialize job manager
	jobConfig := jobs.DefaultManagerConfig()
	jobConfig.DefaultWorkers = g.Config.Jobs.Workers
	jobConfig.EnablePersistence = g.Config.Jobs.EnablePersistence
	g.Background.Jobs = jobs.NewJobManager(jobConfig)
	g.JobManager = g.Background.Jobs // Legacy accessor

	// setup job persistence if database is available and persistence is enabled
	if jobConfig.EnablePersistence && g.DB.Pool != nil {
		err := g.JobManager.SetPersistence(g.DB.Pool)
		if err != nil {
			return err
		}
	}

	// connect to redis
	if g.Config.App.Cache == "redis" || g.Config.Session.Type == "redis" {
		g.Data.redisCache = g.createClientRedisCache()
		g.Data.Cache = g.Data.redisCache
		g.Cache = g.Data.redisCache // Legacy accessor
		g.Data.redisPool = g.Data.redisCache.Conn
	}

	// connect to badger
	if g.Config.App.Cache == "badger" || g.Config.Session.Type == "badger" {
		g.Data.badgerCache = g.createClientBadgerCache()
		g.Data.Cache = g.Data.badgerCache
		g.Cache = g.Data.badgerCache // Legacy accessor
		g.Data.badgerConn = g.Data.badgerCache.Conn

		// start badger garbage collector
		_, err := g.Background.Scheduler.AddFunc("@daily", func() {
			_ = g.Data.badgerCache.Conn.RunValueLogGC(0.7)
		})
		if err != nil {
			return err
		}
	}

	g.InfoLog = infoLog   // Legacy accessor
	g.ErrorLog = errorLog // Legacy accessor
	g.Debug = g.Config.App.Debug
	g.Version = version
	g.RootPath = rootPath
	g.AppName = g.Config.App.Name

	// Setup HTTP router
	g.HTTP.Router = g.routes().(*chi.Mux)
	g.Routes = g.HTTP.Router // Legacy accessor

	g.Server = Server{
		ServerName: g.Config.Server.ServerName,
		Port:       strconv.Itoa(g.Config.Server.Port),
		Secure:     g.Config.Server.Secure,
		URL:        g.Config.Server.URL,
	}

	// create a session
	sess := session.Session{
		CookieLifetime: strconv.Itoa(g.Config.Cookie.Lifetime),
		CookiePersist:  strconv.FormatBool(g.Config.Cookie.Persist),
		CookieName:     g.Config.Cookie.Name,
		SessionType:    g.Config.Session.Type,
		CookieDomain:   g.Config.Cookie.Domain,
		CookieSecure:   strconv.FormatBool(g.Config.Cookie.Secure),
		DBPool:         g.DB.Pool,
	}

	switch g.Config.Session.Type {
	case "redis":
		sess.RedisPool = g.Data.redisCache.Conn
	case "mysql", "postgres", "mariadb", "postgresql", "pgx", "sqlite", "sqlite3":
		sess.DBPool = g.Data.DB.Pool
	}

	g.HTTP.Session = sess.InitSession()
	g.Session = g.HTTP.Session // Legacy accessor
	g.EncryptionKey = g.Config.App.EncryptionKey

	// Setup Jet template engine
	var views *jet.Set
	if g.Debug {
		views = jet.NewSet(
			jet.NewOSFileSystemLoader(fmt.Sprintf("%s/views", rootPath)),
			jet.InDevelopmentMode(),
		)
	} else {
		views = jet.NewSet(
			jet.NewOSFileSystemLoader(fmt.Sprintf("%s/views", rootPath)),
		)
	}
	g.HTTP.JetViews = views
	g.JetViews = views // Legacy accessor

	g.createRenderer()

	// Setup file systems (legacy map for backwards compatibility)
	g.FileSystems = g.createFileSystems()

	// Setup SMS provider
	g.Background.SMS = sms.CreateSMSProvider(g.Config.App.SMSProvider)
	g.SMSProvider = g.Background.SMS // Legacy accessor

	// Setup mail service
	g.Background.Mail = g.createMailer()
	g.Mail = g.Background.Mail // Legacy accessor

	go g.Mail.ListenForMail()

	return nil
}

func (g *Gemquick) Init(p initPaths) error {
	root := p.rootPath
	for _, path := range p.folderNames {
		// create folder if it doesnt exist
		err := g.CreateDirIfNotExists(root + "/" + path)

		if err != nil {
			return err
		}
	}

	return nil
}

// ListenAndServe starts the web server
func (g *Gemquick) ListenAndServe() {
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", g.Config.Server.Port),
		ErrorLog:     g.ErrorLog,
		Handler:      g.Routes,
		IdleTimeout:  30 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 600 * time.Second,
	}

	if g.DB.Pool != nil {
		defer func(Pool *sql.DB) {
			err := Pool.Close()
			if err != nil {
				g.ErrorLog.Println(err)
			}
		}(g.DB.Pool)
	}

	if g.Data.redisPool != nil {
		defer func(redisPool *redis.Pool) {
			err := redisPool.Close()
			if err != nil {
				g.ErrorLog.Println(err)
			}
		}(g.Data.redisPool)
	}

	if g.Data.badgerConn != nil {
		defer func(badgerConn *badger.DB) {
			err := badgerConn.Close()
			if err != nil {
				g.ErrorLog.Println(err)
			}
		}(g.Data.badgerConn)
	}

	// start job manager
	if err := g.JobManager.Start(); err != nil {
		if g.Logger != nil {
			g.Logger.Error("Failed to start job manager", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			g.ErrorLog.Printf("Failed to start job manager: %v", err)
		}
	}

	// ensure job manager is stopped when server shuts down
	defer func() {
		if err := g.JobManager.Stop(); err != nil {
			if g.Logger != nil {
				g.Logger.Error("Failed to stop job manager", map[string]interface{}{
					"error": err.Error(),
				})
			} else {
				g.ErrorLog.Printf("Failed to stop job manager: %v", err)
			}
		}
	}()

	// Log server startup with structured logging
	if g.Logger != nil {
		g.Logger.Info("Starting server", map[string]interface{}{
			"port":    g.Config.Server.Port,
			"version": g.Version,
			"debug":   g.Debug,
		})
	} else {
		g.InfoLog.Printf("Listening on port %d", g.Config.Server.Port)
	}

	err := srv.ListenAndServe()
	
	// Log server shutdown
	if g.Logger != nil {
		if err != nil {
			g.Logger.Fatal("Server failed to start or encountered fatal error", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			g.Logger.Info("Server shutdown gracefully")
		}
	} else {
		g.ErrorLog.Fatal(err)
	}
}

func (g *Gemquick) checkDotEnv(path string) error {
	err := g.CreateFileIfNotExists(fmt.Sprintf("%s/.env", path))

	if err != nil {
		return err
	}

	return nil
}

func (g *Gemquick) startLoggers() (*log.Logger, *log.Logger) {
	var infoLog *log.Logger
	var errorLog *log.Logger

	infoLog = log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime)
	errorLog = log.New(os.Stdout, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

	return infoLog, errorLog
}

func (g *Gemquick) setupStructuredLogging() {
	// Determine log level from config
	logLevel := logging.ParseLogLevel(g.Config.Logging.Level)

	// Enable JSON logging based on config
	enableJSON := g.Config.Logging.Format != "text"

	// Create structured logger
	g.Logging.Logger = logging.New(logging.Config{
		Level:      logLevel,
		Service:    g.AppName,
		EnableJSON: enableJSON,
	})
	g.Logger = g.Logging.Logger // Legacy accessor

	// Create metric registry and application metrics
	g.Logging.Metrics = logging.NewMetricRegistry()
	g.MetricRegistry = g.Logging.Metrics // Legacy accessor

	g.Logging.App = logging.NewApplicationMetrics()
	g.AppMetrics = g.Logging.App // Legacy accessor
	g.Logging.App.Register(g.Logging.Metrics)

	// Create health monitor with version
	g.Logging.Health = logging.NewHealthMonitor(g.Version)
	g.HealthMonitor = g.Logging.Health // Legacy accessor

	// Add default health checks
	if g.Data.DB.Pool != nil {
		g.Logging.Health.AddCheck("database", logging.DatabaseHealthChecker(func() error {
			return g.Data.DB.Pool.Ping()
		}))
	}

	if g.Data.redisCache != nil {
		g.Logging.Health.AddCheck("redis", logging.RedisHealthChecker(func() error {
			conn := g.Data.redisCache.Conn.Get()
			defer conn.Close()
			_, err := conn.Do("PING")
			return err
		}))
	}

	// Log startup message
	g.Logging.Logger.Info("Structured logging initialized", map[string]interface{}{
		"version":    g.Version,
		"app_name":   g.AppName,
		"debug":      g.Debug,
		"log_level":  logLevel.String(),
		"json_logs":  enableJSON,
	})
}

func (g *Gemquick) createRenderer() {
	myRenderer := render.Render{
		Renderer: g.Config.App.Renderer,
		RootPath: g.RootPath,
		Port:     strconv.Itoa(g.Config.Server.Port),
		JetViews: g.HTTP.JetViews,
		Session:  g.HTTP.Session,
	}

	g.HTTP.Render = &myRenderer
	g.Render = g.HTTP.Render // Legacy accessor
}

func (g *Gemquick) createMailer() email.Mail {
	m := email.Mail{
		Templates: g.RootPath + "/email",

		Host:       g.Config.Mail.SMTPHost,
		Username:   g.Config.Mail.SMTPUsername,
		Password:   g.Config.Mail.SMTPPassword,
		Encryption: g.Config.Mail.SMTPEncryption,
		Port:       g.Config.Mail.SMTPPort,

		Domain:   g.Config.Mail.Domain,
		From:     g.Config.Mail.FromAddress,
		FromName: g.Config.Mail.FromName,

		Jobs:    make(chan email.Message, 20),
		Results: make(chan email.Result, 20),

		API:    g.Config.Mail.API,
		APIKey: g.Config.Mail.APIKey,
		APIUrl: g.Config.Mail.APIURL,
	}
	return m
}

func (g *Gemquick) createClientRedisCache() *cache.RedisCache {
	cacheClient := cache.RedisCache{
		Conn:   g.createRedisPool(),
		Prefix: g.Config.Redis.Prefix,
	}
	return &cacheClient
}

func (g *Gemquick) createClientBadgerCache() *cache.BadgerCache {
	cacheClient := cache.BadgerCache{
		Conn: g.createBadgerConn(),
	}
	return &cacheClient
}

func (g *Gemquick) createRedisPool() *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		MaxActive:   10000,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", fmt.Sprintf("%s:%d", g.Config.Redis.Host, g.Config.Redis.Port))

			if err != nil {
				return nil, err
			}

			if g.Config.Redis.Password != "" {
				if _, err := c.Do("AUTH", g.Config.Redis.Password); err != nil {
					closeError := c.Close()
					if closeError != nil {
						return nil, closeError
					}
					return nil, err
				}
			}

			return c, err
		},

		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
}

func (g *Gemquick) createBadgerConn() *badger.DB {
	db, err := badger.Open(badger.DefaultOptions(fmt.Sprintf("%s/tmp/badger", g.RootPath)))
	if err != nil {
		g.ErrorLog.Fatal(err)
	}

	return db
}

// BuildDSN returns the database connection string.
// Deprecated: Use g.Config.Database.DSN(g.RootPath) instead.
func (g *Gemquick) BuildDSN() string {
	return g.Config.Database.DSN(g.RootPath)
}

// createFileSystems initializes file storage systems and registers them with the type-safe registry.
// It also returns a legacy map[string]interface{} for backwards compatibility.
func (g *Gemquick) createFileSystems() map[string]interface{} {
	// Legacy map for backwards compatibility
	legacyFileSystems := make(map[string]interface{})

	if g.Config.Storage.IsMinIOEnabled() {
		minio := &miniofilesystem.Minio{
			Endpoint:  g.Config.Storage.MinIOEndpoint,
			AccessKey: g.Config.Storage.MinIOAccessKey,
			SecretKey: g.Config.Storage.MinIOSecret,
			UseSSL:    g.Config.Storage.MinIOUseSSL,
			Region:    g.Config.Storage.MinIORegion,
			Bucket:    g.Config.Storage.MinIOBucket,
		}

		// Register with type-safe registry
		g.Data.Files.Register("minio", minio)
		// Also add to legacy map for backwards compatibility
		legacyFileSystems["minio"] = minio
	}

	if g.Config.Storage.IsS3Enabled() {
		s3 := &s3filesystem.S3{
			Key:      g.Config.Storage.S3Key,
			Secret:   g.Config.Storage.S3Secret,
			Region:   g.Config.Storage.S3Region,
			Endpoint: g.Config.Storage.S3Endpoint,
			Bucket:   g.Config.Storage.S3Bucket,
		}

		// Register with type-safe registry
		g.Data.Files.Register("s3", s3)
		// Also add to legacy map for backwards compatibility
		legacyFileSystems["s3"] = s3
	}

	return legacyFileSystems
}
