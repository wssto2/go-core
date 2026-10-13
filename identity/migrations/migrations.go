// Package migrations holds the goose migrations of the identity tables:
// accounts, user_signins, tokens (the sessions), user_verification_codes and
// user_reauth_attempts and a phone column on accounts, as MariaDB 10.3 / MySQL
// DDL. An application installs them on the connection the store uses;
// identity.Install does it for you:
//
//	app.Migrations(migrations.Files)         // the primary connection
//	app.Migrations(migrations.Files, Shared) // another one
//
// user_signins, tokens, user_verification_codes and user_reauth_attempts are
// arv-next's tables as they are, so an
// application that has them adopts the files with MarkApplied (or just runs
// them: every statement is IF NOT EXISTS). Versions are the release date; a
// released file is never edited, a change is a new file.
package migrations

import (
	"embed"
	"io/fs"
	"slices"
)

// Files are the migrations, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS

// WithoutAccounts returns Files without the files that only the accounts table needs
// (the table itself, its phone column and the unique e-mail), for an application
// that keeps its people in a table of its own (identity.WithAccounts): the
// sign-ins, tokens, verification codes and re-authentication attempts remain.
func WithoutAccounts() fs.FS { return withoutAccounts{Files} }

type withoutAccounts struct{ fs.FS }

// accountsOnly says whether WithoutAccounts leaves the file out.
func accountsOnly(name string) bool {
	switch name {
	case "20261016000000_identity_accounts.sql", "20261016000005_identity_accounts_phone.sql",
		"20261016000006_identity_accounts_email_unique.sql":
		return true
	}

	return false
}

func (w withoutAccounts) Open(name string) (fs.File, error) {
	if accountsOnly(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	return w.FS.Open(name)
}

func (w withoutAccounts) ReadDir(name string) ([]fs.DirEntry, error) {
	all, err := fs.ReadDir(w.FS, name)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(all, func(e fs.DirEntry) bool { return accountsOnly(e.Name()) }), nil
}
