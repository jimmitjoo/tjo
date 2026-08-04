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
