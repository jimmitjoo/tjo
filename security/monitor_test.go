package security

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestIsIPBlockedIsRaceFree covers a read lock guarding a write. IsIPBlocked
// took RLock and then deleted from blockedIPs and suspiciousIPs when a block
// expired. Several goroutines can hold a read lock at once, so concurrent
// callers deleted from the same maps simultaneously -- the same defect as the
// logger's writeEntry.
func TestIsIPBlockedIsRaceFree(t *testing.T) {
	monitor := NewSecurityMonitor(DefaultSecurityLogger, 3, 10*time.Millisecond)

	// Block a set of addresses, then let the blocks expire so every concurrent
	// caller takes the expiry branch that mutates the maps.
	monitor.mu.Lock()
	for i := 0; i < 20; i++ {
		monitor.blockedIPs[ipFor(i)] = time.Now()
		monitor.suspiciousIPs[ipFor(i)] = 5
	}
	monitor.mu.Unlock()

	time.Sleep(30 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		for r := 0; r < 5; r++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				monitor.IsIPBlocked(ipFor(n))
			}(i)
		}
	}
	wg.Wait()

	for i := 0; i < 20; i++ {
		assert.False(t, monitor.IsIPBlocked(ipFor(i)), "expired block should be gone")
	}
}

func TestIsIPBlockedHonoursDuration(t *testing.T) {
	monitor := NewSecurityMonitor(DefaultSecurityLogger, 3, time.Hour)

	assert.False(t, monitor.IsIPBlocked("203.0.113.1"), "unknown IP must not be blocked")

	monitor.mu.Lock()
	monitor.blockIP("203.0.113.1", "test")
	monitor.mu.Unlock()

	assert.True(t, monitor.IsIPBlocked("203.0.113.1"), "a fresh block should hold")
	assert.False(t, monitor.IsIPBlocked("203.0.113.2"), "blocking one IP must not block another")
}

func ipFor(n int) string {
	return "203.0.113." + string(rune('0'+n/10)) + string(rune('0'+n%10))
}
