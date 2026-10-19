//nolint:testpackage,paralleltest,goconst // t.Setenv tests (not parallel); they use the package's helpers
package gocore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wssto2/go-core/frontend"
	"github.com/wssto2/go-core/ratelimit"
)

func TestNewReadsTheEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	t.Setenv("APP_PORT", "9191")
	t.Setenv("READ_TIMEOUT_SEC", "7")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8, 127.0.0.1")
	t.Setenv("HTTP_MAX_BODY_BYTES", "2048")

	cfg := New(WithLogger(quietLogger())).Config()

	if cfg.App.Env != "staging" || cfg.HTTP.Port != 9191 || cfg.HTTP.ReadTimeout != 7*time.Second ||
		cfg.HTTP.MaxBodyBytes != 2048 {
		t.Fatalf("config = %+v", cfg)
	}

	if got := strings.Join(cfg.HTTP.TrustedProxies, "|"); got != "10.0.0.0/8|127.0.0.1" {
		t.Fatalf("trusted proxies = %q", got)
	}
}

func TestAMalformedSettingStopsStartUpNamingTheVariable(t *testing.T) {
	t.Setenv("APP_PORT", "80a0")
	t.Setenv("TRUSTED_PROXIES", "not-an-ip")

	app := New(WithLogger(quietLogger()))
	_ = app.Env().Required("EMBED_SECRET")

	err := app.Check()
	if err == nil {
		t.Fatal("Check passed")
	}

	for _, want := range []string{"APP_PORT=80a0", "TRUSTED_PROXIES", "EMBED_SECRET is not set"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the report does not name %q:\n%v", want, err)
		}
	}

	if app.Config().HTTP.Port != 8080 {
		t.Errorf("port = %d, want the default", app.Config().HTTP.Port)
	}
}

func TestWithConfigDoesNotReadTheEnvironment(t *testing.T) {
	t.Setenv("APP_PORT", "80a0")

	cfg := DefaultConfig()
	cfg.HTTP.Port = 1234

	app := New(WithConfig(cfg), WithLogger(quietLogger()))

	err := app.Check()
	if err != nil || app.Config().HTTP.Port != 1234 {
		t.Fatalf("port %d, err %v", app.Config().HTTP.Port, err)
	}
}

func TestWithDatabaseReadsItsOwnPrefixAndTheFirstIsThePrimary(t *testing.T) {
	t.Setenv("DB_DATABASE", "ignored") // no implicit DB_*
	t.Setenv("LOCAL_DB_DRIVER", "sqlite")
	t.Setenv("LOCAL_DB_DATABASE", ":memory:")
	t.Setenv("SHARED_DB_DRIVER", "sqlite")
	t.Setenv("SHARED_DB_DATABASE", ":memory:")
	t.Setenv("SHARED_DB_MAX_OPEN_CONNS", "9")
	t.Setenv("OTHER_DB_DRIVER", "sqlite")
	t.Setenv("OTHER_DB_DATABASE", ":memory:")

	app := New(WithLogger(quietLogger()),
		WithDatabase("local"), WithDatabase("shared"), WithDatabase("other", DefaultMaxOpenConns(25)))
	t.Cleanup(app.closeDatabase)

	conns := app.Config().Database.Connections
	if len(conns) != 3 || conns[0].Name != "local" || conns[1].MaxOpenConns != 9 || conns[2].MaxOpenConns != 25 {
		t.Fatalf("connections = %+v", conns)
	}

	if name := app.registry.PrimaryName(); name != "local" {
		t.Fatalf("primary = %q", name)
	}
}

func TestWithoutWithDatabaseThereIsNoDatabase(t *testing.T) {
	t.Setenv("DB_HOST", "somewhere")
	t.Setenv("DB_DATABASE", "x")

	app := New(WithLogger(quietLogger()))
	if app.registry != nil || len(app.Config().Database.Connections) != 0 {
		t.Fatal("an implicit DB_* connection was declared")
	}
}

func TestWithDatabaseNeedsAnEnvironmentSafeName(t *testing.T) {
	app := New(WithLogger(quietLogger()), WithDatabase("my-db"), WithConfig(DefaultConfig()))

	err := app.Check()
	if err == nil || !strings.Contains(err.Error(), `"my-db"`) {
		t.Fatalf("got %v", err)
	}
}

func serveOnce(engine http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	req.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	return w
}

func TestHTTPOptionsReachTheEngineThroughRun(t *testing.T) {
	app := New(WithConfig(DefaultConfig()), WithLogger(quietLogger()), WithPrefix("/api"),
		WithRateLimit(nil, ratelimit.NewInMemoryLimiter(1, time.Minute)),
		WithTrustedOrigins("https://legacy.example.com/some/path"),
		WithCSP("frame-src", "https://maps.example.com"),
	)
	app.Routes(ipRoute.To(showIP))

	booted, err := app.build()
	if err != nil {
		t.Fatal(err)
	}

	err = booted.registerModules()
	if err != nil {
		t.Fatal(err)
	}

	first := serveOnce(booted.engine, "/api/ip")
	if first.Code != http.StatusOK {
		t.Fatalf("first = %d", first.Code)
	}

	csp := first.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "https://legacy.example.com") || !strings.Contains(csp, "https://maps.example.com") {
		t.Errorf("CSP = %q", csp)
	}

	if second := serveOnce(booted.engine, "/api/ip"); second.Code != http.StatusTooManyRequests {
		t.Errorf("the per-IP limit did not apply under the prefix: %d", second.Code)
	}
}

func TestTheSPAShellLeavesOnlyThePrefixOfWithPrefixAlone(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<!doctype html><div id="app"></div>`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	app := New(WithConfig(DefaultConfig()), WithLogger(quietLogger()), WithPrefix("/v9"),
		WithSPAConfig(frontend.SPAConfig{ //nolint:exhaustruct,exhaustruct_v5 // the rest is the default
			TemplatesPath: filepath.Join(dir, "*.html"), TemplateName: "index.html",
		}))

	booted, err := app.build()
	if err != nil {
		t.Fatal(err)
	}

	shell := serveOnce(booted.engine, "/anywhere")
	if shell.Code != http.StatusOK || !strings.Contains(shell.Body.String(), `id="app"`) {
		t.Errorf("shell = %d %q", shell.Code, shell.Body.String())
	}

	api := serveOnce(booted.engine, "/v9/missing")
	if api.Code != http.StatusNotFound || strings.Contains(api.Body.String(), `id="app"`) {
		t.Errorf("the prefix fell to the shell: %d", api.Code)
	}

	other := serveOnce(booted.engine, "/api/missing")
	if !strings.Contains(other.Body.String(), `id="app"`) {
		t.Errorf("/api is not the prefix here, so the shell answers it: %d", other.Code)
	}
}

func TestWithNameIsTheDefaultAndAppNameWins(t *testing.T) {
	t.Setenv("APP_NAME", "")

	if got := New(WithLogger(quietLogger())).Config().App.Name; got != "" {
		t.Fatalf("no default name, got %q", got)
	}

	if got := New(WithName("shop"), WithLogger(quietLogger())).Config().App.Name; got != "shop" {
		t.Fatalf("WithName with APP_NAME empty: %q", got)
	}

	t.Setenv("APP_NAME", "from-env")

	if got := New(WithName("shop"), WithLogger(quietLogger())).Config().App.Name; got != "from-env" {
		t.Fatalf("APP_NAME must win: %q", got)
	}
}
