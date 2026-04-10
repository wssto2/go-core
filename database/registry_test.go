package database

import (
	"log/slog"
	"testing"
)

func TestRegistry_Register_SQLiteConnection(t *testing.T) {
	reg := NewRegistry(slog.Default(), RegistryConfig{})
	t.Cleanup(func() { _ = reg.CloseAll() })

	err := reg.Register(ConnectionConfig{
		Name:     "sqlite",
		Driver:   DriverSQLite,
		Database: "file::memory:?cache=shared",
	})
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	if !reg.Has("sqlite") {
		t.Fatal("expected sqlite connection to be registered")
	}
}

func TestRegistry_Register_UnsupportedDriverFails(t *testing.T) {
	reg := NewRegistry(slog.Default(), RegistryConfig{})

	err := reg.Register(ConnectionConfig{
		Name:     "bad",
		Driver:   "postgres",
		Database: "db",
	})
	if err == nil {
		t.Fatal("expected unsupported driver to fail")
	}
}
