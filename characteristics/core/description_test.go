//nolint:exhaustruct,exhaustruct_v5,paralleltest,varnamelen,lll,goconst // tests in the style of the package's others
package core_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/characteristics/core"
)

func problems(t *testing.T, tree core.Tree) []string {
	t.Helper()

	_, err := tree.Prepare()
	require.Error(t, err)

	var described *core.DescriptionError
	require.ErrorAs(t, err, &described)

	return described.Problems
}

func TestTheDefaultTreesAreValid(t *testing.T) {
	for n := 1; n <= 3; n++ {
		prepared, err := core.DefaultTree(n).Prepare()
		require.NoError(t, err)
		require.Len(t, prepared.Levels, n)
		require.Equal(t, []string{"hr", "en"}, prepared.Languages)
		require.Equal(t, "id", prepared.Levels[0].ID)
		require.Equal(t, core.DefaultMaxText, prepared.Levels[0].NameMax)
	}

	third := core.DefaultTree(3).Levels[2]
	require.Equal(t, "level2_id", third.Parent)
	require.Equal(t, []string{"level1_id"}, third.Ancestors)
}

func TestPrepareDoesNotChangeTheCallersTree(t *testing.T) {
	tree := core.DefaultTree(2)
	tree.Levels[0].ID = ""
	tree.Levels[1].Fields = []core.Field{{Column: "code", Label: core.Text{HR: "Kod"}, Type: core.TypeText}}

	prepared, err := tree.Prepare()
	require.NoError(t, err)
	require.Equal(t, core.DefaultMaxText, prepared.Levels[1].Fields[0].Max)
	require.Zero(t, tree.Levels[1].Fields[0].Max)
	require.Empty(t, tree.Levels[0].ID)
	require.Equal(t, "id", prepared.Levels[0].ID)
}

func TestInvalidDescriptionsNameEveryProblem(t *testing.T) {
	field := func(f core.Field) core.Tree {
		tree := core.DefaultTree(3)
		f.Label = core.Text{HR: "Polje"}
		tree.Levels[2].Fields = []core.Field{f}

		return tree
	}
	mutate := func(fn func(*core.Tree)) core.Tree {
		tree := core.DefaultTree(3)
		fn(&tree)

		return tree
	}

	for name, tc := range map[string]struct {
		tree core.Tree
		want string
	}{
		"no levels":            {core.Tree{}, "there are no levels"},
		"bad table name":       {mutate(func(t *core.Tree) { t.Levels[0].Table = "a; DROP TABLE x" }), "is not a table name"},
		"two levels one table": {mutate(func(t *core.Tree) { t.Levels[1].Table = t.Levels[0].Table }), "is used by another level"},
		"missing name":         {mutate(func(t *core.Tree) { t.Levels[0].Name = "" }), "the name column is missing"},
		"missing label":        {mutate(func(t *core.Tree) { t.Levels[0].Label = core.Text{} }), "the label (hr) is missing"},
		"root with a parent":   {mutate(func(t *core.Tree) { t.Levels[0].Parent = "x" }), "the root level has no parent column"},
		"child without parent": {mutate(func(t *core.Tree) { t.Levels[1].Parent = "" }), "the parent column is missing"},
		"half the ancestors":   {mutate(func(t *core.Tree) { t.Levels[2].Ancestors = []string{"a", "b"} }), "ancestors must name all 1 levels above the parent, or none"},
		"column used twice":    {mutate(func(t *core.Tree) { t.Levels[0].Meta = "name" }), `column "name" is both the name and the meta`},
		"bad column name":      {mutate(func(t *core.Tree) { t.Levels[0].Order = "order desc" }), "is not a column name"},
		"two kinds of i18n":    {mutate(func(t *core.Tree) { t.Levels[0].Translations.Columns = map[string]string{"hr": "x"} }), "a JSON column or one column per language, not both"},
		"language not listed": {mutate(func(t *core.Tree) {
			t.Levels[0].Translations = core.Translations{Columns: map[string]string{"de": "x"}}
		}), `"de", which is not among the languages`},
		"activation no active":    {mutate(func(t *core.Tree) { t.Levels[0].Active = "" }), "activation columns are named but the level has no active column"},
		"unknown type":            {field(core.Field{Column: "a", Type: "money"}), `type "money" is not text, number, yesno or choice`},
		"choice without values":   {field(core.Field{Column: "a", Type: core.TypeChoice}), "a choice field needs choices"},
		"choices on a text":       {field(core.Field{Column: "a", Type: core.TypeText, Choices: []core.Choice{{Value: "x"}}}), "only a choice field has choices"},
		"repeated choice":         {field(core.Field{Column: "a", Type: core.TypeChoice, Choices: []core.Choice{{Value: "x", Label: core.Text{HR: "X"}}, {Value: "x", Label: core.Text{HR: "X"}}}}), `choice "x" is listed twice`},
		"branch of no level":      {field(core.Field{Column: "a", Type: core.TypeText, Under: &core.Under{Level: 3, ID: 1}}), "under must name a row (id above 0) of a level above this one (1 to 2)"},
		"branch without a row":    {field(core.Field{Column: "a", Type: core.TypeText, Under: &core.Under{Level: 2}}), "under must name a row"},
		"field on a taken column": {field(core.Field{Column: "name", Type: core.TypeText}), `column "name" is both the name and an extra column`},
		"same column twice, whole level": {mutate(func(t *core.Tree) {
			f := core.Field{Column: "a", Label: core.Text{HR: "A"}, Type: core.TypeText}
			t.Levels[2].Fields = []core.Field{f, f}
		}), `column "a" is described twice for the same rows`},
	} {
		t.Run(name, func(t *testing.T) { require.Contains(t, strings.Join(problems(t, tc.tree), "\n"), tc.want) })
	}

	// Several problems come back together, each naming its level.
	all := problems(t, mutate(func(t *core.Tree) {
		t.Levels[0].Name = ""
		t.Levels[1].Parent = ""
	}))
	require.Len(t, all, 2)
	require.Contains(t, all[0], "level 1 (characteristics_level1)")
	require.Contains(t, all[1], "level 2 (characteristics_level2)")
}

func TestTheSameColumnMayMeanDifferentThingsInDifferentBranches(t *testing.T) {
	tree := core.DefaultTree(3)
	tree.Levels[2].Fields = []core.Field{
		{Column: "code", Label: core.Text{HR: "Kod marke"}, Type: core.TypeText, Under: &core.Under{Level: 2, ID: 2}},
		{Column: "code", Label: core.Text{HR: "ISO kod"}, Type: core.TypeText, Under: &core.Under{Level: 2, ID: 3}},
	}

	prepared, err := tree.Prepare()
	require.NoError(t, err)

	level := prepared.Levels[2]
	require.Equal(t, "Kod marke", level.FieldsFor([]int{1, 2})[0].Label.HR)
	require.Equal(t, "ISO kod", level.FieldsFor([]int{1, 3})[0].Label.HR)
	require.Empty(t, level.FieldsFor([]int{1, 4}))
}

func TestTextInFallsBack(t *testing.T) {
	require.Equal(t, "Marke", core.Text{HR: "Marke", EN: "Brands"}.In("hr"))
	require.Equal(t, "Brands", core.Text{HR: "Marke", EN: "Brands"}.In("en"))
	require.Equal(t, "Marke", core.Text{HR: "Marke"}.In("en"))
	require.Equal(t, "Brands", core.Text{EN: "Brands"}.In("hr"))
}
