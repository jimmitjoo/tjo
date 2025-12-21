package otel

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func TestStart(t *testing.T) {
	ctx := context.Background()

	newCtx, span := Start(ctx, "test-span")
	defer span.End()

	if newCtx == ctx {
		t.Error("Start() should return a new context")
	}

	if span == nil {
		t.Error("Start() should return a span")
	}
}

func TestSpanFromContext(t *testing.T) {
	ctx := context.Background()

	// Without span, should return no-op span
	span := SpanFromContext(ctx)
	if span == nil {
		t.Error("SpanFromContext() should not return nil")
	}

	// With span
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

	ctx, createdSpan := provider.Tracer().Start(ctx, "test")
	defer createdSpan.End()

	retrievedSpan := SpanFromContext(ctx)
	if retrievedSpan == nil {
		t.Error("SpanFromContext() should return the span from context")
	}
}

func TestTraceIDFromContext(t *testing.T) {
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

	ctx, span := provider.Tracer().Start(context.Background(), "test")
	defer span.End()

	traceID := TraceIDFromContext(ctx)
	if traceID == "" {
		t.Error("TraceIDFromContext() should return a trace ID")
	}

	// Should be a valid 32-character hex string
	if len(traceID) != 32 {
		t.Errorf("TraceID length = %v, want 32", len(traceID))
	}
}

func TestSpanIDFromContext(t *testing.T) {
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

	ctx, span := provider.Tracer().Start(context.Background(), "test")
	defer span.End()

	spanID := SpanIDFromContext(ctx)
	if spanID == "" {
		t.Error("SpanIDFromContext() should return a span ID")
	}

	// Should be a valid 16-character hex string
	if len(spanID) != 16 {
		t.Errorf("SpanID length = %v, want 16", len(spanID))
	}
}

func TestGetTraceInfo(t *testing.T) {
	cfg := Config{
		ServiceName:   "test-service",
		Exporter:      ExporterNone,
		Sampler:       SamplerAlways,
		EnableTracing: true,
	}

	provider, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer provider.Shutdown(context.Background())

	ctx, span := provider.Tracer().Start(context.Background(), "test")
	defer span.End()

	info := GetTraceInfo(ctx)

	if info.TraceID == "" {
		t.Error("TraceInfo.TraceID should not be empty")
	}

	if info.SpanID == "" {
		t.Error("TraceInfo.SpanID should not be empty")
	}

	if !info.Sampled {
		t.Error("TraceInfo.Sampled should be true with AlwaysSample")
	}
}

func TestWithSpan(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		executed := false
		err := WithSpan(context.Background(), "test-operation", func(ctx context.Context) error {
			executed = true

			// Verify span is in context
			span := SpanFromContext(ctx)
			if span == nil {
				t.Error("Span should be in context")
			}

			return nil
		})

		if err != nil {
			t.Errorf("WithSpan() error = %v", err)
		}

		if !executed {
			t.Error("Function should have been executed")
		}
	})

	t.Run("error", func(t *testing.T) {
		expectedErr := errors.New("test error")

		err := WithSpan(context.Background(), "test-operation", func(ctx context.Context) error {
			return expectedErr
		})

		if err != expectedErr {
			t.Errorf("WithSpan() error = %v, want %v", err, expectedErr)
		}
	})
}

func TestWithSpanResult(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		result, err := WithSpanResult(context.Background(), "test-operation", func(ctx context.Context) (string, error) {
			return "hello", nil
		})

		if err != nil {
			t.Errorf("WithSpanResult() error = %v", err)
		}

		if result != "hello" {
			t.Errorf("WithSpanResult() result = %v, want hello", result)
		}
	})

	t.Run("error", func(t *testing.T) {
		expectedErr := errors.New("test error")

		result, err := WithSpanResult(context.Background(), "test-operation", func(ctx context.Context) (int, error) {
			return 0, expectedErr
		})

		if err != expectedErr {
			t.Errorf("WithSpanResult() error = %v, want %v", err, expectedErr)
		}

		if result != 0 {
			t.Errorf("WithSpanResult() result = %v, want 0", result)
		}
	})
}

func TestSpanOptions(t *testing.T) {
	opts := NewSpanOptions().
		WithKind(trace.SpanKindClient).
		WithAttribute("key", "value").
		WithAttributes(
			attribute.String("another", "attr"),
			attribute.Int("count", 42),
		)

	built := opts.Build()

	if len(built) != 3 {
		t.Errorf("Build() returned %v options, want 3", len(built))
	}
}

func TestAddEvent(t *testing.T) {
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

	ctx, span := provider.Tracer().Start(context.Background(), "test")
	defer span.End()

	// Should not panic
	AddEvent(ctx, "test-event", attribute.String("key", "value"))

	// Should not panic with nil context
	AddEvent(context.Background(), "test-event")
}

func TestSetAttributes(t *testing.T) {
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

	ctx, span := provider.Tracer().Start(context.Background(), "test")
	defer span.End()

	// Should not panic
	SetAttributes(ctx, attribute.String("key", "value"), attribute.Int("count", 42))
}

func TestRecordError(t *testing.T) {
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

	ctx, span := provider.Tracer().Start(context.Background(), "test")
	defer span.End()

	testErr := errors.New("test error")

	// Should not panic
	RecordError(ctx, testErr)

	// Should handle nil error
	RecordError(ctx, nil)
}

func TestSetStatus(t *testing.T) {
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

	ctx, span := provider.Tracer().Start(context.Background(), "test")
	defer span.End()

	// Should not panic
	SetStatus(ctx, codes.Ok, "success")
	SetStatus(ctx, codes.Error, "failed")
}

func TestCallerInfo(t *testing.T) {
	file, line, function := CallerInfo(0)

	if file == "" {
		t.Error("CallerInfo() file should not be empty")
	}

	if line == 0 {
		t.Error("CallerInfo() line should not be 0")
	}

	if function == "" {
		t.Error("CallerInfo() function should not be empty")
	}
}

func TestDBAttributes(t *testing.T) {
	attrs := DBAttributes("postgres", "mydb", "SELECT * FROM users")

	if len(attrs) != 3 {
		t.Errorf("DBAttributes() returned %v attrs, want 3", len(attrs))
	}
}

func TestHTTPClientAttributes(t *testing.T) {
	attrs := HTTPClientAttributes("GET", "https://example.com/api", 200)

	if len(attrs) != 3 {
		t.Errorf("HTTPClientAttributes() returned %v attrs, want 3", len(attrs))
	}
}

func TestMessagingAttributes(t *testing.T) {
	attrs := MessagingAttributes("kafka", "my-topic", "send")

	if len(attrs) != 3 {
		t.Errorf("MessagingAttributes() returned %v attrs, want 3", len(attrs))
	}
}

func TestErrorEvent(t *testing.T) {
	err := errors.New("test error")

	name, attrs := ErrorEvent(err)

	if name != "exception" {
		t.Errorf("ErrorEvent() name = %v, want exception", name)
	}

	if len(attrs) != 2 {
		t.Errorf("ErrorEvent() returned %v attrs, want 2", len(attrs))
	}
}

func TestAttrHelpers(t *testing.T) {
	// Just verify these are the correct functions
	_ = String("key", "value")
	_ = Int("key", 42)
	_ = Int64("key", 42)
	_ = Float64("key", 3.14)
	_ = Bool("key", true)
}
