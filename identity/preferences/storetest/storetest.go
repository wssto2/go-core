// Package storetest is the conformance suite every preferences store must pass.
//
//	func TestMyStore(t *testing.T) {
//		storetest.Run(t, func(t *testing.T) preferences.Store { return myStore(t) })
//	}
//
// Each subtest gets a fresh, empty store from the factory.
package storetest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/identity/preferences"
)

// Factory returns a new empty store for one subtest.
type Factory func(t *testing.T) preferences.Store

// Run runs the whole suite.
func Run(t *testing.T, factory Factory) {
	t.Helper()

	for name, test := range map[string]func(*testing.T, preferences.Store){
		"nothing stored is empty":                nothingStored,
		"set then get":                           setThenGet,
		"set replaces":                           setReplaces,
		"accounts are kept apart":                apart,
		"a large object and its unicode survive": large,
	} {
		t.Run(name, func(t *testing.T) { test(t, factory(t)) })
	}
}

func ctx() context.Context { return context.Background() }

// same compares as JSON: a store may reformat whitespace.
func same(t *testing.T, want string, got json.RawMessage) {
	t.Helper()

	require.JSONEq(t, want, string(got))
}

func nothingStored(t *testing.T, s preferences.Store) {
	t.Helper()

	got, err := s.Get(ctx(), 1)
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(string(got)))
}

func setThenGet(t *testing.T, s preferences.Store) {
	t.Helper()

	require.NoError(t, s.Set(ctx(), 1, json.RawMessage(`{"dark_mode":true}`)))

	got, err := s.Get(ctx(), 1)
	require.NoError(t, err)
	same(t, `{"dark_mode":true}`, got)
}

func setReplaces(t *testing.T, s preferences.Store) {
	t.Helper()

	require.NoError(t, s.Set(ctx(), 1, json.RawMessage(`{"a":1,"b":2}`)))
	require.NoError(t, s.Set(ctx(), 1, json.RawMessage(`{"c":3}`)))

	got, err := s.Get(ctx(), 1)
	require.NoError(t, err)
	same(t, `{"c":3}`, got)
}

func apart(t *testing.T, s preferences.Store) {
	t.Helper()

	require.NoError(t, s.Set(ctx(), 1, json.RawMessage(`{"a":1}`)))
	require.NoError(t, s.Set(ctx(), 2, json.RawMessage(`{"b":2}`)))

	one, err := s.Get(ctx(), 1)
	require.NoError(t, err)
	same(t, `{"a":1}`, one)

	two, err := s.Get(ctx(), 2)
	require.NoError(t, err)
	same(t, `{"b":2}`, two)
}

func large(t *testing.T, s preferences.Store) {
	t.Helper()

	obj := `{"name":"Žarko Đurić 🌍","pad":"` + strings.Repeat("x", 40_000) + `"}`

	require.NoError(t, s.Set(ctx(), 1, json.RawMessage(obj)))

	got, err := s.Get(ctx(), 1)
	require.NoError(t, err)
	same(t, obj, got)
}
