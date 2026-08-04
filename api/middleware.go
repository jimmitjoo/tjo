package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	rdebug "runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jimmitjoo/tjo/internal/jsonstrict"
)

// contextKey is a custom type for context keys
type contextKey string

const (
	// ContextKeyAPIVersion stores the API version in context
	ContextKeyAPIVersion contextKey = "api_version"
	// ContextKeyRequestID stores the request ID in context
	ContextKeyRequestID contextKey = "request_id"
	// ContextKeyStartTime stores the request start time
	ContextKeyStartTime contextKey = "start_time"
)

// APIConfig holds API configuration
type APIConfig struct {
	Version        string
	RateLimitPerMin int
	EnableCORS     bool
	AllowedOrigins []string
	EnableMetrics  bool
	Debug          bool
}

// ErrorHandler handles panics and errors in API routes
func ErrorHandler(debug bool) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log stack trace
					if debug {
						fmt.Printf("Panic: %v\n", err)
						fmt.Printf("Stack: %s\n", string(rdebug.Stack()))
					}
					
					// Send error response
					InternalServerError(w, "An unexpected error occurred", 
						WithRequestID(middleware.GetReqID(r.Context())))
				}
			}()
			
			next.ServeHTTP(w, r)
		})
	}
}

// ContentTypeJSON ensures JSON content type for requests
func ContentTypeJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			contentType := r.Header.Get("Content-Type")
			if !strings.HasPrefix(contentType, "application/json") && 
			   !strings.HasPrefix(contentType, "application/xml") &&
			   !strings.HasPrefix(contentType, "multipart/form-data") {
				Error(w, http.StatusUnsupportedMediaType, 
					"UNSUPPORTED_MEDIA_TYPE", 
					"Content-Type must be application/json, application/xml, or multipart/form-data", 
					nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// APIVersion middleware adds API version to context
func APIVersion(version string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), ContextKeyAPIVersion, version)
			w.Header().Set("X-API-Version", version)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestTimer adds timing information to responses
func RequestTimer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx := context.WithValue(r.Context(), ContextKeyStartTime, start)
		
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		
		next.ServeHTTP(ww, r.WithContext(ctx))
		
		duration := time.Since(start)
		w.Header().Set("X-Response-Time", fmt.Sprintf("%dms", duration.Milliseconds()))
	})
}

// CORS adds CORS headers
func CORS(allowedOrigins []string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			
			// Check if origin is allowed
			allowed := false
			for _, allowedOrigin := range allowedOrigins {
				if allowedOrigin == "*" || allowedOrigin == origin {
					allowed = true
					break
				}
			}
			
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-Request-ID")
				w.Header().Set("Access-Control-Expose-Headers", "X-API-Version, X-Response-Time, X-Request-ID")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Max-Age", "3600")
			}
			
			// Handle preflight request
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			
			next.ServeHTTP(w, r)
		})
	}
}

// SecureHeaders adds security headers
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		
		// Only set HSTS on HTTPS
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		
		next.ServeHTTP(w, r)
	})
}

// JSONRequest parses JSON request body
func JSONRequest(dst interface{}) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body == nil {
				Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Request body is empty", nil)
				return
			}
			
			// Read once so the body can be checked for duplicate member names
			// before decoding. encoding/json silently takes the last one, which
			// means this middleware and anything else parsing the same body can
			// disagree about what the request said. See internal/jsonstrict.
			body, err := io.ReadAll(r.Body)
			if err != nil {
				Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Could not read request body", nil)
				return
			}

			if err := jsonstrict.RejectDuplicateKeys(body); err != nil {
				Error(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), nil)
				return
			}

			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.DisallowUnknownFields()

			if err := decoder.Decode(dst); err != nil {
				details := map[string]interface{}{
					"parse_error": err.Error(),
				}
				Error(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON in request body", details)
				return
			}
			
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth checks for authentication
func RequireAuth(authFunc func(r *http.Request) (bool, error)) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorized, err := authFunc(r)
			if err != nil {
				InternalServerError(w, "Authentication check failed")
				return
			}
			
			if !authorized {
				Unauthorized(w, "Authentication required")
				return
			}
			
			next.ServeHTTP(w, r)
		})
	}
}

// GetAPIVersion gets API version from context
func GetAPIVersion(ctx context.Context) string {
	if version, ok := ctx.Value(ContextKeyAPIVersion).(string); ok {
		return version
	}
	return ""
}

// GetRequestStartTime gets request start time from context
func GetRequestStartTime(ctx context.Context) time.Time {
	if start, ok := ctx.Value(ContextKeyStartTime).(time.Time); ok {
		return start
	}
	return time.Time{}
}

// ChainMiddleware chains multiple middleware functions
func ChainMiddleware(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			handler = middlewares[i](handler)
		}
		return handler
	}
}