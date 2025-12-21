package security

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/justinas/nosurf"
)

// CSRFConfig holds CSRF protection configuration
type CSRFConfig struct {
	// Token length in bytes
	TokenLength int
	
	// Cookie settings
	CookieName     string
	CookiePath     string
	CookieDomain   string
	CookieSecure   bool
	CookieHttpOnly bool
	CookieSameSite http.SameSite
	CookieMaxAge   int
	
	// Request header name for CSRF token
	RequestHeader string
	
	// Form field name for CSRF token
	FormField string
	
	// Paths to exempt from CSRF protection
	ExemptPaths []string
	
	// Path patterns to exempt (supports wildcards)
	ExemptGlobs []string
	
	// Methods to exempt from CSRF protection
	ExemptMethods []string
	
	// Custom failure handler
	FailureHandler http.Handler
}

// DefaultCSRFConfig returns secure defaults
func DefaultCSRFConfig() CSRFConfig {
	return CSRFConfig{
		TokenLength:    32,
		CookieName:     "csrf_token",
		CookiePath:     "/",
		CookieSecure:   true,
		CookieHttpOnly: true,
		CookieSameSite: http.SameSiteStrictMode,
		CookieMaxAge:   3600, // 1 hour
		RequestHeader:  "X-CSRF-Token",
		FormField:      "csrf_token",
		ExemptPaths:    []string{"/health", "/metrics", "/health/ready", "/health/live"},
		ExemptGlobs:    []string{"/api/*", "/webhook/*"}, // Secure wildcard matching
		ExemptMethods:  []string{"GET", "HEAD", "OPTIONS"},
	}
}

// DevelopmentCSRFConfig returns more lenient settings for development
func DevelopmentCSRFConfig() CSRFConfig {
	config := DefaultCSRFConfig()
	config.CookieSecure = false
	config.CookieSameSite = http.SameSiteLaxMode
	return config
}

// CSRFMiddleware creates enhanced CSRF protection middleware
func CSRFMiddleware(config CSRFConfig, logger interface{}) func(next http.Handler) http.Handler {
	// Create the nosurf handler with our configuration
	return func(next http.Handler) http.Handler {
		csrfHandler := nosurf.New(next)
		
		// Configure the CSRF handler
		configureCSRFHandler(csrfHandler, config)
		
		// Wrap with additional security checks
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Add custom CSRF token to response headers for AJAX requests
			if token := nosurf.Token(r); token != "" {
				w.Header().Set("X-CSRF-Token", token)
			}
			
			csrfHandler.ServeHTTP(w, r)
		})
	}
}

// configureCSRFHandler applies configuration to nosurf handler
func configureCSRFHandler(handler *nosurf.CSRFHandler, config CSRFConfig) {
	// Set base cookie configuration
	handler.SetBaseCookie(http.Cookie{
		Name:     config.CookieName,
		Path:     config.CookiePath,
		Domain:   config.CookieDomain,
		Secure:   config.CookieSecure,
		HttpOnly: config.CookieHttpOnly,
		SameSite: config.CookieSameSite,
		MaxAge:   config.CookieMaxAge,
	})

	// Exempt specific paths
	for _, path := range config.ExemptPaths {
		handler.ExemptPath(path)
	}

	// Exempt path patterns
	for _, glob := range config.ExemptGlobs {
		handler.ExemptGlob(glob)
	}

	// Set custom failure handler if provided
	if config.FailureHandler != nil {
		handler.SetFailureHandler(config.FailureHandler)
	} else {
		// Default enhanced failure handler
		handler.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Log CSRF failure attempt
			logCSRFFailure(r)
			
			// Return appropriate response based on request type
			if isAJAXRequest(r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":"CSRF token mismatch","code":"CSRF_ERROR"}`))
			} else {
				http.Error(w, "CSRF token mismatch", http.StatusForbidden)
			}
		}))
	}
}

// isAJAXRequest determines if request is an AJAX/API request
func isAJAXRequest(r *http.Request) bool {
	return r.Header.Get("X-Requested-With") == "XMLHttpRequest" ||
		strings.Contains(r.Header.Get("Accept"), "application/json") ||
		strings.HasPrefix(r.URL.Path, "/api/")
}

// logCSRFFailure logs CSRF protection failures for monitoring
func logCSRFFailure(r *http.Request) {
	DefaultSecurityLogger.LogCSRFFailure(r, "CSRF token validation failed")
}

// DoubleSubmitCSRFMiddleware implements double submit cookie pattern
func DoubleSubmitCSRFMiddleware(config CSRFConfig) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip exempt methods
			for _, method := range config.ExemptMethods {
				if r.Method == method {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Skip exempt paths
			for _, path := range config.ExemptPaths {
				if r.URL.Path == path {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Skip exempt globs
			for _, glob := range config.ExemptGlobs {
				if matchGlob(glob, r.URL.Path) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Get CSRF token from cookie
			cookie, err := r.Cookie(config.CookieName)
			if err != nil {
				http.Error(w, "CSRF cookie missing", http.StatusForbidden)
				return
			}

			// Get CSRF token from header or form
			var headerToken string
			if headerToken = r.Header.Get(config.RequestHeader); headerToken == "" {
				headerToken = r.FormValue(config.FormField)
			}

			// Validate tokens match
			if headerToken == "" || headerToken != cookie.Value {
				logCSRFFailure(r)
				http.Error(w, "CSRF token mismatch", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// matchGlob performs secure glob pattern matching
func matchGlob(pattern, path string) bool {
	if pattern == path {
		return true
	}
	
	// Only support trailing wildcards for security
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		
		// Validate that prefix is not empty and doesn't contain suspicious patterns
		if prefix == "" {
			return false // Don't allow just "*"
		}
		
		// Prevent path traversal attempts
		if strings.Contains(prefix, "..") || strings.Contains(path, "..") {
			return false
		}
		
		// For directory-style matching, ensure we don't get false positives
		// "/api/*" should match "/api/users" but not "/apikey"
		if !strings.HasSuffix(prefix, "/") {
			// If no trailing slash, require exact prefix match with a following slash or end
			if !strings.HasPrefix(path, prefix+"/") && path != prefix {
				return false
			}
		}
		
		return strings.HasPrefix(path, prefix)
	}
	
	// No wildcard matching for other patterns for security
	return false
}

// CSRFTokenHelper provides utility functions for CSRF tokens
type CSRFTokenHelper struct {
	config CSRFConfig
}

// NewCSRFTokenHelper creates a new CSRF token helper
func NewCSRFTokenHelper(config CSRFConfig) *CSRFTokenHelper {
	return &CSRFTokenHelper{config: config}
}

// GetToken extracts CSRF token from request
func (h *CSRFTokenHelper) GetToken(r *http.Request) string {
	return nosurf.Token(r)
}

// ValidateToken validates a CSRF token against the request
func (h *CSRFTokenHelper) ValidateToken(r *http.Request, token string) bool {
	return nosurf.VerifyToken(nosurf.Token(r), token)
}

// SetTokenCookie sets CSRF token cookie on response
func (h *CSRFTokenHelper) SetTokenCookie(w http.ResponseWriter, token string) {
	cookie := &http.Cookie{
		Name:     h.config.CookieName,
		Value:    token,
		Path:     h.config.CookiePath,
		Domain:   h.config.CookieDomain,
		Secure:   h.config.CookieSecure,
		HttpOnly: h.config.CookieHttpOnly,
		SameSite: h.config.CookieSameSite,
		MaxAge:   h.config.CookieMaxAge,
	}
	http.SetCookie(w, cookie)
}

// Enhanced CSRF protection with additional security measures
func EnhancedCSRFMiddleware(config CSRFConfig) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// Create base CSRF handler
		csrfHandler := nosurf.New(next)
		configureCSRFHandler(csrfHandler, config)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Additional security checks
			
			// 1. Check referrer header for additional protection
			if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" || r.Method == "DELETE" {
				if !isValidReferrer(r) {
					logCSRFFailure(r)
					http.Error(w, "Invalid referrer", http.StatusForbidden)
					return
				}
			}

			// 2. Check for suspicious patterns
			if isSuspiciousRequest(r) {
				logCSRFFailure(r)
				http.Error(w, "Suspicious request detected", http.StatusForbidden)
				return
			}

			// Add CSRF token to response headers
			if token := nosurf.Token(r); token != "" {
				w.Header().Set("X-CSRF-Token", token)
				w.Header().Set("X-Frame-Options", "SAMEORIGIN") // Additional protection
			}

			csrfHandler.ServeHTTP(w, r)
		})
	}
}

// isValidReferrer checks if the referrer header is valid.
// Uses proper URL parsing to prevent bypass attacks.
func isValidReferrer(r *http.Request) bool {
	referrer := r.Header.Get("Referer")
	if referrer == "" {
		return false // Require referrer for state-changing operations
	}

	// Parse the referrer URL properly
	refURL, err := url.Parse(referrer)
	if err != nil {
		return false
	}

	// Get the expected host
	expectedHost := r.Host
	if expectedHost == "" {
		expectedHost = r.Header.Get("Host")
	}

	// Compare hosts exactly (not as substring!)
	refHost := refURL.Host

	// Handle port normalization - if expected has no port, strip port from referrer
	if !strings.Contains(expectedHost, ":") {
		refHost = strings.Split(refHost, ":")[0]
	}

	return refHost == expectedHost
}

// isSuspiciousRequest detects potentially malicious patterns
func isSuspiciousRequest(r *http.Request) bool {
	userAgent := strings.ToLower(r.UserAgent())
	
	// Check for suspicious user agents
	suspiciousAgents := []string{"bot", "crawler", "spider", "scraper"}
	for _, agent := range suspiciousAgents {
		if strings.Contains(userAgent, agent) {
			return true
		}
	}

	// Check for suspicious headers
	if r.Header.Get("X-Forwarded-For") != "" && r.Header.Get("X-Real-IP") != "" {
		// Multiple IP forwarding headers might indicate proxy manipulation
		return true
	}

	return false
}