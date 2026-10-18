//nolint:testpackage // uses the package's internals: the registry, the problems, closeDatabase
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
	"slices"
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

// deskRepo is shaped like ARV's repositories: built from app.Database() at install time, queried when a request comes.
type deskRepo struct{ db *gorm.DB }

func (r deskRepo) count(ctx context.Context) (int, error) {
	var n int64

	err := r.db.WithContext(ctx).Table("tickets").Count(&n).Error

	return int(n), err
}

type nopWorker struct{}

func (nopWorker) Name() string              { return "nop" }
func (nopWorker) Run(context.Context) error { return nil }

func listenConfig() *net.ListenConfig {
	keepAlive := net.KeepAliveConfig{Enable: false, Idle: 0, Interval: 0, Count: 0}

	return &net.ListenConfig{Control: nil, KeepAlive: 0, KeepAliveConfig: keepAlive}
}

func quietLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// installDesk installs a feature with a repository, a catalogue, a schema, an event consumer, a worker and an OnStart
// check, and returns its route group.
func installDesk(app *App, started *atomic.Bool) *route.Contract {
	repo := deskRepo{db: app.Database()}
	_ = app.Database("shared")

	catalogue := authz.NewCatalogue()
	catalogue.MustDefine("tickets:view")
	app.Permissions(catalogue)

	file := &fstest.MapFile{Data: []byte("-- +goose Up\n"), Mode: 0, ModTime: time.Time{}, Sys: nil}
	app.Schema(Schema{Files: fstest.MapFS{"20260101000000_t.sql": file}, Models: nil})
	app.Events(assigned.To("tickets.audit", ok))
	app.Background(nopWorker{})
	app.OnStart(func(context.Context) error {
		started.Store(true)

		return nil
	})

	list := route.Get[route.None, int]("/v1/tickets").Name("tickets.list").Requires("tickets:view")
	ping := route.Get[route.None, string]("/v1/ping").Name("tickets.ping").Public()

	app.Routes(
		list.To(func(ctx context.Context, _ route.None) (int, error) { return repo.count(ctx) }),
		ping.To(func(context.Context, route.None) (string, error) { return "desk", nil }),
	)

	return route.Group("tickets", list, ping)
}

func offlineApp(opts ...Option) *App {
	base := []Option{Offline(), WithLogger(quietLogger()),
		WithAuthentication(func(c *gin.Context) { c.Next() }), WithAuthorizer(authztest.AllowAll())}

	return New(bootstrap.DefaultConfig(), slices.Concat(base, opts)...)
}

func TestOfflineInstallsARepositoryFeatureCleanly(t *testing.T) {
	t.Parallel()

	var started atomic.Bool

	app := offlineApp()
	group := installDesk(app, &started)

	err := app.Check()
	if err != nil {
		t.Fatalf("an offline install must pass Check: %v", err)
	}

	if got := app.Contracts(); len(got) != 1 || got[0] != group || len(app.Served()) != 2 {
		t.Fatalf("want the 2 routes in the tickets group, got %d routes, %v", len(app.Served()), got)
	}

	if cats := app.Catalogues(); len(cats) != 1 || cats[0].Len() != 1 {
		t.Fatal("the catalogue must be there offline")
	}

	// The handle fails on use, with a message that says why, and constructing a repository did not.
	_, err = deskRepo{db: app.Database()}.count(t.Context())
	if !errors.Is(err, errOffline) {
		t.Fatalf("a query must fail with the offline error, got %v", err)
	}

	if started.Load() {
		t.Fatal("no OnStart check runs offline")
	}
}

func TestOfflineOpensNoDatabase(t *testing.T) {
	t.Parallel()

	listener, err := listenConfig().Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	var dials atomic.Int32

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			dials.Add(1)

			_ = conn.Close()
		}
	}()

	host, port, _ := net.SplitHostPort(listener.Addr().String())
	cfg := bootstrap.DefaultConfig()
	cfg.Database.Connections = []bootstrap.DatabaseConnectionConfig{{
		Name: "local", Driver: "mysql", Host: host, Port: port, Database: "x", Username: "u", Password: "p",
		SQLMode: "", Charset: "", MaxIdleConns: 0, MaxOpenConns: 0, ConnMaxLifetime: 0, Debug: false,
	}}

	// The control: without Offline the same config dials the listener, so a count of zero below means something.
	control := New(cfg, WithLogger(quietLogger()))

	t.Cleanup(control.closeDatabase)

	if dials.Load() == 0 {
		t.Fatal("the control did not dial: this test proves nothing")
	}

	dials.Store(0)

	app := New(cfg, Offline(), WithLogger(quietLogger()))
	_ = app.Database()
	_ = app.Database("shared")

	time.Sleep(100 * time.Millisecond)

	if dials.Load() != 0 || app.registry != nil {
		t.Fatalf("an offline App opened a connection (%d dials)", dials.Load())
	}

	err = app.Check()
	if err != nil {
		t.Fatalf("no database problem is recorded offline: %v", err)
	}
}

func TestOfflineRefusesServingAndMigrating(t *testing.T) {
	t.Parallel()

	const migrate = "migrate"

	for _, args := range [][]string{nil, {migrate}, {migrate, "status"}, {migrate, "mark-applied", "local", "1"}} {
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

	err := offlineApp().RunCommand(t.Context(), []string{"help"}, io.Discard)
	if err != nil {
		t.Fatalf("help must work offline: %v", err)
	}
}

func TestOfflineStillReportsWiringProblems(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	var started atomic.Bool

	app := offlineApp()
	installDesk(app, &started)

	_, err := app.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
}

func TestOfflineLeavesTheRegistryOfWithRegistryUnused(t *testing.T) {
	t.Parallel()

	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	app := offlineApp(WithRegistry(reg))

	err := app.Database().Exec("select 1").Error
	if !errors.Is(err, errOffline) {
		t.Fatalf("Offline wins over WithRegistry, got %v", err)
	}
}

func TestOfflineCreatesNoLogDirectory(t *testing.T) {
	t.Parallel()

	cfg := bootstrap.DefaultConfig()
	cfg.Log.Dir = filepath.Join(t.TempDir(), "logs")

	app := New(cfg, Offline())

	app.Logger().Info("quiet")

	_, err := os.Stat(cfg.Log.Dir)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("an offline App must not create the log directory, got %v", err)
	}

	err = app.Check()
	if err != nil {
		t.Fatal(err)
	}
}
