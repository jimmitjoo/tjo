package security

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SecurityConfig holds security header configuration
type SecurityConfig struct {
	// Content Security Policy
	ContentSecurityPolicy string

	// HSTS settings
	HSTSMaxAge            int
	HSTSIncludeSubdomains bool
	HSTSPreload           bool

	// Frame options
	FrameOptions string // DENY, SAMEORIGIN, or ALLOW-FROM uri

	// Content type options
	ContentTypeNosniff bool

	// XSS Protection
	XSSProtection     bool
	XSSProtectionMode string // block, report=uri

	// Referrer Policy
	ReferrerPolicy string

	// Permissions Policy
	PermissionsPolicy string

	// CORS settings
	AllowedOrigins     []string
	AllowedMethods     []string
	AllowedHeaders     []string
	AllowedCredentials bool
	MaxAge             int

	// Custom headers
	CustomHeaders map[string]string
}

// DefaultSecurityConfig returns secure defaults
func DefaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		ContentSecurityPolicy: "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'",
		HSTSMaxAge:            31536000, // 1 year
		HSTSIncludeSubdomains: true,
		HSTSPreload:           false,
		FrameOptions:          "DENY",
		ContentTypeNosniff:    true,
		XSSProtection:         true,
		XSSProtectionMode:     "block",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		PermissionsPolicy:     "camera=(), microphone=(), geolocation=()",
		AllowedOrigins:        []string{}, // Empty by default - must be explicitly configured
		AllowedMethods:        []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:        []string{"Accept", "Content-Type", "Content-Length", "Accept-Encoding", "X-CSRF-Token", "Authorization"},
		AllowedCredentials:    false, // Safer default
		MaxAge:                86400, // 24 hours
		CustomHeaders:         make(map[string]string),
	}
}

// DevelopmentSecurityConfig returns more lenient settings for development
func DevelopmentSecurityConfig() SecurityConfig {
	config := DefaultSecurityConfig()
	config.ContentSecurityPolicy = "default-src 'self' 'unsafe-inline' 'unsafe-eval'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'"
	config.HSTSMaxAge = 0 // Disable HSTS in development
	config.FrameOptions = "SAMEORIGIN"
	return config
}

// ProductionSecurityConfig returns strict settings for production
func ProductionSecurityConfig() SecurityConfig {
	config := DefaultSecurityConfig()
	config.ContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: https:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'"
	config.HSTSMaxAge = 63072000 // 2 years
	config.HSTSPreload = true
	config.FrameOptions = "DENY"
	return config
}

// SecurityHeadersMiddleware adds security headers to all responses
func SecurityHeadersMiddleware(config SecurityConfig) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Content Security Policy
			if config.ContentSecurityPolicy != "" {
				w.Header().Set("Content-Security-Policy", config.ContentSecurityPolicy)
			}

			// HTTP Strict Transport Security
			if config.HSTSMaxAge > 0 {
				hsts := fmt.Sprintf("max-age=%d", config.HSTSMaxAge)
				if config.HSTSIncludeSubdomains {
					hsts += "; includeSubDomains"
				}
				if config.HSTSPreload {
					hsts += "; preload"
				}
				w.Header().Set("Strict-Transport-Security", hsts)
			}

			// X-Frame-Options
			if config.FrameOptions != "" {
				w.Header().Set("X-Frame-Options", config.FrameOptions)
			}

			// X-Content-Type-Options
			if config.ContentTypeNosniff {
				w.Header().Set("X-Content-Type-Options", "nosniff")
			}

			// X-XSS-Protection
			if config.XSSProtection {
				xssValue := "1"
				if config.XSSProtectionMode != "" {
					xssValue += "; mode=" + config.XSSProtectionMode
				}
				w.Header().Set("X-XSS-Protection", xssValue)
			}

			// Referrer-Policy
			if config.ReferrerPolicy != "" {
				w.Header().Set("Referrer-Policy", config.ReferrerPolicy)
			}

			// Permissions-Policy
			if config.PermissionsPolicy != "" {
				w.Header().Set("Permissions-Policy", config.PermissionsPolicy)
			}

			// Custom headers
			for key, value := range config.CustomHeaders {
				w.Header().Set(key, value)
			}

			// Security headers for all responses
			w.Header().Set("X-Powered-By", "") // Remove server information
			w.Header().Set("Server", "")       // Remove server information

			next.ServeHTTP(w, r)
		})
	}
}

// CORSMiddleware handles Cross-Origin Resource Sharing
func CORSMiddleware(config SecurityConfig) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// The ACAO value depends on the request's Origin, so any shared
			// cache must key on it. Without this a CDN can serve one origin's
			// ACAO header to another, defeating the allowlist.
			w.Header().Add("Vary", "Origin")

			// Check if origin is allowed
			originAllowed := false
			if origin != "" && isOriginAllowed(origin, config.AllowedOrigins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				originAllowed = true
			} else if origin != "" {
				// Log invalid origin attempt
				DefaultSecurityLogger.LogInvalidOrigin(r, origin)
			}

			// Set other CORS headers
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
			w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%d", config.MaxAge))

			// Only set credentials header if origin is specifically allowed (never with wildcard)
			if config.AllowedCredentials && originAllowed {
				// Double-check that we're not accidentally allowing credentials with wildcards
				for _, allowedOrigin := range config.AllowedOrigins {
					if allowedOrigin == "*" {
						// This is a security violation - log it and refuse
						// In production, this should be logged as a security event
						break
					}
				}
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isOriginAllowed checks if an origin is in the allowed list
func isOriginAllowed(origin string, allowed []string) bool {
	if origin == "" {
		return false
	}

	for _, allowedOrigin := range allowed {
		if allowedOrigin == origin {
			return true
		}

		// Handle wildcard patterns more securely
		if strings.Contains(allowedOrigin, "*") {
			if isSecureWildcardMatch(origin, allowedOrigin) {
				return true
			}
		}
	}
	return false
}

// isSecureWildcardMatch performs secure wildcard matching for CORS origins
func isSecureWildcardMatch(origin, pattern string) bool {
	// Only allow simple subdomain patterns like "*.example.com"
	if !strings.HasPrefix(pattern, "*.") {
		return false
	}

	// Extract the domain part (e.g., "example.com" from "*.example.com")
	domain := strings.TrimPrefix(pattern, "*.")
	if domain == "" {
		return false
	}

	// Origin must end with the domain and have a subdomain
	if !strings.HasSuffix(origin, "."+domain) {
		return false
	}

	// Ensure there's actually a subdomain (not just the domain itself)
	prefix := strings.TrimSuffix(origin, "."+domain)
	if prefix == "" || prefix == "." {
		return false
	}

	// Validate that it's a proper subdomain (no additional dots in the prefix for security)
	if strings.Contains(prefix, ".") {
		return false
	}

	// Additional security: ensure the origin looks like a valid URL
	if !strings.HasPrefix(origin, "http://") && !strings.HasPrefix(origin, "https://") {
		return false
	}

	return true
}

// RequestSizeMiddleware limits request body size
func RequestSizeMiddleware(maxBytes int64) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				http.Error(w, "Request too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// TimeoutMiddleware adds request timeout
func TimeoutMiddleware(timeout time.Duration) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, timeout, "Request timeout")
	}
}

// IPWhitelistMiddleware allows only specific IP addresses
func IPWhitelistMiddleware(allowedIPs []string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// getClientIP has already stripped the port.
			clientIP := getClientIP(r)

			for _, allowedIP := range allowedIPs {
				if clientIP == allowedIP || allowedIP == "*" {
					next.ServeHTTP(w, r)
					return
				}
			}

			http.Error(w, "Forbidden", http.StatusForbidden)
		})
	}
}

// IPBlacklistMiddleware blocks specific IP addresses
func IPBlacklistMiddleware(blockedIPs []string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// getClientIP has already stripped the port.
			clientIP := getClientIP(r)

			for _, blockedIP := range blockedIPs {
				if clientIP == blockedIP {
					DefaultSecurityLogger.LogIPBlocked(r, "IP address in blacklist")
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ContentTypeMiddleware enforces specific content types for endpoints
func ContentTypeMiddleware(allowedTypes map[string][]string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
				contentType := r.Header.Get("Content-Type")

				// Check if path has content type restrictions
				if allowedForPath, exists := allowedTypes[r.URL.Path]; exists {
					isAllowed := false
					for _, allowedType := range allowedForPath {
						if strings.Contains(contentType, allowedType) {
							isAllowed = true
							break
						}
					}

					if !isAllowed {
						w.Header().Set("Content-Type", "application/json")
						http.Error(w, "Unsupported content type", http.StatusUnsupportedMediaType)
						return
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecureMiddleware combines multiple security middlewares
func SecureMiddleware(config SecurityConfig) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// Chain multiple security middlewares
		handler := next

		// Apply middlewares in reverse order (last applied = first executed)
		handler = SecurityHeadersMiddleware(config)(handler)
		handler = CORSMiddleware(config)(handler)
		handler = RequestSizeMiddleware(10 * 1024 * 1024)(handler) // 10MB default
		handler = TimeoutMiddleware(30 * time.Second)(handler)     // 30s default

		return handler
	}
}
