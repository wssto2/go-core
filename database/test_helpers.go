package database

import (
	"fmt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// PrepareTestDB opens an in-memory SQLite database, runs AutoMigrate on the
// provided models, and returns the *gorm.DB, a cleanup function and an error.
// The cleanup function closes the underlying sql.DB connection.
func PrepareTestDB(models ...interface{}) (*gorm.DB, func() error, error) {
	conn, err := openSQLiteMemory()
	if err != nil {
		return nil, nil, err
	}

	if len(models) > 0 {
		if err := SafeMigrate(conn, models...); err != nil {
			sqlDB, _ := conn.DB()
			if sqlDB != nil {
				_ = sqlDB.Close()
			}
			return nil, nil, fmt.Errorf("migrate test models: %w", err)
		}
	}

	cleanup := func() error {
		sqlDB, err := conn.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}

	return conn, cleanup, nil
}

// MustPrepareTestDB is like PrepareTestDB but panics on error. It returns a
// cleanup function that does not return an error (panics on close failure).
func MustPrepareTestDB(models ...interface{}) (*gorm.DB, func()) {
	db, cleanup, err := PrepareTestDB(models...)
	if err != nil {
		panic(err)
	}
	return db, func() {
		_ = cleanup()
	}
}

// NewTestRegistry creates a Registry pre-populated with SQLite in-memory connections
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

	for i, name := range names {
		conn, err := openSQLiteMemory()
		if err != nil {
			panic(fmt.Sprintf("database.NewTestRegistry: failed to open SQLite for %q: %v", name, err))
		}
		reg.connections[name] = conn
		if i == 0 {
			reg.primaryName = name
		}
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
