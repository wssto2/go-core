package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMiddlewareRecordsErrors(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	h := m.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, err := w.Write([]byte("err"))
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}))

	req := httptest.NewRequest("GET", "/err", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 500 {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	c := m.requestErrors.WithLabelValues("GET", "/err", "500")
	val := testutil.ToFloat64(c)
	if val != 1 {
		t.Fatalf("expected error counter 1, got %v", val)
	}
}
