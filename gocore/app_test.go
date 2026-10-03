package gocore

import (
	"context"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/route"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
)

func testApp(t *testing.T, names ...string) *App {
	t.Helper()

	reg, cleanup := database.NewTestRegistry(names...)
	t.Cleanup(func() { _ = cleanup() })

	return New(bootstrap.DefaultConfig(), WithRegistry(reg), WithLogger(slog.New(slog.DiscardHandler)))
}

func TestDatabaseUnknownConnectionIsReportedWithRegisteredNames(t *testing.T) {
	app := testApp(t, "local", "shared")

	db := app.Database("sharde")

	if err := db.Exec("select 1").Error; err == nil {
		t.Fatal("the stand-in handle must fail its queries")
	}

	if len(app.problems) != 1 || !strings.Contains(app.problems[0].What, "local, shared") {
		t.Fatalf("want one problem listing the registered connections, got %+v", app.problems)
	}
}

func TestDatabaseWithoutConfigurationIsAProblem(t *testing.T) {
	app := New(bootstrap.DefaultConfig(), WithLogger(slog.New(slog.DiscardHandler)))
	_ = app.Database()

	if len(app.problems) != 1 || !strings.Contains(app.problems[0].Fix, "WithRegistry") {
		t.Fatalf("got %+v", app.problems)
	}
}

func TestLaterMustGetPanicsWithTheNamedError(t *testing.T) {
	app := testApp(t, "local")
	d := Later[fmt.Stringer](app)

	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "fmt.Stringer") {
			t.Fatalf("want a panic naming the type, got %v", r)
		}
	}()

	d.MustGet()
}

func TestAuthorizeSetsTheAuthorizerOnceAndFailReportsAtCheck(t *testing.T) {
	app := New(bootstrap.DefaultConfig(), WithLogger(slog.New(slog.DiscardHandler)))

	app.Authorize(authztest.AllowAll())

	if err := app.Check(); err != nil {
		t.Fatalf("one authorizer is fine: %v", err)
	}

	app.Authorize(authztest.DenyAll())
	app.Fail("access needs a SubjectDirectory", "pass the users")

	var startup *StartupError
	if err := app.Check(); !errors.As(err, &startup) || len(startup.Problems) != 2 {
		t.Fatalf("want two problems, got %v", err)
	}

	if !strings.Contains(startup.Problems[0].What, "set twice") || startup.Problems[1].Fix != "pass the users" {
		t.Fatalf("problems: %v", startup.Problems)
	}
}

func TestPrefixMountsEveryRouteUnderIt(t *testing.T) {
	for prefix, want := range map[string]string{"": "/v1/ping", "/api": "/api/v1/ping", "api/": "/api/v1/ping", "/": "/v1/ping"} {
		app := New(bootstrap.DefaultConfig(), WithLogger(slog.New(slog.DiscardHandler)),
			WithAuthentication(func(c *gin.Context) { c.Next() }), WithPrefix(prefix))
		app.Routes(route.Get[route.None, string]("/v1/ping").To(func(context.Context, route.None) (string, error) { return "pong", nil }))

		handler, err := app.Handler()
		if err != nil {
			t.Fatal(err)
		}

		for path, code := range map[string]int{want: 200, "/v1/ping": map[bool]int{true: 200, false: 404}[want == "/v1/ping"]} {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", path, nil))

			if rec.Code != code {
				t.Errorf("prefix %q: GET %s = %d, want %d", prefix, path, rec.Code, code)
			}
		}

		if got := app.Prefix(); got != strings.TrimSuffix(want, "/v1/ping") {
			t.Errorf("prefix %q: Prefix() = %q", prefix, got)
		}
	}
}
