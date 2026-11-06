//nolint:testpackage,exhaustruct,exhaustruct_v5,paralleltest,varnamelen // the unexported builder
package gocore

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDefaultInfrastructureRegistersMetricsEndpoint(t *testing.T) {
	cfg := DefaultConfig()
	cfg.I18n.Dir = t.TempDir()
	b := newBuilder(cfg).DefaultInfrastructure()

	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	b.engine.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
}

func TestMetricsAccessDecidesWhoReadsMetrics(t *testing.T) {
	for name, tc := range map[string]struct {
		opt  Option
		want int
	}{
		"default": {nil, 200},
		"allowed": {WithMetricsAccess(func(*http.Request) bool { return true }), 200},
		"header":  {WithMetricsAccess(func(r *http.Request) bool { return r.Header.Get("X-Scrape") == "yes" }), 404},
		"without": {WithoutMetrics(), 404},
	} {
		cfg := DefaultConfig()
		cfg.I18n.Dir = t.TempDir()
		b := newBuilder(cfg)

		if tc.opt != nil {
			app := &App{}
			tc.opt(app)
			b.metricsAccess = app.metricsAccess
		}

		b.DefaultInfrastructure()

		rr := httptest.NewRecorder()
		b.engine.ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))

		if rr.Code != tc.want {
			t.Fatalf("%s: status %d, want %d", name, rr.Code, tc.want)
		}
	}
}
