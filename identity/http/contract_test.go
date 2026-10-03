package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/contract"
	"github.com/wssto2/go-core/identity/account"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/navigation"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// withoutVersion drops the first line, which names the go-core version that wrote the file.
func withoutVersion(src string) string {
	_, rest, _ := strings.Cut(src, "\n")

	return rest
}

// The TypeScript identity declares is held to testdata/ts, which vue-core
// commits for its sign-in and session screens. Run with -update after a change
// to a route or a type, and review the diff: it is the contract.
func TestTheDeclaredContractMatchesGolden(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, contract.Generate(dir, identityhttp.Routes))

	files, err := filepath.Glob(filepath.Join(dir, "identity", "*.ts"))
	require.NoError(t, err)
	require.Len(t, files, 3, "entities, schemas and routes")

	for _, file := range files {
		got, err := os.ReadFile(file) //nolint:gosec // a file of the temp dir this test just wrote
		require.NoError(t, err)

		golden := filepath.Join("testdata", "ts", filepath.Base(file))

		if *update {
			require.NoError(t, os.MkdirAll(filepath.Dir(golden), 0o750))
			require.NoError(t, os.WriteFile(golden, []byte(withoutVersion(string(got))), 0o600)) //nolint:gosec // a golden file of this package

			continue
		}

		want, err := os.ReadFile(golden) //nolint:gosec // a golden file of this package
		require.NoError(t, err, "run the test with -update to write %s", golden)
		require.Equal(t, string(want), withoutVersion(string(got)), "%s differs from its golden: run with -update and review", golden)
	}
}

// The payload a login answers is held to testdata/session_payload.json, which
// vue-core parses with parseSessionPayload in its own fixture test.
func TestThePayloadMatchesGolden(t *testing.T) {
	held := authz.MyAccess{
		Subject: authz.Subject{Kind: authz.KindUser, ID: 1},
		Permissions: map[string]authz.PermissionAccess{
			"tickets.ticket:view": {
				Scope: authz.Scope{Level: "organization"}, Qualifier: authz.QualifierAll,
				Clauses: []authz.ClauseInfo{{Scope: authz.Scope{Level: "organization"}, Qualifier: authz.QualifierAll, Role: "Agent", BindingID: 4}},
			},
		},
	}

	h := newHarness(t, func(c *identityhttp.Config) {
		c.Access = func(context.Context) (authz.MyAccess, error) { return held, nil }
		c.Navigation = func(context.Context, account.Account) ([]navigation.Node, error) {
			return []navigation.Node{
				{I18n: "nav.tickets", Icon: "ticket", Route: "tickets.index", Permissions: []string{"tickets.ticket:view"}},
				{I18n: "nav.admin", Children: []navigation.Node{
					{I18n: "nav.roles", Route: "roles.index", Permissions: []string{"access.role:view"}},
				}},
			}, nil
		}
	})

	var pretty bytes.Buffer

	require.NoError(t, json.Indent(&pretty, h.login().Body.Bytes(), "", "  "))

	golden := filepath.Join("testdata", "session_payload.json")
	got := pretty.String() + "\n"

	if *update {
		require.NoError(t, os.WriteFile(golden, []byte(got), 0o600)) //nolint:gosec // a golden file of this package

		return
	}

	want, err := os.ReadFile(golden) //nolint:gosec // a golden file of this package
	require.NoError(t, err, "run the test with -update to write %s", golden)
	require.Equal(t, string(want), got, "the payload differs from its golden: run with -update and review")
}

// Every string an input takes has a bound (max:), so the Zod schema keeps it.
func TestEveryStringInputIsBounded(t *testing.T) {
	for _, spec := range identityhttp.Routes.Specs() {
		if spec.In == nil || spec.In.Kind() != reflect.Struct {
			continue
		}

		for i := range spec.In.NumField() {
			f := spec.In.Field(i)
			if f.Type.Kind() != reflect.String {
				continue
			}

			require.Containsf(t, f.Tag.Get("validation"), "max:", "%s.%s of %s has no max: bound", spec.In.Name(), f.Name, spec)
		}
	}
}
