package tracing

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Tracer provides a minimal tracing surface: start a span and receive a finish func.
type Tracer interface {
	StartSpan(ctx context.Context, name string) (context.Context, func(err error))
}

// context keys
type ctxKey string

const (
	traceIDKey ctxKey = "trace_id"
	spanIDKey  ctxKey = "span_id"
)

// TraceIDFromContext retrieves the trace id stored in the context, if any.
func TraceIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(traceIDKey).(string)
	return v, ok && v != ""
}

// SpanIDFromContext retrieves the span id stored in the context, if any.
func SpanIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(spanIDKey).(string)
	return v, ok && v != ""
}

// SpanRecord stores basic span information for tests and inspection.
type SpanRecord struct {
	TraceID string
	SpanID  string
	Name    string
	Start   time.Time
	End     time.Time
	Errored bool
}

// SimpleTracer is an in-memory tracer useful for tests and as a default.
// It is intentionally minimal and not a full OpenTelemetry replacement.
type SimpleTracer struct {
	mu       sync.Mutex
	spans    map[string]*SpanRecord
	finished []*SpanRecord
}

// NewSimpleTracer constructs a new SimpleTracer instance.
func NewSimpleTracer() *SimpleTracer {
	return &SimpleTracer{spans: make(map[string]*SpanRecord)}
}

// StartSpan starts a new span. If ctx doesn't contain a trace id one is generated.
// Returns a derived context (containing trace/span ids) and a finish func to call
// when the span ends.
func (t *SimpleTracer) StartSpan(ctx context.Context, name string) (context.Context, func(err error)) {
	traceID, ok := TraceIDFromContext(ctx)
	if !ok || traceID == "" {
		traceID = uuid.NewString()
		ctx = context.WithValue(ctx, traceIDKey, traceID)
	}
	spanID := uuid.NewString()
	ctx = context.WithValue(ctx, spanIDKey, spanID)
	rec := &SpanRecord{TraceID: traceID, SpanID: spanID, Name: name, Start: time.Now()}
	t.mu.Lock()
	t.spans[spanID] = rec
	t.mu.Unlock()

	finish := func(err error) {
		rec.End = time.Now()
		if err != nil {
			rec.Errored = true
		}
		t.mu.Lock()
		delete(t.spans, spanID)
		t.finished = append(t.finished, rec)
		t.mu.Unlock()
	}
	return ctx, finish
}

// FinishedSpans returns a copy of finished spans.
func (t *SimpleTracer) FinishedSpans() []*SpanRecord {
	t.mu.Lock()
	sel := make([]*SpanRecord, len(t.finished))
	copy(sel, t.finished)
	t.mu.Unlock()
	return sel
}

// Middleware returns an HTTP middleware that ensures a trace id is present on the
// request context and response header, and starts a span for the request. headerName
// defaults to "X-Trace-ID" when empty.
func Middleware(tr Tracer, headerName string) func(next http.Handler) http.Handler {
	if headerName == "" {
		headerName = "X-Trace-ID"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if id := r.Header.Get(headerName); id != "" {
				ctx = context.WithValue(ctx, traceIDKey, id)
			}
			// start span using method+path as name
			name := r.Method + " " + r.URL.Path
			ctx, finish := tr.StartSpan(ctx, name)
			// ensure header is set to the trace id available in context
			if tid, ok := TraceIDFromContext(ctx); ok {
				w.Header().Set(headerName, tid)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
			finish(nil)
		})
	}
}
