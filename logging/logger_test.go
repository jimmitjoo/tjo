package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		level    LogLevel
		expected string
	}{
		{"trace", TraceLevel, "TRACE"},
		{"debug", DebugLevel, "DEBUG"},
		{"info", InfoLevel, "INFO"},
		{"warn", WarnLevel, "WARN"},
		{"error", ErrorLevel, "ERROR"},
		{"fatal", FatalLevel, "FATAL"},
		{"unknown", LogLevel(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.String())
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected LogLevel
	}{
		{"trace", "TRACE", TraceLevel},
		{"debug", "DEBUG", DebugLevel},
		{"info", "INFO", InfoLevel},
		{"warn", "WARN", WarnLevel},
		{"warning", "WARNING", WarnLevel},
		{"error", "ERROR", ErrorLevel},
		{"fatal", "FATAL", FatalLevel},
		{"lowercase", "info", InfoLevel},
		{"unknown", "UNKNOWN", InfoLevel},
		{"empty", "", InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseLogLevel(tt.input))
		})
	}
}

func TestLoggerNew(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:      DebugLevel,
		Writer:     &buf,
		Service:    "test-service",
		EnableJSON: true,
	})

	assert.NotNil(t, logger)
	assert.Equal(t, DebugLevel, logger.level)
	assert.Equal(t, "test-service", logger.service)
	assert.True(t, logger.enableJSON)
}

func TestLoggerNewDefault(t *testing.T) {
	logger := NewDefault()
	
	assert.NotNil(t, logger)
	assert.Equal(t, InfoLevel, logger.level)
	assert.Equal(t, "tjo", logger.service)
	assert.True(t, logger.enableJSON)
}

func TestLoggerWithFields(t *testing.T) {
	logger := NewDefault()
	
	// Add fields
	logger1 := logger.WithFields(map[string]interface{}{
		"user_id": 123,
		"session": "abc",
	})
	
	// Add more fields
	logger2 := logger1.WithField("request_id", "req-456")
	
	assert.Len(t, logger.fields, 0)
	assert.Len(t, logger1.fields, 2)
	assert.Len(t, logger2.fields, 3)
	
	assert.Equal(t, 123, logger1.fields["user_id"])
	assert.Equal(t, "abc", logger1.fields["session"])
	assert.Equal(t, "req-456", logger2.fields["request_id"])
}

func TestLoggerWithRequestIDAndUserID(t *testing.T) {
	logger := NewDefault()
	
	logger1 := logger.WithRequestID("req-123")
	logger2 := logger1.WithUserID("user-456")
	
	assert.Equal(t, "req-123", logger1.fields["request_id"])
	assert.Equal(t, "user-456", logger2.fields["user_id"])
}

func TestLoggerSetAndGetLevel(t *testing.T) {
	logger := NewDefault()
	
	// Default is InfoLevel
	assert.Equal(t, InfoLevel, logger.GetLevel())
	
	// Set to DebugLevel
	logger.SetLevel(DebugLevel)
	assert.Equal(t, DebugLevel, logger.GetLevel())
}

func TestLoggerJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:      InfoLevel,
		Writer:     &buf,
		Service:    "test",
		EnableJSON: true,
	})

	logger.Info("test message", map[string]interface{}{
		"key": "value",
		"num": 42,
	})

	var logEntry LogEntry
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "INFO", logEntry.Level)
	assert.Equal(t, "test message", logEntry.Message)
	assert.Equal(t, "test", logEntry.Service)
	assert.Equal(t, "value", logEntry.Fields["key"])
	assert.Equal(t, float64(42), logEntry.Fields["num"]) // JSON unmarshals numbers as float64
	assert.NotZero(t, logEntry.Timestamp)
}

func TestLoggerTextOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:      InfoLevel,
		Writer:     &buf,
		Service:    "test",
		EnableJSON: false,
	})

	logger.Info("test message", map[string]interface{}{
		"key": "value",
	})

	output := buf.String()
	assert.Contains(t, output, "[INFO]")
	assert.Contains(t, output, "test message")
	assert.Contains(t, output, "key=value")
}

func TestLoggerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:      WarnLevel,
		Writer:     &buf,
		EnableJSON: true,
	})

	// These should be filtered out
	logger.Trace("trace message")
	logger.Debug("debug message")
	logger.Info("info message")

	// These should be logged
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	
	// Should have 2 lines (warn and error)
	assert.Len(t, lines, 2)
	assert.Contains(t, output, "warn message")
	assert.Contains(t, output, "error message")
}

func TestLoggerFormattedMethods(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:      InfoLevel,
		Writer:     &buf,
		EnableJSON: true,
	})

	logger.Infof("formatted message: %s %d", "hello", 42)

	var logEntry LogEntry
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "INFO", logEntry.Level)
	assert.Equal(t, "formatted message: hello 42", logEntry.Message)
}

func TestLoggerCallerInformation(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:      DebugLevel,
		Writer:     &buf,
		EnableJSON: true,
	})

	logger.Debug("debug with caller")

	var logEntry LogEntry
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.NotEmpty(t, logEntry.Caller)
	assert.Contains(t, logEntry.Caller, "logger_test.go")
}

func TestLoggerRequestIDAndUserID(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:      InfoLevel,
		Writer:     &buf,
		EnableJSON: true,
	})

	logger.WithRequestID("req-123").WithUserID("user-456").Info("test message")

	var logEntry LogEntry
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "req-123", logEntry.RequestID)
	assert.Equal(t, "user-456", logEntry.UserID)
	
	// These should not appear in Fields since they're extracted
	assert.NotContains(t, logEntry.Fields, "request_id")
	assert.NotContains(t, logEntry.Fields, "user_id")
}

func TestLoggerContext(t *testing.T) {
	logger := NewDefault()
	ctx := context.Background()
	
	// Test adding logger to context
	ctx = ToContext(ctx, logger)
	retrievedLogger := FromContext(ctx)
	
	assert.Equal(t, logger, retrievedLogger)
	
	// Test context without logger returns default
	emptyCtx := context.Background()
	defaultLogger := FromContext(emptyCtx)
	assert.Equal(t, GetDefaultLogger(), defaultLogger)
}

func TestGlobalLoggingFunctions(t *testing.T) {
	var buf bytes.Buffer
	
	// Set a custom default logger for testing
	testLogger := New(Config{
		Level:      InfoLevel,
		Writer:     &buf,
		EnableJSON: true,
	})
	SetDefaultLogger(testLogger)
	
	// Test global function
	Info("global info message", map[string]interface{}{"test": true})
	
	var logEntry LogEntry
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)
	
	assert.Equal(t, "INFO", logEntry.Level)
	assert.Equal(t, "global info message", logEntry.Message)
	assert.Equal(t, true, logEntry.Fields["test"])
	
	// Test global formatted function
	buf.Reset()
	Infof("formatted: %d", 123)
	
	err = json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)
	assert.Equal(t, "formatted: 123", logEntry.Message)
}

func TestLoggerConcurrency(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:      InfoLevel,
		Writer:     &buf,
		EnableJSON: true,
	})
	
	done := make(chan bool, 10)
	
	// Run multiple goroutines logging concurrently
	for i := 0; i < 10; i++ {
		go func(id int) {
			logger.Infof("concurrent log %d", id)
			done <- true
		}(i)
	}
	
	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
	
	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	
	// Should have 10 log lines
	assert.Len(t, lines, 10)
	
	// Each line should be valid JSON
	for _, line := range lines {
		var logEntry LogEntry
		err := json.Unmarshal([]byte(line), &logEntry)
		assert.NoError(t, err)
		assert.Equal(t, "INFO", logEntry.Level)
		assert.Contains(t, logEntry.Message, "concurrent log")
	}
}

func TestLoggerFieldMerging(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:      InfoLevel,
		Writer:     &buf,
		EnableJSON: true,
	})
	
	// Create logger with base fields
	baseLogger := logger.WithFields(map[string]interface{}{
		"service": "test",
		"version": "1.0",
	})
	
	// Log with additional fields
	baseLogger.Info("test message", map[string]interface{}{
		"request": "123",
		"version": "2.0", // Should override base field
	})
	
	var logEntry LogEntry
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)
	
	assert.Equal(t, "test", logEntry.Fields["service"])
	assert.Equal(t, "2.0", logEntry.Fields["version"]) // Overridden value
	assert.Equal(t, "123", logEntry.Fields["request"])
}