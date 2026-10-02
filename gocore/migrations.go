package gocore

import (
	"io/fs"

	"github.com/wssto2/go-core/database"
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
}

type migrationSource struct {
	conn string
	fsys fs.FS
	flat bool
}
