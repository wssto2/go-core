package migrations_test

import (
	"io/fs"

	"fmt"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/identity/migrations"
)

func TestMigrationsAreMariaDB103Portable(t *testing.T) {
	dbtest.RequirePortable(t, migrations.Files)
}

func ExampleFiles() {
	entries, _ := migrations.Files.ReadDir(".")
	for _, e := range entries {
		fmt.Println(e.Name())
	}
	// Output:
	// 20261016000000_identity_accounts.sql
	// 20261016000001_identity_signins.sql
	// 20261016000002_identity_tokens.sql
	// 20261016000003_identity_verification_codes.sql
	// 20261016000004_identity_reauth_attempts.sql
	// 20261016000005_identity_accounts_phone.sql
	// 20261016000006_identity_accounts_email_unique.sql
}

func TestWithoutAccountsKeepsTheSessionTables(t *testing.T) {
	entries, err := fs.ReadDir(migrations.WithoutAccounts(), ".")
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	require.Equal(t, []string{
		"20261016000001_identity_signins.sql", "20261016000002_identity_tokens.sql",
		"20261016000003_identity_verification_codes.sql", "20261016000004_identity_reauth_attempts.sql",
	}, names)

	_, err = fs.ReadFile(migrations.WithoutAccounts(), "20261016000000_identity_accounts.sql")
	require.ErrorIs(t, err, fs.ErrNotExist)
}
