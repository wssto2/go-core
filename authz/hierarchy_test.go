package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
)

func TestNewHierarchyValidation(t *testing.T) {
	for name, levels := range map[string][]string{
		"none":         {},
		"duplicate":    {"organization", "dealer", "dealer"},
		"not a word":   {"organization", "Dealer"},
		"with hyphens": {"organization", "sales-region"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := authz.NewHierarchy(levels...)
			assert.Error(t, err)
		})
	}
	flat, err := authz.NewHierarchy("organization")
	require.NoError(t, err, "an application without tenancy has the root only")
	assert.Equal(t, flat.RootLevel(), flat.LeafLevel())
	require.NoError(t, flat.Check(flat.Root()))
	assert.Error(t, flat.Check(authz.Scope{Level: "organization", ID: 1}))
	_, err = flat.WithTenantLevel("organization")
	assert.Error(t, err)

	h, err := authz.NewHierarchy("organization", "dealer", "location")
	require.NoError(t, err)
	assert.Equal(t, "organization", h.RootLevel())
	assert.Equal(t, "location", h.LeafLevel())
	assert.Equal(t, authz.Scope{Level: "organization"}, h.Root())

	_, err = h.WithTenantLevel("organization")
	assert.Error(t, err, "root cannot be the tenant")
	_, err = h.WithTenantLevel("region")
	assert.Error(t, err)
	ht, err := h.WithTenantLevel("dealer")
	require.NoError(t, err)
	assert.Equal(t, "dealer", ht.TenantLevel())
	assert.Empty(t, h.TenantLevel(), "WithTenantLevel returns a copy")
}

func TestHierarchyCheck(t *testing.T) {
	h := authztest.Hierarchy()
	tests := []struct {
		scope authz.Scope
		ok    bool
	}{
		{authztest.Org(), true},
		{authztest.Dealer(3), true},
		{authztest.Location(7), true},
		{authz.Scope{Level: "organization", ID: 1}, false}, // root has no ID
		{authz.Scope{Level: "dealer"}, false},              // below root needs an ID
		{authz.Scope{Level: "dealer", ID: -2}, false},
		{authz.Scope{Level: "planet", ID: 1}, false},
		{authz.Scope{}, false},
	}
	for _, tt := range tests {
		err := h.Check(tt.scope)
		if tt.ok {
			assert.NoError(t, err, tt.scope.String())
		} else {
			assert.ErrorIs(t, err, authz.ErrInvalidScope, tt.scope.String())
		}
	}
}

func TestChainAndContainment(t *testing.T) {
	h := authztest.Hierarchy()
	places := authztest.NewPlaces().AddDealer(1).AddDealer(2).AddLocation(10, 1)
	ctx := context.Background()

	chain, err := h.Chain(ctx, places, authztest.Location(10))
	require.NoError(t, err)
	assert.Equal(t, authz.Chain{authztest.Location(10), authztest.Dealer(1), authztest.Org()}, chain)
	assert.True(t, chain.Within(authztest.Org()))
	assert.True(t, chain.Within(authztest.Dealer(1)))
	assert.True(t, chain.Within(authztest.Location(10)))
	assert.False(t, chain.Within(authztest.Dealer(2)))
	assert.False(t, chain.Within(authztest.Location(11)))
	d, ok := chain.At("dealer")
	require.True(t, ok)
	assert.Equal(t, 1, d.ID)

	root, err := h.Chain(ctx, places, authztest.Org())
	require.NoError(t, err)
	assert.Equal(t, authz.Chain{authztest.Org()}, root)
	assert.False(t, root.Within(authztest.Dealer(1)), "the root is not inside a dealer")

	_, err = h.Chain(ctx, places, authztest.Location(99))
	assert.ErrorIs(t, err, authz.ErrScopeNotFound)
	_, err = h.Chain(ctx, places, authz.Scope{Level: "dealer"})
	assert.ErrorIs(t, err, authz.ErrInvalidScope)

	t.Run("known ancestors skip the resolver", func(t *testing.T) {
		empty := authztest.NewPlaces() // knows nothing
		c, err := h.Chain(ctx, empty, authztest.Location(10), authztest.Dealer(1))
		require.NoError(t, err)
		assert.Equal(t, authz.Chain{authztest.Location(10), authztest.Dealer(1), authztest.Org()}, c)
	})
}

type wrongLevel struct{}

func (wrongLevel) Parent(context.Context, authz.Scope) (authz.Scope, error) {
	return authztest.Location(1), nil // skips a level
}

func TestChainRejectsResolverSkippingALevel(t *testing.T) {
	_, err := authztest.Hierarchy().Chain(context.Background(), wrongLevel{}, authztest.Location(5))
	assert.ErrorIs(t, err, authz.ErrInvalidScope)
}
