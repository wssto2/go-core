//nolint:testpackage // uses the package's testApp
package gocore

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"gorm.io/gorm"
)

func twoMigrations() fstest.MapFS {
	return fstest.MapFS{
		"20260101000000_a.sql": {Data: []byte("-- +goose Up\nCREATE TABLE a (id INTEGER);")},
		"20260102000000_b.sql": {Data: []byte("-- +goose Up\nCREATE TABLE b (id INTEGER);")},
	}
}

// Adopting a database that has table a: mark-applied records it, then migrate runs only b.
func TestMarkAppliedAdoptsTablesThatAlreadyExistOnEveryDatabase(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.Exec("CREATE TABLE a (id INTEGER)").Error)

		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("local", db)

		app := New(bootstrap.DefaultConfig(), WithRegistry(reg), WithLogger(slog.New(slog.DiscardHandler)))
		app.Migrations(twoMigrations())

		var out strings.Builder
		// The file name form, with its directory, and the plain number both work.
		require.NoError(t, app.markApplied(t.Context(), []string{"local", "migrations/20260101000000_a.sql", "20260101000000"}, &out))
		require.Equal(t, "local: 2 migration(s) recorded as applied\n", out.String())

		m, err := app.migrator()
		require.NoError(t, err)

		pending, err := m.Pending(t.Context())
		require.NoError(t, err)
		require.Len(t, pending, 1)
		require.Equal(t, "20260102000000_b.sql", pending[0].File)

		require.NoError(t, app.Migrate(t.Context()), "a would fail with 'table already exists' if it were run")
		require.NoError(t, db.Exec("SELECT 1 FROM b").Error)
	})
}

func TestMarkAppliedCommandRefusesWhatItCannotDo(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"no arguments":       {[]string{"migrate", "mark-applied"}, "usage: migrate mark-applied <connection> <version|file>..."},
		"no version":         {[]string{"migrate", "mark-applied", "local"}, "usage"},
		"not a version":      {[]string{"migrate", "mark-applied", "local", "things.sql"}, `"things.sql" is not a migration version`},
		"unknown version":    {[]string{"migrate", "mark-applied", "local", "42"}, "no migration with version 42"},
		"unknown connection": {[]string{"migrate", "mark-applied", "shared", "20260101000000"}, "shared"},
	} {
		app, _ := runnable(t)
		app.Migrations(twoMigrations())

		err := app.RunCommand(t.Context(), tc.args, io.Discard)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want an error containing %q, got %v", name, tc.want, err)
		}

		if _, err := app.registry.Get("local"); err == nil {
			t.Errorf("%s: the database was left open", name)
		}
	}
}

func TestMarkAppliedCommandWithoutMigrationsIsAnError(t *testing.T) {
	app, _ := runnable(t)

	err := app.RunCommand(t.Context(), []string{"migrate", "mark-applied", "local", "1"}, io.Discard)
	require.ErrorContains(t, err, "no migrations")
}

func TestMarkAppliedCommandRecordsAndPrints(t *testing.T) {
	app, _ := runnable(t)
	app.Migrations(twoMigrations())

	var out strings.Builder

	require.NoError(t, app.RunCommand(t.Context(), []string{"migrate", "mark-applied", "local", "20260101000000_a.sql"}, &out))
	require.Equal(t, "local: 1 migration(s) recorded as applied\n", out.String())
}
