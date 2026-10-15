// Package storetest is the conformance suite every service account store must pass.
//
//	func TestMyStore(t *testing.T) {
//		storetest.Run(t, func(t *testing.T) serviceaccount.Store { return myStore(t) })
//	}
//
// Each subtest gets a fresh, empty store from the factory. The store must be safe for concurrent use: the
// notice claim test calls it from several goroutines.
package storetest

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/wssto2/go-core/identity/serviceaccount"
)

// Factory returns a new empty store for one subtest.
type Factory func(t *testing.T) serviceaccount.Store

// The times of the suite are to the whole second, which every database keeps.
const (
	hour = time.Hour
	day  = 24 * time.Hour
)

// The ids and names of the suite.
const (
	tenantA, tenantB   = 5, 6
	ownerA, ownerB     = 7, 8
	twoDays            = 2 * day
	threeDays          = 3 * day
	soon               = 2 * hour
	lastUse            = 5 * hour
	ownerC             = 9
	unknownAccount     = 99
	unknownKey         = 999
	editor, revoker    = 3, 4
	otherRevoker       = 5
	noticeKeyA         = 7
	noticeKeyB         = 8
	noticeKeyC         = 9
	noticeKeyContested = 11
	twoKeys            = 2
	runs               = 6
	nameBeta           = "beta"
	nameZeta           = "zeta"
)

func base() time.Time { return time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC) }

// Run runs the whole suite.
func Run(t *testing.T, factory Factory) {
	t.Helper()

	for name, test := range map[string]func(*testing.T, serviceaccount.Store){
		"an account round-trips":                    accountRoundTrips,
		"the list is by name and honours the view":  listFollowsTheVisibility,
		"an account summarises its keys":            summaryCountsKeys,
		"an update changes the account's fields":    updateChanges,
		"keys are listed newest first":              keysNewestFirst,
		"a key is found by its hash":                keyByHash,
		"revoking twice keeps the first revocation": revokeKeepsTheFirst,
		"working keys leave out the dead ones":      workingKeys,
		"a notice is claimed once":                  noticeClaimedOnce,
		"concurrent claims have one winner":         noticeOneWinner,
	} {
		t.Run(name, func(t *testing.T) {
			t.Helper()

			test(t, factory(t))
		})
	}
}

// check reports what a test of the suite found.
type checker struct{ t *testing.T }

func (chk checker) noErr(err error, what string) {
	chk.t.Helper()

	if err != nil {
		chk.t.Fatalf("%s: unexpected error: %v", what, err)
	}
}

func (chk checker) isErr(err, target error, what string) {
	chk.t.Helper()

	if !errors.Is(err, target) {
		chk.t.Errorf("%s: got error %v, want %v", what, err, target)
	}
}

func (chk checker) equal(want, got any, what string) {
	chk.t.Helper()

	if !reflect.DeepEqual(want, got) {
		chk.t.Errorf("%s: got %#v, want %#v", what, got, want)
	}
}

func (chk checker) true(ok bool, what string) {
	chk.t.Helper()

	if !ok {
		chk.t.Error(what)
	}
}

// same compares times by the instant, not the zone.
func (chk checker) same(want time.Time, got *time.Time, what string) {
	chk.t.Helper()

	if got == nil || !want.Equal(*got) {
		chk.t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

func account(name string, tenant, owner int) *serviceaccount.Account {
	return &serviceaccount.Account{
		ID: 0, Name: name, Description: name + " feed", TenantID: tenant, OwnerID: owner, CreatedBy: 1, CreatedAt: base(),
		UpdatedBy: 0, UpdatedAt: nil, DeactivatedAt: nil, DeactivatedBy: 0,
	}
}

func mustCreate(t *testing.T, store serviceaccount.Store, made *serviceaccount.Account) {
	t.Helper()

	checker{t}.noErr(store.Create(context.Background(), made), "create "+made.Name)

	if made.ID <= 0 {
		t.Fatalf("create %s: no ID was set", made.Name)
	}
}

// newKey is a key of the account, named name, which its secret is "sak_"+name.
func newKey(accountID int, name string) serviceaccount.Key {
	secret := "sak_" + name

	return serviceaccount.Key{
		ID: 0, ServiceAccountID: accountID, Name: name, Prefix: serviceaccount.PrefixOf(secret),
		Hash: serviceaccount.HashSecret(secret), CreatedBy: 1, CreatedAt: base(),
		LastUsedAt: nil, ExpiresAt: nil, RevokedAt: nil, RevokedBy: 0,
	}
}

// mustKey stores the key. A key is made unused; the store learns of a use through TouchKey, so a LastUsedAt of the
// key given is touched in.
func mustKey(t *testing.T, store serviceaccount.Store, key serviceaccount.Key) serviceaccount.Key {
	t.Helper()

	ctx := context.Background()
	checker{t}.noErr(store.CreateKey(ctx, &key), "create key "+key.Name)

	if key.ID <= 0 {
		t.Fatalf("create key %s: no ID was set", key.Name)
	}

	if key.LastUsedAt != nil {
		checker{t}.noErr(store.TouchKey(ctx, key.ID, *key.LastUsedAt), "touch key "+key.Name)
	}

	return key
}

func accountRoundTrips(t *testing.T, store serviceaccount.Store) {
	t.Helper()

	chk, ctx := checker{t}, context.Background()
	organization, tenant := account("org", 0, ownerA), account("tenant", tenantA, ownerB)
	mustCreate(t, store, organization)
	mustCreate(t, store, tenant)

	got, err := store.Find(ctx, organization.ID, base())
	chk.noErr(err, "find")
	chk.equal(*organization, got.Account, "the account as it was made")
	chk.equal(0, got.TenantID, "the organization's account has no tenant")
	chk.true(got.Active(), "a new account is active")
	chk.equal(0, got.ActiveKeys, "no keys")
	chk.true(got.LastUsedAt == nil, "never used")

	got, err = store.Find(ctx, tenant.ID, base())
	chk.noErr(err, "find the tenant's")
	chk.equal(tenantA, got.TenantID, "the tenant")

	_, err = store.Find(ctx, unknownAccount, base())
	chk.isErr(err, serviceaccount.ErrNotFound, "find an account that is not there")
}

func listFollowsTheVisibility(t *testing.T, store serviceaccount.Store) {
	t.Helper()

	chk, ctx := checker{t}, context.Background()

	for _, made := range []*serviceaccount.Account{
		account(nameZeta, tenantA, 1), account("alpha", tenantB, 1), account("mid", 0, 1), account(nameBeta, tenantA, 1),
	} {
		mustCreate(t, store, made)
	}

	names := func(visible serviceaccount.Visibility) []string {
		list, err := store.List(ctx, visible, base())
		chk.noErr(err, "list")

		out := make([]string, 0, len(list))
		for _, listed := range list {
			out = append(out, listed.Name)
		}

		return out
	}

	all := serviceaccount.Visibility{All: true, Tenants: nil}
	chk.equal([]string{"alpha", nameBeta, "mid", nameZeta}, names(all), "all, by name")
	one := serviceaccount.Visibility{All: false, Tenants: []int{tenantA}}
	two := serviceaccount.Visibility{All: false, Tenants: []int{tenantA, tenantB}}

	chk.equal([]string{nameBeta, nameZeta}, names(one), "one tenant")
	chk.equal([]string{"alpha", nameBeta, nameZeta}, names(two), "two tenants: the organization's account is for the root")
	chk.equal([]string{}, names(serviceaccount.Visibility{All: false, Tenants: nil}), "no tenant, not all: nothing")
}

func summaryCountsKeys(t *testing.T, store serviceaccount.Store) {
	t.Helper()

	chk, ctx := checker{t}, context.Background()
	robot := account("robot", 0, 1)
	mustCreate(t, store, robot)

	later := base().Add(twoDays)

	live := newKey(robot.ID, "live")
	live.LastUsedAt = new(base().Add(hour))
	mustKey(t, store, live)

	recent := newKey(robot.ID, "later")
	recent.LastUsedAt = new(base().Add(lastUse))
	mustKey(t, store, recent)

	gone := newKey(robot.ID, "gone")
	gone.ExpiresAt = new(base().Add(hour))
	mustKey(t, store, gone)

	revoked := mustKey(t, store, newKey(robot.ID, "revoked"))
	chk.noErr(store.RevokeKey(ctx, revoked.ID, base(), 1), "revoke")

	got, err := store.Find(ctx, robot.ID, later)
	chk.noErr(err, "find")
	chk.equal(twoKeys, got.ActiveKeys, "the expired and the revoked key do not count")
	chk.same(base().Add(lastUse), got.LastUsedAt, "the latest use of any key")

	list, err := store.List(ctx, serviceaccount.Visibility{All: true, Tenants: nil}, later)
	chk.noErr(err, "list")
	chk.equal(1, len(list), "one account")

	if len(list) == 1 {
		chk.equal(twoKeys, list[0].ActiveKeys, "the list counts them too")
	}
}

func updateChanges(t *testing.T, store serviceaccount.Store) {
	t.Helper()

	chk, ctx := checker{t}, context.Background()
	robot := account("robot", tenantA, 1)
	mustCreate(t, store, robot)

	later := base().Add(hour)
	changed := *robot
	changed.Name, changed.Description, changed.TenantID, changed.OwnerID = "renamed", "new text", 0, ownerC
	changed.UpdatedBy, changed.UpdatedAt = editor, new(later)
	changed.DeactivatedAt, changed.DeactivatedBy = new(later), editor
	chk.noErr(store.Update(ctx, changed), "update")

	got, err := store.Find(ctx, robot.ID, base())
	chk.noErr(err, "find")
	chk.equal(changed, got.Account, "the tenant went back to none, the owner moved, the account is deactivated")
	chk.true(!got.Active(), "deactivated")
	chk.equal(base(), got.CreatedAt, "what an update does not name stays")
}

func keysNewestFirst(t *testing.T, store serviceaccount.Store) {
	t.Helper()

	chk, ctx := checker{t}, context.Background()
	robot, other := account("robot", 0, 1), account("other", 0, 1)
	mustCreate(t, store, robot)
	mustCreate(t, store, other)

	first := mustKey(t, store, newKey(robot.ID, "first"))

	timed := newKey(robot.ID, "second")
	timed.ExpiresAt = new(base().Add(day))
	second := mustKey(t, store, timed)

	foreign := mustKey(t, store, newKey(other.ID, "foreign"))

	keys, err := store.Keys(ctx, robot.ID)
	chk.noErr(err, "keys")

	chk.equal(twoKeys, len(keys), "the account's two keys, not the other's")

	if len(keys) != twoKeys {
		return
	}

	chk.equal([]int{second.ID, first.ID}, []int{keys[0].ID, keys[1].ID}, "newest first")
	chk.equal("sak_second", keys[0].Prefix, "the prefix")
	chk.same(base().Add(day), keys[0].ExpiresAt, "the expiry")
	chk.true(keys[1].ExpiresAt == nil, "no expiry")

	got, err := store.FindKey(ctx, robot.ID, first.ID)
	chk.noErr(err, "find key")
	chk.equal("first", got.Name, "the key")

	_, err = store.FindKey(ctx, robot.ID, foreign.ID)
	chk.isErr(err, serviceaccount.ErrKeyNotFound, "a key of another account, found through this one")

	_, err = store.FindKey(ctx, robot.ID, unknownKey)
	chk.isErr(err, serviceaccount.ErrKeyNotFound, "a key that is not there")
}

func keyByHash(t *testing.T, store serviceaccount.Store) {
	t.Helper()

	chk, ctx := checker{t}, context.Background()
	robot := account("robot", tenantA, 1)
	mustCreate(t, store, robot)

	key := mustKey(t, store, newKey(robot.ID, "ci"))
	hash := serviceaccount.HashSecret("sak_ci")

	found, owner, err := store.FindByKeyHash(ctx, hash)
	chk.noErr(err, "find by hash")
	chk.equal(key.ID, found.ID, "the key")
	chk.equal(robot.ID, owner.ID, "its account")
	chk.equal(tenantA, owner.TenantID, "and the account's tenant")
	chk.true(found.LastUsedAt == nil, "unused")

	chk.noErr(store.TouchKey(ctx, key.ID, base().Add(hour)), "touch")

	found, _, err = store.FindByKeyHash(ctx, hash)
	chk.noErr(err, "find by hash again")
	chk.same(base().Add(hour), found.LastUsedAt, "the use")

	_, _, err = store.FindByKeyHash(ctx, serviceaccount.HashSecret("sak_unknown"))
	chk.isErr(err, serviceaccount.ErrKeyNotFound, "an unknown secret")

	// the same secret twice is refused: a hash names one key
	duplicate := newKey(robot.ID, "ci")
	duplicate.Name = "duplicate"
	chk.true(store.CreateKey(ctx, &duplicate) != nil, "a second key with the same hash is refused")
}

func revokeKeepsTheFirst(t *testing.T, store serviceaccount.Store) {
	t.Helper()

	chk, ctx := checker{t}, context.Background()
	robot := account("robot", 0, 1)
	mustCreate(t, store, robot)

	key := mustKey(t, store, newKey(robot.ID, "ci"))

	chk.noErr(store.RevokeKey(ctx, key.ID, base().Add(hour), revoker), "revoke")
	chk.noErr(store.RevokeKey(ctx, key.ID, base().Add(soon), otherRevoker), "revoke again")

	got, err := store.FindKey(ctx, robot.ID, key.ID)
	chk.noErr(err, "find key")
	chk.same(base().Add(hour), got.RevokedAt, "the first revocation stays")
	chk.equal(revoker, got.RevokedBy, "and who made it")
	chk.true(!got.Usable(base()), "a revoked key does not work")
}

func workingKeys(t *testing.T, store serviceaccount.Store) {
	t.Helper()

	chk, ctx := checker{t}, context.Background()
	live, off := account("live", 0, 1), account("off", 0, 1)
	mustCreate(t, store, live)
	mustCreate(t, store, off)

	kept := mustKey(t, store, newKey(live.ID, "kept"))

	timedKey := newKey(live.ID, "timed")
	timedKey.ExpiresAt = new(base().Add(threeDays))
	timed := mustKey(t, store, timedKey)

	goneKey := newKey(live.ID, "gone")
	goneKey.ExpiresAt = new(base().Add(hour))
	gone := mustKey(t, store, goneKey)

	revoked := mustKey(t, store, newKey(live.ID, "revoked"))
	mustKey(t, store, newKey(off.ID, "of-a-deactivated-account"))

	chk.noErr(store.RevokeKey(ctx, revoked.ID, base(), 1), "revoke")

	stopped := *off
	stopped.DeactivatedAt = new(base())
	chk.noErr(store.Update(ctx, stopped), "deactivate")

	working, err := store.WorkingKeys(ctx, base().Add(soon))
	chk.noErr(err, "working keys")

	ids := make([]int, 0, len(working))

	for _, found := range working {
		ids = append(ids, found.Key.ID)

		chk.equal(live.ID, found.Account.ID, "the account of the key")
		chk.equal(live.OwnerID, found.Account.OwnerID, "and its owner")
	}

	chk.equal([]int{kept.ID, timed.ID}, ids, "not the expired, the revoked, nor the key of a deactivated account")
	chk.true(!slices.Contains(ids, gone.ID), "the expired key is not working")
}

func noticeClaimedOnce(t *testing.T, store serviceaccount.Store) {
	t.Helper()

	chk, ctx := checker{t}, context.Background()

	sent, err := store.NoticesSent(ctx, nil)
	chk.noErr(err, "sent, of no keys")
	chk.equal(0, len(sent), "nothing")

	claim := func(keyID int, kind serviceaccount.NoticeKind, when time.Time) bool {
		claimed, err := store.ClaimNotice(ctx, keyID, kind, when)
		chk.noErr(err, "claim")

		return claimed
	}

	chk.true(claim(noticeKeyA, serviceaccount.NoticeExpiring, base()), "the first claim wins")
	chk.true(!claim(noticeKeyA, serviceaccount.NoticeExpiring, base().Add(hour)), "a second claim of the same fails")
	chk.true(claim(noticeKeyA, serviceaccount.NoticeUnused, base()), "the other condition of the same key is its own")
	chk.true(claim(noticeKeyB, serviceaccount.NoticeExpiring, base()), "so is the same condition of another key")

	sent, err = store.NoticesSent(ctx, []int{noticeKeyA, noticeKeyB, noticeKeyC})
	chk.noErr(err, "sent")
	slices.Sort(sent[noticeKeyA])
	chk.equal(map[int][]serviceaccount.NoticeKind{
		noticeKeyA: {serviceaccount.NoticeExpiring, serviceaccount.NoticeUnused}, noticeKeyB: {serviceaccount.NoticeExpiring},
	}, sent, "what was claimed")
}

func noticeOneWinner(t *testing.T, store serviceaccount.Store) {
	t.Helper()

	var (
		group sync.WaitGroup
		lock  sync.Mutex
		wins  int
	)

	for range runs {
		group.Go(func() {
			claimed, err := store.ClaimNotice(context.Background(), noticeKeyContested, serviceaccount.NoticeUnused, base())
			if err != nil {
				t.Errorf("claim: %v", err)

				return
			}

			if claimed {
				lock.Lock()
				wins++
				lock.Unlock()
			}
		})
	}

	group.Wait()
	checker{t}.equal(1, wins, "two runs never both tell")
}
