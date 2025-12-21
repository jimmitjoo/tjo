package security

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all security-related configuration
type Config struct {
	// Environment
	Environment string // development, staging, production
	
	// Rate limiting
	RateLimit RateLimitConfig
	
	// Security headers
	Headers SecurityConfig
	
	// CSRF protection
	CSRF CSRFConfig
	
	// IP throttling
	Throttle ThrottleConfig
	
	// General security settings
	EnableRateLimit   bool
	EnableHeaders     bool
	EnableCSRF        bool
	EnableThrottling  bool
	EnableCORS        bool
	EnableIPFilter    bool
	
	// Request limits
	MaxRequestSize int64         // Maximum request body size
	RequestTimeout time.Duration // Request timeout
	
	// IP filtering
	TrustedProxies []string
	BlockedIPs     []string
	AllowedIPs     []string
	
	// API security
	APIKeyRequired bool
	APIKeyHeader   string
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	RequestsPerMinute int           // Number of requests per minute
	BurstSize         int           // Burst allowance
	WindowSize        time.Duration // Time window for rate limiting
	SkipSuccessful    bool          // Only count failed requests (4xx, 5xx)
	SkipPaths         []string      // Paths to skip rate limiting
}

// DefaultRateLimitConfig returns sensible defaults
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 60,
		BurstSize:         10,
		WindowSize:        time.Minute,
		SkipSuccessful:    false,
		SkipPaths:         []string{"/health", "/metrics", "/health/ready", "/health/live"},
	}
}

// LoadFromEnv loads security configuration from environment variables
func LoadFromEnv() Config {
	config := Config{
		Environment: getEnvString("ENVIRONMENT", "development"),
		
		// Default enable settings based on environment
		EnableRateLimit:  getEnvBool("SECURITY_RATE_LIMIT", true),
		EnableHeaders:    getEnvBool("SECURITY_HEADERS", true),
		EnableCSRF:       getEnvBool("SECURITY_CSRF", true),
		EnableThrottling: getEnvBool("SECURITY_THROTTLING", true),
		EnableCORS:       getEnvBool("SECURITY_CORS", true),
		EnableIPFilter:   getEnvBool("SECURITY_IP_FILTER", false),
		
		// Request limits
		MaxRequestSize: getEnvInt64("MAX_REQUEST_SIZE", 10*1024*1024), // 10MB
		RequestTimeout: time.Duration(getEnvInt("REQUEST_TIMEOUT", 30)) * time.Second,
		
		// IP filtering
		TrustedProxies: getEnvStringSlice("TRUSTED_PROXIES", []string{"127.0.0.1", "::1"}),
		BlockedIPs:     getEnvStringSlice("BLOCKED_IPS", []string{}),
		AllowedIPs:     getEnvStringSlice("ALLOWED_IPS", []string{}),
		
		// API security
		APIKeyRequired: getEnvBool("API_KEY_REQUIRED", false),
		APIKeyHeader:   getEnvString("API_KEY_HEADER", "X-API-Key"),
	}
	
	// Load specific configurations
	config.RateLimit = loadRateLimitConfig()
	config.Headers = loadSecurityHeadersConfig(config.Environment)
	config.CSRF = loadCSRFConfig(config.Environment)
	config.Throttle = loadThrottleConfig()
	
	return config
}

// loadRateLimitConfig loads rate limiting configuration from environment
func loadRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: getEnvInt("RATE_LIMIT_RPM", 60),
		BurstSize:         getEnvInt("RATE_LIMIT_BURST", 10),
		WindowSize:        time.Duration(getEnvInt("RATE_LIMIT_WINDOW", 60)) * time.Second,
		SkipSuccessful:    getEnvBool("RATE_LIMIT_SKIP_SUCCESSFUL", false),
		SkipPaths:         getEnvStringSlice("RATE_LIMIT_SKIP_PATHS", []string{"/health", "/metrics", "/health/ready", "/health/live"}),
	}
}

// loadSecurityHeadersConfig loads security headers configuration
func loadSecurityHeadersConfig(environment string) SecurityConfig {
	var config SecurityConfig
	
	if environment == "production" {
		config = ProductionSecurityConfig()
	} else if environment == "development" {
		config = DevelopmentSecurityConfig()
	} else {
		config = DefaultSecurityConfig()
	}
	
	// Override with environment variables if set
	if csp := os.Getenv("CSP_POLICY"); csp != "" {
		config.ContentSecurityPolicy = csp
	}
	
	if hstsMaxAge := getEnvInt("HSTS_MAX_AGE", 0); hstsMaxAge > 0 {
		config.HSTSMaxAge = hstsMaxAge
	}
	
	config.HSTSIncludeSubdomains = getEnvBool("HSTS_INCLUDE_SUBDOMAINS", config.HSTSIncludeSubdomains)
	config.HSTSPreload = getEnvBool("HSTS_PRELOAD", config.HSTSPreload)
	
	if frameOptions := os.Getenv("FRAME_OPTIONS"); frameOptions != "" {
		config.FrameOptions = frameOptions
	}
	
	config.ContentTypeNosniff = getEnvBool("CONTENT_TYPE_NOSNIFF", config.ContentTypeNosniff)
	config.XSSProtection = getEnvBool("XSS_PROTECTION", config.XSSProtection)
	
	if xssMode := os.Getenv("XSS_PROTECTION_MODE"); xssMode != "" {
		config.XSSProtectionMode = xssMode
	}
	
	if referrerPolicy := os.Getenv("REFERRER_POLICY"); referrerPolicy != "" {
		config.ReferrerPolicy = referrerPolicy
	}
	
	if permissionsPolicy := os.Getenv("PERMISSIONS_POLICY"); permissionsPolicy != "" {
		config.PermissionsPolicy = permissionsPolicy
	}
	
	// CORS configuration
	config.AllowedOrigins = getEnvStringSlice("CORS_ALLOWED_ORIGINS", config.AllowedOrigins)
	config.AllowedMethods = getEnvStringSlice("CORS_ALLOWED_METHODS", config.AllowedMethods)
	config.AllowedHeaders = getEnvStringSlice("CORS_ALLOWED_HEADERS", config.AllowedHeaders)
	config.AllowedCredentials = getEnvBool("CORS_ALLOW_CREDENTIALS", config.AllowedCredentials)
	config.MaxAge = getEnvInt("CORS_MAX_AGE", config.MaxAge)
	
	return config
}

// loadCSRFConfig loads CSRF configuration
func loadCSRFConfig(environment string) CSRFConfig {
	var config CSRFConfig
	
	if environment == "development" {
		config = DevelopmentCSRFConfig()
	} else {
		config = DefaultCSRFConfig()
	}
	
	// Override with environment variables
	config.TokenLength = getEnvInt("CSRF_TOKEN_LENGTH", config.TokenLength)
	config.CookieName = getEnvString("CSRF_COOKIE_NAME", config.CookieName)
	config.CookiePath = getEnvString("CSRF_COOKIE_PATH", config.CookiePath)
	config.CookieDomain = getEnvString("CSRF_COOKIE_DOMAIN", config.CookieDomain)
	config.CookieSecure = getEnvBool("CSRF_COOKIE_SECURE", config.CookieSecure)
	config.CookieHttpOnly = getEnvBool("CSRF_COOKIE_HTTP_ONLY", config.CookieHttpOnly)
	config.CookieMaxAge = getEnvInt("CSRF_COOKIE_MAX_AGE", config.CookieMaxAge)
	config.RequestHeader = getEnvString("CSRF_REQUEST_HEADER", config.RequestHeader)
	config.FormField = getEnvString("CSRF_FORM_FIELD", config.FormField)
	config.ExemptPaths = getEnvStringSlice("CSRF_EXEMPT_PATHS", config.ExemptPaths)
	config.ExemptGlobs = getEnvStringSlice("CSRF_EXEMPT_GLOBS", config.ExemptGlobs)
	config.ExemptMethods = getEnvStringSlice("CSRF_EXEMPT_METHODS", config.ExemptMethods)
	
	return config
}

// loadThrottleConfig loads IP throttling configuration
func loadThrottleConfig() ThrottleConfig {
	config := DefaultThrottleConfig()
	
	config.RequestsPerMinute = getEnvInt("THROTTLE_RPM", config.RequestsPerMinute)
	config.BurstSize = getEnvInt("THROTTLE_BURST", config.BurstSize)
	config.WindowSize = time.Duration(getEnvInt("THROTTLE_WINDOW", 60)) * time.Second
	config.EnableProgressive = getEnvBool("THROTTLE_PROGRESSIVE", config.EnableProgressive)
	config.MaxPenaltyMinutes = getEnvInt("THROTTLE_MAX_PENALTY", config.MaxPenaltyMinutes)
	config.EnableSuspiciousDetection = getEnvBool("THROTTLE_SUSPICIOUS_DETECTION", config.EnableSuspiciousDetection)
	config.SuspiciousThreshold = getEnvInt("THROTTLE_SUSPICIOUS_THRESHOLD", config.SuspiciousThreshold)
	config.SuspiciousPenaltyMinutes = getEnvInt("THROTTLE_SUSPICIOUS_PENALTY", config.SuspiciousPenaltyMinutes)
	config.EnableSubnetLimiting = getEnvBool("THROTTLE_SUBNET_LIMITING", config.EnableSubnetLimiting)
	config.SubnetRequestsPerMinute = getEnvInt("THROTTLE_SUBNET_RPM", config.SubnetRequestsPerMinute)
	config.SubnetMask = getEnvInt("THROTTLE_SUBNET_MASK", config.SubnetMask)
	config.WhitelistedIPs = getEnvStringSlice("THROTTLE_WHITELIST", config.WhitelistedIPs)
	config.BlacklistedIPs = getEnvStringSlice("THROTTLE_BLACKLIST", config.BlacklistedIPs)
	config.TrustedProxyHeaders = getEnvStringSlice("THROTTLE_PROXY_HEADERS", config.TrustedProxyHeaders)
	config.TrustedProxies = getEnvStringSlice("THROTTLE_TRUSTED_PROXIES", config.TrustedProxies)
	
	return config
}

// Helper functions for environment variable parsing

func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvStringSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}

// Preset configurations for different environments

// DevelopmentConfig returns security configuration optimized for development
func DevelopmentConfig() Config {
	config := LoadFromEnv()
	config.Environment = "development"
	
	// More lenient settings for development
	config.Headers = DevelopmentSecurityConfig()
	config.CSRF = DevelopmentCSRFConfig()
	config.RateLimit.RequestsPerMinute = 300 // Higher limits for dev
	config.Throttle.RequestsPerMinute = 200
	config.EnableIPFilter = false
	
	return config
}

// ProductionConfig returns security configuration optimized for production
func ProductionConfig() Config {
	config := LoadFromEnv()
	config.Environment = "production"
	
	// Stricter settings for production
	config.Headers = ProductionSecurityConfig()
	config.CSRF = DefaultCSRFConfig()
	config.EnableIPFilter = true
	config.APIKeyRequired = true // Require API keys in production
	
	return config
}

// StagingConfig returns security configuration for staging environment
func StagingConfig() Config {
	config := ProductionConfig() // Same as production but with some relaxed settings
	config.Environment = "staging"
	config.APIKeyRequired = false // Optional API keys in staging
	
	return config
}

// ValidateConfig validates the security configuration
func (c *Config) ValidateConfig() error {
	// Validate rate limiting
	if c.EnableRateLimit {
		if c.RateLimit.RequestsPerMinute <= 0 {
			return fmt.Errorf("rate limit requests per minute must be positive")
		}
		if c.RateLimit.BurstSize <= 0 {
			return fmt.Errorf("rate limit burst size must be positive")
		}
	}
	
	// Validate CSRF
	if c.EnableCSRF {
		if c.CSRF.TokenLength < 16 {
			return fmt.Errorf("CSRF token length must be at least 16 bytes")
		}
		if c.CSRF.CookieMaxAge < 0 {
			return fmt.Errorf("CSRF cookie max age cannot be negative")
		}
	}
	
	// Validate throttling
	if c.EnableThrottling {
		if c.Throttle.RequestsPerMinute <= 0 {
			return fmt.Errorf("throttle requests per minute must be positive")
		}
		if c.Throttle.SuspiciousThreshold <= 0 {
			return fmt.Errorf("suspicious threshold must be positive")
		}
	}
	
	// Validate request limits
	if c.MaxRequestSize <= 0 {
		return fmt.Errorf("max request size must be positive")
	}
	
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("request timeout must be positive")
	}
	
	return nil
}

// GetSecuritySummary returns a summary of enabled security features
func (c *Config) GetSecuritySummary() map[string]interface{} {
	return map[string]interface{}{
		"environment":     c.Environment,
		"rate_limiting":   c.EnableRateLimit,
		"security_headers": c.EnableHeaders,
		"csrf_protection": c.EnableCSRF,
		"ip_throttling":   c.EnableThrottling,
		"cors_enabled":    c.EnableCORS,
		"ip_filtering":    c.EnableIPFilter,
		"api_key_required": c.APIKeyRequired,
		"max_request_size": c.MaxRequestSize,
		"request_timeout": c.RequestTimeout.String(),
		"features": map[string]interface{}{
			"progressive_throttling": c.Throttle.EnableProgressive,
			"suspicious_detection":   c.Throttle.EnableSuspiciousDetection,
			"subnet_limiting":       c.Throttle.EnableSubnetLimiting,
			"hsts_enabled":          c.Headers.HSTSMaxAge > 0,
			"csp_enabled":           c.Headers.ContentSecurityPolicy != "",
		},
	}
}