// Package migrate runs SQL migrations for every connection of a database.Registry.
//
// Migrations live in one directory per connection name, each file a goose SQL
// migration (https://github.com/pressly/goose) named <version>_<name>.sql:
//
//	migrations/
//	  local/20260930121914_user_signins.sql
//	  shared/20261002080000_configurator_index.sql
//
// Each database records what ran on it in its own goose_db_version table, so a
// market's databases each know their own state. Migrations may run out of order:
// a version older than the newest applied one still runs if it has not — which is
// what happens when branches merge, and when a database was adopted with
// MarkApplied and lacks an old migration.
//
//	m := migrate.New(registry, migrationsFS, logger)
//	err := m.Up(ctx)
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"

	coredb "github.com/wssto2/go-core/database"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Migrator runs the migrations of fsys against the registry's connections.
type Migrator struct {
	reg  *coredb.Registry
	fsys fs.FS
	log  *slog.Logger
}

// Status is one migration on one connection.
type Status struct {
	Connection string
	Version    int64
	File       string
	Applied    bool
	AppliedAt  time.Time // zero when pending
}

// New returns a Migrator. fsys holds one directory per connection name, e.g. an
// embed.FS narrowed with fs.Sub to its "migrations" directory.
func New(reg *coredb.Registry, fsys fs.FS, log *slog.Logger) *Migrator {
	return &Migrator{reg: reg, fsys: fsys, log: log}
}

// Up applies every pending migration, connection by connection. It stops at the
// first failure; on MySQL a failed migration's DDL is not rolled back (DDL commits
// implicitly), so the error names the file to repair before running again.
func (m *Migrator) Up(ctx context.Context) error {
	return m.each(ctx, func(conn string, p *goose.Provider) error {
		results, err := p.Up(ctx)
		for _, r := range results {
			m.log.InfoContext(ctx, "migration applied", "connection", conn, "file", r.Source.Path, "duration", r.Duration)
		}

		if err != nil {
			return fmt.Errorf("migrate %s: %w", conn, err)
		}

		if len(results) == 0 {
			m.log.InfoContext(ctx, "migrations up to date", "connection", conn)
		}

		return nil
	})
}

// Status lists every migration of every connection, oldest version first.
func (m *Migrator) Status(ctx context.Context) ([]Status, error) {
	var out []Status

	err := m.each(ctx, func(conn string, p *goose.Provider) error {
		statuses, err := p.Status(ctx)
		if err != nil {
			return fmt.Errorf("status %s: %w", conn, err)
		}

		for _, s := range statuses {
			out = append(out, Status{
				Connection: conn,
				Version:    s.Source.Version,
				File:       s.Source.Path,
				Applied:    s.State == goose.StateApplied,
				AppliedAt:  s.AppliedAt,
			})
		}

		return nil
	})

	return out, err
}

// MarkApplied records versions as applied on conn without running them. It is
// for adopting a database whose schema changes were applied by hand before this
// package tracked them: record exactly the migrations it already has, and Up runs
// the rest. Every version must be a migration file of conn; versions already
// recorded are skipped.
func (m *Migrator) MarkApplied(ctx context.Context, conn string, versions []int64) error {
	p, db, err := m.provider(conn)
	if err != nil {
		return err
	}

	known := map[int64]bool{}
	for _, s := range p.ListSources() {
		known[s.Version] = true
	}

	for _, v := range versions {
		if !known[v] {
			return fmt.Errorf("mark applied %s: no migration with version %d", conn, v)
		}
	}

	// Status creates the version table on a database that has none yet.
	statuses, err := p.Status(ctx)
	if err != nil {
		return fmt.Errorf("mark applied %s: %w", conn, err)
	}

	applied := map[int64]bool{}

	for _, s := range statuses {
		if s.State == goose.StateApplied {
			applied[s.Source.Version] = true
		}
	}

	store, err := database.NewStore(dialectOf(db.Name()), goose.DefaultTablename)
	if err != nil {
		return fmt.Errorf("mark applied %s: %w", conn, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("mark applied %s: %w", conn, err)
	}

	return withTx(ctx, sqlDB, func(tx *sql.Tx) error {
		for _, v := range versions {
			if applied[v] {
				continue
			}

			if err := store.Insert(ctx, tx, database.InsertRequest{Version: v}); err != nil {
				return fmt.Errorf("mark applied %s %d: %w", conn, v, err)
			}

			applied[v] = true
		}

		return nil
	})
}

// each runs fn for every connection that has a migrations directory, in name order.
func (m *Migrator) each(ctx context.Context, fn func(conn string, p *goose.Provider) error) error {
	conns, err := m.connections()
	if err != nil {
		return err
	}

	for _, conn := range conns {
		if err := ctx.Err(); err != nil {
			return err
		}

		p, _, err := m.provider(conn)
		if err != nil {
			return err
		}

		// Never p.Close(): it closes the registry's connection pool.
		if err := fn(conn, p); err != nil {
			return err
		}
	}

	return nil
}

// connections are fsys's top-level directories. Each must be a registered
// connection: a directory without one is a typo, not a database to skip.
func (m *Migrator) connections() ([]string, error) {
	entries, err := fs.ReadDir(m.fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	var conns []string

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		if !m.reg.Has(e.Name()) {
			return nil, fmt.Errorf("migrations/%s: no database connection named %q", e.Name(), e.Name())
		}

		conns = append(conns, e.Name())
	}

	sort.Strings(conns)

	return conns, nil
}

func (m *Migrator) provider(conn string) (*goose.Provider, *gorm.DB, error) {
	db, err := m.reg.Get(conn)
	if err != nil {
		return nil, nil, fmt.Errorf("migrate %s: %w", conn, err)
	}

	dialect := dialectOf(db.Name())
	if dialect == "" {
		return nil, nil, fmt.Errorf("migrate %s: unsupported driver %q", conn, db.Name())
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("migrate %s: %w", conn, err)
	}

	dir, err := fs.Sub(m.fsys, conn)
	if err != nil {
		return nil, nil, fmt.Errorf("migrate %s: %w", conn, err)
	}

	opts := []goose.ProviderOption{
		goose.WithAllowOutofOrder(true),
		goose.WithDisableGlobalRegistry(true),
	}

	// Two deploys, or two instances migrating at start, wait for each other.
	if dialect == goose.DialectMySQL {
		// goose's lock reads DATETIME columns into time.Time; without parseTime it
		// fails every attempt and gives up only after five minutes.
		if d, ok := db.Dialector.(*gormmysql.Dialector); ok && d.DSNConfig != nil && !d.DSNConfig.ParseTime {
			return nil, nil, fmt.Errorf("migrate %s: the MySQL DSN needs parseTime=true", conn)
		}

		locker, err := lock.NewMySQLTableLocker()
		if err != nil {
			return nil, nil, fmt.Errorf("migrate %s: %w", conn, err)
		}

		opts = append(opts, goose.WithLocker(locker))
	}

	p, err := goose.NewProvider(dialect, sqlDB, dir, opts...)
	if errors.Is(err, goose.ErrNoMigrations) {
		return nil, nil, fmt.Errorf("migrations/%s: no migration files", conn)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("migrate %s: %w", conn, err)
	}

	return p, db, nil
}

func dialectOf(driver string) goose.Dialect {
	switch driver {
	case "mysql":
		return goose.DialectMySQL
	case "sqlite":
		return goose.DialectSQLite3
	case "postgres":
		return goose.DialectPostgres
	default:
		return ""
	}
}

func withTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()

		return err
	}

	return tx.Commit()
}
