// Package migrations holds the goose migration of the preferences table, as MariaDB 10.3 / MySQL DDL.
// identity.WithPreferences registers it on the connection identity's tables are on, unless the application brings
// its own preferences.Store (then there is no table). Versions are the release date; a released file is never
// edited, a change is a new file.
package migrations

import "embed"

// Files are the migrations, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS
