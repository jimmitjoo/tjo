package security

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIPThrottler(t *testing.T) {
	config := DefaultThrottleConfig()
	config.RequestsPerMinute = 10
	config.BurstSize = 2
	
	throttler := NewIPThrottler(config)
	
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	
	// Test initial burst allowance
	allowed, reason := throttler.Allow(req)
	assert.True(t, allowed)
	assert.Equal(t, "Request allowed", reason)
	
	allowed, reason = throttler.Allow(req)
	assert.True(t, allowed)
	assert.Equal(t, "Request allowed", reason)
	
	// Third request should be blocked (burst exhausted)
	allowed, reason = throttler.Allow(req)
	assert.False(t, allowed)
	assert.Equal(t, "Rate limit exceeded", reason)
}

func TestThrottlerWithWhitelist(t *testing.T) {
	config := DefaultThrottleConfig()
	config.WhitelistedIPs = []string{"192.168.1.100"}
	
	throttler := NewIPThrottler(config)
	
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:1234"
	
	// Whitelisted IP should always be allowed
	for i := 0; i < 100; i++ {
		allowed, reason := throttler.Allow(req)
		assert.True(t, allowed, "Request %d should be allowed", i+1)
		assert.Equal(t, "IP whitelisted", reason)
	}
}

func TestThrottlerWithBlacklist(t *testing.T) {
	config := DefaultThrottleConfig()
	config.BlacklistedIPs = []string{"192.168.1.200"}
	
	throttler := NewIPThrottler(config)
	
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.200:1234"
	
	allowed, reason := throttler.Allow(req)
	assert.False(t, allowed)
	assert.Equal(t, "IP blacklisted", reason)
}

func TestRecordFailure(t *testing.T) {
	config := DefaultThrottleConfig()
	config.EnableProgressive = true
	config.EnableSuspiciousDetection = true
	config.SuspiciousThreshold = 3
	
	throttler := NewIPThrottler(config)
	
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	
	// Record several failures
	for i := 0; i < 5; i++ {
		throttler.RecordFailure(req, 500) // Server error
	}
	
	// Get IP stats to verify failures were recorded
	stats := throttler.getIPStats("192.168.1.1")
	assert.Equal(t, int64(5), stats.failedRequests)
}

func TestProgressivePenalty(t *testing.T) {
	config := DefaultThrottleConfig()
	config.EnableProgressive = true
	config.MaxPenaltyMinutes = 10
	
	throttler := NewIPThrottler(config)
	
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	
	// Record many failures to trigger penalty
	for i := 0; i < 20; i++ {
		throttler.RecordFailure(req, 429)
	}
	
	stats := throttler.getIPStats("192.168.1.1")
	assert.True(t, time.Now().Before(stats.penaltyUntil), "Should be under penalty")
	
	// Request should be blocked due to penalty
	allowed, reason := throttler.Allow(req)
	assert.False(t, allowed)
	assert.Equal(t, "IP under penalty", reason)
}

func TestSuspiciousBehaviorDetection(t *testing.T) {
	config := DefaultThrottleConfig()
	config.EnableSuspiciousDetection = true
	config.SuspiciousThreshold = 2
	config.SuspiciousPenaltyMinutes = 5
	
	throttler := NewIPThrottler(config)
	
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	
	// Record failures that exceed suspicious threshold
	for i := 0; i < 3; i++ {
		throttler.RecordFailure(req, 404)
	}
	
	stats := throttler.getIPStats("192.168.1.1")
	assert.True(t, time.Now().Before(stats.penaltyUntil), "Should be under penalty for suspicious behavior")
}

func TestSubnetLimiting(t *testing.T) {
	config := DefaultThrottleConfig()
	config.EnableSubnetLimiting = true
	config.SubnetRequestsPerMinute = 5
	config.SubnetMask = 24
	
	throttler := NewIPThrottler(config)
	
	// Make requests from different IPs in the same subnet
	ips := []string{
		"192.168.1.1:1234",
		"192.168.1.2:1234",
		"192.168.1.3:1234",
	}
	
	requestCount := 0
	for i := 0; i < 10; i++ {
		for _, ip := range ips {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = ip
			
			allowed, _ := throttler.Allow(req)
			if allowed {
				requestCount++
			}
		}
	}
	
	// Should be limited by subnet quota
	assert.True(t, requestCount <= 15, "Subnet limiting should prevent too many requests")
}

func TestGetRealIP(t *testing.T) {
	config := DefaultThrottleConfig()
	config.TrustedProxyHeaders = []string{"X-Forwarded-For", "X-Real-IP"}
	config.TrustedProxies = []string{"127.0.0.1", "10.0.0.1"}
	
	throttler := NewIPThrottler(config)
	
	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		expected   string
	}{
		{
			name:       "direct connection",
			remoteAddr: "192.168.1.1:1234",
			headers:    map[string]string{},
			expected:   "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For single IP",
			remoteAddr: "127.0.0.1:1234",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1"},
			expected:   "203.0.113.1",
		},
		{
			name:       "X-Forwarded-For multiple IPs",
			remoteAddr: "127.0.0.1:1234",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1, 198.51.100.1"},
			expected:   "203.0.113.1",
		},
		{
			name:       "X-Real-IP",
			remoteAddr: "127.0.0.1:1234",
			headers:    map[string]string{"X-Real-IP": "203.0.113.1"},
			expected:   "203.0.113.1",
		},
		{
			name:       "untrusted peer cannot spoof X-Forwarded-For",
			remoteAddr: "203.0.113.9:5555",
			headers:    map[string]string{"X-Forwarded-For": "10.1.1.1"},
			expected:   "203.0.113.9",
		},
		{
			name:       "untrusted peer cannot spoof X-Real-IP",
			remoteAddr: "203.0.113.9:5555",
			headers:    map[string]string{"X-Real-IP": "10.1.1.1"},
			expected:   "203.0.113.9",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			
			ip := throttler.getRealIP(req)
			assert.Equal(t, tt.expected, ip)
		})
	}
}

func TestIPThrottleMiddleware(t *testing.T) {
	config := DefaultThrottleConfig()
	config.RequestsPerMinute = 5
	config.BurstSize = 2
	
	middleware := IPThrottleMiddleware(config)
	
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	
	t.Run("allows requests within limit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()
		
		handler.ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "OK", w.Body.String())
	})
	
	t.Run("blocks requests over limit", func(t *testing.T) {
		// Exhaust the limit
		for i := 0; i < 3; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "192.168.1.2:1234"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
		
		// This should be blocked
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.2:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Contains(t, w.Body.String(), "Request throttled")
		assert.Equal(t, "5", w.Header().Get("X-RateLimit-Limit"))
		assert.Equal(t, "0", w.Header().Get("X-RateLimit-Remaining"))
		assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"))
		assert.Equal(t, "60", w.Header().Get("Retry-After"))
	})
	
	t.Run("records failures", func(t *testing.T) {
		// Handler that returns error
		errorHandler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.3:1234"
		w := httptest.NewRecorder()
		
		errorHandler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestGetSubnet(t *testing.T) {
	config := DefaultThrottleConfig()
	throttler := NewIPThrottler(config)
	
	tests := []struct {
		ip       string
		mask     int
		expected string
	}{
		{"192.168.1.100", 24, "192.168.1.0/24"},
		{"10.0.5.10", 16, "10.0.0.0/16"},
		{"172.16.0.1", 24, "172.16.0.0/24"},
	}
	
	for _, tt := range tests {
		result := throttler.getSubnet(tt.ip, tt.mask)
		assert.Equal(t, tt.expected, result, "IP: %s, Mask: %d", tt.ip, tt.mask)
	}
}

func TestGetStats(t *testing.T) {
	config := DefaultThrottleConfig()
	throttler := NewIPThrottler(config)
	
	// Add some test data
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.1:1234"
	throttler.Allow(req1)
	
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.2:1234"
	throttler.Allow(req2)
	throttler.RecordFailure(req2, 500)
	
	stats := throttler.GetStats()
	
	assert.Equal(t, 2, stats["total_ips"])
	assert.GreaterOrEqual(t, stats["total_subnets"].(int), 0)
	assert.GreaterOrEqual(t, stats["penalized_ips"].(int), 0)
	assert.GreaterOrEqual(t, stats["blacklisted_ips"].(int), 0)
}

func TestGetTopThrottledIPs(t *testing.T) {
	config := DefaultThrottleConfig()
	throttler := NewIPThrottler(config)
	
	// Create some test data with different failure rates
	ips := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
	
	for i, ip := range ips {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = ip + ":1234"
		
		// Allow some requests
		for j := 0; j < 10; j++ {
			throttler.Allow(req)
		}
		
		// Record different amounts of failures
		for j := 0; j < i*3; j++ {
			throttler.RecordFailure(req, 500)
		}
	}
	
	topIPs := throttler.GetTopThrottledIPs(2)
	
	assert.LessOrEqual(t, len(topIPs), 2)
	
	if len(topIPs) > 1 {
		// Should be sorted by failure rate descending
		assert.True(t, topIPs[0]["failure_rate"].(float64) >= topIPs[1]["failure_rate"].(float64))
	}
}

func TestConcurrentThrottling(t *testing.T) {
	config := DefaultThrottleConfig()
	config.RequestsPerMinute = 100
	config.BurstSize = 50
	
	throttler := NewIPThrottler(config)
	
	// Test concurrent access from multiple goroutines
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func(id int) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = fmt.Sprintf("192.168.1.%d:1234", id)
			
			for j := 0; j < 20; j++ {
				throttler.Allow(req)
				if j%5 == 0 {
					throttler.RecordFailure(req, 500)
				}
			}
			done <- true
		}(i)
	}
	
	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
	
	stats := throttler.GetStats()
	assert.Equal(t, 10, stats["total_ips"])
}

func TestResponseWriter(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: w, status: 200}
	
	// Test default status
	assert.Equal(t, 200, rw.status)
	
	// Test setting status
	rw.WriteHeader(404)
	assert.Equal(t, 404, rw.status)
	assert.Equal(t, 404, w.Code)
}

func TestThrottlerCleanup(t *testing.T) {
	config := DefaultThrottleConfig()
	throttler := NewIPThrottler(config)
	
	// Add some test entries
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	throttler.Allow(req)
	
	// Verify entry exists
	throttler.mu.RLock()
	initialCount := len(throttler.ipStats)
	throttler.mu.RUnlock()
	
	assert.Equal(t, 1, initialCount)
	
	// Note: In a real test, we'd need to manipulate timestamps or wait for cleanup
	// For now, we're just verifying the structure is correct
}

func TestTrustedProxyHandling(t *testing.T) {
	config := DefaultThrottleConfig()
	config.TrustedProxies = []string{"127.0.0.1", "10.0.0.0/8"}
	
	throttler := NewIPThrottler(config)
	
	tests := []struct {
		ip       string
		expected bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"192.168.1.1", false},
		{"8.8.8.8", false},
	}
	
	for _, tt := range tests {
		result := throttler.isTrustedProxy(tt.ip)
		assert.Equal(t, tt.expected, result, "IP: %s", tt.ip)
	}
}
// TestThrottleNotBypassableByHeaderSpoofing guards the throttle against a
// regression where getRealIP trusted proxy headers without checking that the
// peer was a proxy at all. Rotating a forged X-Forwarded-For minted a fresh
// bucket per request, defeating the limit entirely. See GHSA-hm83-wmj9-52fm.
func TestThrottleNotBypassableByHeaderSpoofing(t *testing.T) {
	const attempts = 50

	countAllowed := func(spoof bool) int {
		config := DefaultThrottleConfig()
		config.RequestsPerMinute = 1
		config.BurstSize = 5
		throttler := NewIPThrottler(config)

		allowed := 0
		for i := 0; i < attempts; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "203.0.113.9:5555" // same attacker every time
			if spoof {
				req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.1.%d.%d", i/256, i%256))
			}
			if ok, _ := throttler.Allow(req); ok {
				allowed++
			}
		}
		return allowed
	}

	spoofed := countAllowed(true)
	direct := countAllowed(false)

	assert.Equal(t, direct, spoofed,
		"spoofed proxy headers must not buy the attacker extra requests")
	assert.LessOrEqual(t, spoofed, 5,
		"attacker should be capped at BurstSize, got %d of %d", spoofed, attempts)
}
