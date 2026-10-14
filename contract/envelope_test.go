//nolint:testpackage,paralleltest,noinlineerr // uses the package's fixtures
package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wssto2/go-core/route"
)

func TestEnvelopedOutputsHaveATypeScriptForm(t *testing.T) {
	fixedVersion(t)

	page := route.Get[ListInput, route.Page[Row]]("/p").Name("env.page")
	meta := route.Post[Draft, route.WithMeta[[]Row, Stats]]("/m").Name("env.meta").Status(201)
	group := route.Group("env", page, meta)

	dir := t.TempDir()
	if err := Generate(dir, group); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(filepath.Join(dir, "env", "routes.ts")) //nolint:gosec // temp dir
	for _, want := range []string{`route<ListInput, Row[]>`, `route<Draft, Row[]>`} {
		if !strings.Contains(string(raw), want) || strings.Contains(string(raw), "ListResult") {
			t.Fatalf("routes.ts misses %q:\n%s", want, raw)
		}
	}
}

func TestWithMetaExportsItsMetaType(t *testing.T) {
	fixedVersion(t)

	meta := route.Post[Draft, route.WithMeta[[]Row, Stats]]("/m").Name("env.meta")
	dir := t.TempDir()

	if err := Generate(dir, route.Group("env", meta)); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(filepath.Join(dir, "env", "entities.ts")) //nolint:gosec // temp dir
	if !strings.Contains(string(raw), "Stats") {
		t.Fatalf("entities.ts misses the meta type:\n%s", raw)
	}
}
