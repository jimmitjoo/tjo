package security

import (
	"net"
	"net/http"
	"strings"
)

// getClientIP extracts client IP from request (secure version)
func getClientIP(r *http.Request) string {
	// For security, only use RemoteAddr unless trusted proxies are configured
	// This prevents IP spoofing attacks via headers
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}

// getClientIPWithTrustedProxies extracts client IP with proxy validation
func getClientIPWithTrustedProxies(r *http.Request, trustedProxies []string) string {
	// If no trusted proxies defined, only use RemoteAddr for security
	if len(trustedProxies) == 0 {
		return getClientIP(r)
	}

	// Check if immediate connection is from trusted proxy
	immediateIP := getClientIP(r)
	trusted := false
	for _, proxy := range trustedProxies {
		if immediateIP == proxy {
			trusted = true
			break
		}
	}

	if !trusted {
		return immediateIP
	}

	// Only trust headers if from verified proxy
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			ip := strings.TrimSpace(xff[:idx])
			if ip != "" && !isPrivateIP(ip) {
				return ip
			}
		} else if xff != "" && !isPrivateIP(xff) {
			return strings.TrimSpace(xff)
		}
	}

	if xri := r.Header.Get("X-Real-IP"); xri != "" && !isPrivateIP(xri) {
		return strings.TrimSpace(xri)
	}

	return immediateIP
}

// isPrivateIP checks if an IP is in a private network range
func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
	}

	for _, cidr := range privateRanges {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if block.Contains(ip) {
			return true
		}
	}

	return false
}
