package security

import (
	"net/http"
	"sync"
	"time"
)

// SecurityMonitor tracks security events and patterns
type SecurityMonitor struct {
	mu              sync.RWMutex
	events          map[string][]SecurityEvent
	suspiciousIPs   map[string]int
	blockedIPs      map[string]time.Time
	alertThreshold  int
	blockDuration   time.Duration
	cleanupInterval time.Duration
	logger          *SecurityLogger
}

// NewSecurityMonitor creates a new security monitor
func NewSecurityMonitor(logger *SecurityLogger, alertThreshold int, blockDuration time.Duration) *SecurityMonitor {
	if logger == nil {
		logger = DefaultSecurityLogger
	}

	sm := &SecurityMonitor{
		events:          make(map[string][]SecurityEvent),
		suspiciousIPs:   make(map[string]int),
		blockedIPs:      make(map[string]time.Time),
		alertThreshold:  alertThreshold,
		blockDuration:   blockDuration,
		cleanupInterval: blockDuration * 2,
		logger:          logger,
	}

	// Start cleanup routine
	go sm.cleanupRoutine()

	return sm
}

// RecordEvent records a security event and checks for patterns
func (sm *SecurityMonitor) RecordEvent(event SecurityEvent) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	clientIP := event.ClientIP

	// Clean up IP address (remove port and additional info)
	if colonIdx := len(clientIP) - 1; colonIdx > 0 {
		for i, char := range clientIP {
			if char == ' ' {
				clientIP = clientIP[:i]
				break
			}
		}
	}

	// Record event
	sm.events[clientIP] = append(sm.events[clientIP], event)

	// Check if this is a suspicious event type
	if sm.isSuspiciousEventType(event.EventType) {
		sm.suspiciousIPs[clientIP]++

		// Check if threshold reached
		if sm.suspiciousIPs[clientIP] >= sm.alertThreshold {
			sm.blockIP(clientIP, "Too many suspicious events")
		}
	}
}

// IsIPBlocked checks if an IP is currently blocked
func (sm *SecurityMonitor) IsIPBlocked(ip string) bool {
	// Read path first: the overwhelmingly common case is an address that is
	// not blocked at all, and that answer needs no write lock.
	sm.mu.RLock()
	blockTime, blocked := sm.blockedIPs[ip]
	expired := blocked && time.Since(blockTime) > sm.blockDuration
	sm.mu.RUnlock()

	if !blocked {
		return false
	}
	if !expired {
		return true
	}

	// Dropping an expired block mutates both maps, so it needs the write lock.
	// This used to delete while holding only RLock: several goroutines can hold
	// a read lock at once, so concurrent callers deleted from the same map
	// simultaneously. Reproducible under -race with enough callers hitting one
	// key in the instant it expires.
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Re-check: another goroutine may have refreshed or removed it while the
	// lock was not held.
	if blockTime, stillBlocked := sm.blockedIPs[ip]; stillBlocked {
		if time.Since(blockTime) <= sm.blockDuration {
			return true
		}
		delete(sm.blockedIPs, ip)
		delete(sm.suspiciousIPs, ip)
	}

	return false
}

// blockIP blocks an IP address for the configured duration
func (sm *SecurityMonitor) blockIP(ip string, reason string) {
	sm.blockedIPs[ip] = time.Now()

	// Log the block
	event := SecurityEvent{
		EventType: EventIPBlocked,
		Severity:  "high",
		ClientIP:  ip,
		Message:   "IP automatically blocked due to suspicious activity",
		Details: map[string]interface{}{
			"reason":           reason,
			"suspicious_count": sm.suspiciousIPs[ip],
			"block_duration":   sm.blockDuration.String(),
		},
		Action: "blocked",
	}
	sm.logger.LogEvent(event)
}

// isSuspiciousEventType determines if an event type is suspicious
func (sm *SecurityMonitor) isSuspiciousEventType(eventType SecurityEventType) bool {
	suspiciousTypes := []SecurityEventType{
		EventCSRFFailure,
		EventSQLInjectionAttempt,
		EventXSSAttempt,
		EventPathTraversal,
		EventRateLimitExceeded,
	}

	for _, suspicious := range suspiciousTypes {
		if eventType == suspicious {
			return true
		}
	}
	return false
}

// cleanupRoutine periodically cleans up old events and expired blocks
func (sm *SecurityMonitor) cleanupRoutine() {
	ticker := time.NewTicker(sm.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		sm.mu.Lock()
		now := time.Now()

		// Clean up expired blocks
		for ip, blockTime := range sm.blockedIPs {
			if now.Sub(blockTime) > sm.blockDuration {
				delete(sm.blockedIPs, ip)
				delete(sm.suspiciousIPs, ip)
			}
		}

		// Clean up old events (keep last 24 hours)
		cutoff := now.Add(-24 * time.Hour)
		for ip, events := range sm.events {
			var recentEvents []SecurityEvent
			for _, event := range events {
				if event.Timestamp.After(cutoff) {
					recentEvents = append(recentEvents, event)
				}
			}
			if len(recentEvents) == 0 {
				delete(sm.events, ip)
			} else {
				sm.events[ip] = recentEvents
			}
		}

		sm.mu.Unlock()
	}
}

// GetSecurityStats returns current security statistics
func (sm *SecurityMonitor) GetSecurityStats() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return map[string]interface{}{
		"blocked_ips":     len(sm.blockedIPs),
		"suspicious_ips":  len(sm.suspiciousIPs),
		"total_events":    sm.getTotalEvents(),
		"alert_threshold": sm.alertThreshold,
		"block_duration":  sm.blockDuration.String(),
	}
}

// getTotalEvents counts total events across all IPs
func (sm *SecurityMonitor) getTotalEvents() int {
	total := 0
	for _, events := range sm.events {
		total += len(events)
	}
	return total
}

// SecurityMonitorMiddleware creates middleware that integrates with security monitor
func SecurityMonitorMiddleware(monitor *SecurityMonitor) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract client IP
			clientIP := getClientIP(r)

			// Check if IP is blocked
			if monitor.IsIPBlocked(clientIP) {
				monitor.logger.LogIPBlocked(r, "IP is currently blocked")
				http.Error(w, "Access denied", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Global security monitor instance
var DefaultSecurityMonitor = NewSecurityMonitor(DefaultSecurityLogger, 5, 10*time.Minute)
