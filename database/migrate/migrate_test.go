package migrate_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/migrate"
)

func file(sql string) *fstest.MapFile { return &fstest.MapFile{Data: []byte("-- +goose Up\n" + sql)} }

var migrations = fstest.MapFS{
	"local/20260101000000_a.sql":  file("CREATE TABLE a (id INTEGER);"),
	"local/20260102000000_b.sql":  file("CREATE TABLE b (id INTEGER);"),
	"local/20260103000000_c.sql":  file("CREATE TABLE c (id INTEGER);"),
	"shared/20260101000000_s.sql": file("CREATE TABLE s (id INTEGER);"),
}

func registry(t *testing.T, names ...string) *database.Registry {
	t.Helper()

	reg := database.NewRegistry(slog.Default(), database.RegistryConfig{})
	t.Cleanup(func() { _ = reg.CloseAll() })

	for _, name := range names {
		err := reg.Register(database.ConnectionConfig{
			Name:     name,
			Driver:   database.DriverSQLite,
			Database: filepath.Join(t.TempDir(), name+".db"),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	return reg
}

func tables(t *testing.T, reg *database.Registry, conn string) map[string]bool {
	t.Helper()

	var names []string
	if err := reg.MustGet(conn).Raw(`SELECT name FROM sqlite_master WHERE type = 'table'`).Scan(&names).Error; err != nil {
		t.Fatal(err)
	}

	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}

	return out
}

func TestUpMigratesEveryConnectionOnce(t *testing.T) {
	ctx := context.Background()
	reg := registry(t, "local", "shared")
	m := migrate.New(reg, migrations, slog.Default())

	for range 2 { // the second run finds nothing to do
		if err := m.Up(ctx); err != nil {
			t.Fatal(err)
		}
	}

	if got := tables(t, reg, "local"); !got["a"] || !got["b"] || !got["c"] || got["s"] {
		t.Errorf("local tables = %v", got)
	}

	if got := tables(t, reg, "shared"); !got["s"] || got["a"] {
		t.Errorf("shared tables = %v", got)
	}

	// The registry's pool is still open: goose's Provider.Close would have closed it.
	if err := reg.MustGet("local").Exec("SELECT 1").Error; err != nil {
		t.Errorf("connection closed after Up: %v", err)
	}
}

func TestMarkAppliedAdoptsAndUpRunsTheRestOutOfOrder(t *testing.T) {
	ctx := context.Background()
	reg := registry(t, "local", "shared")
	m := migrate.New(reg, migrations, slog.Default())

	// The database already has b (applied by hand), but not the older a.
	if err := reg.MustGet("local").Exec("CREATE TABLE b (id INTEGER)").Error; err != nil {
		t.Fatal(err)
	}

	if err := m.MarkApplied(ctx, "local", []int64{20260102000000}); err != nil {
		t.Fatal(err)
	}

	if err := m.MarkApplied(ctx, "local", []int64{20260102000000}); err != nil {
		t.Fatalf("marking an applied version again: %v", err)
	}

	if err := m.Up(ctx); err != nil {
		t.Fatal(err) // b would fail: its table exists
	}

	statuses, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(statuses) != 4 {
		t.Fatalf("got %d statuses, want 4", len(statuses))
	}

	for _, s := range statuses {
		if !s.Applied {
			t.Errorf("%s %s is pending", s.Connection, s.File)
		}
	}
}

func TestMarkAppliedRefusesUnknownVersion(t *testing.T) {
	m := migrate.New(registry(t, "local", "shared"), migrations, slog.Default())

	if err := m.MarkApplied(context.Background(), "local", []int64{20260102000000, 42}); err == nil {
		t.Fatal("want an error for a version with no file")
	}
}

func TestDirectoryWithoutConnectionFails(t *testing.T) {
	m := migrate.New(registry(t, "local"), migrations, slog.Default()) // no "shared"

	if err := m.Up(context.Background()); err == nil {
		t.Fatal("want an error: migrations/shared has no connection")
	}
}
