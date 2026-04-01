package bootstrap

import (
	"context"
	"log/slog"
	"testing"
)

// A tiny fake HTTP server used to verify Shutdown is called.
type fakeHTTPServer struct {
	shutdownCalled bool
	startCalled    bool
}

func (f *fakeHTTPServer) Start() error {
	f.startCalled = true
	return nil
}

func (f *fakeHTTPServer) Shutdown(ctx context.Context) error {
	f.shutdownCalled = true
	return nil
}

// testModule is a minimal Module implementation that records shutdown calls.
type testModule struct {
	name       string
	onShutdown func()
	onBoot     func()
}

func (m *testModule) Name() string                { return m.name }
func (m *testModule) Register(c *Container) error { return nil }
func (m *testModule) Boot(ctx context.Context) error {
	if m.onBoot != nil {
		m.onBoot()
	}
	return nil
}
func (m *testModule) Shutdown(ctx context.Context) error {
	if m.onShutdown != nil {
		m.onShutdown()
	}
	return nil
}

func TestShutdownCallsHTTPServerBeforeModules(t *testing.T) {
	cfg := DefaultConfig()
	cfg.I18n.I18nDir = "/tmp/go-core-i18n"
	builder := New(cfg).DefaultInfrastructure()
	app, _ := builder.Build()

	// prepare fake server and modules
	f := &fakeHTTPServer{}
	app.httpServer = f

	var order []string
	m1 := &testModule{name: "m1", onShutdown: func() { order = append(order, "m1") }}
	m2 := &testModule{name: "m2", onShutdown: func() { order = append(order, "m2") }}
	app.modules = []Module{m1, m2}

	app.Shutdown(slog.Default())

	if !f.shutdownCalled {
		t.Fatalf("expected http server Shutdown to be called")
	}

	// modules should be shutdown in reverse order after server shutdown
	if len(order) != 2 {
		t.Fatalf("expected 2 module shutdowns, got %d", len(order))
	}
	if order[0] != "m2" || order[1] != "m1" {
		t.Fatalf("unexpected shutdown order: %v", order)
	}
}
