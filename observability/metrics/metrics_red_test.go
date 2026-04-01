package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

func findCounterValue(t *testing.T, mfs []*dto.MetricFamily, name string, labels map[string]string) float64 {
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, metric := range mf.GetMetric() {
			ok := true
			for k, v := range labels {
				found := false
				for _, lp := range metric.GetLabel() {
					if lp.GetName() == k && lp.GetValue() == v {
						found = true
						break
					}
				}
				if !found {
					ok = false
					break
				}
			}
			if ok {
				if metric.GetCounter() == nil {
					t.Fatalf("metric %s has no counter", name)
				}
				return metric.GetCounter().GetValue()
			}
		}
	}
	t.Fatalf("metric sample %s %+v not found", name, labels)
	return 0
}

func findHistogramCount(t *testing.T, mfs []*dto.MetricFamily, name string, labels map[string]string) uint64 {
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, metric := range mf.GetMetric() {
			ok := true
			for k, v := range labels {
				found := false
				for _, lp := range metric.GetLabel() {
					if lp.GetName() == k && lp.GetValue() == v {
						found = true
						break
					}
				}
				if !found {
					ok = false
					break
				}
			}
			if ok {
				if metric.GetHistogram() == nil {
					t.Fatalf("metric %s has no histogram", name)
				}
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}
	t.Fatalf("histogram sample %s %+v not found", name, labels)
	return 0
}

func TestMetricsMiddlewareCountsAndDuration(t *testing.T) {
	m := NewMetrics(nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/foo", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("ok"))
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte("err"))
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	srv := httptest.NewServer(m.Middleware()(mux))
	defer srv.Close()

	// Make requests
	for range 2 {
		resp, err := http.Get(srv.URL + "/foo")
		if err != nil {
			t.Fatalf("get foo: %v", err)
		}
		err = resp.Body.Close()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}
	resp, err := http.Get(srv.URL + "/error")
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	err = resp.Body.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Gather metrics
	mfs, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	// Verify request counts
	vc := findCounterValue(t, mfs, "go_core_http_requests_total", map[string]string{"method": "GET", "path": "/foo", "status": "200"})
	if vc != 2 {
		t.Fatalf("expected 2 requests for /foo, got %v", vc)
	}

	// Verify error count
	ve := findCounterValue(t, mfs, "go_core_http_errors_total", map[string]string{"method": "GET", "path": "/error", "status": "500"})
	if ve != 1 {
		t.Fatalf("expected 1 error for /error, got %v", ve)
	}

	// Verify histogram count for /foo
	hc := findHistogramCount(t, mfs, "go_core_http_request_duration_seconds", map[string]string{"method": "GET", "path": "/foo"})
	if hc < 2 {
		t.Fatalf("expected histogram sample count >=2 for /foo, got %d", hc)
	}
}
