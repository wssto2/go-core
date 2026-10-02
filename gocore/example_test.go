package gocore_test

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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
