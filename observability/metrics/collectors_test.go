package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStandardCollectorsExposed(t *testing.T) {
	m := NewMetrics(nil)
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	m.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "go_gc_duration_seconds") && !strings.Contains(body, "process_cpu_seconds_total") {
		t.Fatalf("expected go or process metric in output, got: %s", body)
	}
}
