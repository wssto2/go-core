//nolint:exhaustruct,exhaustruct_v5 // fixtures set only the fields under test
package go2ts_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/go2ts"
)

type unknownRuleInput struct {
	Name string `json:"name" validation:"required|exists:users,name|max:20"`
}

type serverOnlyInput struct {
	Password string `json:"password" validation:"required|password|max:20"`
	Again    string `json:"again"    validation:"same:password|max:20"`
	Kind     int    `json:"kind"     validation:"max:9"`
	Extra    string `json:"extra"    validation:"required_if:kind,1|confirmed:again|different:password|max:20"`
}

type declaredRuleInput struct {
	Code string `json:"code" validation:"unique_for_test|max:5"`
}

type inYearMonthInput struct {
	Kind  string `json:"kind"  validation:"in:foo,bar"`
	Only  string `json:"only"  validation:"required|in:one"`
	Digit string `json:"digit" validation:"in:1,2"`
	Num   int    `json:"num"   validation:"in:1,2"`
	Year  string `json:"year"  validation:"year"`
	Born  string `json:"born"  validation:"required|year"`
	Month string `json:"month" validation:"required|month"`
}

func TestGenerateSchemas_UnknownRuleFailsNamingStructFieldAndRule(t *testing.T) {
	t.Parallel()

	err := go2ts.GenerateSchemas([]any{unknownRuleInput{}}, t.TempDir())
	require.Error(t, err, "an unknown validation rule generated a schema that silently lacks it")

	for _, want := range []string{"unknownRuleInput", "Name", `"exists"`} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestGenerateSchemas_ServerOnlyRulesAreNotAnError(t *testing.T) {
	t.Parallel()

	require.NoError(t, go2ts.GenerateSchemas([]any{serverOnlyInput{}}, t.TempDir()),
		"the validator's cross-field and password rules have no Zod form and are declared server-only")
}

func TestServerOnly_DeclaresAnApplicationsOwnRule(t *testing.T) {
	t.Parallel()

	require.Error(t, go2ts.GenerateSchemas([]any{declaredRuleInput{}}, t.TempDir()), "the rule is not declared yet")

	go2ts.ServerOnly("unique_for_test")

	require.NoError(t, go2ts.GenerateSchemas([]any{declaredRuleInput{}}, t.TempDir()), "a declared rule is server-only")
}

func TestGenerateSchemas_InIsAnEnumForStringsAndYearAndMonthAreRegexes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, go2ts.GenerateSchemas([]any{inYearMonthInput{}}, dir))

	got, err := os.ReadFile(filepath.Join(dir, "inYearMonthInput.ts")) //nolint:gosec // a file of the test's temp dir
	require.NoError(t, err)

	for _, want := range []string{
		`kind: z.enum(["foo", "bar"]),`,
		`only: z.enum(["one"]),`,
		`digit: z.enum(["1", "2"]),`,
		`num: z.union([z.literal(1), z.literal(2)]),`,
		`year: z.string().regex(/^\d{4}$/).or(z.literal('')),`,
		`born: z.string().min(1).regex(/^\d{4}$/),`,
		`month: z.string().min(1).regex(/^(0?[1-9]|1[0-2])$/),`,
	} {
		assert.Contains(t, string(got), want)
	}
}

type trimInput struct {
	Name  string  `json:"name"  validation:"required|max:100|trim"`
	Order string  `json:"order" validation:"trim|max:100|required"`
	Mail  string  `json:"mail"  validation:"email|trim"`
	Note  *string `json:"note"  validation:"max:50|trim"`
	Need  *string `json:"need"  validation:"required|trim|max:50"`
	Bare  string  `json:"bare"  validation:"trim"`
}

type trimInInput struct {
	Kind string `json:"kind" validation:"in:a,b|trim"`
}

type trimIntInput struct {
	Count int `json:"count" validation:"trim"`
}

func TestGenerateSchemas_TrimIsTheFirstCallWhateverTheTagOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, go2ts.GenerateSchemas([]any{trimInput{}}, dir))

	got, err := os.ReadFile(filepath.Join(dir, "trimInput.ts")) //nolint:gosec // a file of the test's temp dir
	require.NoError(t, err)

	for _, want := range []string{
		`name: z.string().trim().min(1).max(100),`,
		`order: z.string().trim().max(100).min(1),`,
		`mail: z.string().trim().email().or(z.literal('')),`,
		`note: z.string().trim().max(50).nullable().optional(),`,
		`need: z.string().trim().min(1).max(50).nullable(),`,
		`bare: z.string().trim(),`,
	} {
		assert.Contains(t, string(got), want)
	}
}

func TestGenerateSchemas_TrimOnAnEnumOrANumberFailsNamingStructAndField(t *testing.T) {
	t.Parallel()

	for input, want := range map[any][]string{
		trimInInput{}:  {"trimInInput", "Kind"},
		trimIntInput{}: {"trimIntInput", "Count"},
	} {
		err := go2ts.GenerateSchemas([]any{input}, t.TempDir())
		require.Error(t, err)

		for _, w := range want {
			assert.Contains(t, err.Error(), w)
		}
	}
}
