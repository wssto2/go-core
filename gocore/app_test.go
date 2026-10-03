package gocore

import (
	"context"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/route"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
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
	if app.Authorizer() == nil {
		t.Fatal("Authorizer returns what was set")
	}

	var startup *StartupError
	if err := app.Check(); !errors.As(err, &startup) || len(startup.Problems) != 2 {
		t.Fatalf("want two problems, got %v", err)
	}

	if !strings.Contains(startup.Problems[0].What, "two features set the authorizer") || startup.Problems[1].Fix != "pass the users" {
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

// mysqlOnly is DDL SQLite cannot run.
var mysqlOnly = fstest.MapFS{
	"20261015000000_widgets.sql": {Data: []byte("-- +goose Up\nCREATE TABLE widgets (id INT NOT NULL AUTO_INCREMENT, PRIMARY KEY (id)) ENGINE=InnoDB;")},
}

func widgetModels(db *gorm.DB) error {
	return db.Exec("CREATE TABLE widgets (id INTEGER PRIMARY KEY AUTOINCREMENT)").Error
}

func TestSchemaUsesTheModelsOnSQLiteUnderAutoMigrate(t *testing.T) {
	reg, cleanup := database.NewTestRegistry("local")
	t.Cleanup(func() { _ = cleanup() })

	app := New(bootstrap.DefaultConfig(), WithRegistry(reg), WithLogger(slog.New(slog.DiscardHandler)), WithAutoMigrate(t.Context()))
	app.Schema(Schema{Files: mysqlOnly, Models: widgetModels})

	if err := app.Migrate(t.Context()); err != nil { // migrating again must not create the tables twice
		t.Fatal(err)
	}

	if err := app.Database().Exec("INSERT INTO widgets (id) VALUES (1)").Error; err != nil {
		t.Fatalf("the table was not created from the models: %v", err)
	}
}

func TestSchemaWithoutModelsOrOutsideTestsRunsTheFiles(t *testing.T) {
	app := testApp(t, "local")
	app.Schema(Schema{Files: mysqlOnly, Models: widgetModels})

	pending := app.pendingProblems(t.Context())
	if len(pending) != 1 || !strings.Contains(pending[0].What, "20261015000000_widgets.sql") {
		t.Fatalf("outside tests the files are the migrations, got %+v", pending)
	}
}

// Two features that need one shared table both register its migrations; the
// files are collected once, so the same version is not "used by two files".
func TestTheSameFilesRegisteredTwiceAreCollectedOnce(t *testing.T) {
	app := testApp(t, "local")
	shared := os.DirFS(t.TempDir())

	app.Schema(Schema{Files: shared})
	app.Migrations(shared)
	app.Migrations(os.DirFS(t.TempDir())) // another directory is another source

	if len(app.migrations) != 2 {
		t.Fatalf("want 2 sources (the shared files once, and the other), got %d", len(app.migrations))
	}

	app.Migrations(mysqlOnly)
	app.Migrations(mysqlOnly) // a map-backed FS cannot be compared: it stays a clash

	if pending := app.pendingProblems(t.Context()); len(pending) != 1 || !strings.Contains(pending[0].What, "used by two migration files") {
		t.Fatalf("a repeated map-backed FS is still the clash it was, got %+v", pending)
	}
}

func TestSchemaTakesAtMostOneConnection(t *testing.T) {
	app := testApp(t, "local")
	app.Schema(Schema{Files: mysqlOnly}, "local", "shared")

	if len(app.problems) != 1 || !strings.Contains(app.problems[0].What, "more than one connection") {
		t.Fatalf("got %+v", app.problems)
	}
}

func TestAuthenticateReplacesWhatTheOptionsSetAndOnlyOnce(t *testing.T) {
	app := testApp(t, "local")
	app.authenticate = []gin.HandlerFunc{passthrough, passthrough}

	app.Authenticate(passthrough)

	if len(app.authenticate) != 1 || len(app.problems) != 0 {
		t.Fatalf("one feature replaces the stand-in: %d middleware, problems %+v", len(app.authenticate), app.problems)
	}

	app.Authenticate(passthrough)

	if len(app.problems) != 1 || !strings.Contains(app.problems[0].What, "two features") {
		t.Fatalf("a second feature is a problem, got %+v", app.problems)
	}
}

func TestFailIsListedWithTheOtherProblems(t *testing.T) {
	app := testApp(t, "local")
	app.Fail("identity needs X", "pass Y")

	err := app.Check()
	if err == nil || !strings.Contains(err.Error(), "identity needs X. Fix: pass Y") {
		t.Fatalf("got %v", err)
	}
}

func TestAuthorizeReplacesWhatTheOptionsSet(t *testing.T) {
	stand := authztest.DenyAll()
	app := New(bootstrap.DefaultConfig(), WithLogger(slog.New(slog.DiscardHandler)), WithAuthorizer(stand))

	engine := authztest.AllowAll()
	app.Authorize(engine)

	if app.Authorizer() != authz.Authorizer(engine) || len(app.problems) != 0 {
		t.Fatalf("a feature replaces the stand-in: %v", app.problems)
	}
}
