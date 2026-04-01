package tracing

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStartSpanRecordsFinishedSpan(t *testing.T) {
	tr := NewSimpleTracer()
	ctx := context.Background()
	ctx2, finish := tr.StartSpan(ctx, "test-span")
	if id, ok := TraceIDFromContext(ctx2); !ok || id == "" {
		t.Fatalf("expected trace id in ctx")
	}
	if sid, ok := SpanIDFromContext(ctx2); !ok || sid == "" {
		t.Fatalf("expected span id in ctx")
	}
	finish(nil)
	spans := tr.FinishedSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 finished span, got %d", len(spans))
	}
	if spans[0].Name != "test-span" {
		t.Fatalf("unexpected span name: %s", spans[0].Name)
	}
}

func TestMiddlewareInjectsTraceID(t *testing.T) {
	tr := NewSimpleTracer()
	mw := Middleware(tr, "X-Trace-ID")
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := TraceIDFromContext(r.Context()); !ok || id == "" {
			t.Fatalf("trace id missing in context")
		}
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/hello", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	hdr := rr.Header().Get("X-Trace-ID")
	if hdr == "" {
		t.Fatalf("expected X-Trace-ID header set")
	}
}

func TestMiddlewarePreservesIncomingHeader(t *testing.T) {
	tr := NewSimpleTracer()
	mw := Middleware(tr, "X-Trace-ID")
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := TraceIDFromContext(r.Context())
		if !ok || id == "" {
			t.Fatalf("trace id missing")
		}
		_, err := w.Write([]byte(id))
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Trace-ID", "incoming-123")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if got := rr.Header().Get("X-Trace-ID"); got != "incoming-123" {
		t.Fatalf("expected response header to equal incoming header, got %s", got)
	}
	body, _ := io.ReadAll(rr.Body)
	if string(body) != "incoming-123" {
		t.Fatalf("expected body to contain trace id from context, got %s", string(body))
	}
}
