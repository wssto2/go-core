package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/wssto2/go-core/observability/tracing"
)

func TestSourceHandlerAddsTraceID(t *testing.T) {
	var buf bytes.Buffer
	h := NewSourceHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{AddSource: false}))
	log := slog.New(h)

	tr := tracing.NewSimpleTracer()
	ctx, finish := tr.StartSpan(context.Background(), "test-span")
	log.Log(ctx, slog.LevelInfo, "hello")
	finish(nil)

	var out map[string]interface{}
	dec := json.NewDecoder(&buf)
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
	if _, ok := out["trace_id"]; !ok {
		t.Fatalf("expected trace_id in logged output")
	}
	if _, ok := out["span_id"]; !ok {
		t.Fatalf("expected span_id in logged output")
	}
}
