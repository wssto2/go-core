// Package storetest is the conformance suite every lock store must pass.
//
//	func TestMyStore(t *testing.T) {
//		storetest.Run(t, func(t *testing.T) lock.Store { return myStore(t) })
//	}
//
// Each subtest gets a fresh, empty store from the factory.
package storetest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/wssto2/go-core/lock"
)

// Factory returns a new empty store for one subtest.
type Factory func(t *testing.T) lock.Store

const (
	key   = "vega-import"
	other = "nightly"
	ana   = "ana-1"
	ivo   = "ivo-2"
	ttl   = time.Minute
	half  = ttl / 2
	twice = 2 * ttl
)

func t0() time.Time { return time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC) }

// Run runs the whole suite.
func Run(t *testing.T, factory Factory) {
	t.Helper()

	for name, test := range map[string]func(*testing.T, lock.Store){
		"a free key is acquired":                     acquireFree,
		"a live lease is refused to another owner":   refusedWhileLive,
		"the owner acquires again and renews":        ownerRenews,
		"an expired lease is taken by another owner": takenAfterExpiry,
		"refresh works for the owner only":           refreshOwnerOnly,
		"refresh with an unchanged time still holds": refreshUnchanged,
		"release works for the owner only":           releaseOwnerOnly,
		"keys are kept apart":                        keysApart,
		"one of many simultaneous acquires wins":     oneWins,
	} {
		t.Run(name, func(t *testing.T) { test(t, factory(t)) })
	}
}

func ctx() context.Context { return context.Background() }

// acquire asks for the key at `now` for ttl, failing the test on an error.
func acquire(t *testing.T, store lock.Store, k, owner string, now time.Time) bool {
	t.Helper()

	got, err := store.Acquire(ctx(), k, owner, now, now.Add(ttl))
	if err != nil {
		t.Fatalf("Acquire(%s, %s): %v", k, owner, err)
	}

	return got
}

func refresh(t *testing.T, store lock.Store, owner string, until time.Time) bool {
	t.Helper()

	got, err := store.Refresh(ctx(), key, owner, until)
	if err != nil {
		t.Fatalf("Refresh(%s, %s): %v", key, owner, err)
	}

	return got
}

func release(t *testing.T, store lock.Store, k, owner string) {
	t.Helper()

	err := store.Release(ctx(), k, owner)
	if err != nil {
		t.Fatalf("Release(%s, %s): %v", k, owner, err)
	}
}

func want(t *testing.T, what string, got, wanted bool) {
	t.Helper()

	if got != wanted {
		t.Errorf("%s: got %v, want %v", what, got, wanted)
	}
}

func acquireFree(t *testing.T, store lock.Store) {
	t.Helper()

	want(t, "acquire of a free key", acquire(t, store, key, ana, t0()), true)
}

func refusedWhileLive(t *testing.T, store lock.Store) {
	t.Helper()

	acquire(t, store, key, ana, t0())

	want(t, "another owner while live", acquire(t, store, key, ivo, t0().Add(half)), false)
	want(t, "another owner at the last instant", acquire(t, store, key, ivo, t0().Add(ttl)), false)
}

func ownerRenews(t *testing.T, store lock.Store) {
	t.Helper()

	acquire(t, store, key, ana, t0())

	want(t, "the owner again", acquire(t, store, key, ana, t0().Add(half)), true)
	// The lease now runs to 1.5 ttl: past the first lease's end, another owner is still refused.
	want(t, "another owner past the first lease", acquire(t, store, key, ivo, t0().Add(ttl+time.Second)), false)
	want(t, "the same instant, same owner, unchanged lease", acquire(t, store, key, ana, t0().Add(half)), true)
}

func takenAfterExpiry(t *testing.T, store lock.Store) {
	t.Helper()

	acquire(t, store, key, ana, t0())

	late := t0().Add(ttl + time.Second)
	want(t, "another owner after the lease", acquire(t, store, key, ivo, late), true)
	want(t, "the old owner is refused now", acquire(t, store, key, ana, late), false)
	want(t, "the old owner cannot refresh", refresh(t, store, ana, late.Add(ttl)), false)
	want(t, "the new owner holds", refresh(t, store, ivo, late.Add(ttl)), true)
}

func refreshOwnerOnly(t *testing.T, store lock.Store) {
	t.Helper()

	want(t, "refresh of a key nobody holds", refresh(t, store, ana, t0().Add(ttl)), false)

	acquire(t, store, key, ana, t0())

	want(t, "refresh by another owner", refresh(t, store, ivo, t0().Add(twice)), false)
	want(t, "refresh by the owner", refresh(t, store, ana, t0().Add(twice)), true)
	want(t, "another owner still refused after the refresh", acquire(t, store, key, ivo, t0().Add(ttl+time.Second)), false)
	want(t, "another owner after the refreshed lease", acquire(t, store, key, ivo, t0().Add(twice+time.Second)), true)
}

func refreshUnchanged(t *testing.T, store lock.Store) {
	t.Helper()

	acquire(t, store, key, ana, t0())

	want(t, "refresh to the same time", refresh(t, store, ana, t0().Add(ttl)), true)
}

func releaseOwnerOnly(t *testing.T, store lock.Store) {
	t.Helper()

	acquire(t, store, key, ana, t0())

	release(t, store, key, ivo) // not the owner: nothing happens
	want(t, "another owner after a foreign release", acquire(t, store, key, ivo, t0().Add(time.Second)), false)

	release(t, store, key, ana)
	want(t, "another owner after the release", acquire(t, store, key, ivo, t0().Add(time.Second)), true)

	release(t, store, "never-held", ana) // a missing key is not an error
}

func keysApart(t *testing.T, store lock.Store) {
	t.Helper()

	acquire(t, store, key, ana, t0())

	want(t, "another key, another owner", acquire(t, store, other, ivo, t0()), true)
	want(t, "another key, same owner", acquire(t, store, other, ana, t0()), false)
}

func oneWins(t *testing.T, store lock.Store) {
	t.Helper()

	const contenders = 8

	var (
		group sync.WaitGroup
		guard sync.Mutex
		wins  int
	)

	for contender := range contenders {
		group.Go(func() {
			got, err := store.Acquire(ctx(), key, fmt.Sprint("owner-", contender), t0(), t0().Add(ttl))
			if err != nil {
				t.Errorf("Acquire: %v", err)

				return
			}

			if got {
				guard.Lock()
				wins++
				guard.Unlock()
			}
		})
	}

	group.Wait()

	if wins != 1 {
		t.Errorf("%d contenders won the key, want exactly 1", wins)
	}
}
