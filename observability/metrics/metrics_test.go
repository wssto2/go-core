package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMiddlewareRecordsCounter(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	h := m.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/hello", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	c := m.requestCount.WithLabelValues("GET", "/hello", "201")
	val := testutil.ToFloat64(c)
	if val != 1 {
		t.Fatalf("expected counter 1, got %v", val)
	}
}

func TestHandlerExposesMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	// ensure there's at least one sample so the metrics handler emits it
	m.requestCount.WithLabelValues("GET", "/metrics", "200").Inc()
	h := m.Handler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "go_core_http_requests_total") {
		t.Fatalf("expected metrics output to contain metric name, got: %s", body)
	}
}
