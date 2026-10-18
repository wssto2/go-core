//nolint:exhaustruct,exhaustruct_v5,paralleltest // tests in the style of the package's others
package go2ts_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database/types"
	"github.com/wssto2/go-core/go2ts"
)

// Level is an int on the wire as text, the way authz.Qualifier is.
type Level int

func (l Level) MarshalText() ([]byte, error) { return []byte("level"), nil }

// PtrLevel marshals through a pointer receiver.
type PtrLevel int

func (l *PtrLevel) MarshalText() ([]byte, error) { return []byte("ptr"), nil }

// JSONLevel has both: encoding/json prefers MarshalJSON, here a number.
type JSONLevel int

func (l JSONLevel) MarshalText() ([]byte, error) { return []byte("text"), nil }
func (l JSONLevel) MarshalJSON() ([]byte, error) { return []byte("1"), nil }

// Addr is a struct written as text.
type Addr struct{ host string }

func (a Addr) MarshalText() ([]byte, error) { return []byte(a.host), nil }

// Digest is a byte array written as text, like a UUID.
type Digest [16]byte

func (d Digest) MarshalText() ([]byte, error) { return []byte("digest"), nil }

type PlainLevel int // no methods: stays a number

type TextFields struct {
	Level    Level              `json:"level"              validation:"required"`
	Maybe    *Level             `json:"maybe"`
	Many     []Level            `json:"many"`
	ByKey    map[Level]string   `json:"byKey"`
	ByName   map[string]Level   `json:"byName"`
	Ptr      PtrLevel           `json:"ptr"`
	PtrMany  []*PtrLevel        `json:"ptrMany"`
	JSON     JSONLevel          `json:"json"`
	Addr     Addr               `json:"addr"`
	Digest   Digest             `json:"digest"`
	Plain    PlainLevel         `json:"plain"`
	Nested   map[string][]Level `json:"nested"`
	Optional Level              `json:"optional,omitempty" validation:"max:8"`
}

func read(t *testing.T, dir, name string) string {
	t.Helper()

	src, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // a file of this test's temp directory
	require.NoError(t, err)

	return string(src)
}

func TestATextMarshalerIsAStringInTypes(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, go2ts.GenerateTypes([]any{TextFields{}}, dir))

	src := read(t, dir, "TextFields.ts")
	for _, want := range []string{
		"level: string;",
		"maybe: string | null;",
		"many: string[];",
		"byKey: Record<string, string>;",
		"byName: Record<string, string>;",
		"ptr: string;",
		"ptrMany: string[];",
		"json: number;",
		"addr: string;",
		"digest: string;",
		"plain: number;",
		"nested: Record<string, string[]>;",
		"optional?: string;",
	} {
		require.Contains(t, src, want)
	}

	_, err := os.Stat(filepath.Join(dir, "Addr.ts"))
	require.True(t, os.IsNotExist(err), "a struct written as text has no file of its own")
}

func TestATextMarshalerIsAStringInZod(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, go2ts.GenerateSchemas([]any{TextFields{}}, dir))

	src := read(t, dir, "TextFields.ts")
	for _, want := range []string{
		"level: z.string().min(1),", // required means non-empty, not a non-zero number
		"maybe: z.string().nullable().optional(),",
		"many: z.array(z.string()),",
		"ptr: z.string(),",
		"json: z.number().int(),",
		"addr: z.string(),",
		"digest: z.string(),",
		"plain: z.number().int(),",
		"optional: z.string().max(8).optional(),",
	} {
		require.Contains(t, src, want)
	}

	require.NotContains(t, src, "refine")

	_, err := os.Stat(filepath.Join(dir, "Addr.ts"))
	require.True(t, os.IsNotExist(err), "a struct written as text has no schema of its own")
}

func TestADeclaredEnumOfATextMarshalerIsItsUnion(t *testing.T) {
	values := []string{"own", "all"}
	entries := []any{TextFields{}, go2ts.Enum(reflect.TypeFor[Level](), values)}

	types := t.TempDir()
	require.NoError(t, go2ts.GenerateTypes(entries, types))

	src := read(t, types, "TextFields.ts")
	for _, want := range []string{
		"level: Level;", "maybe: Level | null;", "many: Level[];", "ptr: string;",
		"byKey: Record<string, string>;", "optional?: Level;",
	} {
		require.Contains(t, src, want)
	}

	require.Contains(t, read(t, types, "TextFields.ts"), "import type { Level } from './Level';")
	require.Contains(t, read(t, types, "Level.ts"), `export type Level = "own" | "all";`)

	schemas := t.TempDir()
	require.NoError(t, go2ts.GenerateSchemas(entries, schemas))

	zod := read(t, schemas, "TextFields.ts")
	for _, want := range []string{
		"level: LevelSchema,", "maybe: LevelSchema.nullable().optional(),",
		"many: z.array(LevelSchema),", "optional: LevelSchema.optional(),",
	} {
		require.Contains(t, zod, want)
	}

	require.Contains(t, read(t, schemas, "Level.ts"), `export const LevelSchema = z.enum(["own", "all"]);`)
}

type Pinned struct {
	When    time.Time            `json:"when"`
	Maybe   *time.Time           `json:"maybe"`
	Day     types.Date           `json:"day"`
	NullDay types.NullDate       `json:"nullDay"`
	At      types.DateTime       `json:"at"`
	Name    types.String         `json:"name"`
	NullStr types.NullString     `json:"nullStr"`
	Count   types.Int            `json:"count"`
	NullInt types.NullInt        `json:"nullInt"`
	Ratio   types.Float          `json:"ratio"`
	Flag    types.Bool           `json:"flag"`
	Kind    types.Enum           `json:"kind"`
	Legacy  types.LegacyBoolEnum `json:"legacy"`
	ZeroDay types.ZeroDate       `json:"zeroDay"`
	ByTime  map[string]time.Time `json:"byTime"`
}

// The mapping of the types go2ts names itself must not move when a type that
// writes itself as text is read.
func TestTheTypesGo2tsKnowAreUnchanged(t *testing.T) {
	types := t.TempDir()
	require.NoError(t, go2ts.GenerateTypes([]any{Pinned{}}, types))

	src := read(t, types, "Pinned.ts")
	for _, want := range []string{
		"when: string;", "maybe: string | null;", "day: string;", "nullDay: string | null;", "at: string;",
		"name: string;", "nullStr: string | null;", "count: number;", "nullInt: number | null;", "ratio: number;",
		"flag: boolean;", "kind: boolean;", "legacy: LegacyBoolEnum;", "zeroDay: ZeroDate;",
		"byTime: Record<string, string>;",
	} {
		require.Contains(t, src, want)
	}

	schemas := t.TempDir()
	require.NoError(t, go2ts.GenerateSchemas([]any{Pinned{}}, schemas))

	zod := read(t, schemas, "Pinned.ts")
	for _, want := range []string{
		"when: z.string().datetime({ offset: true }),",
		"maybe: z.string().datetime({ offset: true }).nullable().optional(),",
		"day: z.string().date(),",
		"nullDay: z.string().date().nullable().optional(),",
		"at: z.string().datetime({ offset: true }),",
		"name: z.string(),",
		"nullStr: z.string().nullable().optional(),",
		"count: z.number().int(),", "ratio: z.number(),", "flag: z.boolean(),",
		"kind: z.number().int(),", "legacy: LegacyBoolEnumSchema,", "zeroDay: ZeroDateSchema,",
	} {
		require.Contains(t, zod, want)
	}
}
