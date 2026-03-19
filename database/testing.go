package database

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestRegistry creates a Registry pre-populated with SQLite in-memory connections
// for the given names. Use this in tests instead of PrepareTestDatabase.
//
// Each name gets its own isolated in-memory SQLite database.
// The returned cleanup function closes all connections.
//
// Example:
//
//	reg, cleanup := database.NewTestRegistry(t, "local", "shared")
//	defer cleanup()
//
//	// Migrate your test schemas
//	conn := reg.MustGet("local")
//	conn.AutoMigrate(&entities.Customer{})
func NewTestRegistry(names ...string) (*Registry, func() error) {
	reg := &Registry{
		connections: make(map[string]*gorm.DB),
		cfg:         RegistryConfig{LogLevel: "silent"}.withDefaults(),
	}

	for _, name := range names {
		conn, err := openSQLiteMemory()
		if err != nil {
			panic(fmt.Sprintf("database.NewTestRegistry: failed to open SQLite for %q: %v", name, err))
		}
		reg.connections[name] = conn
	}

	cleanup := func() error {
		return reg.CloseAll()
	}

	return reg, cleanup
}

// SimulateConnectionLoss temporarily removes a named connection from the registry
// and returns a restore function. Use in tests to verify error handling when
// the database is unavailable.
//
// Example:
//
//	restore := database.SimulateConnectionLoss(reg, "local")
//	defer restore()
//	// code under test should now receive ErrConnectionNotFound
func SimulateConnectionLoss(reg *Registry, name string) func() {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	conn, ok := reg.connections[name]
	if !ok {
		panic(fmt.Sprintf("database.SimulateConnectionLoss: connection %q not found", name))
	}

	delete(reg.connections, name)

	return func() {
		reg.mu.Lock()
		defer reg.mu.Unlock()
		reg.connections[name] = conn
	}
}

func openSQLiteMemory() (*gorm.DB, error) {
	conn, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite in-memory: %w", err)
	}
	return conn, nil
}
