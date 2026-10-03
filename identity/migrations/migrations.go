// Package migrations holds the goose migrations of the identity tables:
// accounts, user_signins and tokens (the sessions), as MariaDB 10.3 / MySQL
// DDL. An application installs them on the connection the store uses;
// identity.Install does it for you:
//
//	app.Migrations(migrations.Files)         // the primary connection
//	app.Migrations(migrations.Files, Shared) // another one
//
// user_signins and tokens are arv-next's tables as they are, so an
// application that has them adopts the files with MarkApplied (or just runs
// them: every statement is IF NOT EXISTS). Versions are the release date; a
// released file is never edited, a change is a new file.
package migrations

import "embed"

// Files are the migrations, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS
