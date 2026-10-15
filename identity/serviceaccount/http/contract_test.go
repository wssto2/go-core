//nolint:gochecknoglobals,lll,paralleltest // tests in the style of the package's others
package http_test

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/contract"
	sahttp "github.com/wssto2/go-core/identity/serviceaccount/http"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// withoutVersion drops the first line, which names the go-core version that wrote the file.
func withoutVersion(src string) string {
	_, rest, _ := strings.Cut(src, "\n")

	return rest
}

// The TypeScript the service accounts declare is held to testdata/ts, which vue-core commits for its screens.
// Run with -update after a change to a route or a type, and review the diff: it is the contract.
func TestTheDeclaredContractMatchesGolden(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, contract.Generate(dir, sahttp.Contract))

	files, err := filepath.Glob(filepath.Join(dir, "serviceaccount", "*.ts"))
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

// Every string an input takes has a bound (max:), so the Zod schema keeps it.
func TestEveryStringInputIsBounded(t *testing.T) {
	for _, spec := range sahttp.Contract.Specs() {
		if spec.In == nil || spec.In.Kind() != reflect.Struct {
			continue
		}

		for f := range spec.In.Fields() {
			if f.Type.Kind() != reflect.String || f.Tag.Get("path") != "" || f.Tag.Get("query") != "" {
				continue
			}

			require.Containsf(t, f.Tag.Get("validation"), "max:", "%s.%s of %s has no max: bound", spec.In.Name(), f.Name, spec)
		}
	}
}
