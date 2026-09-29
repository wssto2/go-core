package authz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
)

func TestFeatureResolverErrorsFailClosedAsInternalErrors(t *testing.T) {
	p := authztestParts(t)
	boom := errors.New("features unavailable")
	e, err := authz.NewEngine(authz.Config{Catalogue: smallCatalogue(t), Hierarchy: p.h, Roles: smallRoles(), Store: p.store, Resolver: p.places,
		Features: authz.FeatureFunc(func(context.Context, authz.Scope, string) (bool, error) { return false, boom })})
	require.NoError(t, err)
	p.places.AddDealer(1)
	_, err = p.store.Bind(context.Background(), authz.Subject{}, authz.Binding{Subject: user(1), Role: authz.RoleRef{Key: "seller"}, Scope: authztest.Dealer(1)})
	require.NoError(t, err)
	ctx := authz.WithPrincipal(context.Background(), authz.User(1, 0))

	err = e.Require(ctx, "auction.lot:view")
	assert.ErrorIs(t, err, boom)
	assert.True(t, apperr.HasCode(err, apperr.CodeInternal), "an outage is not a 403")
	assert.Error(t, e.RequireOn(ctx, "auction.lot:view", authz.Resource{Scope: authztest.Dealer(1)}))
	_, err = e.Access(ctx, "auction.lot:view")
	assert.ErrorIs(t, err, boom)
	_, err = e.MyAccess(ctx)
	assert.ErrorIs(t, err, boom)
	assert.NoError(t, e.Require(ctx, "vehicle.stock:view"), "permissions without a feature are unaffected")
}

func TestEngineExposesItsConfiguration(t *testing.T) {
	w := newSmallWorld(t)
	assert.Same(t, w.Catalogue, w.Engine.Catalogue())
	assert.Same(t, w.Hierarchy, w.Engine.Hierarchy())
	role, ok := w.Engine.PredefinedRole("seller")
	require.True(t, ok)
	assert.Equal(t, "Seller", role.Name)
	_, ok = w.Engine.PredefinedRole("nope")
	assert.False(t, ok)
	roles := w.Engine.PredefinedRoles()
	require.Len(t, roles, len(smallRoles()))
	assert.Equal(t, "dealerlead", roles[0].Key, "sorted by key")
}

func TestKnownAncestorsSaveTheResolver(t *testing.T) {
	w := newSmallWorld(t)
	w.Bind(user(1), "seller", authztest.Dealer(1))
	ctx := w.As(authz.User(1, 0))
	// location 77 is unknown to the resolver, but the caller says it is in dealer 1
	res := authz.Resource{Scope: authztest.Location(77), Ancestors: []authz.Scope{authztest.Dealer(1)}}
	assert.NoError(t, w.Engine.RequireOn(ctx, "vehicle.stock:view", res))
	res.Ancestors = []authz.Scope{authztest.Dealer(2)}
	assert.ErrorIs(t, w.Engine.RequireOn(ctx, "vehicle.stock:view", res), authz.ErrForbidden)
}

func TestAccessNeedsAPrincipal(t *testing.T) {
	w := newSmallWorld(t)
	_, err := w.Engine.Access(context.Background(), "vehicle.stock:view")
	assert.ErrorIs(t, err, authz.ErrNoPrincipal)
	_, err = w.Engine.Access(w.As(authz.User(1, 0)), "nope:view")
	assert.ErrorIs(t, err, authz.ErrUnknownPermission)
}

func TestNewEngineRequiresItsParts(t *testing.T) {
	p := authztestParts(t)
	base := authz.Config{Catalogue: smallCatalogue(t), Hierarchy: p.h, Store: p.store, Resolver: p.places, Features: p.features}
	for name, mutate := range map[string]func(*authz.Config){
		"no catalogue": func(c *authz.Config) { c.Catalogue = nil },
		"no hierarchy": func(c *authz.Config) { c.Hierarchy = nil },
		"no store":     func(c *authz.Config) { c.Store = nil },
		"no resolver":  func(c *authz.Config) { c.Resolver = nil },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.Catalogue = smallCatalogue(t)
			mutate(&cfg)
			_, err := authz.NewEngine(cfg)
			assert.Error(t, err)
		})
	}
	t.Run("an invalid catalogue", func(t *testing.T) {
		bad := authz.NewCatalogue()
		require.NoError(t, bad.Define("a.b:update", authz.Requires("a.b:view")))
		cfg := base
		cfg.Catalogue = bad
		_, err := authz.NewEngine(cfg)
		var ve *authz.ValidationError
		assert.ErrorAs(t, err, &ve)
	})
}

// Own and OwnLocation are not nested: a principal holding both (seller and manager at the same dealer)
// keeps both clauses, so the list reaches its own leads at other locations as well as its location's.
func TestAccessKeepsOwnAndOwnLocationSideBySide(t *testing.T) {
	w := newSmallWorld(t)
	w.Bind(user(7), "seller", authztest.Dealer(1))
	w.Bind(user(7), "manager", authztest.Dealer(1))
	set, err := w.Engine.Access(w.As(authz.User(7, 10)), "crm.lead:view")
	require.NoError(t, err)
	got := map[authz.Qualifier]bool{}
	for _, c := range set.Clauses {
		got[c.Qualifier] = true
	}
	assert.Equal(t, map[authz.Qualifier]bool{authz.QualifierOwn: true, authz.QualifierOwnLocation: true}, got)

	// And both reach a record: its own lead at location 11, and a colleague's lead at location 10.
	assert.NoError(t, w.Engine.RequireOn(w.As(authz.User(7, 10)), "crm.lead:view",
		authz.Resource{Scope: authztest.Location(11), Owner: 7, OwnerLocation: 11}))
	assert.NoError(t, w.Engine.RequireOn(w.As(authz.User(7, 10)), "crm.lead:view",
		authz.Resource{Scope: authztest.Location(10), Owner: 8, OwnerLocation: 10}))
}
