package logger

import (
	"context"
	"log/slog"

	"github.com/wssto2/go-core/observability/tracing"
)

type contextKey string

const (
	ctxKeySourceFile contextKey = "source_file"
	ctxKeySourceLine contextKey = "source_line"
	ctxKeyUserID     contextKey = "user_id"
	ctxKeyRequestID  contextKey = "request_id"
)

// WithSource adds source file/line to the context for logging.
func WithSource(ctx context.Context, file string, line int) context.Context {
	ctx = context.WithValue(ctx, ctxKeySourceFile, file)
	return context.WithValue(ctx, ctxKeySourceLine, line)
}

// WithUser adds user ID to the context for logging.
func WithUser(ctx context.Context, userID int) context.Context {
	return context.WithValue(ctx, ctxKeyUserID, userID)
}

// WithRequestID adds request ID to the context for logging.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, requestID)
}

// SourceHandler wraps a slog.Handler and extracts specific keys from context.
type SourceHandler struct {
	slog.Handler
}

func NewSourceHandler(h slog.Handler) *SourceHandler {
	return &SourceHandler{h}
}

func (h *SourceHandler) Handle(ctx context.Context, record slog.Record) error {
	// Extract values from context
	sourceFile, hasSourceFile := ctx.Value(ctxKeySourceFile).(string)
	sourceLine, hasSourceLine := ctx.Value(ctxKeySourceLine).(int)
	userID, hasUserID := ctx.Value(ctxKeyUserID).(int)
	requestID, hasRequestID := ctx.Value(ctxKeyRequestID).(string)

	// Extract trace/span if present using the tracing helper
	traceID, hasTrace := tracing.TraceIDFromContext(ctx)
	spanID, hasSpan := tracing.SpanIDFromContext(ctx)

	// If no context values, just pass through
	if !hasSourceFile && !hasSourceLine && !hasUserID && !hasRequestID && !hasTrace && !hasSpan {
		return h.Handler.Handle(ctx, record)
	}

	// Create new record with additional attributes
	newRecord := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)

	// Copy existing attributes
	record.Attrs(func(attr slog.Attr) bool {
		newRecord.AddAttrs(attr)
		return true
	})

	// Add context attributes
	if hasSourceFile {
		newRecord.AddAttrs(slog.String("source_file", sourceFile))
	}
	if hasSourceLine {
		newRecord.AddAttrs(slog.Int("source_line", sourceLine))
	}
	if hasUserID {
		newRecord.AddAttrs(slog.Int("user_id", userID))
	}
	if hasRequestID {
		newRecord.AddAttrs(slog.String("request_id", requestID))
	}
	if hasTrace {
		newRecord.AddAttrs(slog.String("trace_id", traceID))
	}
	if hasSpan {
		newRecord.AddAttrs(slog.String("span_id", spanID))
	}

	return h.Handler.Handle(ctx, newRecord)
}
