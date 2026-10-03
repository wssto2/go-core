package migrations_test

import (
	"fmt"
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
}
