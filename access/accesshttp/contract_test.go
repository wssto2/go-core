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
	require.NoError(t, contract.Generate(dir, accesshttp.Declare("", admin.DefaultPermissions).Contract()))

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

// The permissions the routes require are the ones the application's
// permission union must contain: the contract names them.
func TestRoutesRequireTheConfiguredPermissions(t *testing.T) {
	custom := admin.Permissions{
		ViewRoles: "team.role:view", ManageRoles: "team.role:manage", DeleteRoles: "team.role:delete",
		ViewAccess: "team.member:view", ManageBindings: "team.member:manage",
	}

	got := map[string]bool{}

	for _, spec := range accesshttp.Declare("", custom).Contract().Specs() {
		if spec.Permission != "" {
			got[spec.Permission] = true
		}
	}

	for _, id := range custom.All() {
		require.True(t, got[id], id)
	}
}
