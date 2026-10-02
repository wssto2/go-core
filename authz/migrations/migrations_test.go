package migrations_test

import (
	"fmt"
	"testing"

	"github.com/wssto2/go-core/authz/migrations"
	"github.com/wssto2/go-core/database/dbtest"
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
	// 20261015000000_authz_roles.sql
	// 20261015000001_authz_role_permissions.sql
	// 20261015000002_authz_role_bindings.sql
}
