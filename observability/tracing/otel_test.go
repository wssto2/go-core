package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInitOpenTelemetry_StartSpan(t *testing.T) {
	ctx := context.Background()
	tr, shutdown, err := InitOpenTelemetry(ctx, OTelConfig{ServiceName: "go-core-test", Exporter: ExporterNoop})
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	defer func() {
		err = shutdown(ctx)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}()

	ctx2, finish := tr.StartSpan(ctx, "otel-span")
	if tid, ok := TraceIDFromContext(ctx2); !ok || tid == "" {
		t.Fatalf("expected trace id in context")
	}
	if sid, ok := SpanIDFromContext(ctx2); !ok || sid == "" {
		t.Fatalf("expected span id in context")
	}
	finish(nil)
}

func TestOTelMiddlewareInjectsTraceID(t *testing.T) {
	ctx := context.Background()
	tr, shutdown, err := InitOpenTelemetry(ctx, OTelConfig{ServiceName: "go-core-test", Exporter: ExporterNoop})
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	defer func() {
		err = shutdown(ctx)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}()

	mw := Middleware(tr, "X-Trace-ID")
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := TraceIDFromContext(r.Context()); !ok || id == "" {
			t.Fatalf("trace id missing in context")
		}
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/ok", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if hdr := rr.Header().Get("X-Trace-ID"); hdr == "" {
		t.Fatalf("expected X-Trace-ID header set")
	}
}

func TestInitOpenTelemetry_NoopExporter_NoError(t *testing.T) {
	ctx := context.Background()
	tr, shutdown, err := InitOpenTelemetry(ctx, OTelConfig{ServiceName: "test-svc", Exporter: ExporterNoop})
	if err != nil {
		t.Fatalf("noop exporter init failed: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil OTelTracer")
	}
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown failed: %v", err)
	}
}

func TestInitOpenTelemetry_StdoutExporter_NoError(t *testing.T) {
	ctx := context.Background()
	tr, shutdown, err := InitOpenTelemetry(ctx, OTelConfig{ServiceName: "test-svc", Exporter: ExporterStdout})
	if err != nil {
		t.Fatalf("stdout exporter init failed: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil OTelTracer")
	}
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown failed: %v", err)
	}
}

func TestInitOpenTelemetry_EmptyExporter_DefaultsToNoop(t *testing.T) {
	ctx := context.Background()
	_, shutdown, err := InitOpenTelemetry(ctx, OTelConfig{ServiceName: "test-svc"})
	if err != nil {
		t.Fatalf("empty exporter should default to noop, got error: %v", err)
	}
	_ = shutdown(ctx)
}
