package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
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

// CSRFMiddleware creates CSRF protection middleware.
//
// Built on the double-submit implementation below rather than on nosurf, which
// this package used until v0.9.0. nosurf's design predates Sec-Fetch-Site and
// its same-origin check compared r.URL.Scheme -- empty on a server-side request,
// so the check never ran (CVE-2025-46721).
//
// The framework's own router does not use this. It uses the session-backed
// tokens in the root package, which need no masking because the token is never
// exposed to the client. This exists for applications wiring the security
// package up directly, where no session manager is available.
func CSRFMiddleware(config CSRFConfig, logger interface{}) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		issue := issueCSRFCookie(config)
		check := DoubleSubmitCSRFMiddleware(config)
		return issue(check(next))
	}
}

// issueCSRFCookie mints a token when the request has none and publishes it both
// as the cookie the double-submit check reads and as a response header.
//
// The header has to be set from inside the chain, not around it. Reading the
// token from a wrapper outside the CSRF middleware always yielded "", which
// left AJAX and SPA clients with no way to obtain one at all.
func issueCSRFCookie(config CSRFConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(config.CookieName)
			if err != nil || cookie.Value == "" {
				buf := make([]byte, config.TokenLength)
				if _, err := rand.Read(buf); err != nil {
					http.Error(w, "could not generate a CSRF token", http.StatusInternalServerError)
					return
				}
				token := base64.RawURLEncoding.EncodeToString(buf)

				http.SetCookie(w, &http.Cookie{
					Name:     config.CookieName,
					Value:    token,
					Path:     config.CookiePath,
					Domain:   config.CookieDomain,
					Secure:   config.CookieSecure,
					HttpOnly: config.CookieHttpOnly,
					SameSite: config.CookieSameSite,
					MaxAge:   config.CookieMaxAge,
				})
				// So the check below sees it on this same request rather than
				// only on the next one.
				r.AddCookie(&http.Cookie{Name: config.CookieName, Value: token})
				w.Header().Set(config.RequestHeader, token)
			} else {
				w.Header().Set(config.RequestHeader, cookie.Value)
			}

			next.ServeHTTP(w, r)
		})
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

// GetToken extracts the CSRF token from the request's cookie.
func (h *CSRFTokenHelper) GetToken(r *http.Request) string {
	cookie, err := r.Cookie(h.config.CookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// ValidateToken reports whether token matches the request's.
//
// Constant time: a comparison that returns early on the first differing byte
// leaks the token one byte at a time to anyone willing to measure.
func (h *CSRFTokenHelper) ValidateToken(r *http.Request, token string) bool {
	expected := h.GetToken(r)
	if expected == "" || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1
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

// EnhancedCSRFMiddleware adds referrer checking and a frame guard on top of
// CSRFMiddleware.
func EnhancedCSRFMiddleware(config CSRFConfig) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		protected := CSRFMiddleware(config, nil)(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				if !isValidReferrer(r) {
					logCSRFFailure(r)
					http.Error(w, "Invalid referrer", http.StatusForbidden)
					return
				}
			}

			if isSuspiciousRequest(r) {
				logCSRFFailure(r)
				http.Error(w, "Suspicious request detected", http.StatusForbidden)
				return
			}

			w.Header().Set("X-Frame-Options", "SAMEORIGIN")

			protected.ServeHTTP(w, r)
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

	// Deliberately no check on X-Forwarded-For plus X-Real-IP. Rejecting
	// requests that carry both used to 403 every POST behind the default nginx
	// reverse-proxy config, which sets exactly those two headers. The presence
	// of both carries no signal; if proxy trust matters, validate the peer
	// against a configured list and read the forwarded chain from the right
	// (see getClientIPWithTrustedProxies in utils.go).

	return false
}
