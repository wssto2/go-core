package gocore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/migrate"
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
// Nothing runs until the application migrates, see Migrate.
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
// Nothing runs until the application migrates, see Migrate.
func (a *App) MigrationsByConnection(dirs fs.FS) {
	a.migrations = append(a.migrations, migrationSource{fsys: dirs})
	a.migrateNow()
}

type migrationSource struct {
	conn string
	fsys fs.FS
	flat bool
}

// Migrate applies every pending migration collected by Migrations and
// MigrationsByConnection, connection by connection. Run never migrates: it
// refuses to start while any migration is pending. The application decides
// when to migrate, in its main (a "migrate" argument) or as a deploy step:
//
//	if len(os.Args) > 1 && os.Args[1] == "migrate" {
//	    err = app.Migrate(ctx)
//	} else {
//	    err = app.Run()
//	}
//
// It does nothing when no migrations were collected. On MySQL a failed
// migration's DDL is not rolled back, so the error names the file to repair.
func (a *App) Migrate(ctx context.Context) error {
	m, err := a.migrator()
	if err != nil || m == nil {
		return err
	}

	return m.Up(ctx)
}

// migrator is nil when no migrations were collected.
func (a *App) migrator() (*migrate.Migrator, error) {
	if len(a.migrations) == 0 {
		return nil, nil
	}

	if a.registry == nil {
		return nil, errors.New("gocore: migrations were collected but no database is configured: " +
			"add a connection to the database config, or pass gocore.WithRegistry")
	}

	m := migrate.New(a.registry, nil, a.log)

	for _, src := range a.migrations {
		if src.flat {
			m.Add(src.conn, src.fsys)
		} else {
			m.AddDirs(src.fsys)
		}
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
		Fix:  "run the application's migrate step (app.Migrate) before starting it; Run never migrates",
	}}
}
