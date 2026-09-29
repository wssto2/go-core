package authztest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
)

func TestFakeAuthorizer(t *testing.T) {
	ctx := authz.WithPrincipal(context.Background(), authz.User(1, 10))

	t.Run("deny all", func(t *testing.T) {
		f := authztest.DenyAll()
		assert.ErrorIs(t, f.Require(ctx, "a.b:view"), authz.ErrForbidden)
		assert.ErrorIs(t, f.RequireOn(ctx, "a.b:view", authz.Resource{}), authz.ErrForbidden)
		set, err := f.Access(ctx, "a.b:view")
		require.NoError(t, err)
		assert.True(t, set.Empty())
	})
	t.Run("allow all", func(t *testing.T) {
		f := authztest.AllowAll()
		assert.NoError(t, f.Require(ctx, "anything:goes"))
		set, err := f.Access(ctx, "anything:goes")
		require.NoError(t, err)
		assert.False(t, set.Empty())
		assert.Equal(t, authz.User(1, 10), set.Principal)
	})
	t.Run("allow some, and record what was checked", func(t *testing.T) {
		f := authztest.DenyAll().Allow("a.b:view")
		assert.NoError(t, f.Require(ctx, "a.b:view"))
		assert.ErrorIs(t, f.Require(ctx, "a.b:update"), authz.ErrForbidden)
		assert.NoError(t, f.RequireOn(ctx, "a.b:view", authz.Resource{Owner: 7}))
		assert.Equal(t, []string{"a.b:view", "a.b:update", "a.b:view"}, f.Checked())
	})
	t.Run("clauses for a list", func(t *testing.T) {
		clause := authz.Clause{Scope: authztest.Dealer(3), Chain: authz.Chain{authztest.Dealer(3), authztest.Org()}, Qualifier: authz.QualifierOwn}
		f := authztest.DenyAll().WithClauses("a.b:view", clause)
		set, err := f.Access(ctx, "a.b:view")
		require.NoError(t, err)
		assert.Equal(t, []authz.Clause{clause}, set.Clauses)
	})
	t.Run("it is an Authorizer", func(t *testing.T) {
		var _ authz.Authorizer = authztest.AllowAll()
	})
}

func TestWorldWiresARealEngine(t *testing.T) {
	cat := authz.NewCatalogue()
	require.NoError(t, cat.Define("a.b:view"))
	w := authztest.NewWorld(t, cat, authz.Role{Key: "viewer", Name: "Viewer", Grants: authz.Grants(authz.QualifierAll, "a.b:view")})
	w.Places.AddDealer(1).AddLocation(5, 1)
	w.Bind(authz.Subject{Kind: authz.KindUser, ID: 1}, "viewer", authztest.Location(5))

	ctx := w.As(authz.User(1, 5))
	assert.NoError(t, w.Engine.RequireOn(ctx, "a.b:view", authz.Resource{Scope: authztest.Location(5)}))
	assert.ErrorIs(t, w.Engine.RequireOn(ctx, "a.b:view", authz.Resource{Scope: authztest.Dealer(1)}), authz.ErrForbidden)

	custom := w.SaveRole(authz.Role{Name: "custom", Grants: authz.Grants(authz.QualifierAll, "a.b:view")})
	w.BindCustom(authz.Subject{Kind: authz.KindUser, ID: 2}, custom.ID, authztest.Dealer(1))
	assert.NoError(t, w.Engine.Require(w.As(authz.User(2, 0)), "a.b:view"))
}

func TestHierarchyHelper(t *testing.T) {
	h := authztest.Hierarchy()
	assert.Equal(t, "dealer", h.TenantLevel())
	assert.Equal(t, authztest.Org(), h.Root())
	assert.NoError(t, h.Check(authztest.Location(3)))
}

func TestDealershipIsFreshOnEachCall(t *testing.T) {
	a, err := authztest.Dealership()
	require.NoError(t, err)
	b, err := authztest.Dealership()
	require.NoError(t, err)
	require.NoError(t, a.Catalogue.Define("extra.one:view"))
	assert.Equal(t, a.Catalogue.Len()-1, b.Catalogue.Len(), "samples do not share state")
	assert.Equal(t, "Rabljena vozila", b.Modules["usedvehicle.evaluation:view"])
}
