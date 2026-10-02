package gocore

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/logger"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/worker"
	"gorm.io/gorm"
)

// Clock tells the time. Features take it from App.Clock instead of calling
// time.Now, so tests can fix it.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Problem is one thing that stops the application from starting: what is
// wrong and what to do about it.
type Problem struct {
	What string
	Fix  string
}

func (p Problem) String() string { return p.What + ". Fix: " + p.Fix }

// StartupError lists every problem found before the application started.
type StartupError struct {
	Problems []Problem
}

func (e *StartupError) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "gocore: cannot start, %d problem(s):", len(e.Problems))

	for i, p := range e.Problems {
		fmt.Fprintf(&b, "\n  %d. %s", i+1, p)
	}

	return b.String()
}

// App is the application under assembly. Features read its environment
// (Database, Clock, Config, Logger) and hand it what they offer (Routes,
// Background, Permissions). Create it with New; start it with Run.
//
// An App is built on one goroutine, in main or in a test, and is not safe for
// concurrent Install calls.
type App struct {
	cfg        bootstrap.Config
	log        *slog.Logger
	clock      Clock
	registry   *database.Registry
	authorizer authz.Authorizer
	customize  []func(*bootstrap.AppBuilder)

	routes     []route.Handled
	workers    []worker.Worker
	catalogues []*authz.Catalogue
	modules    []bootstrap.Module

	problems []Problem
	laters   []unsetter
	started  bool
}

// Option adjusts New.
type Option func(*App)

// WithAuthorizer sets the authorizer that checks the permission of every
// route declared with Requires. Without one, such a route stops start-up.
func WithAuthorizer(a authz.Authorizer) Option {
	return func(app *App) { app.authorizer = a }
}

// WithClock replaces the system clock.
func WithClock(c Clock) Option {
	return func(app *App) { app.clock = c }
}

// WithLogger replaces the logger built from the log config.
func WithLogger(log *slog.Logger) Option {
	return func(app *App) { app.log = log }
}

// WithRegistry uses reg instead of opening the connections listed in the
// config. The caller closes it when the App is never run.
func WithRegistry(reg *database.Registry) Option {
	return func(app *App) { app.registry = reg }
}

// WithBuilder lets the application configure the bootstrap builder the App is
// built on: rate limits, JWT authentication, the SPA, trusted origins.
func WithBuilder(configure func(*bootstrap.AppBuilder)) Option {
	return func(app *App) { app.customize = append(app.customize, configure) }
}

// New creates an App from the configuration. Opening the logger and the
// database connections can fail; the failure is held and reported by Run with
// the other start-up problems.
func New(cfg bootstrap.Config, opts ...Option) *App {
	app := &App{cfg: cfg, clock: systemClock{}}

	for _, opt := range opts {
		opt(app)
	}

	if app.log == nil {
		log, err := logger.New(logger.Config{
			AppName:    cfg.App.Name,
			LogDir:     cfg.Log.Dir,
			Env:        cfg.App.Env,
			Level:      logger.LogLevel(cfg.Log.Level),
			MaxSizeMB:  cfg.Log.MaxSize,
			MaxBackups: cfg.Log.MaxBackups,
			MaxAgeDays: cfg.Log.MaxAgeDays,
		})
		if err != nil {
			app.fail("the logger could not be opened: "+err.Error(), "check the LOG_* settings, such as LOG_DIR being writable")

			log = slog.Default()
		}

		app.log = log
	}

	if app.registry == nil && len(cfg.Database.Connections) > 0 {
		reg, err := bootstrap.OpenDatabase(app.log, cfg.Database)
		if err != nil {
			app.fail("the database could not be opened: "+err.Error(), "check the connection settings and that the server is reachable")
		}

		app.registry = reg
	}

	return app
}

func (a *App) fail(what, fix string) {
	a.problems = append(a.problems, Problem{What: what, Fix: fix})
}

// Config returns the configuration the App was created with.
func (a *App) Config() bootstrap.Config { return a.cfg }

// Logger returns the application logger.
func (a *App) Logger() *slog.Logger { return a.log }

// Clock returns the application clock.
func (a *App) Clock() Clock { return a.clock }

// Database returns the primary connection, or with an argument the named one.
// A connection that is not registered is reported by Run, listing the
// registered ones; until then the returned handle fails every query.
func (a *App) Database(conn ...database.Connection) *gorm.DB {
	if len(conn) > 1 {
		a.fail("Database was given more than one connection", "call Database once per connection")
	}

	if a.registry == nil {
		a.fail("a feature asked for the database but none is configured",
			"add a connection to the database config, or pass gocore.WithRegistry")

		return database.Unavailable(errNoDatabase)
	}

	if len(conn) == 0 {
		name := a.registry.PrimaryName()
		if name == "" {
			a.fail("a feature asked for the primary database but no connection is registered",
				"add a connection to the database config")

			return database.Unavailable(errNoDatabase)
		}

		return a.registry.MustGet(name)
	}

	db, err := a.registry.Database(conn[0])
	if err != nil {
		a.fail(err.Error(), "register the connection in the database config, or fix its name")

		return database.Unavailable(err)
	}

	return db
}

// Routes collects the routes a feature serves. They are mounted by Run.
func (a *App) Routes(routes ...route.Handled) {
	a.routes = append(a.routes, routes...)
}

// Background collects workers that run for the life of the application: they
// start at boot and are drained at shutdown.
func (a *App) Background(workers ...worker.Worker) {
	a.workers = append(a.workers, workers...)
}

// Permissions adds a catalogue of permissions. Run checks that every
// permission a route Requires is defined in one of them.
func (a *App) Permissions(catalogue *authz.Catalogue) {
	a.catalogues = append(a.catalogues, catalogue)
}

// Modules hosts old-style bootstrap.Modules in the same application, so it can
// move to Install one feature at a time. They register, boot and shut down as
// they always did.
func (a *App) Modules(modules ...bootstrap.Module) {
	a.modules = append(a.modules, modules...)
}
