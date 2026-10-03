package accesshttp_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access/accesshttp"
	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/contract"
)

var update = flag.Bool("update", false, "rewrite the golden TypeScript files")

// The TypeScript contract is generated from the declared routes alone, and the
// result is committed under testdata: a change to a route, an input or a
// response shows up here as a diff to review (go test ./access/accesshttp -update).
func TestContractMatchesGolden(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, contract.Generate(dir, accesshttp.Declare().Contract()))

	for _, name := range []string{"entities.ts", "schemas.ts", "routes.ts"} {
		raw, err := os.ReadFile(filepath.Join(dir, "access", name)) //nolint:gosec // temp dir
		require.NoError(t, err)

		_, got, _ := strings.Cut(string(raw), "\n") // the first line names the go-core version

		golden := filepath.Join("testdata", "access", name)
		if *update {
			require.NoError(t, os.MkdirAll(filepath.Dir(golden), 0o750))
			require.NoError(t, os.WriteFile(golden, []byte(got), 0o600)) //nolint:gosec // fixed testdata path
		}

		want, err := os.ReadFile(golden) //nolint:gosec // fixed testdata path
		require.NoError(t, err)
		require.Equal(t, string(want), got, "%s differs (run go test ./access/accesshttp -update and review the diff)", golden)
	}
}

// The routes require the module's fixed permission ids, so the generated
// TypeScript always matches.
func TestRoutesRequireTheFixedPermissions(t *testing.T) {
	got := map[string]bool{}

	for _, spec := range accesshttp.Declare().Contract().Specs() {
		if spec.Permission != "" {
			got[spec.Permission] = true
		}
	}

	for _, id := range admin.PermissionIDs() {
		require.True(t, got[id], id)
	}
}
