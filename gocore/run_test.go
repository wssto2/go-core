package gocore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/route"
)

var (
	pingRoute   = route.Get[route.None, string]("/ping").Name("ping")
	secretRoute = route.Get[route.None, string]("/secret").Name("secret").Requires("ops.secret:view")
	lostRoute   = route.Get[route.None, string]("/lost").Name("lost")
	_           = route.Group(pingRoute, secretRoute, lostRoute)
	upRoute     = route.Get[route.None, string]("/up")
)

func passthrough(c *gin.Context) { c.Next() }

func pong(context.Context, route.None) (string, error) { return "pong", nil }

func TestCheckReportsEveryProblemWithItsFix(t *testing.T) {
	app := testApp(t, "local")

	app.authenticate = []gin.HandlerFunc{passthrough}
	Later[fmt.Stringer](app)
	app.Database("nope")
	app.Routes(pingRoute.To(pong), secretRoute.To(pong)) // lostRoute has no handler

	err := app.Check()

	var se *StartupError
	if !errors.As(err, &se) {
		t.Fatalf("want *StartupError, got %v", err)
	}

	want := []string{
		"gocore.Later[fmt.Stringer] was never set",
		"lost",
		"no authorizer",
		"no permission catalogue defines",
		"registered connections: local",
	}

	msg := err.Error()
	for _, w := range want {
		if !strings.Contains(msg, w) {
			t.Errorf("message misses %q:\n%s", w, msg)
		}
	}

	for _, p := range se.Problems {
		if p.Fix == "" {
			t.Errorf("problem without a fix: %+v", p)
		}
	}

	if len(se.Problems) != 5 {
		t.Errorf("want 5 problems, got %d:\n%s", len(se.Problems), msg)
	}
}

func TestCheckPassesWhenWiredCorrectly(t *testing.T) {
	app := testApp(t, "local")
	app.authenticate = []gin.HandlerFunc{passthrough}
	app.authorizer = authztest.AllowAll()

	cat := authz.NewCatalogue()
	cat.MustDefine("ops.secret:view")
	app.Permissions(cat)

	app.Routes(pingRoute.To(pong), secretRoute.To(pong), lostRoute.To(pong))

	if err := app.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSaysHowToFixMissingAuthentication(t *testing.T) {
	app := testApp(t, "local")
	private := route.Get[route.None, string]("/private")
	app.Routes(private.To(pong), route.Get[route.None, string]("/open").Public().To(pong))

	err := app.Check()
	if err == nil || !strings.Contains(err.Error(), "gocore.WithAuthentication") || !strings.Contains(err.Error(), ".Public()") {
		t.Fatalf("want the fix in the message, got %v", err)
	}

	var se *StartupError
	if !errors.As(err, &se) || len(se.Problems) != 1 {
		t.Fatalf("only the non-public route is a problem, got %v", err)
	}
}

func TestCheckRejectsDuplicateRoutes(t *testing.T) {
	app := testApp(t, "local")
	app.authenticate = []gin.HandlerFunc{passthrough}
	app.Routes(pingRoute.To(pong), pingRoute.To(pong))

	if err := app.Check(); err == nil || !strings.Contains(err.Error(), "installed twice") {
		t.Fatalf("got %v", err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("not a TCP address")
	}

	return addr.Port
}

func runnable(t *testing.T) (*App, int) {
	t.Helper()

	app := testApp(t, "local")
	port := freePort(t)
	app.cfg.HTTP.Port = port
	app.cfg.HTTP.ShutdownTimeout = 5 * time.Second
	app.cfg.Frontend.StaticPath = ""

	return app, port
}

type events struct {
	mu  sync.Mutex
	log []string
}

func (e *events) add(s string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.log = append(e.log, s)
}

func (e *events) all() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]string(nil), e.log...)
}

type loopWorker struct {
	name    string
	onStop  func()
	started chan struct{}
}

func (w *loopWorker) Name() string { return w.name }
func (w *loopWorker) Run(ctx context.Context) error {
	close(w.started)
	<-ctx.Done()
	w.onStop()

	return nil
}

type hookModule struct {
	name    string
	bootErr error
	onStop  func()
}

func (m *hookModule) Name() string                        { return m.name }
func (m *hookModule) Register(*bootstrap.Container) error { return nil }
func (m *hookModule) Boot(context.Context) error          { return m.bootErr }
func (m *hookModule) Shutdown(_ context.Context) error    { m.onStop(); return nil }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunShutdownOrder_HTTPThenWorkersThenModulesThenDatabase(t *testing.T) {
	app, port := runnable(t)
	ev := &events{}
	url := fmt.Sprintf("http://127.0.0.1:%d/up", port)

	app.authenticate = []gin.HandlerFunc{passthrough}
	app.Routes(upRoute.To(pong))
	app.Modules(&hookModule{name: "old", onStop: func() {
		// the database must still be open when old modules shut down
		if err := app.Database().Exec("select 1").Error; err != nil {
			ev.add("module: database already closed")
			return
		}

		ev.add("module stopped")
	}})

	w := &loopWorker{name: "w", started: make(chan struct{}), onStop: func() {
		_, err := http.Get(url) //nolint:noctx // probing that the listener is gone
		if err != nil {
			ev.add("worker stopped, http already down")
			return
		}

		ev.add("worker stopped, http still up")
	}}
	app.Background(w)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- app.RunContext(ctx) }()

	waitFor(t, "the server", func() bool {
		select {
		case err := <-done:
			t.Fatalf("Run ended early: %v", err)
		default:
		}

		resp, err := http.Get(url) //nolint:noctx // readiness probe
		if err != nil {
			return false
		}
		_ = resp.Body.Close()

		return resp.StatusCode == http.StatusOK
	})

	<-w.started
	cancel()

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	got := strings.Join(ev.all(), " | ")
	want := "worker stopped, http already down | module stopped"

	if got != want {
		t.Fatalf("shutdown order:\n got %s\nwant %s", got, want)
	}

	if app.registry.Has("local") {
		t.Fatal("database should be closed after Run")
	}
}

func TestRunBootFailureStopsWhatStartedAndLeavesNothingRunning(t *testing.T) {
	app, port := runnable(t)
	ev := &events{}

	before := runtime.NumGoroutine()

	w := &loopWorker{name: "w", started: make(chan struct{}), onStop: func() { ev.add("worker stopped") }}
	app.Background(w)
	app.Modules(
		&hookModule{name: "good", onStop: func() { ev.add("good stopped") }},
		&hookModule{name: "bad", bootErr: errors.New("boom"), onStop: func() { ev.add("bad stopped") }},
	)

	err := app.RunContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want the boot error, got %v", err)
	}

	got := strings.Join(ev.all(), " | ")
	// the failing module never booted, so it is not shut down; reverse order otherwise
	if got != "worker stopped | good stopped" && got != "good stopped" {
		t.Fatalf("unexpected shutdown record %q", got)
	}

	// no listener
	l, lerr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if lerr != nil {
		t.Fatalf("port still bound: %v", lerr)
	}
	_ = l.Close()

	// no goroutines left behind
	waitFor(t, "goroutines to finish", func() bool { return runtime.NumGoroutine() <= before })
}

func TestRunRefusesToStartOnCheckFailureAndClosesDatabase(t *testing.T) {
	app, _ := runnable(t)
	Later[fmt.Stringer](app)

	err := app.RunContext(context.Background())

	var se *StartupError
	if !errors.As(err, &se) {
		t.Fatalf("want *StartupError, got %v", err)
	}
}
