//nolint:testpackage,paralleltest,noinlineerr // uses the package's fixtures
package contract

import (
	"net/url"
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

type OnlyFilters struct {
	Filters url.Values `query:"*"`
}

type PagedFilters struct {
	Page    int        `query:"page"`
	Filters url.Values `json:"-"     query:"*"`
}

func generatedFile(t *testing.T, group *route.Contract, file string) string {
	t.Helper()
	fixedVersion(t)

	dir := t.TempDir()
	if err := Generate(dir, group); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(filepath.Join(dir, group.Name(), file)) //nolint:gosec // temp dir

	return string(raw)
}

func TestRoutesDoNotImportTheMetaEntity(t *testing.T) {
	meta := route.Post[Draft, route.WithMeta[[]Row, Stats]]("/m").Name("env.meta")
	group := route.Group("env", meta)

	if routes := generatedFile(t, group, "routes.ts"); strings.Contains(routes, "Stats") {
		t.Fatalf("routes.ts imports the meta entity:\n%s", routes)
	}
}

func TestFreeFormQueryFieldIsLeftOutOfSchemas(t *testing.T) {
	group := route.Group("free", route.Get[OnlyFilters, []Row]("/a").Name("free.a"))

	if schemas := generatedFile(t, group, "schemas.ts"); strings.Contains(schemas, "*") {
		t.Fatalf("schemas.ts has the free-form field:\n%s", schemas)
	}
}

func TestFreeFormQueryInputTakesFilters(t *testing.T) {
	group := route.Group("free",
		route.Get[OnlyFilters, []Row]("/a").Name("free.a"),
		route.Get[PagedFilters, []Row]("/b").Name("free.b"))

	routes := generatedFile(t, group, "routes.ts")
	for _, want := range []string{
		`route<Record<string, QueryValue>, Row[]>("GET", "/a")`,
		`route<PagedFilters & Record<string, QueryValue>, Row[]>("GET", "/b")`,
		`import { route, type QueryValue } from`,
	} {
		if !strings.Contains(routes, want) {
			t.Fatalf("routes.ts misses %q:\n%s", want, routes)
		}
	}
}

type DealerPath struct {
	DealerID int `path:"dealer_id"`
}

type ListRequest struct {
	DealerPath

	Query url.Values `json:"-" query:"*"`
}

type ShowRequest struct {
	DealerPath
}

func TestAnEmptySchemaIsLeftOutOfTheRouteInput(t *testing.T) {
	group := route.Group("dl",
		route.Get[ListRequest, []Row]("/dealers/:dealer_id/rows").Name("dl.list"),
		route.Get[ShowRequest, []Row]("/dealers/:dealer_id/one").Name("dl.one"))

	routes := generatedFile(t, group, "routes.ts")
	for _, want := range []string{
		`route<{ dealer_id: number } & Record<string, QueryValue>, Row[]>("GET", "/dealers/:dealer_id/rows")`,
		`route<{ dealer_id: number }, Row[]>("GET", "/dealers/:dealer_id/one")`,
	} {
		if !strings.Contains(routes, want) {
			t.Fatalf("routes.ts misses %q:\n%s", want, routes)
		}
	}

	if strings.Contains(routes, "ListRequest") || strings.Contains(routes, "ShowRequest") {
		t.Fatalf("routes.ts still names an empty schema:\n%s", routes)
	}

	if schemas := generatedFile(t, group, "schemas.ts"); strings.Contains(schemas, "ListRequest") {
		t.Fatalf("an unused empty schema is emitted:\n%s", schemas)
	}
}
