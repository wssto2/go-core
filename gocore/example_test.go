package gocore_test

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/route"
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
