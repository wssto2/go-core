//nolint:paralleltest // tests in the style of the package
package contract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/route"
)

// A row with no fixed shape is Record<string, unknown>.
func TestGenerate_MapRow_IsARecordOfUnknown(t *testing.T) {
	fixedVersion(t)

	dir := t.TempDir()
	g := route.Group("rows",
		route.Get[route.None, []map[string]any]("/rows").Name("rows.list"),
		route.Get[route.None, map[string]any]("/row").Name("rows.one"),
	)
	require.NoError(t, Generate(dir, g))

	src, err := os.ReadFile(filepath.Join(dir, "rows", "routes.ts")) //nolint:gosec // test temp dir
	require.NoError(t, err)
	assert.Contains(t, string(src), "Record<string, unknown>[]")
	assert.Contains(t, string(src), "Record<string, unknown>")
}
