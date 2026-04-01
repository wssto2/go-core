package database

import (
	"testing"

	"github.com/wssto2/go-core/observability/tracing"
)

func TestRegisterTracingRawExec(t *testing.T) {
	reg, cleanup := NewTestRegistry("local")
	defer func() { _ = cleanup() }()

	conn := reg.MustGet("local")
	tr := tracing.NewSimpleTracer()
	err := EnableDBTracing(conn, tr)
	if err != nil {
		t.Fatalf("EnableDBTracing failed: %v", err)
	}

	// Exec a simple raw SQL statement. This should trigger the raw callback
	// and result in a finished span recorded by the tracer.
	if err := conn.Exec("SELECT 1").Error; err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	spans := tr.FinishedSpans()
	if len(spans) == 0 {
		t.Fatalf("expected spans to be recorded, got none")
	}

	found := false
	for _, s := range spans {
		if s.Name == "db.raw" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a db.raw span, got %v", spans)
	}
}
