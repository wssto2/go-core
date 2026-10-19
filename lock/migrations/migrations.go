// Package migrations holds the goose migration of the locks table, as MariaDB 10.3 / MySQL DDL. lock.Install
// registers it; an application that builds its own lock.Store has no table. Versions are the release date; a
// released file is never edited, a change is a new file.
package migrations

import "embed"

// Files are the migrations, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS
