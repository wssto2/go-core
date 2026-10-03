package gocore_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/route"
	"gorm.io/gorm"
)

const Shared database.Connection = "shared"

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

type Service struct {
	Clock gocore.Clock
}

func (s *Service) Ping(context.Context, route.None) (string, error) {
	return s.Clock.Now().Format("2006-01-02"), nil
}

var Ping = route.Get[route.None, string]("/ping")

// Install is how a feature puts itself together: it reads the environment
// from app and hands over what it offers.
func Install(app *gocore.App) {
	service := &Service{Clock: app.Clock()}

	_ = app.Database() // the primary connection; app.Database(Shared) another one

	app.Routes(Ping.To(service.Ping))
}

func ExampleApp() {
	reg, cleanup := database.NewTestRegistry("local", "shared")
	defer func() { _ = cleanup() }()

	app := gocore.New(bootstrap.DefaultConfig(),
		gocore.WithRegistry(reg),
		gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithClock(fixedClock(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC))),
	)

	Install(app)

	fmt.Println(app.Clock().Now().Format("2006-01-02"), app.Database(Shared) != nil)
	// Output: 2026-10-02 true
}

type Reads interface{ Count() int }

type reads struct{}

func (reads) Count() int { return 3 }

// Later breaks a real cycle between two features: one gets a promise that is
// kept once both exist.
func ExampleLater() {
	app := gocore.New(bootstrap.DefaultConfig(), gocore.WithLogger(slog.New(slog.DiscardHandler)))

	promise := gocore.Later[Reads](app)

	_, err := promise.Get()
	fmt.Println(err)

	promise.Set(reads{})

	r, _ := promise.Get()
	fmt.Println(r.Count())
	// Output:
	// gocore: gocore_test.Reads was used before it was set: call Set once every feature that needs it is installed
	// 3
}

// Run (and Check, which Run calls first) report every wiring problem at once,
// each with its fix.
func ExampleApp_Check() {
	reg, cleanup := database.NewTestRegistry("local")
	defer func() { _ = cleanup() }()

	app := gocore.New(bootstrap.DefaultConfig(),
		gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)))

	gocore.Later[Reads](app) // never set
	_ = app.Database("shared")

	fmt.Println(app.Check())
	// Output:
	// gocore: cannot start, 2 problem(s):
	//   1. database: connection "shared" not found — was it registered at startup? (registered connections: local). Fix: register the connection in the database config, or fix its name
	//   2. gocore.Later[gocore_test.Reads] was never set. Fix: call Set on it once every feature that needs it is installed
}

// WithAuthentication sets how requests are authenticated, once. Every route
// is behind it unless declared Public; without it, a route that is not Public
// stops start-up.
func ExampleWithAuthentication() {
	reg, cleanup := database.NewTestRegistry("local")
	defer func() { _ = cleanup() }()

	signedIn := func(c *gin.Context) { c.Next() } // really: auth.Authenticated(provider), authzhttp.Principals(resolve)

	for _, opts := range [][]gocore.Option{
		{gocore.WithAuthentication(signedIn)},
		{}, // no authentication configured
	} {
		app := gocore.New(bootstrap.DefaultConfig(),
			append(opts, gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)))...)
		Install(app)

		fmt.Println(app.Check() == nil)
	}
	// Output:
	// true
	// false
}

var things = fstest.MapFS{
	"20261015000000_things.sql": {Data: []byte("-- +goose Up\nCREATE TABLE things (id INTEGER);")},
}

// Migrations collects the goose files a feature ships (a flat embed.FS) for a
// connection. Run never migrates: it refuses to start while any is pending,
// and the deploy runs "./myapp migrate" first. Run reads the command line;
// RunCommand takes the arguments, for tests.
func ExampleApp_RunCommand() {
	defer func(args []string) { os.Args = args }(os.Args)

	os.Args = []string{"/srv/myapp"}

	for _, args := range [][]string{{}, {"migrate"}, {"migrate", "status"}, {"help"}, {"serve"}} {
		reg, cleanup := database.NewTestRegistry("local", "shared")
		app := gocore.New(bootstrap.DefaultConfig(),
			gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)))

		app.Migrations(things)         // the primary connection
		app.Migrations(things, Shared) // another one: a module can be installed On(Shared)

		fmt.Println(strings.TrimSpace("$ myapp " + strings.Join(args, " ")))

		if err := app.RunCommand(context.Background(), args, os.Stdout); err != nil {
			fmt.Println(err)
		}

		_ = cleanup()
	}
	// Output:
	// $ myapp
	// gocore: cannot start, 1 problem(s):
	//   1. 2 migration(s) are pending: local/20261015000000_things.sql, shared/20261015000000_things.sql. Fix: run "myapp migrate" before starting it; Run never migrates
	// $ myapp migrate
	// $ myapp migrate status
	// local      pending  20261015000000_things.sql
	// shared     pending  20261015000000_things.sql
	// $ myapp help
	// Usage: myapp [command]
	//
	// Commands:
	//   (none)          check the application, then serve it
	//   migrate         apply every pending migration, then exit
	//   migrate status  list applied and pending migrations, then exit
	//   help            show this list
	// $ myapp serve
	// gocore: unknown command "serve": the commands are migrate, migrate status and help
}

func ExampleApp_Migrate() {
	reg, cleanup := database.NewTestRegistry("local")
	defer func() { _ = cleanup() }()

	app := gocore.New(bootstrap.DefaultConfig(),
		gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)))

	app.Migrations(things)

	fmt.Println(app.Migrate(context.Background()))
	// Output: <nil>
}

// MigrationsByConnection collects the application's own migrations, one
// directory per connection name.
func ExampleApp_MigrationsByConnection() {
	reg, cleanup := database.NewTestRegistry("local")
	defer func() { _ = cleanup() }()

	app := gocore.New(bootstrap.DefaultConfig(),
		gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)))

	app.MigrationsByConnection(fstest.MapFS{"local/20260101000000_orders.sql": things["20261015000000_things.sql"]})

	fmt.Println(app.Migrate(context.Background()))
	// Output: <nil>
}

// WithAutoMigrate applies migrations as they are collected, so a test that
// installs a feature finds its tables ready. gocoretest.New does this.
func ExampleWithAutoMigrate() {
	reg, cleanup := database.NewTestRegistry("local")
	defer func() { _ = cleanup() }()

	app := gocore.New(bootstrap.DefaultConfig(),
		gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithAutoMigrate(context.Background()))

	app.Migrations(things)

	fmt.Println(app.Database().Exec("INSERT INTO things (id) VALUES (1)").Error)
	// Output: <nil>
}

// Schema collects a feature's tables: the goose files every real database
// runs, and the models tests on SQLite create them from instead.
func ExampleApp_Schema() {
	reg, cleanup := database.NewTestRegistry("local")
	defer func() { _ = cleanup() }()

	app := gocore.New(bootstrap.DefaultConfig(),
		gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithAutoMigrate(context.Background()))

	mysqlOnly := fstest.MapFS{
		"20261015000000_gadgets.sql": {Data: []byte("-- +goose Up\nCREATE TABLE gadgets (id INT NOT NULL AUTO_INCREMENT, PRIMARY KEY (id)) ENGINE=InnoDB;")},
	}

	app.Schema(gocore.Schema{
		Files:  mysqlOnly,
		Models: func(db *gorm.DB) error { return db.Exec("CREATE TABLE gadgets (id INTEGER PRIMARY KEY)").Error },
	})

	fmt.Println(app.Database().Exec("INSERT INTO gadgets (id) VALUES (1)").Error)
	// Output: <nil>
}

// Authenticate is how a feature that authenticates requests, identity.Install,
// sets it up; it replaces what the options said.
func ExampleApp_Authenticate() {
	reg, cleanup := database.NewTestRegistry("local")
	defer func() { _ = cleanup() }()

	app := gocore.New(bootstrap.DefaultConfig(),
		gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)))

	app.Authenticate(func(c *gin.Context) { c.Next() })
	Install(app)

	fmt.Println(app.Check() == nil)
	// Output: true
}

// Fail reports what a feature's Install cannot continue with; Run lists it
// with the other start-up problems.
func ExampleApp_Fail() {
	reg, cleanup := database.NewTestRegistry("local")
	defer func() { _ = cleanup() }()

	app := gocore.New(bootstrap.DefaultConfig(),
		gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)))

	app.Fail("tickets needs a mail sender", "pass mail.Install(app) to tickets.Install")

	fmt.Println(app.Check())
	// Output:
	// gocore: cannot start, 1 problem(s):
	//   1. tickets needs a mail sender. Fix: pass mail.Install(app) to tickets.Install
}
