package gocore

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

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
