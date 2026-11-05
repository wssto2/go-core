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

import (
	"embed"
	"io/fs"
	"regexp"
	"strings"
	"testing/fstest"
)

// Files are the migrations, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS

// PrefixSQL puts prefix before the three table names in authz SQL. Comment lines
// and the index and constraint names (uq_role_bindings, ...) stay as they are:
// those are per table. The prefixed DDL is generated from the files by this one
// rewrite, so they stay the only source.
func PrefixSQL(sql, prefix string) string {
	if prefix == "" {
		return sql
	}
	lines := strings.Split(sql, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "--") {
			lines[i] = tableName.ReplaceAllString(l, prefix+"$1")
		}
	}
	return strings.Join(lines, "\n")
}

var tableName = regexp.MustCompile(`\b(roles|role_permissions|role_bindings)\b`)

// WithPrefix is Files for tables named with a prefix ("authz_" gives
// authz_roles, authz_role_permissions, authz_role_bindings), for a database where
// roles is taken. Use the same prefix for the store: access.WithTablePrefix or
// gormstore.WithTablePrefix. Versions are unchanged, so one database cannot hold
// the plain and the prefixed set through goose; pick one per database.
func WithPrefix(prefix string) fs.FS {
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(Files, ".")
	if err != nil {
		panic(err) // the embedded FS always has its root
	}
	for _, e := range entries {
		b, err := Files.ReadFile(e.Name())
		if err != nil {
			panic(err)
		}
		out[e.Name()] = &fstest.MapFile{Data: []byte(PrefixSQL(string(b), prefix))}
	}
	return out
}
