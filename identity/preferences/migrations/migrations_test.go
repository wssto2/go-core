//nolint:paralleltest // tests in the style of the package's others
package migrations_test

import (
	"fmt"
	"testing"

	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/identity/preferences/migrations"
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
	// 20261027000000_identity_preferences.sql
}
