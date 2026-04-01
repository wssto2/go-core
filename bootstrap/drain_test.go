package bootstrap

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wssto2/go-core/health"
)

// fakeServerWithRegistry observes registry state when Shutdown is called.
type fakeServerWithRegistry struct {
	hr             *health.HealthRegistry
	shutdownCalled bool
	sawDraining    bool
}

func (f *fakeServerWithRegistry) Start() error {
	f.shutdownCalled = false
	return nil
}

func (f *fakeServerWithRegistry) Shutdown(ctx context.Context) error {
	f.shutdownCalled = true
	f.sawDraining = f.hr.IsDraining()
	return nil
}

func TestReadinessDraining(t *testing.T) {
	cfg := DefaultConfig()
	cfg.I18n.I18nDir = "/tmp/go-core-i18n"
	builder := New(cfg).DefaultInfrastructure()
	app, _ := builder.Build()

	hr, err := Resolve[*health.HealthRegistry](app.container)
	if err != nil {
		t.Fatalf("resolve health registry: %v", err)
	}

	hr.SetDraining(true)

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()
	app.engine.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected /ready 503 when draining, got %d", w.Code)
	}
	var rd map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rd); err != nil {
		t.Fatalf("unmarshal readiness: %v", err)
	}
	if rd["status"] != "draining" {
		t.Fatalf("expected status draining, got %v", rd["status"])
	}
}

func TestAppShutdownSetsDrainingBeforeHTTPShutdown(t *testing.T) {
	cfg := DefaultConfig()
	cfg.I18n.I18nDir = "/tmp/go-core-i18n"
	builder := New(cfg).DefaultInfrastructure()
	app, _ := builder.Build()

	hr, err := Resolve[*health.HealthRegistry](app.container)
	if err != nil {
		t.Fatalf("resolve health registry: %v", err)
	}

	f := &fakeServerWithRegistry{hr: hr}
	app.httpServer = f

	app.Shutdown(slog.Default())

	if !f.shutdownCalled {
		t.Fatalf("expected http server Shutdown to be called")
	}
	if !f.sawDraining {
		t.Fatalf("expected server to observe draining=true before Shutdown, saw false")
	}
}
