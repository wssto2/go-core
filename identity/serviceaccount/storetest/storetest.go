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
	"slices"
	"sync"
	"testing"
	"time"

	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/identity/serviceaccount"
)

// Factory returns a new empty store for one subtest.
type Factory func(t *testing.T) serviceaccount.Store

// base is a time to the whole second, which every database keeps.
var base = time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)

//go:fix inline
func ptr[T any](v T) *T { return new(v) }

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

func account(name string, tenant, owner int) *serviceaccount.Account {
	return &serviceaccount.Account{Name: name, Description: name + " feed", TenantID: tenant, OwnerID: owner, CreatedBy: 1, CreatedAt: base}
}

func mustCreate(t *testing.T, s serviceaccount.Store, a *serviceaccount.Account) {
	t.Helper()

	require.NoError(t, s.Create(context.Background(), a))
	require.Positive(t, a.ID)
}

func mustKey(t *testing.T, s serviceaccount.Store, accountID int, name string, mod func(*serviceaccount.Key)) serviceaccount.Key {
	t.Helper()

	key := serviceaccount.Key{
		ServiceAccountID: accountID, Name: name, Prefix: serviceaccount.PrefixOf("sak_" + name), Hash: serviceaccount.HashSecret("sak_" + name),
		CreatedBy: 1, CreatedAt: base,
	}
	if mod != nil {
		mod(&key)
	}

	require.NoError(t, s.CreateKey(context.Background(), &key))
	require.Positive(t, key.ID)

	if key.LastUsedAt != nil { // a key is made unused; the store learns of a use through TouchKey
		require.NoError(t, s.TouchKey(context.Background(), key.ID, *key.LastUsedAt))
	}

	return key
}

func accountRoundTrips(t *testing.T, s serviceaccount.Store) {
	ctx := context.Background()
	org, tenant := account("org", 0, 7), account("tenant", 5, 8)
	mustCreate(t, s, org)
	mustCreate(t, s, tenant)

	got, err := s.Find(ctx, org.ID, base)
	require.NoError(t, err)
	assert.Equal(t, *org, got.Account)
	assert.Zero(t, got.TenantID, "the organization's account has no tenant")
	assert.True(t, got.Active())
	assert.Zero(t, got.ActiveKeys)
	assert.Nil(t, got.LastUsedAt)

	got, err = s.Find(ctx, tenant.ID, base)
	require.NoError(t, err)
	assert.Equal(t, 5, got.TenantID)

	_, err = s.Find(ctx, 99, base)
	assert.ErrorIs(t, err, serviceaccount.ErrNotFound)
}

func listFollowsTheVisibility(t *testing.T, s serviceaccount.Store) {
	ctx := context.Background()

	for _, a := range []*serviceaccount.Account{account("zeta", 5, 1), account("alpha", 6, 1), account("mid", 0, 1), account("beta", 5, 1)} {
		mustCreate(t, s, a)
	}

	names := func(v serviceaccount.Visibility) []string {
		list, err := s.List(ctx, v, base)
		require.NoError(t, err)

		out := make([]string, 0, len(list))
		for _, a := range list {
			out = append(out, a.Name)
		}

		return out
	}

	assert.Equal(t, []string{"alpha", "beta", "mid", "zeta"}, names(serviceaccount.Visibility{All: true}), "by name")
	assert.Equal(t, []string{"beta", "zeta"}, names(serviceaccount.Visibility{Tenants: []int{5}}))
	assert.Equal(t, []string{"alpha", "beta", "zeta"}, names(serviceaccount.Visibility{Tenants: []int{5, 6}}), "the organization's account is for the root")
	assert.Empty(t, names(serviceaccount.Visibility{}), "no tenant, not all: nothing")
}

func summaryCountsKeys(t *testing.T, s serviceaccount.Store) {
	ctx := context.Background()
	a := account("robot", 0, 1)
	mustCreate(t, s, a)

	at := base.Add(48 * time.Hour)

	mustKey(t, s, a.ID, "live", func(k *serviceaccount.Key) { k.LastUsedAt = new(base.Add(time.Hour)) })
	mustKey(t, s, a.ID, "later", func(k *serviceaccount.Key) { k.LastUsedAt = new(base.Add(5 * time.Hour)) })
	mustKey(t, s, a.ID, "gone", func(k *serviceaccount.Key) { k.ExpiresAt = new(base.Add(time.Hour)) })
	revoked := mustKey(t, s, a.ID, "revoked", nil)
	require.NoError(t, s.RevokeKey(ctx, revoked.ID, base, 1))

	got, err := s.Find(ctx, a.ID, at)
	require.NoError(t, err)
	assert.Equal(t, 2, got.ActiveKeys, "the expired and the revoked key do not count")
	require.NotNil(t, got.LastUsedAt)
	assert.True(t, got.LastUsedAt.Equal(base.Add(5*time.Hour)), "the latest use of any key: %v", got.LastUsedAt)

	list, err := s.List(ctx, serviceaccount.Visibility{All: true}, at)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, 2, list[0].ActiveKeys)
}

func updateChanges(t *testing.T, s serviceaccount.Store) {
	ctx := context.Background()
	a := account("robot", 5, 1)
	mustCreate(t, s, a)

	later := base.Add(time.Hour)
	changed := *a
	changed.Name, changed.Description, changed.TenantID, changed.OwnerID = "renamed", "new text", 0, 9
	changed.UpdatedBy, changed.UpdatedAt = 3, &later
	changed.DeactivatedAt, changed.DeactivatedBy = &later, 3
	require.NoError(t, s.Update(ctx, changed))

	got, err := s.Find(ctx, a.ID, base)
	require.NoError(t, err)
	assert.Equal(t, changed, got.Account, "the tenant went back to none, the owner moved, the account is deactivated")
	assert.False(t, got.Active())
	assert.Equal(t, base, got.CreatedAt, "what an update does not name stays")
}

func keysNewestFirst(t *testing.T, s serviceaccount.Store) {
	ctx := context.Background()
	a, other := account("robot", 0, 1), account("other", 0, 1)
	mustCreate(t, s, a)
	mustCreate(t, s, other)

	first := mustKey(t, s, a.ID, "first", nil)
	second := mustKey(t, s, a.ID, "second", func(k *serviceaccount.Key) { k.ExpiresAt = new(base.Add(24 * time.Hour)) })
	foreign := mustKey(t, s, other.ID, "foreign", nil)

	keys, err := s.Keys(ctx, a.ID)
	require.NoError(t, err)
	require.Len(t, keys, 2)
	assert.Equal(t, []int{second.ID, first.ID}, []int{keys[0].ID, keys[1].ID})
	assert.Equal(t, "sak_second", keys[0].Prefix)
	assert.True(t, keys[0].ExpiresAt.Equal(base.Add(24*time.Hour)))
	assert.Nil(t, keys[1].ExpiresAt)

	got, err := s.FindKey(ctx, a.ID, first.ID)
	require.NoError(t, err)
	assert.Equal(t, "first", got.Name)

	_, err = s.FindKey(ctx, a.ID, foreign.ID)
	assert.ErrorIs(t, err, serviceaccount.ErrKeyNotFound, "a key of another account is not found through this one")

	_, err = s.FindKey(ctx, a.ID, 999)
	assert.ErrorIs(t, err, serviceaccount.ErrKeyNotFound)
}

func keyByHash(t *testing.T, s serviceaccount.Store) {
	ctx := context.Background()
	a := account("robot", 5, 1)
	mustCreate(t, s, a)

	key := mustKey(t, s, a.ID, "ci", nil)

	got, owner, err := s.FindByKeyHash(ctx, serviceaccount.HashSecret("sak_ci"))
	require.NoError(t, err)
	assert.Equal(t, key.ID, got.ID)
	assert.Equal(t, a.ID, owner.ID)
	assert.Equal(t, 5, owner.TenantID)
	assert.Nil(t, got.LastUsedAt)

	require.NoError(t, s.TouchKey(ctx, key.ID, base.Add(time.Hour)))

	got, _, err = s.FindByKeyHash(ctx, serviceaccount.HashSecret("sak_ci"))
	require.NoError(t, err)
	require.NotNil(t, got.LastUsedAt)
	assert.True(t, got.LastUsedAt.Equal(base.Add(time.Hour)))

	_, _, err = s.FindByKeyHash(ctx, serviceaccount.HashSecret("sak_unknown"))
	assert.ErrorIs(t, err, serviceaccount.ErrKeyNotFound)

	// the same secret twice is refused: a hash names one key
	dup := serviceaccount.Key{ServiceAccountID: a.ID, Name: "dup", Prefix: "sak_ci", Hash: serviceaccount.HashSecret("sak_ci"), CreatedBy: 1, CreatedAt: base}
	assert.Error(t, s.CreateKey(ctx, &dup))
}

func revokeKeepsTheFirst(t *testing.T, s serviceaccount.Store) {
	ctx := context.Background()
	a := account("robot", 0, 1)
	mustCreate(t, s, a)

	key := mustKey(t, s, a.ID, "ci", nil)

	require.NoError(t, s.RevokeKey(ctx, key.ID, base.Add(time.Hour), 4))
	require.NoError(t, s.RevokeKey(ctx, key.ID, base.Add(2*time.Hour), 5))

	got, err := s.FindKey(ctx, a.ID, key.ID)
	require.NoError(t, err)
	require.NotNil(t, got.RevokedAt)
	assert.True(t, got.RevokedAt.Equal(base.Add(time.Hour)), "the first revocation stays")
	assert.Equal(t, 4, got.RevokedBy)
	assert.False(t, got.Usable(base))
}

func workingKeys(t *testing.T, s serviceaccount.Store) {
	ctx := context.Background()
	live, off := account("live", 0, 1), account("off", 0, 1)
	mustCreate(t, s, live)
	mustCreate(t, s, off)

	kept := mustKey(t, s, live.ID, "kept", nil)
	timed := mustKey(t, s, live.ID, "timed", func(k *serviceaccount.Key) { k.ExpiresAt = new(base.Add(72 * time.Hour)) })
	gone := mustKey(t, s, live.ID, "gone", func(k *serviceaccount.Key) { k.ExpiresAt = new(base.Add(time.Hour)) })
	revoked := mustKey(t, s, live.ID, "revoked", nil)
	mustKey(t, s, off.ID, "of-a-deactivated-account", nil)

	require.NoError(t, s.RevokeKey(ctx, revoked.ID, base, 1))

	stopped := *off
	stopped.DeactivatedAt = new(base)
	require.NoError(t, s.Update(ctx, stopped))

	working, err := s.WorkingKeys(ctx, base.Add(2*time.Hour))
	require.NoError(t, err)

	var ids []int
	for _, w := range working {
		ids = append(ids, w.Key.ID)
		assert.Equal(t, live.ID, w.Account.ID)
		assert.Equal(t, live.OwnerID, w.Account.OwnerID)
	}

	assert.Equal(t, []int{kept.ID, timed.ID}, ids, "not the expired, the revoked, nor the key of a deactivated account")
	assert.NotContains(t, ids, gone.ID)
}

func noticeClaimedOnce(t *testing.T, s serviceaccount.Store) {
	ctx := context.Background()

	sent, err := s.NoticesSent(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, sent)

	claimed, err := s.ClaimNotice(ctx, 7, serviceaccount.NoticeExpiring, base)
	require.NoError(t, err)
	assert.True(t, claimed)

	claimed, err = s.ClaimNotice(ctx, 7, serviceaccount.NoticeExpiring, base.Add(time.Hour))
	require.NoError(t, err)
	assert.False(t, claimed, "a second claim of one condition of one key fails")

	claimed, err = s.ClaimNotice(ctx, 7, serviceaccount.NoticeUnused, base)
	require.NoError(t, err)
	assert.True(t, claimed, "the other condition of the same key is its own")

	claimed, err = s.ClaimNotice(ctx, 8, serviceaccount.NoticeExpiring, base)
	require.NoError(t, err)
	assert.True(t, claimed, "so is the same condition of another key")

	sent, err = s.NoticesSent(ctx, []int{7, 8, 9})
	require.NoError(t, err)
	slices.Sort(sent[7])
	assert.Equal(t, map[int][]serviceaccount.NoticeKind{
		7: {serviceaccount.NoticeExpiring, serviceaccount.NoticeUnused}, 8: {serviceaccount.NoticeExpiring},
	}, sent)
}

func noticeOneWinner(t *testing.T, s serviceaccount.Store) {
	const runs = 6

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)

	for range runs {
		wg.Go(func() {
			claimed, err := s.ClaimNotice(context.Background(), 11, serviceaccount.NoticeUnused, base)
			assert.NoError(t, err)

			if claimed {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		})
	}

	wg.Wait()
	assert.Equal(t, 1, wins, "two runs never both tell")
}
