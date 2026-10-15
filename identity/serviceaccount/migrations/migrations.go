// Package migrations holds the goose migrations of the service account tables: service_accounts,
// service_account_keys and service_account_key_notices, as MariaDB 10.3 / MySQL DDL. identity.WithServiceAccounts
// registers them on the connection identity's tables are on.
//
// An application that already has the tables (arv-next's, with dealer_id and responsible_user_id where these
// have tenant_id and owner_id) renames the two columns in a migration of its own and marks these files
// applied. Versions are the release date; a released file is never edited, a change is a new file.
package migrations

import "embed"

// Files are the migrations, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS
