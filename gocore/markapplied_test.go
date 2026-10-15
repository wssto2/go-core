package gocore_test

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/gocore"
	"gorm.io/gorm"
)

const (
	markCommand = "mark-applied"
	localConn   = "local"
	migrateCmd  = "migrate"
	versionA    = "20260101000000"
)

func gooseFile(sql string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("-- +goose Up\n" + sql), Mode: 0, ModTime: time.Time{}, Sys: nil}
}

func twoMigrations() fstest.MapFS {
	return fstest.MapFS{
		versionA + "_a.sql":    gooseFile("CREATE TABLE a (id INTEGER);"),
		"20260102000000_b.sql": gooseFile("CREATE TABLE b (id INTEGER);"),
	}
}

func newApp(reg *database.Registry) *gocore.App {
	quiet := gocore.WithLogger(slog.New(slog.DiscardHandler))

	return gocore.New(bootstrap.DefaultConfig(), gocore.WithRegistry(reg), quiet)
}

func appOver(reg *database.Registry) *gocore.App {
	app := newApp(reg)
	app.Migrations(twoMigrations())

	return app
}

func scratchApp(t *testing.T) *gocore.App {
	t.Helper()

	reg, cleanup := database.NewTestRegistry(localConn)

	t.Cleanup(func() { _ = cleanup() })

	return appOver(reg)
}

// Adopting a database that has table a: MarkApplied records it, then Migrate runs only b.
func TestMarkAppliedAdoptsTablesThatAlreadyExistOnEveryDatabase(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, conn *gorm.DB) {
		t.Helper()
		require.NoError(t, conn.Exec("CREATE TABLE a (id INTEGER)").Error)

		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{
			LogLevel: "", SlowQueryThreshold: 0, LogParameters: false,
		})
		reg.AddConnection(localConn, conn)

		app := appOver(reg)

		// The file name form, with its directory, and the plain number both work; a version twice is recorded once.
		require.NoError(t, app.MarkApplied(t.Context(), localConn, "migrations/"+versionA+"_a.sql", versionA))
		require.NoError(t, app.Migrate(t.Context()), "a would fail with 'table already exists' if it were run")
		require.NoError(t, conn.Exec("SELECT 1 FROM b").Error)
	})
}

func TestMarkAppliedCommandOnEveryDatabase(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, conn *gorm.DB) {
		t.Helper()
		require.NoError(t, conn.Exec("CREATE TABLE a (id INTEGER)").Error)

		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{
			LogLevel: "", SlowQueryThreshold: 0, LogParameters: false,
		})
		reg.AddConnection(localConn, conn)

		var out strings.Builder

		args := []string{migrateCmd, markCommand, localConn, versionA + "_a.sql"}
		require.NoError(t, appOver(reg).RunCommand(t.Context(), args, &out))
		require.Equal(t, "local: 1 migration(s) recorded as applied\n", out.String())
	})
}

func TestMarkAppliedCommandRefusesWhatItCannotDo(t *testing.T) {
	t.Parallel()

	for name, scenario := range map[string]struct {
		args []string
		want string
	}{
		"no arguments": {[]string{migrateCmd, markCommand}, "takes a connection and at least one version"},
		"no version":   {[]string{migrateCmd, markCommand, localConn}, "at least one version"},
		"not a version": {
			[]string{migrateCmd, markCommand, localConn, "things.sql"}, `"things.sql": not a migration version`,
		},
		"unknown version":    {[]string{migrateCmd, markCommand, localConn, "42"}, "no migration with version 42"},
		"unknown connection": {[]string{migrateCmd, markCommand, "shared", versionA}, "shared"},
	} {
		err := scratchApp(t).RunCommand(t.Context(), scenario.args, io.Discard)
		if err == nil || !strings.Contains(err.Error(), scenario.want) {
			t.Errorf("%s: want an error containing %q, got %v", name, scenario.want, err)
		}
	}
}

func TestMarkAppliedWithoutMigrationsIsAnError(t *testing.T) {
	t.Parallel()

	reg, cleanup := database.NewTestRegistry(localConn)

	t.Cleanup(func() { _ = cleanup() })

	app := newApp(reg)

	require.ErrorContains(t, app.MarkApplied(t.Context(), localConn, "1"), "no migrations")
}
