package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
)

func TestDenyByDefault(t *testing.T) {
	w := newSmallWorld(t)

	t.Run("no principal", func(t *testing.T) {
		err := w.Engine.Require(context.Background(), "vehicle.stock:view")
		assert.ErrorIs(t, err, authz.ErrNoPrincipal)
		assert.True(t, apperr.HasCode(err, apperr.CodeUnauthenticated))
	})
	t.Run("no bindings", func(t *testing.T) {
		ctx := w.As(authz.User(1, 0))
		err := w.Engine.Require(ctx, "vehicle.stock:view")
		assert.ErrorIs(t, err, authz.ErrForbidden)
		assert.True(t, apperr.HasCode(err, apperr.CodePermissionDenied))
		assert.True(t, apperr.HasReason(err, authz.ReasonForbidden))
		assert.ErrorIs(t, w.Engine.RequireOn(ctx, "vehicle.stock:view", authz.Resource{Scope: authztest.Dealer(1)}), authz.ErrForbidden)

		set, err := w.Engine.Access(ctx, "vehicle.stock:view")
		require.NoError(t, err)
		assert.True(t, set.Empty())
	})
	t.Run("unknown permission is a programming error, not a silent deny", func(t *testing.T) {
		err := w.Engine.Require(w.As(authz.User(1, 0)), "vehicle.stock:fly")
		assert.ErrorIs(t, err, authz.ErrUnknownPermission)
		assert.True(t, apperr.HasCode(err, apperr.CodeInternal))
	})
	t.Run("a permission the role lacks", func(t *testing.T) {
		w.Bind(user(2), "seller", authztest.Dealer(1))
		assert.ErrorIs(t, w.Engine.Require(w.As(authz.User(2, 0)), "iam.role:manage"), authz.ErrForbidden)
		assert.NoError(t, w.Engine.Require(w.As(authz.User(2, 0)), "vehicle.stock:view"))
	})
}

func TestRequireOnScopeContainment(t *testing.T) {
	tests := []struct {
		name    string
		binding authz.Scope
		record  authz.Scope
		want    bool
	}{
		{"dealer binding, record in one of its locations", authztest.Dealer(1), authztest.Location(10), true},
		{"dealer binding, record in another location of the dealer", authztest.Dealer(1), authztest.Location(11), true},
		{"dealer binding, record of the dealer itself", authztest.Dealer(1), authztest.Dealer(1), true},
		{"dealer binding never sees another dealer's location", authztest.Dealer(1), authztest.Location(20), false},
		{"dealer binding never sees another dealer", authztest.Dealer(1), authztest.Dealer(2), false},
		{"dealer binding, organization-wide record", authztest.Dealer(1), authz.Scope{}, false},
		{"location binding, same location", authztest.Location(10), authztest.Location(10), true},
		{"location binding, sibling location", authztest.Location(10), authztest.Location(11), false},
		{"location binding, the dealer as a whole", authztest.Location(10), authztest.Dealer(1), false},
		{"organization binding, any dealer", authztest.Org(), authztest.Dealer(2), true},
		{"organization binding, any location", authztest.Org(), authztest.Location(20), true},
		{"organization binding, organization-wide record", authztest.Org(), authz.Scope{}, true},
		{"record in a place that does not exist", authztest.Dealer(1), authztest.Location(99), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newSmallWorld(t)
			w.Bind(user(1), "seller", tt.binding)
			err := w.Engine.RequireOn(w.As(authz.User(1, 0)), "vehicle.stock:view", authz.Resource{Scope: tt.record})
			if tt.want {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, authz.ErrForbidden)
			}
		})
	}
}

func TestRequireOnQualifiers(t *testing.T) {
	const me, other = 5, 6
	tests := []struct {
		name       string
		role       string
		perm       string
		principal  authz.Principal
		res        authz.Resource
		want       bool
		denyReason string
	}{
		{"own: my record", "seller", "crm.lead:update", authz.User(me, 0), authz.Resource{Owner: me}, true, ""},
		{"own: someone else's record", "seller", "crm.lead:update", authz.User(me, 0), authz.Resource{Owner: other}, false, ""},
		{"own: unowned record is mine for leads (the pool)", "seller", "crm.lead:view", authz.User(me, 0), authz.Resource{Owner: 0}, true, ""},
		{"own: unowned record is not mine for offers", "seller", "crm.offer:update", authz.User(me, 0), authz.Resource{Owner: 0}, false, ""},
		{"all: any offer for viewing", "seller", "crm.offer:view", authz.User(me, 0), authz.Resource{Owner: other}, true, ""},
		{"own location: my location", "manager", "crm.lead:view", authz.User(me, 10), authz.Resource{OwnerLocation: 10}, true, ""},
		{"own location: another location", "manager", "crm.lead:view", authz.User(me, 10), authz.Resource{OwnerLocation: 11}, false, ""},
		{"own location: user without a location", "manager", "crm.lead:view", authz.User(me, 0), authz.Resource{OwnerLocation: 0}, false, ""},
		{"own location: record without a location", "manager", "crm.lead:view", authz.User(me, 10), authz.Resource{}, false, ""},
		{"all: any owner, any location", "dealerlead", "crm.lead:view", authz.User(me, 0), authz.Resource{Owner: other, OwnerLocation: 99}, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newSmallWorld(t)
			w.Bind(tt.principal.Subject, tt.role, authztest.Dealer(1))
			tt.res.Scope = authztest.Location(10)
			err := w.Engine.RequireOn(w.As(tt.principal), tt.perm, tt.res)
			if tt.want {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, authz.ErrForbidden)
			}
		})
	}
}

func TestServiceAccountsNeverMatchOwn(t *testing.T) {
	w := newSmallWorld(t)
	sa := authz.ServiceAccount(5)
	w.Bind(sa.Subject, "seller", authztest.Dealer(1))
	ctx := w.As(sa)
	// service account 5 must not be mistaken for user 5, the owner of the record
	assert.ErrorIs(t, w.Engine.RequireOn(ctx, "crm.lead:update", authz.Resource{Scope: authztest.Dealer(1), Owner: 5}), authz.ErrForbidden)
	assert.ErrorIs(t, w.Engine.RequireOn(ctx, "crm.lead:view", authz.Resource{Scope: authztest.Dealer(1), Owner: 0}), authz.ErrForbidden)
	// but it holds "all" grants like a person, within its scope
	assert.NoError(t, w.Engine.RequireOn(ctx, "crm.offer:view", authz.Resource{Scope: authztest.Location(10)}))
	assert.ErrorIs(t, w.Engine.RequireOn(ctx, "crm.offer:view", authz.Resource{Scope: authztest.Location(20)}), authz.ErrForbidden)
	// the same binding for user 5 is a different subject
	assert.ErrorIs(t, w.Engine.Require(w.As(authz.User(5, 0)), "crm.offer:view"), authz.ErrForbidden)
}

func TestAttributeConstraints(t *testing.T) {
	w := newSmallWorld(t)
	w.Bind(user(1), "usedseller", authztest.Dealer(1))
	ctx := w.As(authz.User(1, 0))
	res := func(kind string, present bool) authz.Resource {
		r := authz.Resource{Scope: authztest.Location(10)}
		if present {
			r.Attrs = map[string]string{"vehiclekind": kind}
		}
		return r
	}
	assert.NoError(t, w.Engine.RequireOn(ctx, "crm.lead:view", res("used", true)))
	assert.ErrorIs(t, w.Engine.RequireOn(ctx, "crm.lead:view", res("new", true)), authz.ErrForbidden)
	assert.ErrorIs(t, w.Engine.RequireOn(ctx, "crm.lead:view", res("", false)), authz.ErrForbidden, "an unknown kind never matches a constraint")

	set, err := w.Engine.Access(ctx, "crm.lead:view")
	require.NoError(t, err)
	require.Len(t, set.Clauses, 1)
	assert.Equal(t, map[string][]string{"vehiclekind": {"used"}}, set.Clauses[0].Attrs)
	assert.False(t, set.Unrestricted(w.Hierarchy))
}

func TestAttributesOnlyConstrainPermissionsThatDeclareThem(t *testing.T) {
	w := newSmallWorld(t)
	// the role constrains vehiclekind, but vehicle.stock:view does not declare it
	w.SaveRole(authz.Role{Name: "used only", Grants: authz.Grants(authz.QualifierAll, "vehicle.stock:view"),
		Attrs: map[string][]string{"vehiclekind": {"used"}}})
	w.BindCustom(user(1), 1, authztest.Dealer(1))
	set, err := w.Engine.Access(w.As(authz.User(1, 0)), "vehicle.stock:view")
	require.NoError(t, err)
	require.Len(t, set.Clauses, 1)
	assert.Empty(t, set.Clauses[0].Attrs)
	assert.NoError(t, w.Engine.RequireOn(w.As(authz.User(1, 0)), "vehicle.stock:view", authz.Resource{Scope: authztest.Dealer(1)}))
}

func TestWidestWinsAcrossBindings(t *testing.T) {
	t.Run("all beats own for the same scope", func(t *testing.T) {
		w := newSmallWorld(t)
		w.Bind(user(1), "seller", authztest.Dealer(1))     // leads: own
		w.Bind(user(1), "dealerlead", authztest.Dealer(1)) // leads: all
		ctx := w.As(authz.User(1, 0))

		assert.NoError(t, w.Engine.RequireOn(ctx, "crm.lead:update", authz.Resource{Scope: authztest.Location(10), Owner: 99}))
		set, err := w.Engine.Access(ctx, "crm.lead:update")
		require.NoError(t, err)
		require.Len(t, set.Clauses, 1, "the own clause is covered by the all clause")
		assert.Equal(t, authz.QualifierAll, set.Clauses[0].Qualifier)
		assert.Equal(t, authz.QualifierAll, set.Widest())
	})
	t.Run("wider scope beats a narrower one", func(t *testing.T) {
		w := newSmallWorld(t)
		w.Bind(user(1), "seller", authztest.Location(10))
		w.Bind(user(1), "seller", authztest.Dealer(1))
		set, err := w.Engine.Access(w.As(authz.User(1, 0)), "crm.offer:view")
		require.NoError(t, err)
		require.Len(t, set.Clauses, 1)
		assert.Equal(t, authztest.Dealer(1), set.Clauses[0].Scope)
	})
	t.Run("neither covers the other: both stay, OR semantics", func(t *testing.T) {
		w := newSmallWorld(t)
		w.Bind(user(1), "seller", authztest.Dealer(1))        // leads: own, whole dealer
		w.Bind(user(1), "dealerlead", authztest.Location(10)) // leads: all, one location
		ctx := w.As(authz.User(1, 0))

		set, err := w.Engine.Access(ctx, "crm.lead:view")
		require.NoError(t, err)
		assert.Len(t, set.Clauses, 2)

		other := func(loc int) authz.Resource { return authz.Resource{Scope: authztest.Location(loc), Owner: 99} }
		mine := func(loc int) authz.Resource { return authz.Resource{Scope: authztest.Location(loc), Owner: 1} }
		assert.NoError(t, w.Engine.RequireOn(ctx, "crm.lead:view", other(10)), "all at location 10")
		assert.ErrorIs(t, w.Engine.RequireOn(ctx, "crm.lead:view", other(11)), authz.ErrForbidden, "someone else's lead at 11")
		assert.NoError(t, w.Engine.RequireOn(ctx, "crm.lead:view", mine(11)), "own lead at 11")
		assert.ErrorIs(t, w.Engine.RequireOn(ctx, "crm.lead:view", mine(20)), authz.ErrForbidden, "own lead at another dealer")
	})
	t.Run("wider attribute allowance beats a constrained one", func(t *testing.T) {
		w := newSmallWorld(t)
		w.Bind(user(1), "usedseller", authztest.Dealer(1))
		w.Bind(user(1), "dealerlead", authztest.Dealer(1))
		set, err := w.Engine.Access(w.As(authz.User(1, 0)), "crm.lead:view")
		require.NoError(t, err)
		require.Len(t, set.Clauses, 1)
		assert.Empty(t, set.Clauses[0].Attrs)
	})
	t.Run("two constrained bindings stay separate", func(t *testing.T) {
		w := newSmallWorld(t)
		w.SaveRole(authz.Role{Name: "new leads", Grants: authz.Grants(authz.QualifierAll, "crm.lead:view"), Attrs: map[string][]string{"vehiclekind": {"new"}}})
		w.Bind(user(1), "usedseller", authztest.Dealer(1))
		w.BindCustom(user(1), 1, authztest.Dealer(1))
		ctx := w.As(authz.User(1, 0))
		for _, kind := range []string{"used", "new"} {
			assert.NoError(t, w.Engine.RequireOn(ctx, "crm.lead:view", authz.Resource{Scope: authztest.Dealer(1), Attrs: map[string]string{"vehiclekind": kind}}), kind)
		}
		assert.ErrorIs(t, w.Engine.RequireOn(ctx, "crm.lead:view", authz.Resource{Scope: authztest.Dealer(1), Attrs: map[string]string{"vehiclekind": "boat"}}), authz.ErrForbidden)
	})
}

func TestTenantIsTheHardWall(t *testing.T) {
	w := newSmallWorld(t)
	w.Bind(user(1), "dealerlead", authztest.Dealer(1))
	w.Bind(user(2), "dealerlead", authztest.Location(20)) // dealer 2
	w.Bind(user(3), "dealerlead", authztest.Org())
	w.Bind(user(4), "dealerlead", authztest.Dealer(1))
	w.Bind(user(4), "dealerlead", authztest.Dealer(2))

	tenant := func(id int) (int, bool) {
		eff, err := w.Engine.Effective(context.Background(), user(id))
		require.NoError(t, err)
		return eff.Tenant()
	}
	id, ok := tenant(1)
	assert.True(t, ok)
	assert.Equal(t, 1, id)
	id, ok = tenant(2)
	assert.True(t, ok)
	assert.Equal(t, 2, id, "a location binding pins its dealer")
	_, ok = tenant(3)
	assert.False(t, ok, "an organization binding crosses tenants")
	_, ok = tenant(4)
	assert.False(t, ok, "two tenants pin none")
	_, ok = tenant(99)
	assert.False(t, ok)

	eff, _ := w.Engine.Effective(context.Background(), user(4))
	ids, root := eff.Tenants()
	assert.Equal(t, []int{1, 2}, ids)
	assert.False(t, root)

	// the clause chain carries the dealer, which is what authzgorm filters by
	set, err := w.Engine.Access(w.As(authz.User(2, 0)), "crm.lead:view")
	require.NoError(t, err)
	require.Len(t, set.Clauses, 1)
	d, ok := set.Clauses[0].Chain.At("dealer")
	require.True(t, ok)
	assert.Equal(t, 2, d.ID)
}

func TestFeatureGate(t *testing.T) {
	w := newSmallWorld(t)
	w.Bind(user(1), "seller", authztest.Dealer(1)) // auction on
	w.Bind(user(2), "seller", authztest.Dealer(2)) // auction off
	w.Bind(user(3), "seller", authztest.Location(10))
	w.Bind(user(4), "webmaster", authztest.Org())

	perm := "auction.lot:view"
	assert.NoError(t, w.Engine.Require(w.As(authz.User(1, 0)), perm))
	assert.ErrorIs(t, w.Engine.Require(w.As(authz.User(2, 0)), perm), authz.ErrForbidden, "role grants it, dealer lacks the feature")
	assert.NoError(t, w.Engine.Require(w.As(authz.User(3, 0)), perm), "the feature is the dealer's, found through the location")

	set, err := w.Engine.Access(w.As(authz.User(2, 0)), perm)
	require.NoError(t, err)
	assert.True(t, set.Empty())

	t.Run("record-level: the feature of the record's dealer", func(t *testing.T) {
		ctx := w.As(authz.User(4, 0)) // organization binding
		assert.NoError(t, w.Engine.RequireOn(ctx, perm, authz.Resource{Scope: authztest.Location(10)}), "dealer 1 has it")
		assert.ErrorIs(t, w.Engine.RequireOn(ctx, perm, authz.Resource{Scope: authztest.Location(20)}), authz.ErrForbidden, "dealer 2 does not")
	})
	t.Run("switching the feature on takes effect at once", func(t *testing.T) {
		w.Features.Set(authztest.Dealer(2), "auction", true)
		assert.NoError(t, w.Engine.Require(w.As(authz.User(2, 0)), perm))
	})
	t.Run("permissions without a feature never ask", func(t *testing.T) {
		calls := 0
		p := authztestParts(t)
		e, err := authz.NewEngine(authz.Config{Catalogue: smallCatalogue(t), Hierarchy: p.h, Roles: smallRoles(), Store: p.store, Resolver: p.places,
			Features: authz.FeatureFunc(func(context.Context, authz.Scope, string) (bool, error) { calls++; return true, nil })})
		require.NoError(t, err)
		p.places.AddDealer(1)
		_, err = p.store.Bind(context.Background(), authz.Subject{}, authz.Binding{Subject: user(1), Role: authz.RoleRef{Key: "seller"}, Scope: authztest.Dealer(1)})
		require.NoError(t, err)
		ctx := authz.WithPrincipal(context.Background(), authz.User(1, 0))
		require.NoError(t, e.Require(ctx, "vehicle.stock:view"))
		assert.Zero(t, calls)
		require.NoError(t, e.Require(ctx, "auction.lot:view"))
		assert.Equal(t, 1, calls)
	})
}

func TestNewEngineNeedsAFeatureResolverForGatedPermissions(t *testing.T) {
	w := authztestParts(t)
	_, err := authz.NewEngine(authz.Config{Catalogue: smallCatalogue(t), Hierarchy: w.h, Store: w.store, Resolver: w.places})
	assert.ErrorContains(t, err, "FeatureResolver")
}

func TestOrganizationOnlyPermissionsNeedTheRoot(t *testing.T) {
	w := newSmallWorld(t)
	w.Bind(user(1), "importer", authztest.Dealer(1)) // computed: has report.group:view, but bound below the root
	w.Bind(user(2), "importer", authztest.Org())
	ctx1, ctx2 := w.As(authz.User(1, 0)), w.As(authz.User(2, 0))

	assert.NoError(t, w.Engine.Require(ctx1, "crm.lead:view"), "the rest of the role works at the dealer")
	assert.ErrorIs(t, w.Engine.Require(ctx1, "report.group:view"), authz.ErrForbidden)
	assert.NoError(t, w.Engine.Require(ctx2, "report.group:view"))
}

func TestComputedRolesInTheEngine(t *testing.T) {
	w := newSmallWorld(t)
	w.Bind(user(1), "webmaster", authztest.Org())
	w.Bind(user(2), "importer", authztest.Org())
	assert.NoError(t, w.Engine.Require(w.As(authz.User(1, 0)), "system.job:run"))
	assert.ErrorIs(t, w.Engine.Require(w.As(authz.User(2, 0)), "system.job:run"), authz.ErrForbidden)
	assert.NoError(t, w.Engine.Require(w.As(authz.User(2, 0)), "iam.role:manage"))
	set, err := w.Engine.Access(w.As(authz.User(1, 0)), "crm.lead:view")
	require.NoError(t, err)
	assert.True(t, set.Unrestricted(w.Hierarchy))
}

func TestUnresolvableBindingsGrantNothing(t *testing.T) {
	w := newSmallWorld(t)
	w.Bind(user(1), "no-such-role", authztest.Dealer(1))
	w.Bind(user(1), "seller", authztest.Dealer(99)) // dealer that no longer exists
	w.Bind(user(1), "seller", authz.Scope{Level: "dealer"})
	w.BindCustom(user(1), 12345, authztest.Dealer(1)) // custom role that was deleted
	ctx := w.As(authz.User(1, 0))
	assert.ErrorIs(t, w.Engine.Require(ctx, "vehicle.stock:view"), authz.ErrForbidden)

	// one good binding still works next to the broken ones
	w.Bind(user(1), "seller", authztest.Dealer(1))
	assert.NoError(t, w.Engine.Require(ctx, "vehicle.stock:view"))
}

func TestGrantsThatNoLongerFitTheCatalogueAreIgnored(t *testing.T) {
	w := newSmallWorld(t)
	// stored before a permission was removed / with a hand-edited qualifier
	w.SaveRole(authz.Role{Name: "stale", Grants: []authz.Grant{
		{Permission: "gone.thing:view", Qualifier: authz.QualifierAll},
		{Permission: "vehicle.stock:view", Qualifier: authz.QualifierOwn}, // not ownable
		{Permission: "crm.offer:view", Qualifier: authz.QualifierAll},
	}})
	w.BindCustom(user(1), 1, authztest.Dealer(1))
	ctx := w.As(authz.User(1, 0))
	assert.ErrorIs(t, w.Engine.Require(ctx, "vehicle.stock:view"), authz.ErrForbidden)
	assert.NoError(t, w.Engine.Require(ctx, "crm.offer:view"))
}

func TestMyAccess(t *testing.T) {
	w := newSmallWorld(t)
	w.Bind(user(1), "seller", authztest.Location(10))
	w.Bind(user(1), "dealerlead", authztest.Dealer(1))
	w.Bind(user(2), "seller", authztest.Dealer(2)) // auction off

	got, err := w.Engine.MyAccess(w.As(authz.User(1, 0)))
	require.NoError(t, err)
	assert.False(t, got.Root)
	lead := got.Permissions["crm.lead:view"]
	assert.Equal(t, authztest.Dealer(1), lead.Scope, "the widest scope")
	assert.Equal(t, authz.QualifierAll, lead.Qualifier, "the widest qualifier")
	assert.NotEmpty(t, lead.Clauses)
	assert.Contains(t, got.Permissions, "auction.lot:view")
	assert.NotContains(t, got.Permissions, "iam.role:manage")

	got2, err := w.Engine.MyAccess(w.As(authz.User(2, 0)))
	require.NoError(t, err)
	assert.Equal(t, []string{"auction.lot:view"}, got2.Unavailable)
	assert.NotContains(t, got2.Permissions, "auction.lot:view")

	_, err = w.Engine.MyAccess(context.Background())
	assert.ErrorIs(t, err, authz.ErrNoPrincipal)
}
