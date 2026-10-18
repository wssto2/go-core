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
	"reflect"
	"strings"
	"testing"

	"github.com/wssto2/go-core/identity/preferences"
)

// Factory returns a new empty store for one subtest.
type Factory func(t *testing.T) preferences.Store

// padding makes an object wider than a TEXT column (65535 bytes) would hold.
const padding = 70_000

const (
	first  = 1
	second = 2
)

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

// set stores raw for the account, failing the test on an error.
func set(t *testing.T, store preferences.Store, accountID int, raw string) {
	t.Helper()

	err := store.Set(ctx(), accountID, json.RawMessage(raw))
	if err != nil {
		t.Fatalf("Set(%d): %v", accountID, err)
	}
}

// expect reads the account's object and compares it to want as JSON: a store may reformat whitespace.
func expect(t *testing.T, store preferences.Store, accountID int, want string) {
	t.Helper()

	got, err := store.Get(ctx(), accountID)
	if err != nil {
		t.Fatalf("Get(%d): %v", accountID, err)
	}

	var wantValue, gotValue any

	err = json.Unmarshal([]byte(want), &wantValue)
	if err != nil {
		t.Fatalf("want is not JSON: %v", err)
	}

	err = json.Unmarshal(got, &gotValue)
	if err != nil {
		t.Fatalf("Get(%d) is not JSON: %q: %v", accountID, got, err)
	}

	if !reflect.DeepEqual(wantValue, gotValue) {
		t.Errorf("Get(%d): got %.200s, want %.200s", accountID, got, want)
	}
}

func nothingStored(t *testing.T, store preferences.Store) {
	t.Helper()

	got, err := store.Get(ctx(), first)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if strings.TrimSpace(string(got)) != "" {
		t.Errorf("Get of an account with nothing stored: got %q, want empty", got)
	}
}

func setThenGet(t *testing.T, store preferences.Store) {
	t.Helper()

	set(t, store, first, `{"dark_mode":true}`)
	expect(t, store, first, `{"dark_mode":true}`)
}

func setReplaces(t *testing.T, store preferences.Store) {
	t.Helper()

	set(t, store, first, `{"a":1,"b":2}`)
	set(t, store, first, `{"c":3}`)
	expect(t, store, first, `{"c":3}`)
}

func apart(t *testing.T, store preferences.Store) {
	t.Helper()

	set(t, store, first, `{"a":1}`)
	set(t, store, second, `{"b":2}`)
	expect(t, store, first, `{"a":1}`)
	expect(t, store, second, `{"b":2}`)
}

func large(t *testing.T, store preferences.Store) {
	t.Helper()

	object := `{"name":"Žarko Đurić 🌍","pad":"` + strings.Repeat("x", padding) + `"}`

	set(t, store, first, object)
	expect(t, store, first, object)
}
