// Package config provides structured configuration with validation for Gemquick applications.
// It groups related configuration into logical sections and validates all values at startup.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all application configuration grouped by domain.
// Use Load() to create a validated Config from environment variables.
type Config struct {
	App      AppConfig
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Session  SessionConfig
	Cookie   CookieConfig
	Mail     MailConfig
	Storage  StorageConfig
	Logging  LoggingConfig
	Jobs     JobsConfig
	CORS     CORSConfig
}

// AppConfig holds core application settings
type AppConfig struct {
	Name          string
	Debug         bool
	EncryptionKey string
	Renderer      string
	Cache         string // redis, badger, or empty
	SMSProvider   string // SMS provider name
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Port       int
	ServerName string
	URL        string
	Secure     bool
}

// DatabaseConfig holds database connection settings
type DatabaseConfig struct {
	Type        string
	Host        string
	Port        int
	User        string
	Password    string
	Name        string
	SSLMode     string
	TablePrefix string
}

// RedisConfig holds Redis connection settings
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	Prefix   string
}

// SessionConfig holds session management settings
type SessionConfig struct {
	Type string // cookie, redis, database, badger
}

// CookieConfig holds cookie settings
type CookieConfig struct {
	Name     string
	Lifetime int // in minutes
	Persist  bool
	Secure   bool
	Domain   string
}

// MailConfig holds email settings
type MailConfig struct {
	// SMTP settings
	SMTPHost       string
	SMTPPort       int
	SMTPUsername   string
	SMTPPassword   string
	SMTPEncryption string

	// From settings
	FromAddress string
	FromName    string
	Domain      string

	// API-based mailer settings
	API    string
	APIKey string
	APIURL string
}

// StorageConfig holds file storage settings
type StorageConfig struct {
	// S3 settings
	S3Key      string
	S3Secret   string
	S3Region   string
	S3Endpoint string
	S3Bucket   string

	// MinIO settings
	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecret    string
	MinIORegion    string
	MinIOBucket    string
	MinIOUseSSL    bool
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level  string // trace, debug, info, warn, error, fatal
	Format string // json, text
}

// JobsConfig holds background job settings
type JobsConfig struct {
	Workers           int
	EnablePersistence bool
}

// CORSConfig holds CORS settings
type CORSConfig struct {
	AllowedOrigins []string // Empty means block all cross-origin requests
}

// Load reads configuration from environment variables and validates it.
// Returns an error if required values are missing or invalid.
func Load() (*Config, error) {
	cfg := &Config{}

	// App config
	cfg.App.Name = os.Getenv("APP_NAME")
	cfg.App.Debug = envBool("DEBUG", false)
	cfg.App.EncryptionKey = os.Getenv("KEY")
	cfg.App.Renderer = envDefault("RENDERER", "jet")
	cfg.App.Cache = os.Getenv("CACHE")
	cfg.App.SMSProvider = os.Getenv("SMS_PROVIDER")

	// Server config
	cfg.Server.Port = envInt("PORT", 4000)
	cfg.Server.ServerName = os.Getenv("SERVER_NAME")
	cfg.Server.URL = os.Getenv("APP_URL")
	cfg.Server.Secure = envBool("SECURE", true)

	// Database config
	cfg.Database.Type = os.Getenv("DATABASE_TYPE")
	cfg.Database.Host = os.Getenv("DATABASE_HOST")
	cfg.Database.Port = envInt("DATABASE_PORT", 5432)
	cfg.Database.User = os.Getenv("DATABASE_USER")
	cfg.Database.Password = os.Getenv("DATABASE_PASS")
	cfg.Database.Name = os.Getenv("DATABASE_NAME")
	cfg.Database.SSLMode = envDefault("DATABASE_SSL_MODE", "disable")
	cfg.Database.TablePrefix = os.Getenv("DATABASE_TABLE_PREFIX")

	// Redis config
	cfg.Redis.Host = envDefault("REDIS_HOST", "localhost")
	cfg.Redis.Port = envInt("REDIS_PORT", 6379)
	cfg.Redis.Password = os.Getenv("REDIS_PASSWORD")
	cfg.Redis.Prefix = os.Getenv("REDIS_PREFIX")

	// Session config
	cfg.Session.Type = envDefault("SESSION_TYPE", "cookie")

	// Cookie config
	cfg.Cookie.Name = envDefault("COOKIE_NAME", "gemquick_session")
	cfg.Cookie.Lifetime = envInt("COOKIE_LIFETIME", 1440) // 24 hours default
	cfg.Cookie.Persist = envBool("COOKIE_PERSISTS", true)
	cfg.Cookie.Secure = envBool("COOKIE_SECURE", true)
	cfg.Cookie.Domain = os.Getenv("COOKIE_DOMAIN")

	// Mail config
	cfg.Mail.SMTPHost = os.Getenv("SMTP_HOST")
	cfg.Mail.SMTPPort = envInt("SMTP_PORT", 587)
	cfg.Mail.SMTPUsername = os.Getenv("SMTP_USERNAME")
	cfg.Mail.SMTPPassword = os.Getenv("SMTP_PASSWORD")
	cfg.Mail.SMTPEncryption = envDefault("SMTP_ENCRYPTION", "tls")
	cfg.Mail.FromAddress = os.Getenv("MAIL_FROM_ADDRESS")
	cfg.Mail.FromName = os.Getenv("MAIL_FROM_NAME")
	cfg.Mail.Domain = os.Getenv("MAIL_DOMAIN")
	cfg.Mail.API = os.Getenv("MAILER_API")
	cfg.Mail.APIKey = os.Getenv("MAILER_KEY")
	cfg.Mail.APIURL = os.Getenv("MAILER_URL")

	// Storage config - S3
	cfg.Storage.S3Key = os.Getenv("S3_KEY")
	cfg.Storage.S3Secret = os.Getenv("S3_SECRET")
	cfg.Storage.S3Region = os.Getenv("S3_REGION")
	cfg.Storage.S3Endpoint = os.Getenv("S3_ENDPOINT")
	cfg.Storage.S3Bucket = os.Getenv("S3_BUCKET")

	// Storage config - MinIO
	cfg.Storage.MinIOEndpoint = os.Getenv("MINIO_ENDPOINT")
	cfg.Storage.MinIOAccessKey = os.Getenv("MINIO_ACCESS_KEY")
	cfg.Storage.MinIOSecret = os.Getenv("MINIO_SECRET")
	cfg.Storage.MinIORegion = os.Getenv("MINIO_REGION")
	cfg.Storage.MinIOBucket = os.Getenv("MINIO_BUCKET")
	cfg.Storage.MinIOUseSSL = envBool("MINIO_USE_SSL", false)

	// Logging config
	cfg.Logging.Level = envDefault("LOG_LEVEL", "info")
	cfg.Logging.Format = envDefault("LOG_FORMAT", "json")

	// Jobs config
	cfg.Jobs.Workers = envInt("JOB_WORKERS", 5)
	cfg.Jobs.EnablePersistence = envBool("JOB_ENABLE_PERSISTENCE", false)

	// CORS config
	cfg.CORS.AllowedOrigins = envStringSlice("CORS_ALLOWED_ORIGINS")

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks that all configuration values are valid.
// Returns a combined error with all validation failures.
func (c *Config) Validate() error {
	var errs []string

	// Server validation
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		errs = append(errs, fmt.Sprintf("invalid PORT: %d (must be 1-65535)", c.Server.Port))
	}

	// Database validation (only if type is set)
	if c.Database.Type != "" {
		validDBTypes := map[string]bool{
			"postgres": true, "postgresql": true, "pgx": true,
			"mysql": true, "mariadb": true,
			"sqlite": true, "sqlite3": true,
		}
		if !validDBTypes[strings.ToLower(c.Database.Type)] {
			errs = append(errs, fmt.Sprintf("invalid DATABASE_TYPE: %s", c.Database.Type))
		}
	}

	// Session validation
	validSessionTypes := map[string]bool{
		"cookie": true, "redis": true, "database": true, "badger": true,
	}
	if !validSessionTypes[c.Session.Type] {
		errs = append(errs, fmt.Sprintf("invalid SESSION_TYPE: %s", c.Session.Type))
	}

	// Redis validation (if redis session is used)
	if c.Session.Type == "redis" && c.Redis.Host == "" {
		errs = append(errs, "REDIS_HOST is required when SESSION_TYPE=redis")
	}

	// Cookie validation
	if c.Cookie.Lifetime < 0 {
		errs = append(errs, "COOKIE_LIFETIME cannot be negative")
	}

	// Logging validation
	validLogLevels := map[string]bool{
		"trace": true, "debug": true, "info": true,
		"warn": true, "error": true, "fatal": true,
	}
	if !validLogLevels[strings.ToLower(c.Logging.Level)] {
		errs = append(errs, fmt.Sprintf("invalid LOG_LEVEL: %s", c.Logging.Level))
	}

	validLogFormats := map[string]bool{"json": true, "text": true}
	if !validLogFormats[strings.ToLower(c.Logging.Format)] {
		errs = append(errs, fmt.Sprintf("invalid LOG_FORMAT: %s", c.Logging.Format))
	}

	// Jobs validation
	if c.Jobs.Workers < 1 {
		errs = append(errs, "JOB_WORKERS must be at least 1")
	}

	if len(errs) > 0 {
		return errors.New("configuration errors: " + strings.Join(errs, "; "))
	}

	return nil
}

// Helper functions

func envDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func envBool(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return defaultVal
	}
	return b
}

func envInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return i
}

func envStringSlice(key string) []string {
	val := os.Getenv(key)
	if val == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// DSN returns the database connection string for the configured database type
func (c *DatabaseConfig) DSN(rootPath string) string {
	switch strings.ToLower(c.Type) {
	case "postgres", "postgresql", "pgx":
		dsn := fmt.Sprintf("host=%s port=%d user=%s dbname=%s sslmode=%s timezone=UTC connect_timeout=5",
			c.Host, c.Port, c.User, c.Name, c.SSLMode)
		if c.Password != "" {
			dsn = fmt.Sprintf("%s password=%s", dsn, c.Password)
		}
		return dsn

	case "mysql", "mariadb":
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?collation=utf8mb4_unicode_ci&parseTime=true&loc=UTC&timeout=5s",
			c.User, c.Password, c.Host, c.Port, c.Name)

	case "sqlite", "sqlite3":
		if !strings.HasPrefix(c.Name, "/") && !strings.Contains(c.Name, ":") {
			return fmt.Sprintf("%s/data/%s", rootPath, c.Name)
		}
		return c.Name

	default:
		return ""
	}
}

// IsEnabled returns true if a database is configured
func (c *DatabaseConfig) IsEnabled() bool {
	return c.Type != ""
}

// IsS3Enabled returns true if S3 storage is configured
func (c *StorageConfig) IsS3Enabled() bool {
	return c.S3Bucket != ""
}

// IsMinIOEnabled returns true if MinIO storage is configured
func (c *StorageConfig) IsMinIOEnabled() bool {
	return c.MinIOSecret != ""
}
