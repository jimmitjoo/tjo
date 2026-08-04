package security

import (
	"net"
	"net/http"
	"strings"
)

// getClientIP extracts client IP from request (secure version)
func getClientIP(r *http.Request) string {
	// For security, only use RemoteAddr unless trusted proxies are configured
	// This prevents IP spoofing attacks via headers.
	//
	// net.SplitHostPort rather than LastIndex(":"): an IPv6 literal is full of
	// colons, and chopping at the last one turned "[2001:db8::1]:4444" into
	// "[2001:db8:", which matched no blacklist entry.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// getClientIPWithTrustedProxies extracts the client IP, honouring forwarding
// headers only for connections that arrive from a configured proxy.
//
// X-Forwarded-For is built left to right — "client, proxy1, proxy2" — with each
// hop appending the address it received the request from. Everything a proxy
// did not append itself came from the client, so the *leftmost* entry is
// precisely the one not to trust: a client can put anything there and a
// correctly configured nginx will happily forward it.
//
// This walks the list from the right, stepping over entries that are themselves
// configured proxies, and returns the first address that is not one. Values
// that do not parse as an IP end the walk rather than being returned, so a
// client cannot make an arbitrary string into the rate-limit key.
func getClientIPWithTrustedProxies(r *http.Request, trustedProxies []string) string {
	peer := getClientIP(r)

	if len(trustedProxies) == 0 || !isTrustedProxyAddr(peer, trustedProxies) {
		return peer
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		hops := strings.Split(xff, ",")
		for i := len(hops) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(hops[i])

			// A malformed hop means everything further left is unusable: we
			// can no longer tell which entries a proxy appended.
			if net.ParseIP(candidate) == nil {
				break
			}

			if !isTrustedProxyAddr(candidate, trustedProxies) {
				return candidate
			}
		}
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" && net.ParseIP(xri) != nil {
		return xri
	}

	return peer
}

// isTrustedProxyAddr reports whether ip is one of the configured proxies.
func isTrustedProxyAddr(ip string, trustedProxies []string) bool {
	for _, proxy := range trustedProxies {
		if ip == proxy {
			return true
		}
	}
	return false
}
