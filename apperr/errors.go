// Package apperr defines the standard structured error type used across go-core
// and all consuming services.
//
// All errors that cross a package boundary or reach the HTTP layer must be
// *AppError values. Use the constructor helpers:
//
//	apperr.BadRequest("invalid input")
//	apperr.NotFound("user not found")
//	apperr.Internal(err)
//	apperr.Wrap(err, "context message", apperr.CodeInternal)
//
// The error carries a semantic Code (used to map to HTTP status), a
// user-friendly Message, a LogLevel hint, and the source file/line for
// diagnostics.
package apperr

import (
	"errors"
	"fmt"
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
	Err      error             // The original error
	Code     Code              // Semantic error code
	Message  string            // User-friendly message
	LogLevel Level             // Suggestion for logging level
	Fields   map[string]string // Optional field-level detail (e.g. for 409 conflicts)
	File     string            // Source file
	Line     int               // Source line
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

func newWithSkip(err error, message string, code Code, skip int) *AppError {
	_, file, line, _ := runtime.Caller(skip)
	return &AppError{
		Err:      err,
		Code:     code,
		Message:  message,
		LogLevel: LevelError,
		File:     file,
		Line:     line,
	}
}

// New creates a generic AppError.
func New(err error, message string, code Code) *AppError {
	return newWithSkip(err, message, code, 2)
}

// Wrap wraps err in a new AppError with the given message and code.
// If err is already an AppError, it is preserved as the cause.
// Unlike the old behavior, the new message and code are ALWAYS applied —
// use this when you want to add context at a higher layer.
func Wrap(err error, message string, code Code) *AppError {
	if err == nil {
		return New(nil, message, code)
	}
	_, file, line, _ := runtime.Caller(1)
	return &AppError{
		Err:      err, // preserve original as cause, accessible via errors.As/Is
		Code:     code,
		Message:  message,
		LogLevel: LevelError,
		File:     file,
		Line:     line,
	}
}

// WrapPreserve wraps err but preserves the original AppError's status code and log level.
// Use this when you want to add a message without changing how the error is classified.
func WrapPreserve(err error, message string) *AppError {
	var appErr *AppError
	if errors.As(err, &appErr) {
		_, file, line, _ := runtime.Caller(1)
		return &AppError{
			Err:      err,
			Code:     appErr.Code,
			Message:  message + ": " + appErr.Message,
			LogLevel: appErr.LogLevel,
			File:     file,
			Line:     line,
		}
	}
	return New(err, message, CodeInternal)
}

// WithLog sets the log level.
func (e *AppError) WithLog(level Level) *AppError {
	e.LogLevel = level
	return e
}

// WithFields attaches field-level detail to the error (e.g. {"code": "already taken"}).
// The error handler will include these in the response body.
func (e *AppError) WithFields(fields map[string]string) *AppError {
	e.Fields = fields
	return e
}

// Common Constructors

func NotFound(message string) *AppError {
	return New(nil, message, CodeNotFound).WithLog(LevelInfo)
}

func BadRequest(message string) *AppError {
	return New(nil, message, CodeBadRequest).WithLog(LevelWarn)
}

func Internal(err error) *AppError {
	return New(err, "internal server error", CodeInternal).WithLog(LevelError)
}

func Unauthorized(message string) *AppError {
	return New(nil, message, CodeUnauthenticated).WithLog(LevelWarn)
}

func Forbidden(message string) *AppError {
	return New(nil, message, CodePermissionDenied).WithLog(LevelWarn)
}
