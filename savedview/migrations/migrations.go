// Package migrations holds the goose migration of the saved-view module, as MariaDB 10.3 / MySQL DDL: the table
// saved_views. savedview.Install registers it; an application that wants the table on its own passes it to
// app.Migrations:
//
//	app.Migrations(migrations.Files)
//
// The file creates the table exactly as an application that grew it as saved_filters has it after renaming the table
// and its two indexes, so such an application renames and adopts the version with MarkApplied. Versions are the
// release date; a released file is never edited, a change is a new file.
package migrations

import "embed"

// Files are the migrations, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS
