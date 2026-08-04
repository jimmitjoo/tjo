package security

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCSRFMiddlewarePublishesToken covers issue #18. The X-CSRF-Token response
// header was set from a wrapper around nosurf, but nosurf installs the token
// into a new request value inside its own ServeHTTP -- so nosurf.Token always
// returned "" there and the header was always empty. AJAX and SPA clients had
// no way to obtain a token, which is why /api/* ended up exempted instead.
func TestCSRFMiddlewarePublishesToken(t *testing.T) {
	handler := CSRFMiddleware(DevelopmentCSRFConfig(), nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

	req := httptest.NewRequest("GET", "/page", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-CSRF-Token"),
		"no token published; an AJAX client cannot obtain one")
	assert.NotEmpty(t, w.Result().Cookies(), "no CSRF cookie set")
}

// TestSuspiciousRequestAllowsStandardProxyHeaders covers the other half:
// isSuspiciousRequest rejected any request carrying both X-Forwarded-For and
// X-Real-IP, which is exactly what the default nginx reverse-proxy config
// sets. Every POST behind nginx got 403 Suspicious request detected.
func TestSuspiciousRequestAllowsStandardProxyHeaders(t *testing.T) {
	req := httptest.NewRequest("POST", "/submit", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	req.Header.Set("X-Real-IP", "203.0.113.7")

	assert.False(t, isSuspiciousRequest(req),
		"a standard nginx proxy setup must not be treated as an attack")
}

// TestMatchGlob pins the pattern matching that decides which routes are exempt
// from CSRF. Over-matching here silently removes protection from routes nobody
// meant to exempt, which is why only trailing wildcards are supported.
func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"/api/users", "/api/users", true},
		{"/api/users", "/api/other", false},

		{"/api/*", "/api/users", true},
		{"/api/*", "/api/v1/users", true},
		{"/api/*", "/api/", true},

		// The classic prefix bug: /api* must not match /apikey.
		{"/api*", "/apikey", false},
		{"/api*", "/api/users", true},
		{"/api*", "/api", true},

		// A bare wildcard would exempt everything.
		{"*", "/anything", false},

		// Traversal in either side is refused outright.
		{"/api/*", "/api/../admin", false},
		{"/../*", "/anything", false},

		// Leading and infix wildcards are deliberately unsupported.
		{"*/users", "/api/users", false},
		{"/api/*/users", "/api/v1/users", false},

		{"", "", true},
		{"/api/*", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+" vs "+tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, matchGlob(tt.pattern, tt.path))
		})
	}
}

// TestIsValidReferrer covers the referrer check used by
// EnhancedCSRFMiddleware. Substring comparison here would let
// example.com.evil.test through.
func TestIsValidReferrer(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		referrer string
		want     bool
	}{
		{"same origin", "example.com", "https://example.com/page", true},
		{"same origin with path and query", "example.com", "https://example.com/a/b?c=1", true},
		{"different host", "example.com", "https://evil.test/page", false},
		{"suffix attack", "example.com", "https://example.com.evil.test/page", false},
		{"prefix attack", "example.com", "https://notexample.com/page", false},
		{"missing referrer", "example.com", "", false},
		{"referrer port stripped when host has none", "example.com", "https://example.com:8443/page", true},
		{"host with port matches exactly", "example.com:8080", "https://example.com:8080/x", true},
		{"host with port rejects other port", "example.com:8080", "https://example.com:9999/x", false},
		{"garbage referrer", "example.com", "://not a url", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("POST", "/submit", nil)
			r.Host = tt.host
			if tt.referrer != "" {
				r.Header.Set("Referer", tt.referrer)
			}
			assert.Equal(t, tt.want, isValidReferrer(r))
		})
	}
}
