package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
)

// Level defines the log level for the error.
type Level string

const (
	LevelNone  Level = "none"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// AppError is the standard error type for the application.
// It wraps the original error and adds context like HTTP status, user message, and log level.
type AppError struct {
	Err        error             // The original error
	StatusCode int               // HTTP Status Code
	Message    string            // User-friendly message
	LogLevel   Level             // Suggestion for logging level
	File       string            // Source file
	Line       int               // Source line
	Fields     map[string]string // specific error fields (e.g. validation)
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

// New creates a generic AppError.
func New(err error, message string, code int) *AppError {
	if err == nil {
		err = errors.New(message)
	}
	
	_, file, line, _ := runtime.Caller(1)

	return &AppError{
		Err:        err,
		StatusCode: code,
		Message:    message,
		LogLevel:   LevelError,
		File:       file,
		Line:       line,
		Fields:     make(map[string]string),
	}
}

// Wrap wraps an existing error into an AppError.
// If the error is already an AppError, it returns it (optionally updating the message).
func Wrap(err error, message string, code int) *AppError {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	
	return New(err, message, code)
}

// WithLog sets the log level.
func (e *AppError) WithLog(level Level) *AppError {
	e.LogLevel = level
	return e
}

// WithField adds a field to the error (useful for validation).
func (e *AppError) WithField(key, value string) *AppError {
	if e.Fields == nil {
		e.Fields = make(map[string]string)
	}
	e.Fields[key] = value
	return e
}

// Common Constructors

func NotFound(message string) *AppError {
	return New(nil, message, http.StatusNotFound).WithLog(LevelInfo)
}

func BadRequest(message string) *AppError {
	return New(nil, message, http.StatusBadRequest).WithLog(LevelWarn)
}

func Internal(err error) *AppError {
	return New(err, "internal server error", http.StatusInternalServerError).WithLog(LevelError)
}

func Unauthorized(message string) *AppError {
	return New(nil, message, http.StatusUnauthorized).WithLog(LevelWarn)
}

func Forbidden(message string) *AppError {
	return New(nil, message, http.StatusForbidden).WithLog(LevelWarn)
}

// Validation Error Helpers

type ValidationError struct {
	*AppError
	Errors map[string][]string
}

func NewValidationError(msg string, fieldErrors map[string][]string) *ValidationError {
	return &ValidationError{
		AppError: &AppError{
			Err:        errors.New("validation failed"),
			StatusCode: http.StatusUnprocessableEntity,
			Message:    msg,
			LogLevel:   LevelWarn,
		},
		Errors: fieldErrors,
	}
}
