// Package database provides GORM-based database access, connection registry,
// repository helpers, transaction management, and schema migration utilities.
//
// Services register named connections at startup through the Registry:
//
//	database.Register("primary", gormDB)
//
// Repositories retrieve the primary connection from the registry:
//
//	db := bootstrap.MustResolve[*database.Registry](c).Primary()
//
// Transactions are managed through the Transactor interface so that callers
// do not depend on GORM directly:
//
//	err := tx.WithinTransaction(ctx, func(ctx context.Context) error { ... })
package database

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"sync"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Registry manages a set of named database connection pools.
// It replaces the package-level global connection map from the original code.
//
// Create one Registry per application at startup, register all connections,
// then pass it to your router/handlers via dependency injection.
//
// Applications with one database:
//
//	reg := database.NewRegistry(cfg)
//	reg.MustRegister(database.ConnectionConfig{Name: "local", ...})
//
// Applications with multiple databases:
//
//	reg.MustRegister(database.ConnectionConfig{Name: "local", ...})
//	reg.MustRegister(database.ConnectionConfig{Name: "shared", ...})
//	reg.MustRegister(database.ConnectionConfig{Name: "etx_hr", ...})
type Registry struct {
	mu          sync.RWMutex
	connections map[string]*gorm.DB
	primaryName string
	cfg         RegistryConfig
	log         *slog.Logger
}

// NewRegistry creates a new empty Registry with the given options.
func NewRegistry(log *slog.Logger, cfg RegistryConfig) *Registry {
	return &Registry{
		connections: make(map[string]*gorm.DB),
		cfg:         cfg.withDefaults(),
		log:         log,
	}
}

// NewRegistryFromConfigs creates a new Registry with the given options and pre-registered connections.
func NewRegistryFromConfigs(log *slog.Logger, rcfg RegistryConfig, conns []ConnectionConfig) *Registry {
	reg := NewRegistry(log, rcfg)
	for _, conn := range conns {
		reg.MustRegister(conn)
	}
	return reg
}

// Register opens a connection pool for the given config and stores it under cfg.Name.
// Returns an error if the config is invalid or the connection cannot be opened.
// Safe to call concurrently.
func (r *Registry) Register(cfg ConnectionConfig) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}

	cfg = cfg.withDefaults()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Already registered — skip silently.
	// Re-registering the same name would replace a live pool and leak connections.
	if _, exists := r.connections[cfg.Name]; exists {
		return nil
	}

	conn, err := r.openConnection(cfg)
	if err != nil {
		return ErrConnectionFailed{Name: cfg.Name, Err: err}
	}

	r.connections[cfg.Name] = conn

	// First registered connection becomes the primary automatically
	if r.primaryName == "" {
		r.primaryName = cfg.Name
	}

	return nil
}

// MustRegister is like Register but panics on error.
// Use at application startup where a missing DB connection is unrecoverable.
func (r *Registry) MustRegister(cfg ConnectionConfig) {
	if err := r.Register(cfg); err != nil {
		panic(fmt.Sprintf("database.Registry.MustRegister: %v", err))
	}
}

// Get returns the *gorm.DB pool for the given name.
// Returns ErrConnectionNotFound if the name was never registered.
func (r *Registry) Get(name string) (*gorm.DB, error) {
	r.mu.RLock()
	conn, ok := r.connections[name]
	r.mu.RUnlock()

	if !ok {
		return nil, ErrConnectionNotFound{Name: name}
	}

	return conn, nil
}

// MustGet is like Get but panics on error.
// Use in middleware where a missing connection is a programming error, not a
// runtime condition.
func (r *Registry) MustGet(name string) *gorm.DB {
	conn, err := r.Get(name)
	if err != nil {
		panic(fmt.Sprintf("database.Registry.MustGet: %v", err))
	}
	return conn
}

// Primary returns the default database connection.
// Panics if no connections have been registered.
func (r *Registry) Primary() *gorm.DB {
	r.mu.RLock()
	name := r.primaryName
	r.mu.RUnlock()

	if name == "" {
		panic("database.Registry: no connections registered, cannot get Primary")
	}

	return r.MustGet(name)
}

func (r *Registry) PrimaryName() string {
	r.mu.RLock()
	name := r.primaryName
	r.mu.RUnlock()

	return name
}

// Has returns true if a connection with the given name is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	_, ok := r.connections[name]
	r.mu.RUnlock()
	return ok
}

// Names returns all registered connection names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.connections))
	for name := range r.connections {
		names = append(names, name)
	}
	return names
}

// CloseAll closes all registered connection pools.
// Call during graceful shutdown.
func (r *Registry) CloseAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for name, conn := range r.connections {
		sqlDB, err := conn.DB()
		if err != nil {
			errs = append(errs, fmt.Errorf("get sql.DB for %q: %w", name, err))
			continue
		}
		if err := sqlDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %q: %w", name, err))
		}
		delete(r.connections, name)
	}

	if len(errs) > 0 {
		return fmt.Errorf("CloseAll errors: %v", errs)
	}
	return nil
}

// AddConnection lets you inject a pre-built *gorm.DB directly.
// Useful in tests where you want to inject a SQLite in-memory connection
// without going through the MySQL driver.
//
// Example:
//
//	conn, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
//	reg.AddConnection("local", conn)
func (r *Registry) AddConnection(name string, conn *gorm.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connections[name] = conn
}

// --- private ---

func (r *Registry) openConnection(cfg ConnectionConfig) (*gorm.DB, error) {
	driver := cfg.Driver
	if driver == "" {
		driver = DriverMySQL
	}
	switch driver {
	case DriverSQLite:
		return r.openSQLite(cfg)
	case DriverMySQL:
		return r.openMySQL(cfg)
	default:
		return nil, fmt.Errorf("unsupported driver %q", driver)
	}
}

func (r *Registry) openMySQL(cfg ConnectionConfig) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database,
	)

	conn, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:                 r.buildLogger(),
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := conn.DB()
	if err != nil {
		// Attempt best-effort cleanup to avoid leaking the underlying connection pool.
		// conn.DB() failing after a successful gorm.Open is unusual, but guard anyway.
		if db, e := conn.DB(); e == nil {
			_ = db.Close()
		}
		return nil, err
	}

	ApplyPoolSettings(sqlDB, cfg)

	return conn, nil
}

func (r *Registry) openSQLite(cfg ConnectionConfig) (*gorm.DB, error) {
	conn, err := gorm.Open(sqlite.Open(cfg.Database), &gorm.Config{
		Logger:                 r.buildLogger(),
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := conn.DB()
	if err != nil {
		if db, e := conn.DB(); e == nil {
			_ = db.Close()
		}
		return nil, err
	}

	ApplyPoolSettings(sqlDB, cfg)

	return conn, nil
}

func (r *Registry) buildLogger() logger.Interface {
	level := logger.Error
	switch r.cfg.LogLevel {
	case "silent":
		level = logger.Silent
	case "warn":
		level = logger.Warn
	case "info":
		level = logger.Info
	}

	return logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             r.cfg.SlowQueryThreshold,
			LogLevel:                  level,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
			Colorful:                  false,
		},
	)
}
