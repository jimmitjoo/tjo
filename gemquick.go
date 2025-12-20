package gemquick

import (
	"database/sql"
	"fmt"
	"github.com/jimmitjoo/gemquick/filesystems/miniofilesystem"
	"github.com/jimmitjoo/gemquick/filesystems/s3filesystem"
	"github.com/jimmitjoo/gemquick/jobs"
	"github.com/jimmitjoo/gemquick/logging"
	"github.com/jimmitjoo/gemquick/sms"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CloudyKit/jet/v6"
	"github.com/alexedwards/scs/v2"
	"github.com/dgraph-io/badger/v3"
	"github.com/go-chi/chi/v5"
	"github.com/gomodule/redigo/redis"
	"github.com/jimmitjoo/gemquick/cache"
	"github.com/jimmitjoo/gemquick/email"
	"github.com/jimmitjoo/gemquick/render"
	"github.com/jimmitjoo/gemquick/session"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
)

const version = "0.0.1"

var myRedisCache *cache.RedisCache
var myBadgerCache *cache.BadgerCache
var redisPool *redis.Pool
var badgerConn *badger.DB

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
	config        config

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

type config struct {
	port        string
	renderer    string
	cookie      cookieConfig
	sessionType string
	database    databaseConfig
	redis       redisConfig
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
	if os.Getenv("DATABASE_TYPE") != "" {
		db, err := g.OpenDB(os.Getenv("DATABASE_TYPE"), g.BuildDSN())
		if err != nil {
			errorLog.Println(err)
			os.Exit(1)
		}

		dbConfig := Database{
			DataType:    os.Getenv("DATABASE_TYPE"),
			Pool:        db,
			TablePrefix: os.Getenv("DATABASE_TABLE_PREFIX"),
		}
		g.Data.DB = dbConfig
		g.DB = dbConfig // Legacy accessor
	}

	scheduler := cron.New()
	g.Background.Scheduler = scheduler
	g.Scheduler = scheduler // Legacy accessor

	// initialize job manager
	jobConfig := jobs.DefaultManagerConfig()
	if os.Getenv("JOB_WORKERS") != "" {
		if workers, err := strconv.Atoi(os.Getenv("JOB_WORKERS")); err == nil {
			jobConfig.DefaultWorkers = workers
		}
	}
	if os.Getenv("JOB_ENABLE_PERSISTENCE") == "true" {
		jobConfig.EnablePersistence = true
	}
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
	if os.Getenv("CACHE") == "redis" || os.Getenv("SESSION_TYPE") == "redis" {
		myRedisCache = g.createClientRedisCache()
		g.Data.Cache = myRedisCache
		g.Cache = myRedisCache // Legacy accessor
		redisPool = myRedisCache.Conn
	}

	// connect to badger
	if os.Getenv("CACHE") == "badger" || os.Getenv("SESSION_TYPE") == "badger" {
		myBadgerCache = g.createClientBadgerCache()
		g.Data.Cache = myBadgerCache
		g.Cache = myBadgerCache // Legacy accessor
		badgerConn = myBadgerCache.Conn

		// start badger garbage collector
		_, err := g.Background.Scheduler.AddFunc("@daily", func() {
			_ = myBadgerCache.Conn.RunValueLogGC(0.7)
		})
		if err != nil {
			return err
		}
	}

	g.InfoLog = infoLog   // Legacy accessor
	g.ErrorLog = errorLog // Legacy accessor
	g.Debug, _ = strconv.ParseBool(os.Getenv("DEBUG"))
	g.Version = version
	g.RootPath = rootPath
	g.AppName = os.Getenv("APP_NAME")

	// Setup HTTP router
	g.HTTP.Router = g.routes().(*chi.Mux)
	g.Routes = g.HTTP.Router // Legacy accessor

	g.config = config{
		port:     os.Getenv("PORT"),
		renderer: os.Getenv("RENDERER"),
		cookie: cookieConfig{
			name:     os.Getenv("COOKIE_NAME"),
			lifetime: os.Getenv("COOKIE_LIFETIME"),
			persist:  os.Getenv("COOKIE_PERSISTS"),
			secure:   os.Getenv("COOKIE_SECURE"),
			domain:   os.Getenv("COOKIE_DOMAIN"),
		},
		sessionType: os.Getenv("SESSION_TYPE"),
		database: databaseConfig{
			database: os.Getenv("DATABASE_TYPE"),
			dsn:      g.BuildDSN(),
		},
		redis: redisConfig{
			host:     os.Getenv("REDIS_HOST"),
			port:     os.Getenv("REDIS_PORT"),
			password: os.Getenv("REDIS_PASSWORD"),
			prefix:   os.Getenv("REDIS_PREFIX"),
		},
	}

	secure := true
	if strings.ToLower(os.Getenv("SECURE")) == "false" {
		secure = false
	}

	g.Server = Server{
		ServerName: os.Getenv("SERVER_NAME"),
		Port:       os.Getenv("PORT"),
		Secure:     secure,
		URL:        os.Getenv("APP_URL"),
	}

	// create a session
	sess := session.Session{
		CookieLifetime: g.config.cookie.lifetime,
		CookiePersist:  g.config.cookie.persist,
		CookieName:     g.config.cookie.name,
		SessionType:    g.config.sessionType,
		CookieDomain:   g.config.cookie.domain,
		DBPool:         g.DB.Pool,
	}

	switch g.config.sessionType {
	case "redis":
		sess.RedisPool = myRedisCache.Conn
	case "mysql", "postgres", "mariadb", "postgresql", "pgx", "sqlite", "sqlite3":
		sess.DBPool = g.Data.DB.Pool
	}

	g.HTTP.Session = sess.InitSession()
	g.Session = g.HTTP.Session // Legacy accessor
	g.EncryptionKey = os.Getenv("KEY")

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
	g.Background.SMS = sms.CreateSMSProvider(os.Getenv("SMS_PROVIDER"))
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
		Addr:         fmt.Sprintf(":%s", os.Getenv("PORT")),
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

	if redisPool != nil {
		defer func(redisPool *redis.Pool) {
			err := redisPool.Close()
			if err != nil {
				g.ErrorLog.Println(err)
			}
		}(redisPool)
	}

	if badgerConn != nil {
		defer func(badgerConn *badger.DB) {
			err := badgerConn.Close()
			if err != nil {
				g.ErrorLog.Println(err)
			}
		}(badgerConn)
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
			"port":    os.Getenv("PORT"),
			"version": g.Version,
			"debug":   g.Debug,
		})
	} else {
		g.InfoLog.Printf("Listening on port %s", os.Getenv("PORT"))
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
	// Determine log level from environment
	logLevel := logging.InfoLevel
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		logLevel = logging.ParseLogLevel(envLevel)
	}

	// Enable JSON logging in production
	enableJSON := true
	if os.Getenv("LOG_FORMAT") == "text" {
		enableJSON = false
	}

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

	if myRedisCache != nil {
		g.Logging.Health.AddCheck("redis", logging.RedisHealthChecker(func() error {
			conn := myRedisCache.Conn.Get()
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
		Renderer: g.config.renderer,
		RootPath: g.RootPath,
		Port:     g.config.port,
		JetViews: g.HTTP.JetViews,
		Session:  g.HTTP.Session,
	}

	g.HTTP.Render = &myRenderer
	g.Render = g.HTTP.Render // Legacy accessor
}

func (g *Gemquick) createMailer() email.Mail {
	port, _ := strconv.Atoi(os.Getenv("SMTP_PORT"))
	m := email.Mail{
		Templates: g.RootPath + "/email",

		Host:       os.Getenv("SMTP_HOST"),
		Username:   os.Getenv("SMTP_USERNAME"),
		Password:   os.Getenv("SMTP_PASSWORD"),
		Encryption: os.Getenv("SMTP_ENCRYPTION"),
		Port:       port,

		Domain:   os.Getenv("MAIL_DOMAIN"),
		From:     os.Getenv("MAIL_FROM_ADDRESS"),
		FromName: os.Getenv("MAIL_FROM_NAME"),

		Jobs:    make(chan email.Message, 20),
		Results: make(chan email.Result, 20),

		API:    os.Getenv("MAILER_API"),
		APIKey: os.Getenv("MAILER_KEY"),
		APIUrl: os.Getenv("MAILER_URL"),
	}
	return m
}

func (g *Gemquick) createClientRedisCache() *cache.RedisCache {
	cacheClient := cache.RedisCache{
		Conn:   g.createRedisPool(),
		Prefix: g.config.redis.prefix,
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
			c, err := redis.Dial("tcp", fmt.Sprintf("%s:%s", os.Getenv("REDIS_HOST"), os.Getenv("REDIS_PORT")))

			if err != nil {
				return nil, err
			}

			if os.Getenv("REDIS_PASSWORD") != "" {
				if _, err := c.Do("AUTH", os.Getenv("REDIS_PASSWORD")); err != nil {
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

func (g *Gemquick) BuildDSN() string {
	var dsn string

	switch os.Getenv("DATABASE_TYPE") {
	case "postgres", "postgresql", "pgx":
		dsn = fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s timezone=UTC connect_timeout=5",
			os.Getenv("DATABASE_HOST"),
			os.Getenv("DATABASE_PORT"),
			os.Getenv("DATABASE_USER"),
			os.Getenv("DATABASE_NAME"),
			os.Getenv("DATABASE_SSL_MODE"))

		if os.Getenv("DATABASE_PASS") != "" {
			dsn = fmt.Sprintf("%s password=%s", dsn, os.Getenv("DATABASE_PASS"))
		}

	case "mysql", "mariadb":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?collation=utf8mb4_unicode_ci&parseTime=true&loc=UTC&timeout=5s",
			os.Getenv("DATABASE_USER"),
			os.Getenv("DATABASE_PASS"),
			os.Getenv("DATABASE_HOST"),
			os.Getenv("DATABASE_PORT"),
			os.Getenv("DATABASE_NAME"))

	case "sqlite", "sqlite3":
		// For SQLite, we typically use just the database file path
		// If DATABASE_NAME contains a full path, use it as is
		// Otherwise, use it as a filename in the data directory
		dbPath := os.Getenv("DATABASE_NAME")
		if !strings.HasPrefix(dbPath, "/") && !strings.Contains(dbPath, ":") {
			// Relative path - put it in the data directory
			dsn = fmt.Sprintf("%s/data/%s", g.RootPath, dbPath)
		} else {
			// Absolute path or special SQLite DSN (like :memory:)
			dsn = dbPath
		}

	default:
	}

	return dsn
}

// createFileSystems initializes file storage systems and registers them with the type-safe registry.
// It also returns a legacy map[string]interface{} for backwards compatibility.
func (g *Gemquick) createFileSystems() map[string]interface{} {
	// Legacy map for backwards compatibility
	legacyFileSystems := make(map[string]interface{})

	if os.Getenv("MINIO_SECRET") != "" {
		useSSL := false
		if os.Getenv("MINIO_USE_SSL") == "true" {
			useSSL = true
		}

		minio := &miniofilesystem.Minio{
			Endpoint:  os.Getenv("MINIO_ENDPOINT"),
			AccessKey: os.Getenv("MINIO_ACCESS_KEY"),
			SecretKey: os.Getenv("MINIO_SECRET"),
			UseSSL:    useSSL,
			Region:    os.Getenv("MINIO_REGION"),
			Bucket:    os.Getenv("MINIO_BUCKET"),
		}

		// Register with type-safe registry
		g.Data.Files.Register("minio", minio)
		// Also add to legacy map for backwards compatibility
		legacyFileSystems["minio"] = minio
	}

	if os.Getenv("S3_BUCKET") != "" {
		s3 := &s3filesystem.S3{
			Key:      os.Getenv("S3_KEY"),
			Secret:   os.Getenv("S3_SECRET"),
			Region:   os.Getenv("S3_REGION"),
			Endpoint: os.Getenv("S3_ENDPOINT"),
			Bucket:   os.Getenv("S3_BUCKET"),
		}

		// Register with type-safe registry
		g.Data.Files.Register("s3", s3)
		// Also add to legacy map for backwards compatibility
		legacyFileSystems["s3"] = s3
	}

	return legacyFileSystems
}
