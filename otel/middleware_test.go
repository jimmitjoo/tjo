package otel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware_CreatesSpan(t *testing.T) {
	cfg := Config{
		ServiceName:   "test-service",
		Exporter:      ExporterNone,
		EnableTracing: true,
	}

	provider, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer provider.Shutdown(context.Background())

	// Create a test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that span is in context
		span := SpanFromContext(r.Context())
		if span == nil {
			t.Error("Span should be in context")
		}

		// Check that trace ID is available
		traceID := TraceIDFromContext(r.Context())
		if traceID == "" {
			t.Error("Trace ID should be available")
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Wrap with middleware
	wrapped := provider.Middleware()(handler)

	// Make request
	req := httptest.NewRequest("GET", "/test/path", nil)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Status code = %v, want %v", rr.Code, http.StatusOK)
	}

	// Check that trace ID header is set
	traceID := rr.Header().Get("X-Trace-ID")
	if traceID == "" {
		t.Error("X-Trace-ID header should be set")
	}
}

func TestMiddleware_DisabledProvider(t *testing.T) {
	cfg := Config{
		ServiceName:   "test-service",
		Exporter:      ExporterNone,
		EnableTracing: true,
	}

	provider, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Shutdown provider to disable it
	provider.Shutdown(context.Background())

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := provider.Middleware()(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if !handlerCalled {
		t.Error("Handler should still be called even when tracing is disabled")
	}

	if rr.Code != http.StatusOK {
		t.Errorf("Status code = %v, want %v", rr.Code, http.StatusOK)
	}
}

func TestMiddleware_RecordsStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"success", http.StatusOK},
		{"created", http.StatusCreated},
		{"bad request", http.StatusBadRequest},
		{"not found", http.StatusNotFound},
		{"server error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				ServiceName:   "test-service",
				Exporter:      ExporterNone,
				EnableTracing: true,
			}

			provider, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer provider.Shutdown(context.Background())

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			wrapped := provider.Middleware()(handler)

			req := httptest.NewRequest("GET", "/test", nil)
			rr := httptest.NewRecorder()

			wrapped.ServeHTTP(rr, req)

			if rr.Code != tt.statusCode {
				t.Errorf("Status code = %v, want %v", rr.Code, tt.statusCode)
			}
		})
	}
}

func TestMiddleware_GlobalFunction(t *testing.T) {
	// Test the global Middleware function (uses global tracer)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := Middleware("test-service")(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Status code = %v, want %v", rr.Code, http.StatusOK)
	}
}

func TestResponseWriter_WriteHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	rw := newResponseWriter(rr)

	rw.WriteHeader(http.StatusCreated)

	if rw.statusCode != http.StatusCreated {
		t.Errorf("statusCode = %v, want %v", rw.statusCode, http.StatusCreated)
	}

	// Second WriteHeader should be ignored
	rw.WriteHeader(http.StatusBadRequest)

	if rw.statusCode != http.StatusCreated {
		t.Errorf("statusCode = %v after second call, want %v", rw.statusCode, http.StatusCreated)
	}
}

func TestResponseWriter_Write(t *testing.T) {
	rr := httptest.NewRecorder()
	rw := newResponseWriter(rr)

	data := []byte("Hello, World!")
	n, err := rw.Write(data)

	if err != nil {
		t.Errorf("Write() error = %v", err)
	}

	if n != len(data) {
		t.Errorf("Write() returned %v, want %v", n, len(data))
	}

	if rw.bytesWritten != int64(len(data)) {
		t.Errorf("bytesWritten = %v, want %v", rw.bytesWritten, len(data))
	}

	// Write should trigger implicit WriteHeader with 200
	if rw.statusCode != http.StatusOK {
		t.Errorf("statusCode = %v, want %v", rw.statusCode, http.StatusOK)
	}
}

func TestResponseWriter_Unwrap(t *testing.T) {
	rr := httptest.NewRecorder()
	rw := newResponseWriter(rr)

	unwrapped := rw.Unwrap()

	if unwrapped != rr {
		t.Error("Unwrap() should return the original ResponseWriter")
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		remoteIP string
		want     string
	}{
		{
			name:     "X-Forwarded-For single IP",
			headers:  map[string]string{"X-Forwarded-For": "192.168.1.1"},
			remoteIP: "10.0.0.1:12345",
			want:     "192.168.1.1",
		},
		{
			name:     "X-Forwarded-For multiple IPs",
			headers:  map[string]string{"X-Forwarded-For": "192.168.1.1, 10.0.0.2"},
			remoteIP: "10.0.0.1:12345",
			want:     "192.168.1.1",
		},
		{
			name:     "X-Real-IP",
			headers:  map[string]string{"X-Real-IP": "192.168.1.1"},
			remoteIP: "10.0.0.1:12345",
			want:     "192.168.1.1",
		},
		{
			name:     "RemoteAddr with port",
			headers:  map[string]string{},
			remoteIP: "192.168.1.1:12345",
			want:     "192.168.1.1",
		},
		{
			name:     "RemoteAddr without port",
			headers:  map[string]string{},
			remoteIP: "192.168.1.1",
			want:     "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteIP
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			got := getClientIP(req)
			if got != tt.want {
				t.Errorf("getClientIP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScheme(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*http.Request)
		want   string
	}{
		{
			name:  "HTTP",
			setup: func(r *http.Request) {},
			want:  "http",
		},
		{
			name: "X-Forwarded-Proto HTTPS",
			setup: func(r *http.Request) {
				r.Header.Set("X-Forwarded-Proto", "https")
			},
			want: "https",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			tt.setup(req)

			got := scheme(req)
			if got != tt.want {
				t.Errorf("scheme() = %v, want %v", got, tt.want)
			}
		})
	}
}
