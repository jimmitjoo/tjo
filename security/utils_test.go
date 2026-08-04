package security

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGetClientIP covers the port stripping, including IPv6 literals, which a
// LastIndex(":") split used to mangle into "[2001:db8:".
func TestGetClientIP(t *testing.T) {
	tests := []struct {
		remoteAddr string
		want       string
	}{
		{"203.0.113.5:4444", "203.0.113.5"},
		{"[2001:db8::1]:4444", "2001:db8::1"},
		{"203.0.113.5", "203.0.113.5"}, // no port
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.remoteAddr, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remoteAddr
			assert.Equal(t, tt.want, getClientIP(r))
		})
	}
}

// TestGetClientIPWithTrustedProxies is the important one. X-Forwarded-For is
// built left to right, so the leftmost entry is whatever the client sent — a
// value an attacker controls even behind a correctly configured proxy. This
// function used to return exactly that, and would return a non-IP string
// verbatim, letting a client rotate the rate-limit key at will.
func TestGetClientIPWithTrustedProxies(t *testing.T) {
	const proxy = "10.0.0.1"
	const attacker = "203.0.113.99"

	tests := []struct {
		name    string
		peer    string
		xff     string
		realIP  string
		trusted []string
		want    string
	}{
		{
			name: "no trusted proxies configured: headers are ignored",
			peer: attacker + ":5555", xff: "1.2.3.4", trusted: nil,
			want: attacker,
		},
		{
			name: "peer is not a trusted proxy: headers are ignored",
			peer: attacker + ":5555", xff: "1.2.3.4", trusted: []string{proxy},
			want: attacker,
		},
		{
			name: "forged leftmost entry must not win",
			peer: proxy + ":5555", xff: "1.2.3.4, " + attacker, trusted: []string{proxy},
			want: attacker,
		},
		{
			name: "several forged entries still must not win",
			peer: proxy + ":5555", xff: "1.2.3.4, 9.9.9.9, " + attacker, trusted: []string{proxy},
			want: attacker,
		},
		{
			name: "a non-IP hop is never returned",
			peer: proxy + ":5555", xff: "not-an-ip, " + attacker, trusted: []string{proxy},
			want: attacker,
		},
		{
			name: "garbage all the way: fall back to the peer",
			peer: proxy + ":5555", xff: "not-an-ip", trusted: []string{proxy},
			want: proxy,
		},
		{
			name: "chained proxies are stepped over",
			peer: proxy + ":5555", xff: attacker + ", 10.0.0.2", trusted: []string{proxy, "10.0.0.2"},
			want: attacker,
		},
		{
			name: "single honest hop from a trusted proxy",
			peer: proxy + ":5555", xff: attacker, trusted: []string{proxy},
			want: attacker,
		},
		{
			name: "X-Real-IP is honoured from a trusted proxy",
			peer: proxy + ":5555", realIP: attacker, trusted: []string{proxy},
			want: attacker,
		},
		{
			name: "a non-IP X-Real-IP is rejected",
			peer: proxy + ":5555", realIP: "not-an-ip", trusted: []string{proxy},
			want: proxy,
		},
		{
			name: "no headers at all",
			peer: proxy + ":5555", trusted: []string{proxy},
			want: proxy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.peer
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.realIP != "" {
				r.Header.Set("X-Real-IP", tt.realIP)
			}

			assert.Equal(t, tt.want, getClientIPWithTrustedProxies(r, tt.trusted))
		})
	}
}

// TestClientIPCannotBeChosenByTheClient is the property the table above exists
// to protect: whatever a client puts in the header, the resulting key must not
// change. Otherwise it can mint a fresh rate-limit bucket per request.
func TestClientIPCannotBeChosenByTheClient(t *testing.T) {
	const proxy = "10.0.0.1"
	const attacker = "203.0.113.99"

	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		r, _ := http.NewRequest("GET", "/", nil)
		r.RemoteAddr = proxy + ":5555"
		r.Header.Set("X-Forwarded-For", forgedIP(i)+", "+attacker)
		seen[getClientIPWithTrustedProxies(r, []string{proxy})] = true
	}

	assert.Len(t, seen, 1, "the client changed its own identity by varying a header: %v", seen)
	assert.True(t, seen[attacker], "expected the address the proxy appended")
}

func forgedIP(i int) string {
	return "1.2." + string(rune('0'+i/10)) + "." + string(rune('0'+i%10))
}
