package gocore

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/route"
	"gorm.io/gorm"
)

// tickets is a feature shaped like ARV's: a repository built from app.Database() at install time, queried only when a
// request comes.
type ticketRepo struct{ db *gorm.DB }

func (r ticketRepo) count(ctx context.Context) (int, error) {
	var n int64

	err := r.db.WithContext(ctx).Table("tickets").Count(&n).Error

	return int(n), err
}

var (
	listTickets = route.Get[route.None, int]("/v1/tickets").Name("tickets.list").Requires("tickets:view")
	pingTickets = route.Get[route.None, string]("/v1/ping").Name("tickets.ping").Public()
)

var ticketsContract = route.Group("tickets", listTickets, pingTickets)

type nopWorker struct{}

func (nopWorker) Name() string              { return "nop" }
func (nopWorker) Run(context.Context) error { return nil }

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func installDesk(app *App, started *atomic.Bool) {
	repo := ticketRepo{db: app.Database()}
	_ = app.Database("shared")

	catalogue := authz.NewCatalogue()
	catalogue.MustDefine("tickets:view")
	app.Permissions(catalogue)

	app.Schema(Schema{Files: fstest.MapFS{"20260101000000_t.sql": {Data: []byte("-- +goose Up\n")}}})
	app.Events(assigned.To("tickets.audit", ok))
	app.Background(nopWorker{})
	app.OnStart(func(context.Context) error {
		started.Store(true)

		return nil
	})
	app.Routes(listTickets.To(func(ctx context.Context, _ route.None) (int, error) { return repo.count(ctx) }), pingTickets.To(
		func(context.Context, route.None) (string, error) { return "pong", nil }))
}

func offlineApp(opts ...Option) *App {
	return New(bootstrap.DefaultConfig(), append([]Option{Offline(), WithLogger(discardLogger()),
		WithAuthentication(func(c *gin.Context) { c.Next() }), WithAuthorizer(authztest.AllowAll())}, opts...)...)
}

func TestOfflineInstallsARepositoryFeatureCleanly(t *testing.T) {
	var started atomic.Bool

	app := offlineApp()
	installDesk(app, &started)

	if err := app.Check(); err != nil {
		t.Fatalf("an offline install must pass Check: %v", err)
	}

	if got := app.Contracts(); len(got) != 1 || got[0] != ticketsContract || len(app.Served()) != 2 {
		t.Fatalf("want the 2 routes in the tickets group, got %d routes, %v", len(app.Served()), got)
	}

	if cats := app.Catalogues(); len(cats) != 1 || cats[0].Len() != 1 {
		t.Fatal("the catalogue must be there offline")
	}

	// The handle fails on use, with a message that says why, and constructing a repository did not.
	_, err := ticketRepo{db: app.Database()}.count(t.Context())
	if !errors.Is(err, errOffline) {
		t.Fatalf("a query must fail with the offline error, got %v", err)
	}

	if started.Load() {
		t.Fatal("no OnStart check runs offline")
	}
}

func TestOfflineOpensNoDatabase(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	var dials atomic.Int32

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}

			dials.Add(1)
			_ = c.Close()
		}
	}()

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	cfg := bootstrap.DefaultConfig()
	cfg.Database.Connections = []bootstrap.DatabaseConnectionConfig{{Name: "local", Driver: "mysql", Host: host, Port: port, Database: "x", Username: "u", Password: "p"}}

	t.Run("without Offline the same config dials", func(t *testing.T) {
		app := New(cfg, WithLogger(discardLogger()))

		t.Cleanup(app.closeDatabase)

		if dials.Load() == 0 {
			t.Fatal("the control did not dial: this test proves nothing")
		}
	})

	dials.Store(0)

	app := New(cfg, Offline(), WithLogger(discardLogger()))
	_ = app.Database()
	_ = app.Database("shared")
	time.Sleep(100 * time.Millisecond)

	if dials.Load() != 0 || app.registry != nil {
		t.Fatalf("an offline App opened a connection (%d dials)", dials.Load())
	}

	if err := app.Check(); err != nil {
		t.Fatalf("no database problem is recorded offline: %v", err)
	}
}

func TestOfflineRefusesServingAndMigrating(t *testing.T) {
	for _, args := range [][]string{nil, {"migrate"}, {"migrate", "status"}, {"migrate", "mark-applied", "local", "1"}} {
		var started atomic.Bool

		app := offlineApp()
		installDesk(app, &started)

		err := app.RunCommand(t.Context(), args, io.Discard)
		if !errors.Is(err, errOffline) {
			t.Fatalf("%q: want the offline refusal, got %v", args, err)
		}

		if started.Load() {
			t.Fatalf("%q ran an OnStart check", args)
		}
	}

	app := offlineApp()
	if err := app.RunCommand(t.Context(), []string{"help"}, io.Discard); err != nil {
		t.Fatalf("help must work offline: %v", err)
	}
}

func TestOfflineStillReportsWiringProblems(t *testing.T) {
	app := offlineApp()
	_ = Later[string](app)

	err := app.Check()
	if err == nil || !strings.Contains(err.Error(), "Later[string] was never set") {
		t.Fatalf("want the unset Later, got %v", err)
	}

	if len(app.problems) != 0 {
		t.Fatalf("the unset Later is found by Check, not recorded: %+v", app.problems)
	}
}

func TestOfflineHandlerAnswersWithoutADatabase(t *testing.T) {
	var started atomic.Bool

	app := offlineApp()
	installDesk(app, &started)

	if _, err := app.Handler(); err != nil {
		t.Fatalf("Handler: %v", err)
	}
}

func TestOfflineLeavesTheRegistryOfWithRegistryUnused(t *testing.T) {
	reg, cleanup := database.NewTestRegistry("local")
	t.Cleanup(func() { _ = cleanup() })

	app := offlineApp(WithRegistry(reg))

	if err := app.Database().Exec("select 1").Error; !errors.Is(err, errOffline) {
		t.Fatalf("Offline wins over WithRegistry, got %v", err)
	}
}

func TestOfflineCreatesNoLogDirectory(t *testing.T) {
	cfg := bootstrap.DefaultConfig()
	cfg.Log.Dir = filepath.Join(t.TempDir(), "logs")

	app := New(cfg, Offline())

	app.Logger().Info("quiet")

	if _, err := os.Stat(cfg.Log.Dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("an offline App must not create the log directory, got %v", err)
	}

	if err := app.Check(); err != nil {
		t.Fatal(err)
	}
}
