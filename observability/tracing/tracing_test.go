package tracing

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
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

func TestSimpleTracer_ConcurrentFinish_NoRace(t *testing.T) {
	tr := NewSimpleTracer()
	ctx := context.Background()

	const goroutines = 20
	finishFns := make([]func(error), goroutines)
	for i := range finishFns {
		_, finish := tr.StartSpan(ctx, "span")
		finishFns[i] = finish
	}

	var wg sync.WaitGroup
	// 20 goroutines call finish concurrently.
	for _, fn := range finishFns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn(nil)
		}()
	}
	// Another goroutine reads FinishedSpans concurrently.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = tr.FinishedSpans()
		}
	}()

	wg.Wait()
}

func TestMiddlewareInjectsTraceID(t *testing.T) {
	tr := NewSimpleTracer()
	mw := Middleware(tr, "X-Trace-Id")
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
	hdr := rr.Header().Get("X-Trace-Id")
	if hdr == "" {
		t.Fatalf("expected X-Trace-Id header set")
	}
}

func TestMiddlewarePreservesIncomingHeader(t *testing.T) {
	tr := NewSimpleTracer()
	mw := Middleware(tr, "X-Trace-Id")
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
	req.Header.Set("X-Trace-Id", "incoming-123")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if got := rr.Header().Get("X-Trace-Id"); got != "incoming-123" {
		t.Fatalf("expected response header to equal incoming header, got %s", got)
	}
	body, _ := io.ReadAll(rr.Body)
	if string(body) != "incoming-123" {
		t.Fatalf("expected body to contain trace id from context, got %s", string(body))
	}
}
