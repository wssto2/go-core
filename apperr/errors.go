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
	"strings"
)

// apperrPkgPrefix identifies stack frames belonging to this package so they
// can be skipped when locating the real caller site.
const apperrPkgPrefix = "github.com/wssto2/go-core/apperr."

const maxStackDepth = 32

// Level defines the log level for the error.
type Level string

const (
	LevelNone  Level = "none"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Reason is a stable, machine-readable identifier for a specific error case
// (e.g. "vehicle.vin_already_exists"). Unlike Code — which is a coarse
// category used to derive the HTTP status — Reason identifies the exact
// business rule that failed, so clients can translate it and translations
// don't break when the English Message wording changes.
//
// Reasons are part of the API contract: namespace them ("vehicle.*",
// "auth.*") and never rename them casually.
type Reason string

// AppError is the standard error type for the application.
// It wraps the original error and adds context like HTTP status, user message, and log level.
type AppError struct {
	Err      error             // The original error
	Code     Code              // Semantic error code
	Reason   Reason            // Stable machine-readable identifier for the specific case (optional)
	Params   map[string]any    // Interpolation params for translating Reason (optional)
	Message  string            // User-friendly message
	LogLevel Level             // Suggestion for logging level
	Fields   map[string]string // Optional field-level detail (e.g. for 409 conflicts)
	File     string            // Source file of the real call site (outside this package)
	Line     int               // Source line of the real call site
	Stack    []string          // Call stack ("file:line" entries), outermost first, excluding this package's own frames
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

// captureStack walks the call stack starting at its caller, skipping any
// leading frames that belong to this package (so wrapper functions like
// Internal/NotFound/BadRequest don't themselves show up as the call site),
// and returns the first frame outside the package plus the full remaining
// stack.
func captureStack() (file string, line int, stack []string) {
	var pcs [maxStackDepth]uintptr
	n := runtime.Callers(2, pcs[:]) // skip runtime.Callers and captureStack itself
	frames := runtime.CallersFrames(pcs[:n])

	skippingInternal := true
	for {
		frame, more := frames.Next()
		if skippingInternal && strings.HasPrefix(frame.Function, apperrPkgPrefix) {
			if !more {
				break
			}
			continue
		}
		skippingInternal = false

		stack = append(stack, fmt.Sprintf("%s:%d", frame.File, frame.Line))
		if file == "" {
			file, line = frame.File, frame.Line
		}
		if !more {
			break
		}
	}
	return file, line, stack
}

func newWithSkip(err error, message string, code Code) *AppError {
	file, line, stack := captureStack()
	return &AppError{
		Err:      err,
		Code:     code,
		Message:  message,
		LogLevel: LevelError,
		File:     file,
		Line:     line,
		Stack:    stack,
	}
}

// New creates a generic AppError.
func New(err error, message string, code Code) *AppError {
	return newWithSkip(err, message, code)
}

// Wrap wraps err in a new AppError with the given message and code.
// If err is already an AppError, it is preserved as the cause.
// Unlike the old behavior, the new message and code are ALWAYS applied —
// use this when you want to add context at a higher layer.
func Wrap(err error, message string, code Code) *AppError {
	if err == nil {
		return New(nil, message, code)
	}
	file, line, stack := captureStack()
	return &AppError{
		Err:      err, // preserve original as cause, accessible via errors.As/Is
		Code:     code,
		Message:  message,
		LogLevel: LevelError,
		File:     file,
		Line:     line,
		Stack:    stack,
	}
}

// WrapPreserve wraps err but preserves the original AppError's status code and log level.
// Use this when you want to add a message without changing how the error is classified.
func WrapPreserve(err error, message string) *AppError {
	var appErr *AppError
	if errors.As(err, &appErr) {
		file, line, stack := captureStack()
		return &AppError{
			Err:      err,
			Code:     appErr.Code,
			Reason:   appErr.Reason,
			Params:   appErr.Params,
			Message:  message + ": " + appErr.Message,
			LogLevel: appErr.LogLevel,
			File:     file,
			Line:     line,
			Stack:    stack,
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

func HasCode(err error, code Code) bool {
	var appErr *AppError
	return errors.As(err, &appErr) && appErr.Code == code
}

// WithReason attaches a stable machine-readable reason (and optional
// interpolation params) to the error, so clients can translate the specific
// error case. Params beyond the first map are merged into the first.
//
//	apperr.BadRequest("vehicle with this VIN already exists").
//		WithReason("vehicle.vin_already_exists", map[string]any{"vin": vin})
func (e *AppError) WithReason(reason Reason, params ...map[string]any) *AppError {
	e.Reason = reason
	for _, p := range params {
		if e.Params == nil {
			e.Params = make(map[string]any, len(p))
		}
		for k, v := range p {
			e.Params[k] = v
		}
	}
	return e
}

// HasReason reports whether err (or anything it wraps) is an AppError with the
// given Reason.
func HasReason(err error, reason Reason) bool {
	var appErr *AppError
	return errors.As(err, &appErr) && appErr.Reason == reason
}

// Reasoner is implemented by domain error types that carry a stable,
// machine-readable reason and optional interpolation params.
//
// The reason is returned as a plain string (not Reason) so domain packages can
// implement this interface structurally without importing apperr:
//
//	type ErrVINAlreadyExists struct{ VIN string }
//
//	func (e ErrVINAlreadyExists) Error() string {
//		return "vehicle with VIN " + e.VIN + " already exists"
//	}
//
//	func (e ErrVINAlreadyExists) ErrorReason() (string, map[string]any) {
//		return "vehicle.vin_already_exists", map[string]any{"vin": e.VIN}
//	}
type Reasoner interface {
	ErrorReason() (reason string, params map[string]any)
}

// fromErr builds an AppError from a (typically domain) error. The message is
// taken from err.Error(), and if err (or anything it wraps) implements
// Reasoner, Reason and Params are extracted automatically.
func fromErr(err error, code Code) *AppError {
	message := "unknown error"
	if err != nil {
		message = err.Error()
	}
	ae := newWithSkip(err, message, code)
	var r Reasoner
	if errors.As(err, &r) {
		reason, params := r.ErrorReason()
		ae.Reason = Reason(reason)
		ae.Params = params
	}
	return ae
}

// Typed-error constructors. Prefer these over the string constructors when the
// error case is a domain rule the frontend needs to identify/translate.

// BadRequestErr wraps a domain error as CodeBadRequest, extracting
// Reason/Params if it implements Reasoner.
func BadRequestErr(err error) *AppError {
	return fromErr(err, CodeBadRequest).WithLog(LevelWarn)
}

// NotFoundErr wraps a domain error as CodeNotFound, extracting Reason/Params
// if it implements Reasoner.
func NotFoundErr(err error) *AppError {
	return fromErr(err, CodeNotFound).WithLog(LevelInfo)
}

// ForbiddenErr wraps a domain error as CodePermissionDenied, extracting
// Reason/Params if it implements Reasoner.
func ForbiddenErr(err error) *AppError {
	return fromErr(err, CodePermissionDenied).WithLog(LevelWarn)
}

// ConflictErr wraps a domain error as CodeAlreadyExists, extracting
// Reason/Params if it implements Reasoner.
func ConflictErr(err error) *AppError {
	return fromErr(err, CodeAlreadyExists).WithLog(LevelWarn)
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
