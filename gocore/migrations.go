package gocore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/migrate"
	"gorm.io/gorm"
)

// Migrations collects a flat directory of goose migration files (an embed.FS
// of *.sql at its root, as a module ships them) to run on one connection: the
// primary one, or the one named as the second argument.
//
//	//go:embed *.sql
//	var migrations embed.FS
//
//	app.Migrations(migrations)         // the primary connection
//	app.Migrations(migrations, Shared) // another one
//
// Nothing runs until the application migrates: "./myapp migrate", see RunCommand.
func (a *App) Migrations(files fs.FS, conn ...database.Connection) {
	if len(conn) > 1 {
		a.fail("Migrations was given more than one connection", "call Migrations once per connection")
		return
	}

	name := ""
	if len(conn) == 1 {
		name = conn[0].String()
	}

	a.migrations = append(a.migrations, migrationSource{conn: name, fsys: files, flat: true})
	a.migrateNow()
}

// MigrationsByConnection collects the application's own migrations: one
// directory per connection name, each holding goose files.
//
//	migrations/
//	  local/20260930121914_user_signins.sql
//	  shared/20261002080000_configurator_index.sql
//
// Nothing runs until the application migrates: "./myapp migrate", see RunCommand.
func (a *App) MigrationsByConnection(dirs fs.FS) {
	a.migrations = append(a.migrations, migrationSource{fsys: dirs})
	a.migrateNow()
}

// Schema is a feature's tables: its goose files, which every real database
// runs, and how to create the same tables from its GORM models, which tests on
// SQLite use instead because SQLite cannot run MySQL DDL.
type Schema struct {
	// Files is a flat directory of goose files, as for Migrations.
	Files fs.FS
	// Models creates the tables from the models, for example gormstore.Migrate. It
	// runs only in an application created with WithAutoMigrate (gocoretest.New) on
	// a SQLite connection, in place of Files; a test that needs the real DDL runs
	// Files on MySQL or MariaDB through dbtest.
	Models func(*gorm.DB) error
}

// Schema collects a feature's tables for the primary connection, or the one
// named as the second argument. Outside tests it is Migrations(s.Files, conn).
//
//	app.Schema(gocore.Schema{Files: migrations.Files, Models: gormstore.Migrate})
//
// A feature should keep a schema test that holds Files equal to Models on MySQL
// and MariaDB, so the tables tests get are the ones production gets.
func (a *App) Schema(s Schema, conn ...database.Connection) {
	if len(conn) > 1 {
		a.fail("Schema was given more than one connection", "call Schema once per connection")
		return
	}

	name := ""
	if len(conn) == 1 {
		name = conn[0].String()
	}

	a.migrations = append(a.migrations, migrationSource{conn: name, fsys: s.Files, flat: true, models: s.Models})
	a.migrateNow()
}

type migrationSource struct {
	conn string
	fsys fs.FS
	flat bool
	// models, when set, replaces the files on SQLite under WithAutoMigrate.
	models func(*gorm.DB) error
	// created is set once models ran.
	created bool
}

// Migrate applies every pending migration collected by Migrations and
// MigrationsByConnection, connection by connection. Run never migrates: it
// refuses to start while any migration is pending. A deploy runs
// "./myapp migrate" (see RunCommand) before "./myapp"; call Migrate yourself in
// tests and in an application with its own command line. It does nothing when no migrations were collected. On MySQL a failed
// migration's DDL is not rolled back, so the error names the file to repair.
func (a *App) Migrate(ctx context.Context) error {
	if err := a.createFromModels(); err != nil {
		return err
	}

	m, err := a.migrator()
	if err != nil || m == nil {
		return err
	}

	return m.Up(ctx)
}

// fromModels reports whether the source's tables are created from its models
// here: in a test application, on SQLite.
func (a *App) fromModels(src migrationSource) bool {
	if a.autoMigrate == nil || src.models == nil || a.registry == nil {
		return false
	}

	db, err := a.connection(src.conn)

	return err == nil && db.Name() == "sqlite"
}

func (a *App) connection(name string) (*gorm.DB, error) {
	if name == "" {
		name = a.registry.PrimaryName()
	}

	return a.registry.Database(database.Connection(name))
}

// createFromModels creates, once, the tables of the sources that use their models.
func (a *App) createFromModels() error {
	for i := range a.migrations {
		src := &a.migrations[i]
		if src.created || !a.fromModels(*src) {
			continue
		}

		db, err := a.connection(src.conn)
		if err != nil {
			return err
		}

		if err := src.models(db); err != nil {
			return fmt.Errorf("gocore: creating a feature's tables from its models: %w", err)
		}

		src.created = true
	}

	return nil
}

// migrator is nil when no migrations were collected.
func (a *App) migrator() (*migrate.Migrator, error) {
	if len(a.migrations) == 0 {
		return nil, nil
	}

	if a.registry == nil {
		if len(a.problems) > 0 {
			return nil, &StartupError{Problems: append([]Problem(nil), a.problems...)}
		}

		return nil, errors.New("gocore: migrations were collected but no database is configured: " +
			"add a connection to the database config, or pass gocore.WithRegistry")
	}

	m := migrate.New(a.registry, nil, a.log)

	collected := 0

	for _, src := range a.migrations {
		if a.fromModels(src) {
			continue
		}

		collected++

		if src.flat {
			m.Add(src.conn, src.fsys)
		} else {
			m.AddDirs(src.fsys)
		}
	}

	if collected == 0 {
		return nil, nil
	}

	return m, nil
}

// migrateNow applies the migrations as they are collected, when the App was
// created with WithAutoMigrate.
func (a *App) migrateNow() {
	if a.autoMigrate == nil {
		return
	}

	if err := a.Migrate(a.autoMigrate); err != nil {
		a.fail("migrating on install failed: "+err.Error(), "fix the migration named in the message")
	}
}

// pendingProblems reports migrations that have not run yet, or that cannot be
// merged (two files with one version).
func (a *App) pendingProblems(ctx context.Context) []Problem {
	m, err := a.migrator()
	if err != nil {
		return []Problem{{What: err.Error(), Fix: "see the message"}}
	}

	if m == nil {
		return nil
	}

	pending, err := m.Pending(ctx)
	if err != nil {
		return []Problem{{What: "the migrations could not be checked: " + err.Error(),
			Fix: "fix the migration files or the database connection named in the message"}}
	}

	if len(pending) == 0 {
		return nil
	}

	names := make([]string, 0, len(pending))
	for _, p := range pending {
		names = append(names, p.Connection+"/"+p.File)
	}

	shown := names
	if len(shown) > 5 {
		shown = shown[:5]
	}

	list := strings.Join(shown, ", ")
	if len(names) > len(shown) {
		list += fmt.Sprintf(" and %d more", len(names)-len(shown))
	}

	return []Problem{{
		What: fmt.Sprintf("%d migration(s) are pending: %s", len(names), list),
		Fix:  fmt.Sprintf("run %q before starting it; Run never migrates", binary()+" migrate"),
	}}
}

// binary is the name the operator typed to start the application.
func binary() string {
	if len(os.Args) == 0 {
		return "app"
	}

	return filepath.Base(os.Args[0])
}
