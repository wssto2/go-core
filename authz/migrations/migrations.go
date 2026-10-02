// Package migrations holds the goose migrations of the authz tables: roles,
// role_permissions and role_bindings, as MariaDB 10.3 / MySQL DDL. An
// application installs them on the connection the stores use:
//
//	app.Migrations(authzmigrations.Files)         // the primary connection
//	app.Migrations(authzmigrations.Files, Shared) // another one
//
// The files are the same DDL as gormstore.MySQLSchema, so an application that
// created the tables from it adopts them with MarkApplied (or just runs them:
// every statement is IF NOT EXISTS). Versions are the release date; a released
// file is never edited, a change is a new file.
package migrations

import "embed"

// Files are the migrations, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS
