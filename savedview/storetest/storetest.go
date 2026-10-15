// Package storetest is the conformance suite every saved-view store must pass.
//
//	func TestMyStore(t *testing.T) {
//		storetest.Run(t, func(t *testing.T) core.Store { return myStore(t) })
//	}
//
// Each subtest gets a fresh, empty store from the factory.
package storetest

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wssto2/go-core/savedview/core"
)

// Factory returns a new empty store for one subtest.
type Factory func(t *testing.T) core.Store

// The people and lists of the suite.
const (
	ana     = 1
	ivo     = 2
	catalog = "vehicles.catalogue"
	orders  = "orders.list"
	state   = `{"filters":{"brand":"10"},"search":"clio"}`

	// fits a text column only as characters, not bytes: the state may hold any letters.
	longLetters = 3900
)

func at() time.Time { return time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC) }

// Run runs the whole suite.
func Run(t *testing.T, factory Factory) {
	t.Helper()

	for name, test := range map[string]func(*testing.T, core.Store){
		"a view round-trips":                           roundTrips,
		"the list is by name and one list only":        listByName,
		"saving again changes the state and the time":  saveChanges,
		"saving the same thing twice is not a failure": saveUnchanged,
		"a name is unique per person and list":         nameUnique,
		"rows are the person's alone":                  personsAlone,
		"counting is per person and list":              countScoped,
		"a delete removes the view once":               deleteOnce,
		"a long state and any letters survive":         longState,
	} {
		t.Run(name, func(t *testing.T) {
			t.Helper()

			test(t, factory(t))
		})
	}
}

func check(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

func equal[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()

	if got != want {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

func isNotFound(t *testing.T, what string, err error) {
	t.Helper()

	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("%s: got %v, want core.ErrNotFound", what, err)
	}
}

// keep saves a new view and returns it with its id.
func keep(t *testing.T, store core.Store, person int, list, name string) core.View {
	t.Helper()

	view := core.View{ID: 0, UserID: person, ListKey: list, Name: name, State: state, CreatedAt: at(), UpdatedAt: at()}
	check(t, store.Save(t.Context(), &view))

	if view.ID == 0 {
		t.Fatal("Save did not set the id of a new view")
	}

	return view
}

func roundTrips(t *testing.T, store core.Store) {
	t.Helper()

	view := keep(t, store, ana, catalog, "New Clio")

	got, err := store.FindByName(t.Context(), ana, catalog, "New Clio")
	check(t, err)
	equal(t, "id", got.ID, view.ID)
	equal(t, "person", got.UserID, ana)
	equal(t, "list", got.ListKey, catalog)
	equal(t, "name", got.Name, "New Clio")
	equal(t, "state", got.State, state)
	equal(t, "created", got.CreatedAt, at())
	equal(t, "updated", got.UpdatedAt, at())

	_, err = store.FindByName(t.Context(), ana, catalog, "Other")
	isNotFound(t, "an unknown name", err)
}

func listByName(t *testing.T, store core.Store) {
	t.Helper()

	keep(t, store, ana, catalog, "b")
	keep(t, store, ana, catalog, "a")
	keep(t, store, ana, orders, "c")

	views, err := store.List(t.Context(), ana, catalog)
	check(t, err)
	equal(t, "count", len(views), len("ab"))
	equal(t, "first", views[0].Name, "a")
	equal(t, "second", views[1].Name, "b")

	none, err := store.List(t.Context(), ana, "nothing.here")
	check(t, err)
	equal(t, "an empty list", len(none), 0)
}

func saveChanges(t *testing.T, store core.Store) {
	t.Helper()

	view := keep(t, store, ana, catalog, "x")

	later := at().Add(time.Hour)
	view.State, view.UpdatedAt = `{"search":"megane"}`, later
	check(t, store.Save(t.Context(), &view))

	got, err := store.FindByName(t.Context(), ana, catalog, "x")
	check(t, err)
	equal(t, "id", got.ID, view.ID)
	equal(t, "state", got.State, `{"search":"megane"}`)
	equal(t, "updated", got.UpdatedAt, later)
	equal(t, "created", got.CreatedAt, at())
}

// Some databases report 0 rows changed for an update that wrote what was already there; that is not a missing row.
func saveUnchanged(t *testing.T, store core.Store) {
	t.Helper()

	view := keep(t, store, ana, catalog, "x")

	check(t, store.Save(t.Context(), &view))
	check(t, store.Save(t.Context(), &view))
}

func nameUnique(t *testing.T, store core.Store) {
	t.Helper()

	keep(t, store, ana, catalog, "x")

	view := core.View{ID: 0, UserID: ana, ListKey: catalog, Name: "x", State: state, CreatedAt: at(), UpdatedAt: at()}
	if store.Save(t.Context(), &view) == nil {
		t.Fatal("a second view with the same person, list and name was saved")
	}

	keep(t, store, ana, orders, "x")  // another list
	keep(t, store, ivo, catalog, "x") // another person
}

func personsAlone(t *testing.T, store core.Store) {
	t.Helper()

	mine := keep(t, store, ana, catalog, "mine")
	theirs := keep(t, store, ivo, catalog, "mine")

	views, err := store.List(t.Context(), ivo, catalog)
	check(t, err)
	equal(t, "ivo sees one", len(views), 1)
	equal(t, "ivo sees his own", views[0].ID, theirs.ID)

	found, err := store.FindByName(t.Context(), ivo, catalog, "mine")
	check(t, err)
	equal(t, "found his own by name", found.ID, theirs.ID)

	// Ivo cannot change or delete Ana's row by its id.
	hijack := mine
	hijack.UserID, hijack.State = ivo, `{"search":"hijacked"}`
	isNotFound(t, "changing another person's view", store.Save(t.Context(), &hijack))
	isNotFound(t, "deleting another person's view", store.Delete(t.Context(), ivo, mine.ID))

	got, err := store.FindByName(t.Context(), ana, catalog, "mine")
	check(t, err)
	equal(t, "ana's state is untouched", got.State, state)
}

func countScoped(t *testing.T, store core.Store) {
	t.Helper()

	keep(t, store, ana, catalog, "a")
	keep(t, store, ana, catalog, "b")
	keep(t, store, ana, orders, "c")
	keep(t, store, ivo, catalog, "d")

	for _, probe := range []struct {
		person int
		list   string
		want   int
	}{{ana, catalog, 2}, {ana, orders, 1}, {ivo, catalog, 1}, {ivo, orders, 0}} {
		got, err := store.Count(t.Context(), probe.person, probe.list)
		check(t, err)
		equal(t, "count", got, probe.want)
	}
}

func deleteOnce(t *testing.T, store core.Store) {
	t.Helper()

	view := keep(t, store, ana, catalog, "x")

	check(t, store.Delete(t.Context(), ana, view.ID))
	isNotFound(t, "the second delete", store.Delete(t.Context(), ana, view.ID))
	_, err := store.FindByName(t.Context(), ana, catalog, "x")
	isNotFound(t, "the view is gone", err)
}

func longState(t *testing.T, store core.Store) {
	t.Helper()

	long := `{"search":"` + strings.Repeat("č", longLetters) + `"}`
	view := core.View{ID: 0, UserID: ana, ListKey: catalog, Name: "Šđčćž Ž", State: long, CreatedAt: at(), UpdatedAt: at()}
	check(t, store.Save(t.Context(), &view))

	got, err := store.FindByName(t.Context(), ana, catalog, "Šđčćž Ž")
	check(t, err)
	equal(t, "state", got.State, long)
}
