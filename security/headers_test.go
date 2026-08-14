package security

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	config := DefaultSecurityConfig()
	middleware := SecurityHeadersMiddleware(config)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	// Over TLS, because HSTS is only sent over a secure connection -- see
	// TestHSTSIsNotSentOverPlainHTTP.
	req := httptest.NewRequest("GET", "/test", nil)
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Check security headers
	assert.Equal(t, config.ContentSecurityPolicy, w.Header().Get("Content-Security-Policy"))
	assert.Contains(t, w.Header().Get("Strict-Transport-Security"), "max-age=31536000")
	assert.Equal(t, config.FrameOptions, w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "1; mode=block", w.Header().Get("X-XSS-Protection"))
	assert.Equal(t, config.ReferrerPolicy, w.Header().Get("Referrer-Policy"))
	assert.Equal(t, config.PermissionsPolicy, w.Header().Get("Permissions-Policy"))

	// Check server info is removed
	assert.Empty(t, w.Header().Get("X-Powered-By"))
	assert.Empty(t, w.Header().Get("Server"))
}

func TestProductionSecurityConfig(t *testing.T) {
	config := ProductionSecurityConfig()

	assert.Equal(t, 63072000, config.HSTSMaxAge) // 2 years
	assert.True(t, config.HSTSPreload)
	assert.Equal(t, "DENY", config.FrameOptions)
	assert.Contains(t, config.ContentSecurityPolicy, "frame-ancestors 'none'")
}

func TestDevelopmentSecurityConfig(t *testing.T) {
	config := DevelopmentSecurityConfig()

	assert.Equal(t, 0, config.HSTSMaxAge) // Disabled
	assert.Equal(t, "SAMEORIGIN", config.FrameOptions)
	assert.Contains(t, config.ContentSecurityPolicy, "unsafe-inline")
	assert.Contains(t, config.ContentSecurityPolicy, "unsafe-eval")
}

func TestHSTSHeader(t *testing.T) {
	tests := []struct {
		name     string
		config   SecurityConfig
		expected string
	}{
		{
			name: "basic HSTS",
			config: SecurityConfig{
				HSTSMaxAge: 3600,
			},
			expected: "max-age=3600",
		},
		{
			name: "HSTS with subdomains",
			config: SecurityConfig{
				HSTSMaxAge:            3600,
				HSTSIncludeSubdomains: true,
			},
			expected: "max-age=3600; includeSubDomains",
		},
		{
			name: "HSTS with preload",
			config: SecurityConfig{
				HSTSMaxAge:            3600,
				HSTSIncludeSubdomains: true,
				HSTSPreload:           true,
			},
			expected: "max-age=3600; includeSubDomains; preload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := SecurityHeadersMiddleware(tt.config)
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			req.TLS = &tls.ConnectionState{}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.expected, w.Header().Get("Strict-Transport-Security"))
		})
	}
}

func TestCORSMiddleware(t *testing.T) {
	config := SecurityConfig{
		AllowedOrigins:     []string{"https://example.com", "https://api.example.com"},
		AllowedMethods:     []string{"GET", "POST", "PUT"},
		AllowedHeaders:     []string{"Content-Type", "Authorization"},
		AllowedCredentials: true,
		MaxAge:             86400,
	}

	middleware := CORSMiddleware(config)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allowed origin", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "GET, POST, PUT", w.Header().Get("Access-Control-Allow-Methods"))
		assert.Equal(t, "Content-Type, Authorization", w.Header().Get("Access-Control-Allow-Headers"))
		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
		assert.Equal(t, "86400", w.Header().Get("Access-Control-Max-Age"))
	})

	t.Run("disallowed origin", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "https://malicious.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("preflight request", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
	})
}

func TestOriginMatching(t *testing.T) {
	tests := []struct {
		origin   string
		allowed  []string
		expected bool
	}{
		{"https://example.com", []string{"https://example.com"}, true},
		{"https://example.com", []string{"*"}, false}, // Wildcard "*" no longer supported for security
		{"https://sub.example.com", []string{"*.example.com"}, true},
		{"https://example.com", []string{"https://other.com"}, false},
		{"https://malicious.com", []string{"https://example.com"}, false},
	}

	for _, tt := range tests {
		result := isOriginAllowed(tt.origin, tt.allowed)
		assert.Equal(t, tt.expected, result, "Origin: %s, Allowed: %v", tt.origin, tt.allowed)
	}
}

func TestRequestSizeMiddleware(t *testing.T) {
	maxSize := int64(100) // 100 bytes
	middleware := RequestSizeMiddleware(maxSize)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("small request allowed", func(t *testing.T) {
		body := strings.NewReader("small body")
		req := httptest.NewRequest("POST", "/test", body)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("large request blocked", func(t *testing.T) {
		largeBody := strings.NewReader(strings.Repeat("x", 200)) // 200 bytes
		req := httptest.NewRequest("POST", "/test", largeBody)
		req.Header.Set("Content-Length", "200") // Set content length
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	})
}

func TestTimeoutMiddleware(t *testing.T) {
	timeout := 100 * time.Millisecond
	middleware := TimeoutMiddleware(timeout)

	t.Run("fast request succeeds", func(t *testing.T) {
		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(50 * time.Millisecond) // Less than timeout
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("slow request times out", func(t *testing.T) {
		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond) // More than timeout
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "Request timeout")
	})
}

func TestIPWhitelistMiddleware(t *testing.T) {
	allowedIPs := []string{"192.168.1.1", "10.0.0.0/8"}
	middleware := IPWhitelistMiddleware(allowedIPs)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allowed IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("blocked IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.2.1:1234"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestIPBlacklistMiddleware(t *testing.T) {
	blockedIPs := []string{"192.168.1.100", "10.0.0.1"}
	middleware := IPBlacklistMiddleware(blockedIPs)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allowed IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("blocked IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.100:1234"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestContentTypeMiddleware(t *testing.T) {
	allowedTypes := map[string][]string{
		"/api/json":   {"application/json"},
		"/api/upload": {"multipart/form-data", "application/octet-stream"},
		"/api/form":   {"application/x-www-form-urlencoded"},
	}

	middleware := ContentTypeMiddleware(allowedTypes)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allowed content type", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/json", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("disallowed content type", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/json", strings.NewReader("xml"))
		req.Header.Set("Content-Type", "application/xml")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
	})

	t.Run("unrestricted path", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/other", strings.NewReader("anything"))
		req.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("GET request not restricted", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/json", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestSecureMiddleware(t *testing.T) {
	config := DefaultSecurityConfig()
	middleware := SecureMiddleware(config)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should have security headers
	assert.NotEmpty(t, w.Header().Get("Content-Security-Policy"))
	assert.NotEmpty(t, w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
}

func TestCustomHeaders(t *testing.T) {
	config := DefaultSecurityConfig()
	config.CustomHeaders = map[string]string{
		"X-Custom-Header":  "custom-value",
		"X-Another-Header": "another-value",
	}

	middleware := SecurityHeadersMiddleware(config)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, "custom-value", w.Header().Get("X-Custom-Header"))
	assert.Equal(t, "another-value", w.Header().Get("X-Another-Header"))
}

func TestXSSProtectionVariations(t *testing.T) {
	tests := []struct {
		name     string
		config   SecurityConfig
		expected string
	}{
		{
			name:     "XSS protection disabled",
			config:   SecurityConfig{XSSProtection: false},
			expected: "",
		},
		{
			name:     "XSS protection enabled without mode",
			config:   SecurityConfig{XSSProtection: true},
			expected: "1",
		},
		{
			name:     "XSS protection with block mode",
			config:   SecurityConfig{XSSProtection: true, XSSProtectionMode: "block"},
			expected: "1; mode=block",
		},
		{
			name:     "XSS protection with report mode",
			config:   SecurityConfig{XSSProtection: true, XSSProtectionMode: "report=https://example.com/report"},
			expected: "1; mode=report=https://example.com/report",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := SecurityHeadersMiddleware(tt.config)
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if tt.expected == "" {
				assert.Empty(t, w.Header().Get("X-XSS-Protection"))
			} else {
				assert.Equal(t, tt.expected, w.Header().Get("X-XSS-Protection"))
			}
		})
	}
}

// TestIPMiddlewaresHandleIPv6 covers issue #17. The port was stripped twice --
// once in getClientIP, once in each middleware -- and both used
// strings.LastIndex(":"), which mangles IPv6 literals. "[2001:db8::1]:4444"
// became "[2001:db8:", so the blacklist matched nothing and failed open.
func TestIPMiddlewaresHandleIPv6(t *testing.T) {
	const v6 = "2001:db8::1"

	t.Run("blacklist blocks IPv6", func(t *testing.T) {
		handler := IPBlacklistMiddleware([]string{v6})(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "[" + v6 + "]:4444"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code, "blacklisted IPv6 client was let through")
	})

	t.Run("whitelist admits IPv6", func(t *testing.T) {
		handler := IPWhitelistMiddleware([]string{v6})(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "[" + v6 + "]:4444"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "whitelisted IPv6 client was rejected")
	})
}

// HSTS over plain HTTP is forbidden by RFC 6797 §7.2, and the reason is
// practical: a site that sends it before TLS works pins every visitor's browser
// to HTTPS for a year, client-side, and no server-side change undoes that.
//
// Turning HSTS on and turning TLS on are separate acts. This is what stops the
// first from breaking a site while the second is being arranged.
func TestHSTSIsNotSentOverPlainHTTP(t *testing.T) {
	config := SecurityConfig{HSTSMaxAge: 3600}
	handler := SecurityHeadersMiddleware(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	cases := []struct {
		name    string
		prepare func(*http.Request)
		want    string
	}{
		{"plain HTTP", func(*http.Request) {}, ""},
		{"over TLS", func(r *http.Request) { r.TLS = &tls.ConnectionState{} }, "max-age=3600"},
		{
			// The common deployment: TLS terminated at a proxy, cleartext to
			// this server. Refusing to trust the header would mean no HSTS for
			// most production sites, which is the wrong failure.
			"behind a proxy that terminated TLS",
			func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") },
			"max-age=3600",
		},
		{
			"behind a proxy on plain HTTP",
			func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "http") },
			"",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			c.prepare(req)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if got := w.Header().Get("Strict-Transport-Security"); got != c.want {
				t.Errorf("Strict-Transport-Security is %q, want %q", got, c.want)
			}
		})
	}
}
