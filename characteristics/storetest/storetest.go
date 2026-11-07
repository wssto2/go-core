// Package storetest is the conformance suite of the characteristics module: what a core.Store over a described tree
// must do, and what the service built on it must do over a real database. Run it from the store's tests once for each
// table shape the store is meant to serve (go-core's own tables, site-builder's, arv-next's).
//
//	storetest.Run(t, storetest.Harness{Tree: tree, New: newStore, Raw: raw, Poke: poke, Undescribed: undescribed})
//
//nolint:cyclop,depguard,exhaustruct,exhaustruct_v5,funlen,gochecknoglobals,goconst,lll,mnd,nonamedreturns,prealloc,thelper,varnamelen // a conformance suite in the style of a test
package storetest

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/characteristics/core"
)

// Harness is the table shape under test. The suite writes through the Store and looks at the rows through Raw, which
// bypasses the description: that is how it sees the columns the description does not name.
type Harness struct {
	// Tree is the prepared description of the shape.
	Tree core.Tree
	// New returns a store over empty tables (auto-increment ids from 1).
	New func(t *testing.T) core.Store
	// Raw returns every column of a row as text, NULL as "NULL", times as "2006-01-02 15:04:05".
	Raw func(t *testing.T, level, id int) map[string]string
	// Poke sets one column of a row directly in SQL.
	Poke func(t *testing.T, level, id int, column string, value any)
	// Undescribed holds, for the levels that have columns the Tree does not name, one such column and a value to
	// put there.
	Undescribed map[int]map[string]any
	// Unset is how a "never" time column reads in this shape besides NULL (a legacy zero-date stand-in); optional.
	Unset string
}

// Run runs the whole suite.
func Run(t *testing.T, h Harness) {
	t.Helper()

	for name, fn := range map[string]func(*testing.T, Harness){
		"RoundTripAndAncestors":       roundTrip,
		"OrderAndMove":                orderAndMove,
		"AuditColumns":                auditColumns,
		"UndescribedColumnsUntouched": undescribed,
		"Translations":                translations,
		"BranchScopedColumn":          branchScoped,
		"CacheDroppedOnWrite":         cacheDropped,
		"DeleteRefusedWhileUsed":      deleteRefused,
		"OptionsOfABranch":            options,
	} {
		t.Run(name, func(t *testing.T) { fn(t, h) })
	}
}

var (
	t1 = time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)
	t2 = t1.Add(time.Hour)
	t3 = t2.Add(time.Hour)
	t4 = t3.Add(time.Hour)
)

type clock struct{ at time.Time }

func (c clock) Now() time.Time { return c.at }

type usageFunc func(level, id int) []core.Use

func (f usageFunc) Uses(_ context.Context, level, id int) ([]core.Use, error) {
	return f(level, id), nil
}

func stampOf(actor int, at time.Time) core.Stamp { return core.Stamp{Actor: actor, At: at} }

// sample is an item of the level with a value in every field that applies.
func sample(tree core.Tree, level int, path []int, name string, active bool) core.Item {
	def, _ := tree.Level(level)
	item := core.Item{Level: level, Path: path, Name: name, Active: active, Values: map[string]any{}}

	if len(path) > 0 {
		item.ParentID = path[len(path)-1]
	}

	if def.Translations.JSON != "" || len(def.Translations.Columns) > 0 {
		item.Translations = map[string]string{"hr": name, "en": name + " en"}
	}

	if def.Meta != "" {
		item.Meta = map[string]any{"logo": "/resources/1"}
	}

	for _, f := range def.FieldsFor(path) {
		item.Values[f.Column] = sampleValue(f)
	}

	return item
}

func sampleValue(f core.Field) any {
	switch f.Type {
	case core.TypeNumber:
		return int64(1)
	case core.TypeYesNo:
		return true
	case core.TypeChoice:
		return f.Choices[0].Value
	case core.TypeText:
		return "abcdefgh"[:min(f.Max, 4)]
	}

	return nil
}

// chain creates one item on each level down to the given one, each under the previous, and returns their ids.
func chain(t *testing.T, store core.Store, tree core.Tree, to int) []int {
	t.Helper()

	var path []int

	for level := 1; level <= to; level++ {
		item := sample(tree, level, path, fmt.Sprintf("Item %d", level), true)
		require.NoError(t, store.Create(t.Context(), &item, stampOf(7, t1)))
		require.NotZero(t, item.ID)

		path = append(path, item.ID)
	}

	return path
}

func service(h Harness, store core.Store, usage core.Usage, ttl time.Duration) *core.Service {
	return core.New(h.Tree, store, core.Config{Usage: usage, Clock: clock{at: t1}, CacheTTL: ttl})
}

func roundTrip(t *testing.T, h Harness) {
	store := h.New(t)
	depth := len(h.Tree.Levels)
	ids := chain(t, store, h.Tree, depth)

	for level := 1; level <= depth; level++ {
		def, _ := h.Tree.Level(level)
		want := sample(h.Tree, level, ids[:level-1], fmt.Sprintf("Item %d", level), true)

		got, err := store.Row(t.Context(), level, ids[level-1])
		require.NoError(t, err)
		require.Equal(t, want.Name, got.Name)
		require.Equal(t, want.ParentID, got.ParentID, "parent of level %d", level)
		require.Equal(t, want.Meta, got.Meta)
		require.Equal(t, orEmpty(want.Translations), got.Translations)

		if def.Active != "" {
			require.True(t, got.Active)
		}

		for column, value := range want.Values {
			require.Equal(t, value, got.Values[column], "level %d %s", level, column)
		}

		// The ancestor columns hold the ids above the parent.
		raw := h.Raw(t, level, ids[level-1])
		for i, column := range def.Ancestors {
			require.Equal(t, strconv.Itoa(ids[i]), raw[column], "level %d ancestor %s", level, column)
		}

		rows, err := store.Rows(t.Context(), level)
		require.NoError(t, err)
		require.Len(t, rows, 1)

		_, err = store.Row(t.Context(), level, 9999)
		require.ErrorIs(t, err, core.ErrNotFound)
	}
}

func orderAndMove(t *testing.T, h Harness) {
	for level := 1; level <= len(h.Tree.Levels); level++ {
		def, _ := h.Tree.Level(level)
		if def.Order == "" {
			continue
		}

		store := h.New(t)
		path := chain(t, store, h.Tree, level-1)

		// A second parent shows that order counts per parent.
		var lone core.Item

		if level > 1 {
			other := sample(h.Tree, level-1, path[:len(path)-1], "Other parent", true)
			require.NoError(t, store.Create(t.Context(), &other, stampOf(7, t1)))

			lone = sample(h.Tree, level, append(slices(path[:len(path)-1]), other.ID), "Lone", true)
			require.NoError(t, store.Create(t.Context(), &lone, stampOf(7, t1)))
		}

		var ids []int

		for _, name := range []string{"A", "B", "C"} {
			item := sample(h.Tree, level, path, name, true)
			require.NoError(t, store.Create(t.Context(), &item, stampOf(7, t1)))
			ids = append(ids, item.ID)
		}

		parent := 0
		if level > 1 {
			parent = path[len(path)-1]
		}

		svc := service(h, store, nil, 0)
		names := func() string {
			var out []string

			rows, err := store.Rows(t.Context(), level)
			require.NoError(t, err)

			for _, row := range rows {
				if row.ParentID == parent {
					out = append(out, row.Name)
				}
			}

			return strings.Join(out, "")
		}

		loneOrder := func() string {
			if level == 1 {
				return "-"
			}

			return h.Raw(t, level, lone.ID)[def.Order]
		}

		require.Equal(t, "ABC", names(), "level %d", level)
		require.Equal(t, map[bool]string{true: "-", false: "1"}[level == 1], loneOrder(), "order restarts under another parent")

		before := h.Raw(t, level, ids[0])

		require.NoError(t, svc.Move(t.Context(), level, ids[2], 1))
		require.Equal(t, "CAB", names())

		require.NoError(t, svc.Move(t.Context(), level, ids[2], 99)) // past the end is the end
		require.Equal(t, "ABC", names())

		require.Equal(t, map[bool]string{true: "-", false: "1"}[level == 1], loneOrder(), "a row under another parent keeps its place")

		// Like Laravel's reorder, moving writes the order column and no audit column.
		after := h.Raw(t, level, ids[0])

		for _, column := range []string{def.Audit.UpdatedBy, def.Audit.UpdatedAt} {
			if column != "" {
				require.Equal(t, before[column], after[column], "level %d %s", level, column)
			}
		}
	}
}

// slices is a copy, so that appending never writes into the caller's array.
func slices(in []int) []int { return append([]int(nil), in...) }

// unset reports whether a "never" value is in the column.
func (h Harness) unset(raw string) bool {
	return raw == "NULL" || raw == "0" || raw == "" || strings.HasPrefix(raw, "0000") || (h.Unset != "" && raw == h.Unset)
}

func (h Harness) expect(t *testing.T, raw map[string]string, column, want string) {
	t.Helper()

	if column != "" {
		require.Equal(t, want, raw[column], column)
	}
}

func (h Harness) expectUnset(t *testing.T, raw map[string]string, column string) {
	t.Helper()

	if column != "" {
		require.True(t, h.unset(raw[column]), "%s should be unset, is %q", column, raw[column])
	}
}

func stamp(at time.Time) string { return at.Format("2006-01-02 15:04:05") }

// auditColumns holds the audit columns to what Laravel's model hooks and arv-next's service write.
func auditColumns(t *testing.T, h Harness) {
	for level := 1; level <= len(h.Tree.Levels); level++ {
		def, _ := h.Tree.Level(level)
		a := def.Audit
		store := h.New(t)
		path := chain(t, store, h.Tree, level-1)

		// Created switched off: the creator and time; the editor and time only where the shape sets them on create.
		item := sample(h.Tree, level, path, "Audited", false)
		require.NoError(t, store.Create(t.Context(), &item, stampOf(7, t1)))

		raw := h.Raw(t, level, item.ID)
		h.expect(t, raw, a.CreatedBy, "7")
		h.expect(t, raw, a.CreatedAt, stamp(t1))

		if a.UpdatedByOnCreate {
			h.expect(t, raw, a.UpdatedBy, "7")
		} else {
			h.expectUnset(t, raw, a.UpdatedBy)
		}

		if a.UpdatedAtOnCreate {
			h.expect(t, raw, a.UpdatedAt, stamp(t1))
		} else {
			h.expectUnset(t, raw, a.UpdatedAt)
		}

		for _, column := range []string{a.ActivatedBy, a.ActivatedAt, a.DeactivatedBy, a.DeactivatedAt} {
			h.expectUnset(t, raw, column)
		}

		// An update: the editor and time; the creator stays.
		item.Name = "Audited again"
		require.NoError(t, store.Update(t.Context(), item, stampOf(8, t2)))

		raw = h.Raw(t, level, item.ID)
		h.expect(t, raw, a.UpdatedBy, "8")
		h.expect(t, raw, a.UpdatedAt, stamp(t2))
		h.expect(t, raw, a.CreatedBy, "7")
		h.expect(t, raw, a.CreatedAt, stamp(t1))

		if def.Active == "" {
			continue
		}

		// Switched on: the activator and time, and the editor.
		require.NoError(t, store.SetActive(t.Context(), level, item.ID, true, stampOf(9, t3)))

		raw = h.Raw(t, level, item.ID)
		h.expect(t, raw, a.ActivatedBy, "9")
		h.expect(t, raw, a.ActivatedAt, stamp(t3))
		h.expect(t, raw, a.UpdatedBy, "9")
		h.expect(t, raw, a.UpdatedAt, stamp(t3))
		h.expectUnset(t, raw, a.DeactivatedBy)
		h.expectUnset(t, raw, a.DeactivatedAt)

		// Switched off: the deactivator and time; the activation stays.
		require.NoError(t, store.SetActive(t.Context(), level, item.ID, false, stampOf(10, t4)))

		raw = h.Raw(t, level, item.ID)
		h.expect(t, raw, a.DeactivatedBy, "10")
		h.expect(t, raw, a.DeactivatedAt, stamp(t4))
		h.expect(t, raw, a.ActivatedBy, "9")
		h.expect(t, raw, a.ActivatedAt, stamp(t3))

		// Switching off a row that is off changes nothing, not even the editor.
		require.NoError(t, store.SetActive(t.Context(), level, item.ID, false, stampOf(11, t4.Add(time.Hour))))
		require.Equal(t, raw, h.Raw(t, level, item.ID))

		// Created switched on: the activator and time at once.
		on := sample(h.Tree, level, path, "Born active", true)
		require.NoError(t, store.Create(t.Context(), &on, stampOf(7, t1)))

		raw = h.Raw(t, level, on.ID)
		h.expect(t, raw, a.ActivatedBy, "7")
		h.expect(t, raw, a.ActivatedAt, stamp(t1))
		h.expectUnset(t, raw, a.DeactivatedAt)
	}
}

// undescribed: a value put in a column the Tree does not name survives every write.
func undescribed(t *testing.T, h Harness) {
	require.NotEmpty(t, h.Undescribed, "the shape must have a column its description leaves out")

	for level, columns := range h.Undescribed {
		def, _ := h.Tree.Level(level)
		store := h.New(t)
		path := chain(t, store, h.Tree, level-1)

		keeper := sample(h.Tree, level, path, "Keeper", true)
		require.NoError(t, store.Create(t.Context(), &keeper, stampOf(7, t1)))

		for column, marker := range columns {
			h.Poke(t, level, keeper.ID, column, marker)
		}

		check := func(step string) {
			raw := h.Raw(t, level, keeper.ID)
			for column, marker := range columns {
				require.Equal(t, fmt.Sprint(marker), raw[column], "level %d %s after %s", level, column, step)
			}
		}

		sibling := sample(h.Tree, level, path, "Sibling", true)
		require.NoError(t, store.Create(t.Context(), &sibling, stampOf(7, t1)))
		check("a sibling was created")

		keeper.Name = "Kept and renamed"
		require.NoError(t, store.Update(t.Context(), keeper, stampOf(8, t2)))
		check("update")

		if def.Active != "" {
			require.NoError(t, store.SetActive(t.Context(), level, keeper.ID, false, stampOf(8, t2)))
			require.NoError(t, store.SetActive(t.Context(), level, keeper.ID, true, stampOf(8, t3)))
			check("switching")
		}

		if def.Order != "" {
			require.NoError(t, service(h, store, nil, 0).Move(t.Context(), level, keeper.ID, 2))
			check("move")
		}

		_, err := service(h, store, nil, 0).Delete(t.Context(), level, sibling.ID, false)
		require.NoError(t, err)
		check("deleting a sibling")

		// The read never shows them either.
		row, err := store.Row(t.Context(), level, keeper.ID)
		require.NoError(t, err)

		for column := range columns {
			require.NotContains(t, row.Values, column)
		}
	}
}

// translations: the JSON column keeps the languages the description does not set; per-language columns are written one
// by one.
func translations(t *testing.T, h Harness) {
	found := false

	for level := 1; level <= len(h.Tree.Levels); level++ {
		def, _ := h.Tree.Level(level)
		if def.Translations.JSON == "" && len(def.Translations.Columns) == 0 {
			continue
		}

		found = true
		store := h.New(t)
		path := chain(t, store, h.Tree, level-1)

		item := sample(h.Tree, level, path, "Named", true)
		require.NoError(t, store.Create(t.Context(), &item, stampOf(7, t1)))

		if def.Translations.JSON != "" {
			h.Poke(t, level, item.ID, def.Translations.JSON, `{"hr":"Named","en":"Named en","de":"Benannt"}`)
		}

		// Only English is given: Croatian stays, and so does a language the tree does not list.
		change := item
		change.Translations = map[string]string{"en": "Renamed"}
		require.NoError(t, store.Update(t.Context(), change, stampOf(8, t2)))

		raw := h.Raw(t, level, item.ID)
		got, err := store.Row(t.Context(), level, item.ID)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"hr": "Named", "en": "Renamed"}, got.Translations)

		if def.Translations.JSON != "" {
			require.Contains(t, raw[def.Translations.JSON], `"de":"Benannt"`)
		}

		// An empty text clears a language.
		change.Translations = map[string]string{"en": ""}
		require.NoError(t, store.Update(t.Context(), change, stampOf(8, t2)))

		got, err = store.Row(t.Context(), level, item.ID)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"hr": "Named"}, got.Translations)

		// Nil keeps them all.
		change.Translations = nil
		change.Name = "Named twice"
		require.NoError(t, store.Update(t.Context(), change, stampOf(8, t2)))

		got, err = store.Row(t.Context(), level, item.ID)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"hr": "Named"}, got.Translations)
	}

	if !found {
		t.Skip("the shape keeps no translations")
	}
}

// scopedField finds a field limited to a branch.
func scopedField(tree core.Tree) (level int, f core.Field, ok bool) {
	for i, def := range tree.Levels {
		for _, field := range def.Fields {
			if field.Under != nil {
				return i + 1, field, true
			}
		}
	}

	return 0, core.Field{}, false
}

// branchScoped: a column described for one branch is read and written there and nowhere else.
func branchScoped(t *testing.T, h Harness) {
	level, field, ok := scopedField(h.Tree)
	if !ok {
		t.Skip("the shape has no branch-scoped column")
	}

	store := h.New(t)
	under := *field.Under
	inside, outside := seedBranches(t, store, h.Tree, level, under)

	// The value is in the database for both; only the branch's row shows it.
	h.Poke(t, level, inside.ID, field.Column, "xy")
	h.Poke(t, level, outside.ID, field.Column, "xy")

	svc := service(h, store, nil, 0)

	in, err := svc.Get(t.Context(), level, inside.ID)
	require.NoError(t, err)
	require.Contains(t, in.Values, field.Column)

	out, err := svc.Get(t.Context(), level, outside.ID)
	require.NoError(t, err)
	require.NotContains(t, out.Values, field.Column)

	// Writing it outside the branch is refused; inside, a read-only field refuses a change and others take it.
	_, err = svc.Update(t.Context(), 1, level, outside.ID, core.Input{Name: "Out", Values: map[string]any{field.Column: "zz"}})
	require.Error(t, err)
	require.Equal(t, 400, apperr.GetHTTPStatus(err))

	_, err = svc.Update(t.Context(), 1, level, inside.ID, core.Input{Name: "In", Values: map[string]any{field.Column: "zz"}})

	if field.ReadOnly {
		require.Equal(t, 400, apperr.GetHTTPStatus(err))
	} else {
		require.NoError(t, err)
		require.Equal(t, "zz", h.Raw(t, level, inside.ID)[field.Column])
	}

	// An update that leaves it alone changes nothing in it, for either row.
	_, err = svc.Update(t.Context(), 1, level, outside.ID, core.Input{Name: "Out again"})
	require.NoError(t, err)
	require.Equal(t, "xy", h.Raw(t, level, outside.ID)[field.Column])
}

// seedBranches builds the tree so that a row of the field's level sits under the branch row and another one under its
// sibling, and returns both. The branch row is the one whose id is under.ID: its level gets that many rows.
func seedBranches(t *testing.T, store core.Store, tree core.Tree, level int, under core.Under) (inside, outside core.Item) {
	t.Helper()
	require.GreaterOrEqual(t, under.ID, 2, "a branch scope needs a sibling row to be outside of it")

	var path []int

	for l := 1; l < under.Level; l++ {
		item := sample(tree, l, path, "Up", true)
		require.NoError(t, store.Create(t.Context(), &item, stampOf(7, t1)))

		path = append(path, item.ID)
	}

	var branches [][]int

	for i := 1; i <= under.ID; i++ {
		item := sample(tree, under.Level, path, fmt.Sprintf("Branch %d", i), true)
		require.NoError(t, store.Create(t.Context(), &item, stampOf(7, t1)))
		require.Equal(t, i, item.ID, "the first rows of an empty table are numbered from 1")

		branches = append(branches, append(slices(path), item.ID))
	}

	descend := func(p []int, name string) core.Item {
		var item core.Item

		for l := under.Level + 1; l <= level; l++ {
			item = sample(tree, l, p, name, true)
			require.NoError(t, store.Create(t.Context(), &item, stampOf(7, t1)))

			p = append(slices(p), item.ID)
		}

		return item
	}

	return descend(branches[under.ID-1], "Inside"), descend(branches[0], "Outside")
}

// cacheDropped: reads are served from the cache until the service writes.
func cacheDropped(t *testing.T, h Harness) {
	store := h.New(t)
	ids := chain(t, store, h.Tree, 1)
	def, _ := h.Tree.Level(1)
	svc := service(h, store, nil, time.Hour)

	read := func() string {
		nodes, err := svc.Tree(t.Context())
		require.NoError(t, err)
		require.Len(t, nodes, 1)

		return nodes[0].Item.Name
	}

	require.Equal(t, "Item 1", read())
	require.Equal(t, "Item 1", read())
	require.Equal(t, 1, svc.Loads(), "the second read came from the cache")

	h.Poke(t, 1, ids[0], def.Name, "Changed behind its back")
	require.Equal(t, "Item 1", read(), "a change made elsewhere shows after the cache expires")

	_, err := svc.Update(t.Context(), 1, 1, ids[0], core.Input{Name: "Renamed"})
	require.NoError(t, err)
	require.Equal(t, "Renamed", read(), "a write through the service drops the cache")

	_, err = svc.Create(t.Context(), 1, 1, 0, core.Input{Name: "Second"})
	require.NoError(t, err)

	nodes, err := svc.Tree(t.Context())
	require.NoError(t, err)
	require.Len(t, nodes, 2)

	_, err = svc.SetActive(t.Context(), 1, 1, nodes[1].Item.ID, true)
	if def.Active != "" {
		require.NoError(t, err)

		again, _ := svc.Get(t.Context(), 1, nodes[1].Item.ID)
		require.True(t, again.Active)
	}

	require.NoError(t, svc.Move(t.Context(), 1, nodes[1].Item.ID, 1))

	if def.Order != "" {
		nodes, err = svc.Tree(t.Context())
		require.NoError(t, err)
		require.Equal(t, "Second", nodes[0].Item.Name)
	}

	_, err = svc.Delete(t.Context(), 1, nodes[1].Item.ID, true)
	require.NoError(t, err)

	nodes, err = svc.Tree(t.Context())
	require.NoError(t, err)
	require.Len(t, nodes, 1)
}

// deleteRefused: a used item, or one with a used item below it, is not deleted unless forced; switching off always
// works; with no usage port the answer says so.
func deleteRefused(t *testing.T, h Harness) {
	store := h.New(t)
	depth := len(h.Tree.Levels)
	ids := chain(t, store, h.Tree, depth)

	used := usageFunc(func(level, id int) []core.Use {
		if level == depth && id == ids[depth-1] {
			return []core.Use{{What: "pages", Count: 2}, {What: "hosts", Count: 1}}
		}

		return nil
	})
	svc := service(h, store, used, 0)

	report, err := svc.Usage(t.Context(), 1, ids[0])
	require.NoError(t, err)
	require.True(t, report.Known)
	require.Equal(t, []core.Use{{What: "pages", Count: 2}, {What: "hosts", Count: 1}}, report.Uses, "the use below counts for the top")
	require.Equal(t, depth-1, report.Children)

	_, err = svc.Delete(t.Context(), 1, ids[0], false)
	require.True(t, apperr.HasReason(err, core.ReasonInUse), "%v", err)
	require.Equal(t, 409, apperr.GetHTTPStatus(err))
	h.mustExist(t, store, 1, ids[0])

	def, _ := h.Tree.Level(depth)
	if def.Active != "" {
		_, err = svc.SetActive(t.Context(), 1, depth, ids[depth-1], false)
		require.NoError(t, err, "deactivating is always possible")
	}

	result, err := svc.Delete(t.Context(), depth, ids[depth-1], true)
	require.NoError(t, err)
	require.Equal(t, 1, result.Deleted)

	// Nothing is used any more, but the item still has an item below it.
	if depth > 2 {
		_, err = svc.Delete(t.Context(), 1, ids[0], false)
		require.True(t, apperr.HasReason(err, core.ReasonHasChildren), "%v", err)
	}

	// Forced, the whole branch goes, deepest first.
	result, err = svc.Delete(t.Context(), 1, ids[0], true)
	require.NoError(t, err)
	require.Equal(t, depth-1, result.Deleted)

	for level := 1; level <= depth; level++ {
		rows, err := store.Rows(t.Context(), level)
		require.NoError(t, err)
		require.Empty(t, rows)
	}

	// No port: the answer says the usage is unknown, and the delete is not blocked.
	again := chain(t, store, h.Tree, 1)
	unknown := service(h, store, nil, 0)

	report, err = unknown.Usage(t.Context(), 1, again[0])
	require.NoError(t, err)
	require.False(t, report.Known)

	result, err = unknown.Delete(t.Context(), 1, again[0], false)
	require.NoError(t, err)
	require.False(t, result.UsageKnown)
}

func (h Harness) mustExist(t *testing.T, store core.Store, level, id int) {
	t.Helper()

	_, err := store.Row(t.Context(), level, id)
	require.NoError(t, err)
}

// options: the active children of one branch, labelled in the request's language, valued by id or a described column.
func options(t *testing.T, h Harness) {
	store := h.New(t)
	ids := chain(t, store, h.Tree, 1)

	if len(h.Tree.Levels) < 2 {
		t.Skip("one level only")
	}

	def, _ := h.Tree.Level(2)
	if def.Active == "" {
		t.Skip("level 2 has no active flag")
	}

	var kids []core.Item

	for _, name := range []string{"On", "Off", "Other on"} {
		item := sample(h.Tree, 2, ids, name, name != "Off")
		require.NoError(t, store.Create(t.Context(), &item, stampOf(7, t1)))

		kids = append(kids, item)
	}

	svc := service(h, store, nil, 0)

	got, err := svc.Options(t.Context(), core.OptionsQuery{Level: 2, Parent: ids[0], Lang: "en"})
	require.NoError(t, err)
	require.Equal(t, []core.Option{
		{ID: kids[0].ID, Value: strconv.Itoa(kids[0].ID), Label: labelIn(def, "On", "en")},
		{ID: kids[2].ID, Value: strconv.Itoa(kids[2].ID), Label: labelIn(def, "Other on", "en")},
	}, got)

	_, err = svc.Options(t.Context(), core.OptionsQuery{Level: 2})
	require.Equal(t, 400, apperr.GetHTTPStatus(err))

	_, err = svc.Options(t.Context(), core.OptionsQuery{Level: 2, Parent: ids[0], Value: "no_such_column"})
	require.Equal(t, 400, apperr.GetHTTPStatus(err))

	// A described column is the value; items whose column is empty are left out.
	for _, f := range def.Fields {
		if f.Type != core.TypeText || f.Under != nil {
			continue
		}

		got, err = svc.Options(t.Context(), core.OptionsQuery{Level: 2, Parent: ids[0], Value: f.Column})
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Equal(t, sampleValue(f), got[0].Value)

		break
	}
}

func labelIn(def core.Level, name, lang string) string {
	if def.Translations.JSON != "" || len(def.Translations.Columns) > 0 {
		if lang == "en" {
			return name + " en"
		}
	}

	return name
}

func orEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}

	return m
}
