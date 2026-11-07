//nolint:paralleltest // tests in the style of the package's others
package migrations_test

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/characteristics/migrations"
	"github.com/wssto2/go-core/database/dbtest"
)

func TestMigrationsAreMariaDB103Portable(t *testing.T) {
	dbtest.RequirePortable(t, migrations.Files)
}

func names(t *testing.T, fsys fs.FS) []string {
	t.Helper()

	entries, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)

	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}

	return out
}

func TestFirstShipsFewerLevels(t *testing.T) {
	require.Len(t, names(t, migrations.Files), migrations.Levels)
	require.Equal(t, []string{"20261107000001_characteristics_level1.sql"}, names(t, migrations.First(1)))
	require.Len(t, names(t, migrations.First(2)), 2)
	require.Len(t, names(t, migrations.First(3)), 3)

	_, err := fs.ReadFile(migrations.First(2), "20261107000003_characteristics_level3.sql")
	require.ErrorIs(t, err, fs.ErrNotExist)

	dbtest.RequirePortable(t, migrations.First(2))
}

func ExampleFiles() {
	entries, _ := fs.ReadDir(migrations.Files, ".")
	for _, e := range entries {
		fmt.Println(e.Name())
	}
	// Output:
	// 20261107000001_characteristics_level1.sql
	// 20261107000002_characteristics_level2.sql
	// 20261107000003_characteristics_level3.sql
}
