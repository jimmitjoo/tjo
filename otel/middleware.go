package otel

import (
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode    int
	bytesWritten  int64
	headerWritten bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // Default to 200
	}
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.headerWritten {
		rw.statusCode = code
		rw.headerWritten = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.headerWritten {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
}

// Unwrap returns the underlying ResponseWriter for compatibility with
// http.ResponseController and other wrappers.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Middleware returns an HTTP middleware that traces requests.
// It extracts trace context from incoming requests and creates spans.
func (p *Provider) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !p.IsEnabled() {
				next.ServeHTTP(w, r)
				return
			}

			// Extract trace context from incoming request
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Start span
			spanName := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
			ctx, span := p.tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(httpServerAttributes(r)...),
			)
			defer span.End()

			// Wrap response writer to capture status
			rw := newResponseWriter(w)

			// Add trace ID to response headers for debugging
			if span.SpanContext().HasTraceID() {
				rw.Header().Set("X-Trace-ID", span.SpanContext().TraceID().String())
			}

			// Serve the request
			start := time.Now()
			next.ServeHTTP(rw, r.WithContext(ctx))
			duration := time.Since(start)

			// Record response attributes
			span.SetAttributes(
				semconv.HTTPStatusCode(rw.statusCode),
				attribute.Int64("http.response.size", rw.bytesWritten),
				attribute.Float64("http.duration_ms", float64(duration.Milliseconds())),
			)

			// Set span status based on HTTP status code
			if rw.statusCode >= 500 {
				span.SetStatus(codes.Error, http.StatusText(rw.statusCode))
			} else if rw.statusCode >= 400 {
				// Client errors are not span errors, but we note them
				span.SetStatus(codes.Unset, "")
			} else {
				span.SetStatus(codes.Ok, "")
			}
		})
	}
}

// httpServerAttributes returns standard HTTP server span attributes.
func httpServerAttributes(r *http.Request) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.HTTPMethod(r.Method),
		semconv.HTTPURL(sanitizeURL(r)),
		semconv.HTTPScheme(scheme(r)),
		semconv.NetHostName(r.Host),
		semconv.HTTPTarget(r.URL.Path),
	}

	if r.URL.RawQuery != "" {
		attrs = append(attrs, attribute.String("http.query", r.URL.RawQuery))
	}

	if userAgent := r.UserAgent(); userAgent != "" {
		attrs = append(attrs, semconv.HTTPUserAgent(userAgent))
	}

	if contentLength := r.ContentLength; contentLength > 0 {
		attrs = append(attrs, semconv.HTTPRequestContentLength(int(contentLength)))
	}

	if clientIP := getClientIP(r); clientIP != "" {
		attrs = append(attrs, semconv.NetSockPeerAddr(clientIP))
	}

	return attrs
}

// sanitizeURL returns a URL string safe for logging (no sensitive query params).
func sanitizeURL(r *http.Request) string {
	u := *r.URL
	// Clear potentially sensitive query parameters
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// scheme returns the request scheme.
func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}

// getClientIP extracts the client IP address from the request.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	addr := r.RemoteAddr
	// Remove port if present
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}

// MiddlewareFunc is a convenience type for middleware without a provider.
// Use this when you want to use the global tracer.
func Middleware(serviceName string) func(http.Handler) http.Handler {
	tracer := otel.Tracer(serviceName)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract trace context from incoming request
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Start span
			spanName := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
			ctx, span := tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(httpServerAttributes(r)...),
			)
			defer span.End()

			// Wrap response writer
			rw := newResponseWriter(w)

			// Add trace ID to response header
			if span.SpanContext().HasTraceID() {
				rw.Header().Set("X-Trace-ID", span.SpanContext().TraceID().String())
			}

			// Serve request
			start := time.Now()
			next.ServeHTTP(rw, r.WithContext(ctx))
			duration := time.Since(start)

			// Record response
			span.SetAttributes(
				semconv.HTTPStatusCode(rw.statusCode),
				attribute.Float64("http.duration_ms", float64(duration.Milliseconds())),
			)

			if rw.statusCode >= 500 {
				span.SetStatus(codes.Error, http.StatusText(rw.statusCode))
			}
		})
	}
}
