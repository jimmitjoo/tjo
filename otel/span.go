package otel

import (
	"context"
	"fmt"
	"runtime"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Start creates a new span with the given name.
// It returns the updated context and the span.
// Remember to call span.End() when the operation is complete.
//
// Usage:
//
//	ctx, span := otel.Start(ctx, "operation_name")
//	defer span.End()
func Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer("").Start(ctx, name, opts...)
}

// StartWithTracer creates a new span using the specified tracer.
func StartWithTracer(ctx context.Context, tracer trace.Tracer, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return tracer.Start(ctx, name, opts...)
}

// SpanFromContext returns the current span from the context.
// Returns a no-op span if no span is present.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// TraceIDFromContext extracts the trace ID from the context.
// Returns an empty string if no trace is active.
func TraceIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return ""
	}
	sc := span.SpanContext()
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}

// SpanIDFromContext extracts the span ID from the context.
// Returns an empty string if no span is active.
func SpanIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return ""
	}
	sc := span.SpanContext()
	if !sc.HasSpanID() {
		return ""
	}
	return sc.SpanID().String()
}

// TraceInfo returns both trace ID and span ID from context.
type TraceInfo struct {
	TraceID string
	SpanID  string
	Sampled bool
}

// GetTraceInfo extracts trace information from context.
func GetTraceInfo(ctx context.Context) TraceInfo {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return TraceInfo{}
	}
	sc := span.SpanContext()
	return TraceInfo{
		TraceID: sc.TraceID().String(),
		SpanID:  sc.SpanID().String(),
		Sampled: sc.IsSampled(),
	}
}

// AddEvent adds an event to the current span.
func AddEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return
	}
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetAttributes sets attributes on the current span.
func SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return
	}
	span.SetAttributes(attrs...)
}

// RecordError records an error on the current span.
// The error is recorded as an event and the span status is set to Error.
func RecordError(ctx context.Context, err error, opts ...trace.EventOption) {
	span := trace.SpanFromContext(ctx)
	if span == nil || err == nil {
		return
	}
	span.RecordError(err, opts...)
	span.SetStatus(codes.Error, err.Error())
}

// SetStatus sets the status of the current span.
func SetStatus(ctx context.Context, code codes.Code, description string) {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return
	}
	span.SetStatus(code, description)
}

// WithSpan executes a function within a new span.
// The span is automatically ended when the function returns.
// If the function returns an error, it is recorded on the span.
//
// Usage:
//
//	err := otel.WithSpan(ctx, "process_data", func(ctx context.Context) error {
//	    // Your code here
//	    return nil
//	})
func WithSpan(ctx context.Context, name string, fn func(context.Context) error, opts ...trace.SpanStartOption) error {
	ctx, span := otel.Tracer("").Start(ctx, name, opts...)
	defer span.End()

	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// WithSpanResult executes a function within a new span and returns both result and error.
// Useful when you need to return a value from the traced operation.
func WithSpanResult[T any](ctx context.Context, name string, fn func(context.Context) (T, error), opts ...trace.SpanStartOption) (T, error) {
	ctx, span := otel.Tracer("").Start(ctx, name, opts...)
	defer span.End()

	result, err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return result, err
}

// SpanOptions provides a fluent interface for building span options.
type SpanOptions struct {
	opts []trace.SpanStartOption
}

// NewSpanOptions creates a new SpanOptions builder.
func NewSpanOptions() *SpanOptions {
	return &SpanOptions{}
}

// WithKind sets the span kind.
func (s *SpanOptions) WithKind(kind trace.SpanKind) *SpanOptions {
	s.opts = append(s.opts, trace.WithSpanKind(kind))
	return s
}

// WithAttribute adds a string attribute.
func (s *SpanOptions) WithAttribute(key, value string) *SpanOptions {
	s.opts = append(s.opts, trace.WithAttributes(attribute.String(key, value)))
	return s
}

// WithAttributes adds multiple attributes.
func (s *SpanOptions) WithAttributes(attrs ...attribute.KeyValue) *SpanOptions {
	s.opts = append(s.opts, trace.WithAttributes(attrs...))
	return s
}

// Build returns the accumulated span options.
func (s *SpanOptions) Build() []trace.SpanStartOption {
	return s.opts
}

// CallerInfo returns file and line information for the caller.
// Useful for adding source location to spans.
func CallerInfo(skip int) (file string, line int, function string) {
	pc, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		return "", 0, ""
	}
	if fn := runtime.FuncForPC(pc); fn != nil {
		function = fn.Name()
	}
	return file, line, function
}

// AddCallerInfo adds source location as span attributes.
func AddCallerInfo(ctx context.Context) {
	file, line, function := CallerInfo(1)
	SetAttributes(ctx,
		attribute.String("code.filepath", file),
		attribute.Int("code.lineno", line),
		attribute.String("code.function", function),
	)
}

// Attr is a convenience function to create attributes.
var (
	String  = attribute.String
	Int     = attribute.Int
	Int64   = attribute.Int64
	Float64 = attribute.Float64
	Bool    = attribute.Bool
)

// Common semantic convention helpers.

// DBAttributes returns common database span attributes.
func DBAttributes(system, name, statement string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("db.system", system),
	}
	if name != "" {
		attrs = append(attrs, attribute.String("db.name", name))
	}
	if statement != "" {
		attrs = append(attrs, attribute.String("db.statement", statement))
	}
	return attrs
}

// HTTPClientAttributes returns common HTTP client span attributes.
func HTTPClientAttributes(method, url string, statusCode int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("http.method", method),
		attribute.String("http.url", url),
		attribute.Int("http.status_code", statusCode),
	}
}

// MessagingAttributes returns common messaging span attributes.
func MessagingAttributes(system, destination, operation string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("messaging.system", system),
		attribute.String("messaging.destination.name", destination),
		attribute.String("messaging.operation", operation),
	}
}

// ErrorEvent creates a standard error event.
func ErrorEvent(err error) (string, []attribute.KeyValue) {
	return "exception", []attribute.KeyValue{
		attribute.String("exception.type", fmt.Sprintf("%T", err)),
		attribute.String("exception.message", err.Error()),
	}
}
