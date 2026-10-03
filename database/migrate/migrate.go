// Package migrate runs SQL migrations for every connection of a database.Registry.
//
// An application keeps its migrations in one directory per connection name,
// each file a goose SQL migration (https://github.com/pressly/goose) named
// <version>_<name>.sql:
//
//	migrations/
//	  local/20260930121914_user_signins.sql
//	  shared/20261002080000_configurator_index.sql
//
// A module ships one flat directory of such files (an embed.FS) for the
// connection it lives on, and hands it over with Add. All sources of a
// connection run as one set, in version order; two files with the same version
// stop the run and are both named. A module's files use its release date as the
// version (20261015000000_authz_roles.sql) and never change once released.
//
// Each database records what ran on it in its own goose_db_version table, so a
// market's databases each know their own state. Migrations may run out of order:
// a version older than the newest applied one still runs if it has not — which is
// what happens when branches merge, and when a database was adopted with
// MarkApplied and lacks an old migration.
//
//	m := migrate.New(registry, migrationsFS, logger)
//	m.Add("", authzmigrations.Files) // a module's files, on the primary connection
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
	"strconv"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"

	coredb "github.com/wssto2/go-core/database"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Migrator runs the migrations of its sources against the registry's connections.
type Migrator struct {
	reg     *coredb.Registry
	sources []source
	log     *slog.Logger
}

// source is the migration files of one place: a directory per connection
// (conn is empty and flat false) or one flat directory for one connection.
type source struct {
	conn string // "" is the primary connection when flat
	fsys fs.FS
	flat bool
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
// embed.FS narrowed with fs.Sub to its "migrations" directory. It may be nil
// when every source is added with Add.
func New(reg *coredb.Registry, fsys fs.FS, log *slog.Logger) *Migrator {
	m := &Migrator{reg: reg, log: log}
	if fsys != nil {
		m.AddDirs(fsys)
	}

	return m
}

// Add adds a flat directory of migration files for one connection, as a
// module ships them (an embed.FS of *.sql at its root). An empty conn is the
// registry's primary connection. It returns m so calls chain.
func (m *Migrator) Add(conn string, files fs.FS) *Migrator {
	m.sources = append(m.sources, source{conn: conn, fsys: files, flat: true})
	return m
}

// AddDirs adds more migrations in the layout New takes: one directory per
// connection name. It returns m so calls chain.
func (m *Migrator) AddDirs(fsys fs.FS) *Migrator {
	m.sources = append(m.sources, source{fsys: fsys})
	return m
}

// Pending lists the migrations no database has applied yet, oldest first
// within each connection. An empty result means every database is up to date.
func (m *Migrator) Pending(ctx context.Context) ([]Status, error) {
	all, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}

	var pending []Status

	for _, s := range all {
		if !s.Applied {
			pending = append(pending, s)
		}
	}

	return pending, nil
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

// connections are the connections that have migrations: the top-level
// directories of the per-connection sources and the connections modules named.
// Each must be registered: a name without a connection is a typo, not a
// database to skip.
func (m *Migrator) connections() ([]string, error) {
	seen := map[string]bool{}

	for _, src := range m.sources {
		names, err := src.connections(m.reg)
		if err != nil {
			return nil, err
		}

		for _, n := range names {
			seen[n] = true
		}
	}

	conns := make([]string, 0, len(seen))
	for n := range seen {
		conns = append(conns, n)
	}

	sort.Strings(conns)

	return conns, nil
}

func (s source) connections(reg *coredb.Registry) ([]string, error) {
	if s.flat {
		conn := s.conn
		if conn == "" {
			conn = reg.PrimaryName()
		}

		if conn == "" {
			return nil, errors.New("migrations: a module added migrations for the primary connection but no connection is registered")
		}

		if !reg.Has(conn) {
			return nil, fmt.Errorf("migrations for connection %q: no database connection with that name", conn)
		}

		return []string{conn}, nil
	}

	entries, err := fs.ReadDir(s.fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	var conns []string

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		if !reg.Has(e.Name()) {
			return nil, fmt.Errorf("migrations/%s: no database connection named %q", e.Name(), e.Name())
		}

		conns = append(conns, e.Name())
	}

	return conns, nil
}

// files merges every source's files for conn into one directory.
func (m *Migrator) files(conn string) (fs.FS, error) {
	merged := &mergedFS{files: map[string]fs.FS{}}
	versions := map[int64]string{}

	for n, src := range m.sources {
		dir, label, err := src.dirFor(m.reg, conn, n+1)
		if err != nil {
			return nil, err
		}

		if dir == nil {
			continue
		}

		entries, err := fs.ReadDir(dir, ".")
		if err != nil {
			return nil, fmt.Errorf("migrate %s: reading %s: %w", conn, label, err)
		}

		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".sql") {
				continue
			}

			version, err := goose.NumericComponent(name)
			if err != nil {
				return nil, fmt.Errorf("migrate %s: %s in %s: %w", conn, name, label, err)
			}

			desc := name + " (" + label + ")"
			if other, dup := versions[version]; dup {
				return nil, fmt.Errorf("migrate %s: version %d is used by two migration files: %s and %s; "+
					"rename the one you own to a different version", conn, version, other, desc)
			}

			versions[version] = desc
			merged.files[name] = dir
		}
	}

	if len(merged.files) == 0 {
		return nil, fmt.Errorf("migrations for %s: no migration files", conn)
	}

	return merged, nil
}

// dirFor is the directory of this source's files for conn, nil when the source
// has none for it, and a label for messages.
func (s source) dirFor(reg *coredb.Registry, conn string, n int) (fs.FS, string, error) {
	if s.flat {
		c := s.conn
		if c == "" {
			c = reg.PrimaryName()
		}

		if c != conn {
			return nil, "", nil
		}

		return s.fsys, "module migrations, source " + strconv.Itoa(n), nil
	}

	info, err := fs.Stat(s.fsys, conn)
	if err != nil || !info.IsDir() {
		return nil, "", nil //nolint:nilerr // no directory for this connection in this source
	}

	dir, err := fs.Sub(s.fsys, conn)
	if err != nil {
		return nil, "", fmt.Errorf("migrate %s: %w", conn, err)
	}

	return dir, "migrations/" + conn, nil
}

// mergedFS presents files from several directories as one flat directory.
type mergedFS struct {
	files map[string]fs.FS // file name -> the directory that holds it
}

func (m *mergedFS) Open(name string) (fs.File, error) {
	dir, ok := m.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	return dir.Open(name)
}

func (m *mergedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}

	names := make([]string, 0, len(m.files))
	for n := range m.files {
		names = append(names, n)
	}

	sort.Strings(names)

	entries := make([]fs.DirEntry, 0, len(names))

	for _, n := range names {
		info, err := fs.Stat(m.files[n], n)
		if err != nil {
			return nil, err
		}

		entries = append(entries, fs.FileInfoToDirEntry(info))
	}

	return entries, nil
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

	dir, err := m.files(conn)
	if err != nil {
		return nil, nil, err
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
