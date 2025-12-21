package tjo

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrorCode represents categories of errors for classification and handling
type ErrorCode int

const (
	// ErrInternal represents internal server errors
	ErrInternal ErrorCode = iota
	// ErrValidation represents input validation failures
	ErrValidation
	// ErrNotFound represents resource not found errors
	ErrNotFound
	// ErrUnauthorized represents authentication/authorization failures
	ErrUnauthorized
	// ErrForbidden represents access denied errors
	ErrForbidden
	// ErrDatabase represents database operation failures
	ErrDatabase
	// ErrExternal represents external service failures
	ErrExternal
	// ErrConfiguration represents configuration errors
	ErrConfiguration
	// ErrTimeout represents operation timeout errors
	ErrTimeout
)

// String returns the string representation of an ErrorCode
func (c ErrorCode) String() string {
	switch c {
	case ErrInternal:
		return "internal"
	case ErrValidation:
		return "validation"
	case ErrNotFound:
		return "not_found"
	case ErrUnauthorized:
		return "unauthorized"
	case ErrForbidden:
		return "forbidden"
	case ErrDatabase:
		return "database"
	case ErrExternal:
		return "external"
	case ErrConfiguration:
		return "configuration"
	case ErrTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// HTTPStatus returns the appropriate HTTP status code for an ErrorCode
func (c ErrorCode) HTTPStatus() int {
	switch c {
	case ErrValidation:
		return http.StatusBadRequest
	case ErrNotFound:
		return http.StatusNotFound
	case ErrUnauthorized:
		return http.StatusUnauthorized
	case ErrForbidden:
		return http.StatusForbidden
	case ErrTimeout:
		return http.StatusGatewayTimeout
	case ErrExternal:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

// TjoError is the standard error type for the framework.
// It provides structured error information including operation context,
// error classification, and optional additional context.
type TjoError struct {
	// Op is the operation that failed (e.g., "database.query", "auth.validate")
	Op string
	// Err is the underlying error
	Err error
	// Code categorizes the error for handling
	Code ErrorCode
	// Context provides additional error context as key-value pairs
	Context map[string]interface{}
}

// Error implements the error interface
func (e *TjoError) Error() string {
	if e.Op != "" {
		return fmt.Sprintf("%s: %v", e.Op, e.Err)
	}
	return e.Err.Error()
}

// Unwrap returns the underlying error for errors.Is and errors.As
func (e *TjoError) Unwrap() error {
	return e.Err
}

// WithContext adds context to the error and returns it for chaining
func (e *TjoError) WithContext(key string, value interface{}) *TjoError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// WrapError wraps an error with operation context and classification.
// Use this when catching errors from lower-level operations.
func WrapError(op string, err error, code ErrorCode) *TjoError {
	if err == nil {
		return nil
	}
	return &TjoError{
		Op:   op,
		Err:  err,
		Code: code,
	}
}

// NewError creates a new TjoError with a message
func NewError(op string, message string, code ErrorCode) *TjoError {
	return &TjoError{
		Op:   op,
		Err:  errors.New(message),
		Code: code,
	}
}

// IsCode checks if an error is a TjoError with a specific code
func IsCode(err error, code ErrorCode) bool {
	var gErr *TjoError
	if errors.As(err, &gErr) {
		return gErr.Code == code
	}
	return false
}

// GetCode returns the ErrorCode from an error, or ErrInternal if not a TjoError
func GetCode(err error) ErrorCode {
	var gErr *TjoError
	if errors.As(err, &gErr) {
		return gErr.Code
	}
	return ErrInternal
}

// GetHTTPStatus returns the appropriate HTTP status for an error
func GetHTTPStatus(err error) int {
	return GetCode(err).HTTPStatus()
}

// Common pre-defined errors for convenience
var (
	// ErrNotFoundError is returned when a resource is not found
	ErrNotFoundError = NewError("", "resource not found", ErrNotFound)
	// ErrUnauthorizedError is returned when authentication fails
	ErrUnauthorizedError = NewError("", "unauthorized", ErrUnauthorized)
	// ErrForbiddenError is returned when access is denied
	ErrForbiddenError = NewError("", "access denied", ErrForbidden)
	// ErrInternalError is returned for internal server errors
	ErrInternalError = NewError("", "internal server error", ErrInternal)
	// ErrValidationError is returned for validation failures
	ErrValidationError = NewError("", "validation failed", ErrValidation)
)

// ValidationError represents a validation error with field-specific messages
type ValidationError struct {
	TjoError
	Fields map[string]string
}

// NewValidationError creates a new validation error with field messages
func NewValidationError(fields map[string]string) *ValidationError {
	return &ValidationError{
		TjoError: TjoError{
			Op:   "validation",
			Err:  errors.New("validation failed"),
			Code: ErrValidation,
		},
		Fields: fields,
	}
}

// AddField adds a field error and returns the error for chaining
func (e *ValidationError) AddField(field, message string) *ValidationError {
	if e.Fields == nil {
		e.Fields = make(map[string]string)
	}
	e.Fields[field] = message
	return e
}

// HasErrors returns true if there are any validation errors
func (e *ValidationError) HasErrors() bool {
	return len(e.Fields) > 0
}
