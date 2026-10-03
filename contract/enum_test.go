package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/contract"
	"github.com/wssto2/go-core/route"
)

type Priority string

const (
	PriorityLow  Priority = "low"
	PriorityHigh Priority = "high"
)

type Severity string // named string type that is not declared: stays string

type PriorityInput struct {
	Priority Priority   `json:"priority" validation:"required|max:8"`
	Optional *Priority  `json:"optional"`
	Many     []Priority `json:"many"`
}

type PriorityOut struct {
	Priority Priority `json:"priority"`
	Severity Severity `json:"severity"`
}

func generated(t *testing.T, g *route.Contract, file string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, contract.Generate(dir, g))

	src, err := os.ReadFile(filepath.Join(dir, g.Name(), file)) //nolint:gosec // temp dir
	require.NoError(t, err)

	return string(src)
}

func TestEnumsAreRenderedAsUnionsAndZodEnums(t *testing.T) {
	set := route.Post[PriorityInput, PriorityOut]("/priority").Name("tickets.set")
	g := route.Group("tickets", set).Types(route.Enum(PriorityLow, PriorityHigh))

	entities := generated(t, g, "entities.ts")
	require.Contains(t, entities, `export type Priority = "low" | "high";`)
	require.Contains(t, entities, "priority: Priority;")
	require.Contains(t, entities, "severity: string;", "an undeclared named string type stays string")

	schemas := generated(t, g, "schemas.ts")
	require.Contains(t, schemas, `export const PrioritySchema = z.enum(["low", "high"]);`)
	require.Contains(t, schemas, "priority: PrioritySchema,", "no .min or .max on an enum")
	require.Contains(t, schemas, "optional: PrioritySchema.nullable().optional(),")
	require.Contains(t, schemas, "many: z.array(PrioritySchema),")
	require.Less(t, strings.Index(schemas, "PrioritySchema ="), strings.Index(schemas, "PriorityInputSchema ="), "an enum is declared before the schema using it")
}

func TestUndeclaredEnumStaysString(t *testing.T) {
	set := route.Post[PriorityInput, PriorityOut]("/priority").Name("tickets.set")

	require.Contains(t, generated(t, route.Group("tickets", set), "entities.ts"), "priority: string;")
	require.Contains(t, generated(t, route.Group("tickets", set), "schemas.ts"), "priority: z.string().min(1).max(8),")
}

func TestAnEnumNoInputUsesIsLeftOutOfSchemas(t *testing.T) {
	show := route.Get[ShowInput, PriorityOut]("/priority/:id").Name("tickets.show")
	g := route.Group("tickets", show).Types(route.Enum(PriorityLow, PriorityHigh))

	require.NotContains(t, generated(t, g, "schemas.ts"), "PrioritySchema")
}

func TestEnumProblemsNameTheFix(t *testing.T) {
	show := route.Get[ShowInput, PriorityOut]("/priority/:id")

	err := contract.Generate(t.TempDir(), route.Group("tickets", show).Types(route.Enum[Priority]()))
	require.ErrorContains(t, err, "has no values")

	err = contract.Generate(t.TempDir(), route.Group("tickets", show).Types(route.Enum(PriorityLow, PriorityLow)))
	require.ErrorContains(t, err, `lists "low" twice`)

	err = contract.Generate(t.TempDir(), route.Group("tickets", show).Types(route.Enum("a")))
	require.ErrorContains(t, err, "named string type")
}
