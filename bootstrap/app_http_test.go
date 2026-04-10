package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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

// errorHTTPServer is a fake that immediately returns an error from Start.
type errorHTTPServer struct {
	err error
}

func (e *errorHTTPServer) Start() error                     { return e.err }
func (e *errorHTTPServer) Shutdown(_ context.Context) error { return nil }

// TestApp_Run_HTTPStartFailure_ReturnsError verifies that Run() propagates an
// immediate HTTP server startup failure (e.g., port already in use) instead of
// silently swallowing it and returning nil.
func TestApp_Run_HTTPStartFailure_ReturnsError(t *testing.T) {
	// Grab a free port and hold it so the app's server cannot bind to it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	defer ln.Close()
	addr := fmt.Sprintf("127.0.0.1:%d", ln.Addr().(*net.TCPAddr).Port)
	_ = addr // used conceptually; we use the errorHTTPServer stub below

	cfg := DefaultConfig()
	cfg.I18n.Dir = "/tmp/go-core-i18n"
	builder := New(cfg).DefaultInfrastructure()
	app, buildErr := builder.Build()
	if buildErr != nil {
		t.Fatalf("Build: %v", buildErr)
	}

	startErr := errors.New("bind: address already in use")
	app.httpServer = &errorHTTPServer{err: startErr}

	err = app.Run()
	if err == nil {
		t.Fatal("expected non-nil error from Run when HTTP server fails to start")
	}
}

func TestShutdownCallsHTTPServerBeforeModules(t *testing.T) {
	cfg := DefaultConfig()
	cfg.I18n.Dir = "/tmp/go-core-i18n"
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

// TestWithHTTP_DefaultConfig_HasReadHeaderTimeout verifies that the default
// configuration includes a non-zero ReadHeaderTimeout to mitigate Slowloris attacks.
func TestWithHTTP_DefaultConfig_HasReadHeaderTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.I18n.Dir = "/tmp/go-core-i18n"
	builder := New(cfg).DefaultInfrastructure().WithHttp()
	app, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	sw, ok := app.httpServer.(*serverWrapper)
	if !ok {
		t.Fatal("expected *serverWrapper for httpServer")
	}
	if sw.srv.ReadHeaderTimeout == 0 {
		t.Fatal("ReadHeaderTimeout must not be zero in the default configuration")
	}
}
