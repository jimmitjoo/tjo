package gemquick

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/CloudyKit/jet/v6"
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
	}

	scheduler := cron.New()
	g.Background.Scheduler = scheduler

	// initialize job manager
	jobConfig := jobs.DefaultManagerConfig()
	jobConfig.DefaultWorkers = g.Config.Jobs.Workers
	jobConfig.EnablePersistence = g.Config.Jobs.EnablePersistence
	g.Background.Jobs = jobs.NewJobManager(jobConfig)

	// setup job persistence if database is available and persistence is enabled
	if jobConfig.EnablePersistence && g.Data.DB.Pool != nil {
		err := g.Background.Jobs.SetPersistence(g.Data.DB.Pool)
		if err != nil {
			return err
		}
	}

	// connect to redis
	if g.Config.App.Cache == "redis" || g.Config.Session.Type == "redis" {
		g.Data.redisCache = g.createClientRedisCache()
		g.Data.Cache = g.Data.redisCache
		g.Data.redisPool = g.Data.redisCache.Conn
	}

	// connect to badger
	if g.Config.App.Cache == "badger" || g.Config.Session.Type == "badger" {
		g.Data.badgerCache = g.createClientBadgerCache()
		g.Data.Cache = g.Data.badgerCache
		g.Data.badgerConn = g.Data.badgerCache.Conn

		// start badger garbage collector
		_, err := g.Background.Scheduler.AddFunc("@daily", func() {
			_ = g.Data.badgerCache.Conn.RunValueLogGC(0.7)
		})
		if err != nil {
			return err
		}
	}

	g.Debug = g.Config.App.Debug
	g.Version = version
	g.RootPath = rootPath
	g.AppName = g.Config.App.Name

	// Setup HTTP router
	g.HTTP.Router = g.routes().(*chi.Mux)

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
		DBPool:         g.Data.DB.Pool,
	}

	switch g.Config.Session.Type {
	case "redis":
		sess.RedisPool = g.Data.redisCache.Conn
	case "mysql", "postgres", "mariadb", "postgresql", "pgx", "sqlite", "sqlite3":
		sess.DBPool = g.Data.DB.Pool
	}

	g.HTTP.Session = sess.InitSession()
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

	g.createRenderer()

	// Setup file systems
	g.createFileSystems()

	// Setup SMS provider
	g.Background.SMS = sms.CreateSMSProvider(g.Config.App.SMSProvider)

	// Setup mail service
	g.Background.Mail = g.createMailer()

	go g.Background.Mail.ListenForMail()

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

// ListenAndServe starts the web server with graceful shutdown support.
// It handles SIGINT and SIGTERM signals to gracefully stop the server,
// waiting for in-flight requests to complete before shutting down.
func (g *Gemquick) ListenAndServe() {
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", g.Config.Server.Port),
		ErrorLog:     g.Logging.Error,
		Handler:      g.HTTP.Router,
		IdleTimeout:  30 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 600 * time.Second,
	}

	// Channel for shutdown signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Channel for server errors
	serverErr := make(chan error, 1)

	// Start job manager
	if err := g.Background.Jobs.Start(); err != nil {
		if g.Logging.Logger != nil {
			g.Logging.Logger.Error("Failed to start job manager", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			g.Logging.Error.Printf("Failed to start job manager: %v", err)
		}
	}

	// Start server in goroutine
	go func() {
		if g.Logging.Logger != nil {
			g.Logging.Logger.Info("Starting server", map[string]interface{}{
				"port":    g.Config.Server.Port,
				"version": g.Version,
				"debug":   g.Debug,
			})
		} else {
			g.Logging.Info.Printf("Listening on port %d", g.Config.Server.Port)
		}

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case err := <-serverErr:
		if g.Logging.Logger != nil {
			g.Logging.Logger.Fatal("Server error", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			g.Logging.Error.Fatalf("Server error: %v", err)
		}
	case sig := <-quit:
		if g.Logging.Logger != nil {
			g.Logging.Logger.Info("Received shutdown signal", map[string]interface{}{
				"signal": sig.String(),
			})
		} else {
			g.Logging.Info.Printf("Received shutdown signal: %v", sig)
		}
	}

	// Begin graceful shutdown
	if g.Logging.Logger != nil {
		g.Logging.Logger.Info("Shutting down server...")
	} else {
		g.Logging.Info.Println("Shutting down server...")
	}

	// Create context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop job manager
	if err := g.Background.Jobs.Stop(); err != nil {
		if g.Logging.Logger != nil {
			g.Logging.Logger.Error("Failed to stop job manager", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			g.Logging.Error.Printf("Failed to stop job manager: %v", err)
		}
	}

	// Stop scheduler if running
	if g.Background.Scheduler != nil {
		g.Background.Scheduler.Stop()
	}

	// Shutdown HTTP server (waits for in-flight requests)
	if err := srv.Shutdown(ctx); err != nil {
		if g.Logging.Logger != nil {
			g.Logging.Logger.Error("Server forced to shutdown", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			g.Logging.Error.Printf("Server forced to shutdown: %v", err)
		}
	}

	// Close database connection
	if g.Data.DB.Pool != nil {
		if err := g.Data.DB.Pool.Close(); err != nil {
			g.Logging.Error.Printf("Error closing database: %v", err)
		}
	}

	// Close Redis connection
	if g.Data.redisPool != nil {
		if err := g.Data.redisPool.Close(); err != nil {
			g.Logging.Error.Printf("Error closing Redis: %v", err)
		}
	}

	// Close Badger connection
	if g.Data.badgerConn != nil {
		if err := g.Data.badgerConn.Close(); err != nil {
			g.Logging.Error.Printf("Error closing Badger: %v", err)
		}
	}

	if g.Logging.Logger != nil {
		g.Logging.Logger.Info("Server shutdown complete")
	} else {
		g.Logging.Info.Println("Server shutdown complete")
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

	// Create metric registry and application metrics
	g.Logging.Metrics = logging.NewMetricRegistry()

	g.Logging.App = logging.NewApplicationMetrics()
	g.Logging.App.Register(g.Logging.Metrics)

	// Create health monitor with version
	g.Logging.Health = logging.NewHealthMonitor(g.Version)

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
		g.Logging.Error.Fatal(err)
	}

	return db
}

// BuildDSN returns the database connection string.
// Deprecated: Use g.Config.Database.DSN(g.RootPath) instead.
func (g *Gemquick) BuildDSN() string {
	return g.Config.Database.DSN(g.RootPath)
}

// createFileSystems initializes file storage systems and registers them with the type-safe registry.
func (g *Gemquick) createFileSystems() {
	if g.Config.Storage.IsMinIOEnabled() {
		minio := &miniofilesystem.Minio{
			Endpoint:  g.Config.Storage.MinIOEndpoint,
			AccessKey: g.Config.Storage.MinIOAccessKey,
			SecretKey: g.Config.Storage.MinIOSecret,
			UseSSL:    g.Config.Storage.MinIOUseSSL,
			Region:    g.Config.Storage.MinIORegion,
			Bucket:    g.Config.Storage.MinIOBucket,
		}
		g.Data.Files.Register("minio", minio)
	}

	if g.Config.Storage.IsS3Enabled() {
		s3 := &s3filesystem.S3{
			Key:      g.Config.Storage.S3Key,
			Secret:   g.Config.Storage.S3Secret,
			Region:   g.Config.Storage.S3Region,
			Endpoint: g.Config.Storage.S3Endpoint,
			Bucket:   g.Config.Storage.S3Bucket,
		}
		g.Data.Files.Register("s3", s3)
	}
}
