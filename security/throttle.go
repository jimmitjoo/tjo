package security

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ThrottleConfig holds IP-based throttling configuration
type ThrottleConfig struct {
	// Basic rate limiting
	RequestsPerMinute int
	BurstSize         int
	WindowSize        time.Duration

	// Progressive penalties
	EnableProgressive bool
	MaxPenaltyMinutes int

	// Suspicious behavior detection
	EnableSuspiciousDetection bool
	SuspiciousThreshold       int // Failed requests in window
	SuspiciousPenaltyMinutes  int

	// Subnet-based limiting
	EnableSubnetLimiting    bool
	SubnetRequestsPerMinute int
	SubnetMask              int // /24, /16, etc.

	// Whitelist/Blacklist
	WhitelistedIPs []string
	BlacklistedIPs []string

	// Custom headers to check for real IP
	TrustedProxyHeaders []string
	TrustedProxies      []string
}

// DefaultThrottleConfig returns sensible defaults
func DefaultThrottleConfig() ThrottleConfig {
	return ThrottleConfig{
		RequestsPerMinute:         100,
		BurstSize:                 20,
		WindowSize:                time.Minute,
		EnableProgressive:         true,
		MaxPenaltyMinutes:         60,
		EnableSuspiciousDetection: true,
		SuspiciousThreshold:       10,
		SuspiciousPenaltyMinutes:  15,
		EnableSubnetLimiting:      true,
		SubnetRequestsPerMinute:   500,
		SubnetMask:                24,
		TrustedProxyHeaders:       []string{"X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP"},
		TrustedProxies:            []string{"127.0.0.1", "::1"},
	}
}

// IPThrottler manages IP-based request throttling
type IPThrottler struct {
	config      ThrottleConfig
	ipStats     map[string]*ipStatistics
	subnetStats map[string]*subnetStatistics
	mu          sync.RWMutex

	// stop ends the cleanup goroutine. Without it every throttler leaked a
	// goroutine and a ticker for the lifetime of the process.
	stop     chan struct{}
	stopOnce sync.Once
}

// maxTrackedIPs caps ipStats. Entries are created per unique client address,
// so without a ceiling a botnet grows the map until the process runs out of
// memory. The cleanup sweep is periodic and cannot bound size on its own.
const maxTrackedIPs = 100000

// ipStatistics tracks statistics for individual IP addresses
type ipStatistics struct {
	tokens         float64
	lastUpdate     time.Time
	totalRequests  int64
	failedRequests int64
	lastFailure    time.Time
	penaltyUntil   time.Time
	// blacklistedUntil replaces a bool plus a goroutine that slept for the
	// penalty and flipped it back. That spawned one uncancellable goroutine
	// per blacklisting -- at a rate the attacker controls, parked for up to
	// two hours each.
	blacklistedUntil time.Time
	mu               sync.Mutex
}

// isBlacklistedNow reports whether the penalty is still in force. Caller must
// hold stats.mu.
func (s *ipStatistics) isBlacklistedNow(now time.Time) bool {
	return now.Before(s.blacklistedUntil)
}

// subnetStatistics tracks statistics for IP subnets
type subnetStatistics struct {
	tokens     float64
	lastUpdate time.Time
	ipCount    int
	mu         sync.Mutex
}

// NewIPThrottler creates a new IP throttler
func NewIPThrottler(config ThrottleConfig) *IPThrottler {
	throttler := &IPThrottler{
		config:      config,
		ipStats:     make(map[string]*ipStatistics),
		subnetStats: make(map[string]*subnetStatistics),
		stop:        make(chan struct{}),
	}

	// Start cleanup goroutine
	go throttler.cleanup()

	return throttler
}

// Allow checks if a request from an IP should be allowed
func (t *IPThrottler) Allow(r *http.Request) (bool, string) {
	clientIP := t.getRealIP(r)

	// Check blacklist first
	if t.isBlacklisted(clientIP) {
		return false, "IP blacklisted"
	}

	// Check whitelist
	if t.isWhitelisted(clientIP) {
		return true, "IP whitelisted"
	}

	// Get or create IP statistics
	stats := t.getIPStats(clientIP)

	stats.mu.Lock()
	defer stats.mu.Unlock()

	now := time.Now()

	// Check if IP is under penalty
	if now.Before(stats.penaltyUntil) {
		return false, "IP under penalty"
	}

	// Check if IP is temporarily blacklisted
	if stats.isBlacklistedNow(now) {
		return false, "IP temporarily blacklisted"
	}

	// Update token bucket
	timePassed := now.Sub(stats.lastUpdate)
	stats.lastUpdate = now

	// Calculate current rate limit (may be reduced due to penalties)
	currentLimit := t.getCurrentLimit(stats)

	// Add tokens based on time passed
	tokensToAdd := timePassed.Seconds() * (float64(currentLimit) / 60.0)
	stats.tokens += tokensToAdd

	// Cap at burst size
	burstSize := float64(t.config.BurstSize)
	if stats.tokens > burstSize {
		stats.tokens = burstSize
	}

	// Check subnet limiting if enabled
	if t.config.EnableSubnetLimiting {
		if !t.allowSubnet(clientIP) {
			return false, "Subnet rate limit exceeded"
		}
	}

	// Check if request can be allowed
	if stats.tokens >= 1.0 {
		stats.tokens -= 1.0
		stats.totalRequests++
		return true, "Request allowed"
	}

	return false, "Rate limit exceeded"
}

// RecordFailure records a failed request for an IP
func (t *IPThrottler) RecordFailure(r *http.Request, statusCode int) {
	if statusCode < 400 {
		return // Not a failure
	}

	clientIP := t.getRealIP(r)
	stats := t.getIPStats(clientIP)

	stats.mu.Lock()
	defer stats.mu.Unlock()

	stats.failedRequests++
	stats.lastFailure = time.Now()

	// Apply progressive penalties if enabled
	if t.config.EnableProgressive {
		t.applyProgressivePenalty(stats)
	}

	// Check for suspicious behavior
	if t.config.EnableSuspiciousDetection {
		t.checkSuspiciousBehavior(stats)
	}
}

// getCurrentLimit calculates current rate limit with penalties
func (t *IPThrottler) getCurrentLimit(stats *ipStatistics) int {
	baseLimit := t.config.RequestsPerMinute

	if !t.config.EnableProgressive {
		return baseLimit
	}

	// Reduce limit based on failure rate
	if stats.totalRequests > 0 {
		failureRate := float64(stats.failedRequests) / float64(stats.totalRequests)
		if failureRate > 0.5 {
			return baseLimit / 4 // Severely limit problematic IPs
		} else if failureRate > 0.2 {
			return baseLimit / 2 // Moderately limit
		}
	}

	return baseLimit
}

// applyProgressivePenalty applies increasing penalties for repeated failures
func (t *IPThrottler) applyProgressivePenalty(stats *ipStatistics) {
	// Calculate penalty duration based on failure count
	penaltyMinutes := int(stats.failedRequests / 10) // 1 minute per 10 failures
	if penaltyMinutes > t.config.MaxPenaltyMinutes {
		penaltyMinutes = t.config.MaxPenaltyMinutes
	}

	if penaltyMinutes > 0 {
		stats.penaltyUntil = time.Now().Add(time.Duration(penaltyMinutes) * time.Minute)
	}
}

// checkSuspiciousBehavior detects and penalizes suspicious behavior patterns
func (t *IPThrottler) checkSuspiciousBehavior(stats *ipStatistics) {
	// Check if recent failures exceed threshold
	recentWindow := 5 * time.Minute
	if time.Since(stats.lastFailure) < recentWindow &&
		stats.failedRequests >= int64(t.config.SuspiciousThreshold) {

		// Apply suspicious behavior penalty
		penaltyDuration := time.Duration(t.config.SuspiciousPenaltyMinutes) * time.Minute
		stats.penaltyUntil = time.Now().Add(penaltyDuration)

		// Temporarily blacklist if very suspicious
		if stats.failedRequests >= int64(t.config.SuspiciousThreshold*2) {
			stats.blacklistedUntil = time.Now().Add(penaltyDuration * 2)
			// Auto-unblacklist after penalty period
		}
	}
}

// allowSubnet checks subnet-based rate limiting
func (t *IPThrottler) allowSubnet(clientIP string) bool {
	subnet := t.getSubnet(clientIP, t.config.SubnetMask)
	if subnet == "" {
		return true // Allow if we can't determine subnet
	}

	t.mu.RLock()
	subnetStats, exists := t.subnetStats[subnet]
	t.mu.RUnlock()

	if !exists {
		t.mu.Lock()
		if subnetStats, exists = t.subnetStats[subnet]; !exists {
			subnetStats = &subnetStatistics{
				tokens:     float64(t.config.SubnetRequestsPerMinute),
				lastUpdate: time.Now(),
			}
			t.subnetStats[subnet] = subnetStats
		}
		t.mu.Unlock()
	}

	subnetStats.mu.Lock()
	defer subnetStats.mu.Unlock()

	// Update subnet tokens
	now := time.Now()
	timePassed := now.Sub(subnetStats.lastUpdate)
	subnetStats.lastUpdate = now

	tokensToAdd := timePassed.Seconds() * (float64(t.config.SubnetRequestsPerMinute) / 60.0)
	subnetStats.tokens += tokensToAdd

	// Cap at configured limit
	if subnetStats.tokens > float64(t.config.SubnetRequestsPerMinute) {
		subnetStats.tokens = float64(t.config.SubnetRequestsPerMinute)
	}

	// Check if subnet request can be allowed
	if subnetStats.tokens >= 1.0 {
		subnetStats.tokens -= 1.0
		return true
	}

	return false
}

// getRealIP extracts the real client IP considering proxies.
//
// Proxy headers are consulted only when the peer is itself a trusted proxy.
// RemoteAddr is the one value a client cannot forge, so it decides whether
// the headers are worth anything at all.
func (t *IPThrottler) getRealIP(r *http.Request) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}

	// Headers from an untrusted peer are attacker-controlled. Ignoring them
	// is what keeps the throttle, the penalties and the blacklist meaningful.
	if !t.isTrustedProxy(peer) {
		return peer
	}

	for _, header := range t.config.TrustedProxyHeaders {
		if ip := r.Header.Get(header); ip != "" {
			// Handle comma-separated IPs (X-Forwarded-For)
			ips := strings.Split(ip, ",")
			for _, singleIP := range ips {
				cleanIP := strings.TrimSpace(singleIP)
				if t.isValidIP(cleanIP) && !t.isTrustedProxy(cleanIP) {
					return cleanIP
				}
			}
		}
	}

	return peer
}

// getIPStats gets or creates IP statistics
func (t *IPThrottler) getIPStats(ip string) *ipStatistics {
	t.mu.RLock()
	stats, exists := t.ipStats[ip]
	t.mu.RUnlock()

	if !exists {
		t.mu.Lock()
		if stats, exists = t.ipStats[ip]; !exists {
			stats = &ipStatistics{
				tokens:     float64(t.config.BurstSize),
				lastUpdate: time.Now(),
			}
			t.ipStats[ip] = stats
		}
		t.mu.Unlock()
	}

	return stats
}

// getSubnet calculates subnet for an IP address
func (t *IPThrottler) getSubnet(ip string, mask int) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return ""
	}

	// Create subnet mask
	var ipNet *net.IPNet
	if parsedIP.To4() != nil {
		// IPv4
		_, ipNet, _ = net.ParseCIDR(fmt.Sprintf("%s/%d", ip, mask))
	} else {
		// IPv6 - use /64 by default
		_, ipNet, _ = net.ParseCIDR(fmt.Sprintf("%s/64", ip))
	}

	if ipNet != nil {
		return ipNet.String()
	}

	return ""
}

// isWhitelisted checks if IP is whitelisted
func (t *IPThrottler) isWhitelisted(ip string) bool {
	for _, whiteIP := range t.config.WhitelistedIPs {
		if ip == whiteIP || t.isInCIDR(ip, whiteIP) {
			return true
		}
	}
	return false
}

// isBlacklisted checks if IP is blacklisted
func (t *IPThrottler) isBlacklisted(ip string) bool {
	for _, blackIP := range t.config.BlacklistedIPs {
		if ip == blackIP || t.isInCIDR(ip, blackIP) {
			return true
		}
	}
	return false
}

// isTrustedProxy checks if IP is a trusted proxy
func (t *IPThrottler) isTrustedProxy(ip string) bool {
	for _, proxyIP := range t.config.TrustedProxies {
		if ip == proxyIP || t.isInCIDR(ip, proxyIP) {
			return true
		}
	}
	return false
}

// isInCIDR checks if IP is in CIDR range
func (t *IPThrottler) isInCIDR(ip, cidr string) bool {
	if !strings.Contains(cidr, "/") {
		return false
	}

	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}

	parsedIP := net.ParseIP(ip)
	return parsedIP != nil && network.Contains(parsedIP)
}

// isValidIP checks if string is a valid IP address
func (t *IPThrottler) isValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

// cleanup removes old statistics entries
// Stop ends the cleanup goroutine. Safe to call more than once.
func (t *IPThrottler) Stop() {
	t.stopOnce.Do(func() {
		close(t.stop)
	})
}

func (t *IPThrottler) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
		}

		t.mu.Lock()
		now := time.Now()

		// Clean up IP stats
		for ip, stats := range t.ipStats {
			stats.mu.Lock()
			if now.Sub(stats.lastUpdate) > 30*time.Minute && !stats.isBlacklistedNow(now) {
				delete(t.ipStats, ip)
			}
			stats.mu.Unlock()
		}

		// Clean up subnet stats
		for subnet, stats := range t.subnetStats {
			stats.mu.Lock()
			if now.Sub(stats.lastUpdate) > 30*time.Minute {
				delete(t.subnetStats, subnet)
			}
			stats.mu.Unlock()
		}

		t.mu.Unlock()
	}
}

// GetStats returns current throttling statistics
func (t *IPThrottler) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := map[string]interface{}{
		"total_ips":     len(t.ipStats),
		"total_subnets": len(t.subnetStats),
	}

	// Count penalized IPs
	penalizedIPs := 0
	blacklistedIPs := 0

	for _, ipStat := range t.ipStats {
		ipStat.mu.Lock()
		if time.Now().Before(ipStat.penaltyUntil) {
			penalizedIPs++
		}
		if ipStat.isBlacklistedNow(time.Now()) {
			blacklistedIPs++
		}
		ipStat.mu.Unlock()
	}

	stats["penalized_ips"] = penalizedIPs
	stats["blacklisted_ips"] = blacklistedIPs

	return stats
}

// IPThrottleMiddleware creates IP-based throttling middleware
func IPThrottleMiddleware(config ThrottleConfig) func(next http.Handler) http.Handler {
	throttler := NewIPThrottler(config)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, reason := throttler.Allow(r)

			if !allowed {
				// Set rate limit headers
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", config.RequestsPerMinute))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Minute).Unix()))
				w.Header().Set("Retry-After", "60")

				http.Error(w, fmt.Sprintf("Request throttled: %s", reason), http.StatusTooManyRequests)
				return
			}

			// Wrap response writer to capture status code for failure recording
			ww := &responseWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(ww, r)

			// Record failures for learning
			throttler.RecordFailure(r, ww.status)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// GetTopThrottledIPs returns most throttled IPs for monitoring
func (t *IPThrottler) GetTopThrottledIPs(limit int) []map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	type ipInfo struct {
		IP             string
		TotalRequests  int64
		FailedRequests int64
		FailureRate    float64
		Blacklisted    bool
	}

	var ips []ipInfo

	for ip, stats := range t.ipStats {
		stats.mu.Lock()
		failureRate := float64(0)
		if stats.totalRequests > 0 {
			failureRate = float64(stats.failedRequests) / float64(stats.totalRequests)
		}

		ips = append(ips, ipInfo{
			IP:             ip,
			TotalRequests:  stats.totalRequests,
			FailedRequests: stats.failedRequests,
			FailureRate:    failureRate,
			Blacklisted:    stats.isBlacklistedNow(time.Now()),
		})
		stats.mu.Unlock()
	}

	// Sort by failure rate descending
	sort.Slice(ips, func(i, j int) bool {
		return ips[i].FailureRate > ips[j].FailureRate
	})

	// Convert to map slice and limit results
	result := make([]map[string]interface{}, 0, limit)
	for i, ip := range ips {
		if i >= limit {
			break
		}
		result = append(result, map[string]interface{}{
			"ip":              ip.IP,
			"total_requests":  ip.TotalRequests,
			"failed_requests": ip.FailedRequests,
			"failure_rate":    ip.FailureRate,
			"blacklisted":     ip.Blacklisted,
		})
	}

	return result
}
