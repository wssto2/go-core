package migrations_test

import (
	"fmt"
	"testing"

	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/identity/serviceaccount/migrations"
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
	// 20261020000000_service_accounts.sql
	// 20261020000001_service_account_keys.sql
	// 20261020000002_service_account_key_notices.sql
}
