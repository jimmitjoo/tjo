package otel

import (
	"context"

	"github.com/jimmitjoo/gemquick/logging"
	"go.opentelemetry.io/otel/trace"
)

// LoggerWithTrace returns a logger with trace context fields added.
// This enables log correlation with traces.
//
// Usage:
//
//	logger := otel.LoggerWithTrace(ctx, g.Logging.Logger)
//	logger.Info("Processing request", nil)
//	// Logs will include trace_id and span_id fields
func LoggerWithTrace(ctx context.Context, logger *logging.Logger) *logging.Logger {
	if logger == nil {
		return nil
	}

	span := trace.SpanFromContext(ctx)
	if span == nil {
		return logger
	}

	sc := span.SpanContext()
	if !sc.IsValid() {
		return logger
	}

	fields := make(map[string]interface{})

	if sc.HasTraceID() {
		fields["trace_id"] = sc.TraceID().String()
	}

	if sc.HasSpanID() {
		fields["span_id"] = sc.SpanID().String()
	}

	if sc.IsSampled() {
		fields["trace_sampled"] = true
	}

	if len(fields) == 0 {
		return logger
	}

	return logger.WithFields(fields)
}

// TraceFields returns log fields with trace context.
// Use this when you want to add trace context to a log call.
//
// Usage:
//
//	logger.Info("Processing", otel.TraceFields(ctx))
func TraceFields(ctx context.Context) map[string]interface{} {
	fields := make(map[string]interface{})

	span := trace.SpanFromContext(ctx)
	if span == nil {
		return fields
	}

	sc := span.SpanContext()
	if !sc.IsValid() {
		return fields
	}

	if sc.HasTraceID() {
		fields["trace_id"] = sc.TraceID().String()
	}

	if sc.HasSpanID() {
		fields["span_id"] = sc.SpanID().String()
	}

	return fields
}

// MergeFields merges trace fields with additional fields.
func MergeFields(ctx context.Context, additional map[string]interface{}) map[string]interface{} {
	fields := TraceFields(ctx)
	for k, v := range additional {
		fields[k] = v
	}
	return fields
}

// LogContext provides logging methods that automatically include trace context.
type LogContext struct {
	ctx    context.Context
	logger *logging.Logger
}

// NewLogContext creates a new LogContext.
func NewLogContext(ctx context.Context, logger *logging.Logger) *LogContext {
	return &LogContext{
		ctx:    ctx,
		logger: LoggerWithTrace(ctx, logger),
	}
}

// Trace logs a trace message with trace context.
func (l *LogContext) Trace(message string, fields ...map[string]interface{}) {
	if l.logger == nil {
		return
	}
	l.logger.Trace(message, fields...)
}

// Debug logs a debug message with trace context.
func (l *LogContext) Debug(message string, fields ...map[string]interface{}) {
	if l.logger == nil {
		return
	}
	l.logger.Debug(message, fields...)
}

// Info logs an info message with trace context.
func (l *LogContext) Info(message string, fields ...map[string]interface{}) {
	if l.logger == nil {
		return
	}
	l.logger.Info(message, fields...)
}

// Warn logs a warning message with trace context.
func (l *LogContext) Warn(message string, fields ...map[string]interface{}) {
	if l.logger == nil {
		return
	}
	l.logger.Warn(message, fields...)
}

// Error logs an error message with trace context.
func (l *LogContext) Error(message string, fields ...map[string]interface{}) {
	if l.logger == nil {
		return
	}
	l.logger.Error(message, fields...)
}

// WithField adds a field and returns a new LogContext.
func (l *LogContext) WithField(key string, value interface{}) *LogContext {
	if l.logger == nil {
		return l
	}
	return &LogContext{
		ctx:    l.ctx,
		logger: l.logger.WithField(key, value),
	}
}

// WithFields adds fields and returns a new LogContext.
func (l *LogContext) WithFields(fields map[string]interface{}) *LogContext {
	if l.logger == nil {
		return l
	}
	return &LogContext{
		ctx:    l.ctx,
		logger: l.logger.WithFields(fields),
	}
}
