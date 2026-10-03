// Package migrations holds the goose migration of the notification inbox, as
// MariaDB 10.3 / MySQL DDL: the notifications table. notification.Install
// registers it; an application that wants the table on its own passes it to
// app.Migrations:
//
//	app.Migrations(migrations.Files)         // the primary connection
//	app.Migrations(migrations.Files, Shared) // another one
//
// The file is arv-next's notifications table as it is, so an application that
// has it adopts the file with MarkApplied (or just runs it: the statement is
// IF NOT EXISTS). Versions are the release date; a released file is never
// edited, a change is a new file.
package migrations

import "embed"

// Files are the migrations, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS
