package bootstrap

import (
	"net/http/httptest"
	"testing"
)

func TestDefaultInfrastructureRegistersMetricsEndpoint(t *testing.T) {
	cfg := DefaultConfig()
	cfg.I18n.I18nDir = t.TempDir()
	b := New(cfg).DefaultInfrastructure()

	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	b.engine.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
}
