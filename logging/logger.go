package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// LogLevel represents the severity of a log entry
type LogLevel int

const (
	TraceLevel LogLevel = iota
	DebugLevel
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case TraceLevel:
		return "TRACE"
	case DebugLevel:
		return "DEBUG"
	case InfoLevel:
		return "INFO"
	case WarnLevel:
		return "WARN"
	case ErrorLevel:
		return "ERROR"
	case FatalLevel:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// ParseLogLevel converts a string to LogLevel
func ParseLogLevel(level string) LogLevel {
	switch strings.ToUpper(level) {
	case "TRACE":
		return TraceLevel
	case "DEBUG":
		return DebugLevel
	case "INFO":
		return InfoLevel
	case "WARN", "WARNING":
		return WarnLevel
	case "ERROR":
		return ErrorLevel
	case "FATAL":
		return FatalLevel
	default:
		return InfoLevel
	}
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Caller    string                 `json:"caller,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	UserID    string                 `json:"user_id,omitempty"`
	Service   string                 `json:"service,omitempty"`
}

// Logger represents a structured logger
type Logger struct {
	level      LogLevel
	writer     io.Writer
	service    string
	enableJSON bool
	mu         sync.RWMutex
	fields     map[string]interface{}
}

// Config holds logger configuration
type Config struct {
	Level      LogLevel
	Writer     io.Writer
	Service    string
	EnableJSON bool
}

// New creates a new Logger instance
func New(config Config) *Logger {
	if config.Writer == nil {
		config.Writer = os.Stdout
	}

	return &Logger{
		level:      config.Level,
		writer:     config.Writer,
		service:    config.Service,
		enableJSON: config.EnableJSON,
		fields:     make(map[string]interface{}),
	}
}

// NewDefault creates a logger with sensible defaults
func NewDefault() *Logger {
	return New(Config{
		Level:      InfoLevel,
		Writer:     os.Stdout,
		Service:    "tjo",
		EnableJSON: true,
	})
}

// WithFields returns a new logger with additional fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	l.mu.RLock()
	newFields := make(map[string]interface{})
	for k, v := range l.fields {
		newFields[k] = v
	}
	l.mu.RUnlock()

	for k, v := range fields {
		newFields[k] = v
	}

	return &Logger{
		level:      l.level,
		writer:     l.writer,
		service:    l.service,
		enableJSON: l.enableJSON,
		fields:     newFields,
	}
}

// WithField returns a new logger with an additional field
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return l.WithFields(map[string]interface{}{key: value})
}

// WithRequestID returns a new logger with request ID
func (l *Logger) WithRequestID(requestID string) *Logger {
	return l.WithField("request_id", requestID)
}

// WithUserID returns a new logger with user ID
func (l *Logger) WithUserID(userID string) *Logger {
	return l.WithField("user_id", userID)
}

// SetLevel sets the minimum log level
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// GetLevel returns the current log level
func (l *Logger) GetLevel() LogLevel {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

// log writes a log entry
func (l *Logger) log(level LogLevel, message string, fields map[string]interface{}) {
	l.mu.RLock()
	if level < l.level {
		l.mu.RUnlock()
		return
	}
	l.mu.RUnlock()

	entry := LogEntry{
		Timestamp: time.Now().UTC(),
		Level:     level.String(),
		Message:   message,
		Service:   l.service,
	}

	// Merge logger fields with entry fields
	l.mu.RLock()
	if len(l.fields) > 0 || len(fields) > 0 {
		entry.Fields = make(map[string]interface{})
		for k, v := range l.fields {
			entry.Fields[k] = v
		}
		for k, v := range fields {
			entry.Fields[k] = v
		}
	}

	// Add request ID if available in fields
	if requestID, ok := entry.Fields["request_id"].(string); ok {
		entry.RequestID = requestID
		delete(entry.Fields, "request_id")
	}

	// Add user ID if available in fields
	if userID, ok := entry.Fields["user_id"].(string); ok {
		entry.UserID = userID
		delete(entry.Fields, "user_id")
	}
	l.mu.RUnlock()

	// Add caller information for debug and above
	if level >= DebugLevel {
		if pc, file, line, ok := runtime.Caller(2); ok {
			if fn := runtime.FuncForPC(pc); fn != nil {
				entry.Caller = fmt.Sprintf("%s:%d", getShortFile(file), line)
			}
		}
	}

	l.writeEntry(entry)

	// Exit for fatal logs
	if level == FatalLevel {
		os.Exit(1)
	}
}

// writeEntry writes the log entry to the output
func (l *Logger) writeEntry(entry LogEntry) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.enableJSON {
		data, _ := json.Marshal(entry)
		fmt.Fprintf(l.writer, "%s\n", data)
	} else {
		// Simple text format for development
		timestamp := entry.Timestamp.Format("2006-01-02 15:04:05")
		caller := ""
		if entry.Caller != "" {
			caller = fmt.Sprintf(" [%s]", entry.Caller)
		}

		fieldsStr := ""
		if len(entry.Fields) > 0 {
			parts := make([]string, 0, len(entry.Fields))
			for k, v := range entry.Fields {
				parts = append(parts, fmt.Sprintf("%s=%v", k, v))
			}
			fieldsStr = fmt.Sprintf(" {%s}", strings.Join(parts, ", "))
		}

		fmt.Fprintf(l.writer, "%s [%s]%s %s%s\n",
			timestamp, entry.Level, caller, entry.Message, fieldsStr)
	}
}

// getShortFile returns the short filename
func getShortFile(file string) string {
	parts := strings.Split(file, "/")
	if len(parts) > 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return file
}

// Trace logs a trace message
func (l *Logger) Trace(message string, fields ...map[string]interface{}) {
	mergedFields := make(map[string]interface{})
	for _, f := range fields {
		for k, v := range f {
			mergedFields[k] = v
		}
	}
	l.log(TraceLevel, message, mergedFields)
}

// Debug logs a debug message
func (l *Logger) Debug(message string, fields ...map[string]interface{}) {
	mergedFields := make(map[string]interface{})
	for _, f := range fields {
		for k, v := range f {
			mergedFields[k] = v
		}
	}
	l.log(DebugLevel, message, mergedFields)
}

// Info logs an info message
func (l *Logger) Info(message string, fields ...map[string]interface{}) {
	mergedFields := make(map[string]interface{})
	for _, f := range fields {
		for k, v := range f {
			mergedFields[k] = v
		}
	}
	l.log(InfoLevel, message, mergedFields)
}

// Warn logs a warning message
func (l *Logger) Warn(message string, fields ...map[string]interface{}) {
	mergedFields := make(map[string]interface{})
	for _, f := range fields {
		for k, v := range f {
			mergedFields[k] = v
		}
	}
	l.log(WarnLevel, message, mergedFields)
}

// Error logs an error message
func (l *Logger) Error(message string, fields ...map[string]interface{}) {
	mergedFields := make(map[string]interface{})
	for _, f := range fields {
		for k, v := range f {
			mergedFields[k] = v
		}
	}
	l.log(ErrorLevel, message, mergedFields)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(message string, fields ...map[string]interface{}) {
	mergedFields := make(map[string]interface{})
	for _, f := range fields {
		for k, v := range f {
			mergedFields[k] = v
		}
	}
	l.log(FatalLevel, message, mergedFields)
}

// Tracef logs a formatted trace message
func (l *Logger) Tracef(format string, args ...interface{}) {
	l.log(TraceLevel, fmt.Sprintf(format, args...), nil)
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.log(DebugLevel, fmt.Sprintf(format, args...), nil)
}

// Infof logs a formatted info message
func (l *Logger) Infof(format string, args ...interface{}) {
	l.log(InfoLevel, fmt.Sprintf(format, args...), nil)
}

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.log(WarnLevel, fmt.Sprintf(format, args...), nil)
}

// Errorf logs a formatted error message
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.log(ErrorLevel, fmt.Sprintf(format, args...), nil)
}

// Fatalf logs a formatted fatal message and exits
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.log(FatalLevel, fmt.Sprintf(format, args...), nil)
}

// Default logger instance
var defaultLogger = NewDefault()

// SetDefaultLogger sets the global default logger
func SetDefaultLogger(logger *Logger) {
	defaultLogger = logger
}

// GetDefaultLogger returns the global default logger
func GetDefaultLogger() *Logger {
	return defaultLogger
}

// Global logging functions using the default logger

// Trace logs a trace message using the default logger
func Trace(message string, fields ...map[string]interface{}) {
	defaultLogger.Trace(message, fields...)
}

// Debug logs a debug message using the default logger
func Debug(message string, fields ...map[string]interface{}) {
	defaultLogger.Debug(message, fields...)
}

// Info logs an info message using the default logger
func Info(message string, fields ...map[string]interface{}) {
	defaultLogger.Info(message, fields...)
}

// Warn logs a warning message using the default logger
func Warn(message string, fields ...map[string]interface{}) {
	defaultLogger.Warn(message, fields...)
}

// Error logs an error message using the default logger
func Error(message string, fields ...map[string]interface{}) {
	defaultLogger.Error(message, fields...)
}

// Fatal logs a fatal message using the default logger and exits
func Fatal(message string, fields ...map[string]interface{}) {
	defaultLogger.Fatal(message, fields...)
}

// Tracef logs a formatted trace message using the default logger
func Tracef(format string, args ...interface{}) {
	defaultLogger.Tracef(format, args...)
}

// Debugf logs a formatted debug message using the default logger
func Debugf(format string, args ...interface{}) {
	defaultLogger.Debugf(format, args...)
}

// Infof logs a formatted info message using the default logger
func Infof(format string, args ...interface{}) {
	defaultLogger.Infof(format, args...)
}

// Warnf logs a formatted warning message using the default logger
func Warnf(format string, args ...interface{}) {
	defaultLogger.Warnf(format, args...)
}

// Errorf logs a formatted error message using the default logger
func Errorf(format string, args ...interface{}) {
	defaultLogger.Errorf(format, args...)
}

// Fatalf logs a formatted fatal message using the default logger and exits
func Fatalf(format string, args ...interface{}) {
	defaultLogger.Fatalf(format, args...)
}

// Context key type for logger
type contextKey string

const (
	loggerContextKey contextKey = "logger"
)

// FromContext retrieves a logger from context or returns the default logger
func FromContext(ctx context.Context) *Logger {
	if logger, ok := ctx.Value(loggerContextKey).(*Logger); ok {
		return logger
	}
	return defaultLogger
}

// ToContext adds a logger to context
func ToContext(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey, logger)
}