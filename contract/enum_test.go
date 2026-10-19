package contract_test

import (
	"errors"
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

func TestAnEnumCanHaveAnotherTypeScriptName(t *testing.T) {
	set := route.Post[PriorityInput, PriorityOut]("/priority").Name("tickets.set")
	g := route.Group("tickets", set).Types(route.Enum(PriorityLow, PriorityHigh).As("TicketPriority"))

	entities := generated(t, g, "entities.ts")
	require.Contains(t, entities, `export type TicketPriority = "low" | "high";`)
	require.Contains(t, entities, "priority: TicketPriority;")

	schemas := generated(t, g, "schemas.ts")
	require.Contains(t, schemas, `export const TicketPrioritySchema = z.enum(["low", "high"]);`)
	require.Contains(t, schemas, "priority: TicketPrioritySchema,")
}

// Rank is an int that is a JSON string on the wire, the way authz.Qualifier is.
type Rank int

const (
	RankLow Rank = iota + 1
	RankHigh
)

func (r Rank) MarshalText() ([]byte, error) {
	if r < RankLow || r > RankHigh {
		return nil, errors.New("invalid rank") //nolint:err113 // a test double
	}

	return []byte([]string{"", "low", "high"}[r]), nil
}

// JSONRank has MarshalJSON too, which encoding/json prefers.
type JSONRank int

func (JSONRank) MarshalText() ([]byte, error) { return []byte("x"), nil }
func (JSONRank) MarshalJSON() ([]byte, error) { return []byte("1"), nil }

type RankInput struct {
	Rank Rank `json:"rank" validation:"required"`
}

type RankOut struct {
	Rank  Rank     `json:"rank"`
	Other *Rank    `json:"other"`
	Many  []Rank   `json:"many"`
	Plain JSONRank `json:"plain"`
}

func TestATextEnumIsRenderedAsAUnionAndAZodEnum(t *testing.T) {
	t.Parallel()

	set := route.Post[RankInput, RankOut]("/rank").Name("tickets.rank")
	grp := route.Group("tickets", set).Types(route.TextEnum(RankLow, RankHigh))

	entities := generated(t, grp, "entities.ts")
	require.Contains(t, entities, `export type Rank = "low" | "high";`)
	require.Contains(t, entities, "rank: Rank;")
	require.Contains(t, entities, "other: Rank | null;")
	require.Contains(t, entities, "many: Rank[];")
	require.Contains(t, entities, "plain: number;", "MarshalJSON wins over MarshalText")

	schemas := generated(t, grp, "schemas.ts")
	require.Contains(t, schemas, `export const RankSchema = z.enum(["low", "high"]);`)
	require.Contains(t, schemas, "rank: RankSchema,")
}

func TestATextMarshalerThatIsNotDeclaredIsAString(t *testing.T) {
	t.Parallel()

	set := route.Post[RankInput, RankOut]("/rank").Name("tickets.rank")
	g := route.Group("tickets", set)

	require.Contains(t, generated(t, g, "entities.ts"), "rank: string;")
	require.Contains(t, generated(t, g, "schemas.ts"), "rank: z.string().min(1),")
}

func TestATextEnumNeedsATypeWrittenAsText(t *testing.T) {
	t.Parallel()

	show := route.Get[ShowInput, RankOut]("/rank/:id")

	err := contract.Generate(t.TempDir(), route.Group("tickets", show).Types(route.TextEnum(JSONRank(1))))
	require.ErrorContains(t, err, "written as text")

	require.Panics(t, func() { route.TextEnum(Rank(0), Rank(9)) }, "a value that does not marshal is a declaration bug")
}

// A route whose output is a text-written int (or a list of them) has the string type in the route table, as go2ts
// gives it in entities.ts.
func TestATextWrittenIntIsAStringInTheRouteTable(t *testing.T) {
	t.Parallel()

	one := route.Get[ShowInput, Rank]("/ranks/:id").Name("ranks.show")
	many := route.Get[route.Empty, []Rank]("/ranks").Name("ranks.list")
	json := route.Get[ShowInput, JSONRank]("/json-ranks/:id").Name("ranks.json")
	routes := generated(t, route.Group("ranks", one, many, json), "routes.ts")

	require.Contains(t, routes, "string[]")
	require.NotContains(t, routes, "Rank[]")
	require.NotContains(t, routes, "number[]")
	require.Regexp(t, `show: route<ShowInput, string>`, routes)
	require.Regexp(t, `json: route<ShowInput, number>`, routes, "MarshalJSON wins over MarshalText, as in encoding/json")
}
