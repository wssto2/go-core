package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
)

// The Dealership sample is a real role x permission matrix (162 permissions,
// ten job roles). These tests hold it to the model: it validates, and the engine
// grants exactly what the matrix says, per role.

func dealershipWorld(t *testing.T) (*authztest.World, *authztest.Sample) {
	t.Helper()
	sample := authztest.MustDealership()
	w := authztest.NewWorld(t, sample.Catalogue, sample.Roles...)
	w.Places.AddDealer(1).AddDealer(2).AddLocation(10, 1).AddLocation(20, 2)
	return w, sample
}

func TestDealershipSampleValidates(t *testing.T) {
	sample := authztest.MustDealership()
	all := sample.Catalogue.All()
	assert.Len(t, all, 162)
	var system, orgOnly, sensitive, ownable int
	for _, p := range all {
		if p.System {
			system++
		}
		if p.OrganizationOnly {
			orgOnly++
		}
		if p.Sensitive {
			sensitive++
		}
		if p.Ownable() {
			ownable++
		}
	}
	assert.Equal(t, 22, system)
	assert.Equal(t, 14, orgOnly)
	assert.Positive(t, sensitive)
	assert.Positive(t, ownable)

	require.NoError(t, sample.Catalogue.Validate())
	require.Len(t, sample.Roles, 12)
	for _, r := range sample.Roles {
		assert.NoError(t, r.Validate(sample.Catalogue), r.Key)
	}
}

func TestDealershipEngineGrantsExactlyTheMatrix(t *testing.T) {
	w, sample := dealershipWorld(t)
	ctx := context.Background()
	scopeFor := func(r authz.Role) authz.Scope {
		for _, g := range r.Grants {
			if p, _ := sample.Catalogue.Lookup(g.Permission); p.OrganizationOnly {
				return authztest.Org() // distribution centre: bound at the organization
			}
		}
		return authztest.Dealer(1)
	}
	for i, role := range sample.Roles {
		if role.Computed != nil {
			continue
		}
		t.Run(role.Key, func(t *testing.T) {
			subject := user(100 + i)
			w.Bind(subject, role.Key, scopeFor(role))
			want := map[string]authz.Qualifier{}
			for _, g := range role.Grants {
				want[g.Permission] = g.Qualifier
			}
			principal := authz.User(subject.ID, 10)
			for _, p := range sample.Catalogue.All() {
				set, err := w.Engine.Access(authz.WithPrincipal(ctx, principal), p.ID)
				require.NoError(t, err)
				q, granted := want[p.ID]
				if !granted {
					assert.True(t, set.Empty(), "%s must not hold %s", role.Key, p.ID)
					continue
				}
				require.Len(t, set.Clauses, 1, "%s / %s", role.Key, p.ID)
				assert.Equal(t, q, set.Clauses[0].Qualifier, "%s / %s", role.Key, p.ID)
			}
		})
	}
}

func TestDealershipVehicleKindRolesAreConstrained(t *testing.T) {
	w, _ := dealershipWorld(t)
	w.Bind(user(1), "salesused", authztest.Dealer(1))
	w.Bind(user(2), "salesnew", authztest.Dealer(1))
	w.Bind(user(3), "salesmanager", authztest.Dealer(1))
	lead := func(id int, kind string, owner int) error {
		return w.Engine.RequireOn(w.As(authz.User(id, 10)), "crm.lead:view", authz.Resource{
			Scope: authztest.Location(10), Owner: owner, OwnerLocation: 10,
			Attrs: map[string]string{authztest.AttrVehicleKind: kind},
		})
	}
	assert.NoError(t, lead(1, "used", 1), "own used lead")
	assert.ErrorIs(t, lead(1, "new", 1), authz.ErrForbidden, "own new lead: wrong kind")
	assert.ErrorIs(t, lead(1, "used", 77), authz.ErrForbidden, "someone else's used lead")
	assert.NoError(t, lead(1, "used", 0), "the unassigned pool counts as own")
	assert.NoError(t, lead(2, "new", 2))
	assert.NoError(t, lead(3, "used", 77), "the sales manager sees every lead of its location")
	assert.NoError(t, lead(3, "new", 77))

	// a used-vehicle salesperson sees every offer of the location but edits only their own
	offer := func(perm string, owner int) error {
		return w.Engine.RequireOn(w.As(authz.User(1, 10)), perm, authz.Resource{
			Scope: authztest.Location(10), Owner: owner, OwnerLocation: 10,
			Attrs: map[string]string{authztest.AttrVehicleKind: "used"},
		})
	}
	assert.NoError(t, offer("crm.offer:view", 77))
	assert.ErrorIs(t, offer("crm.offer:update", 77), authz.ErrForbidden)
	assert.NoError(t, offer("crm.offer:update", 1))
}

func TestDealershipComputedRolesAndDelegation(t *testing.T) {
	w, _ := dealershipWorld(t)
	w.Bind(user(1), "webmaster", authztest.Org())
	w.Bind(user(2), "importer", authztest.Org())
	ctx := w.As(authz.User(1, 0))

	eff, err := w.Engine.Effective(context.Background(), user(1))
	require.NoError(t, err)
	assert.Len(t, eff.Permissions(), 162)
	eff, err = w.Engine.Effective(context.Background(), user(2))
	require.NoError(t, err)
	assert.Len(t, eff.Permissions(), 162-22)
	assert.NoError(t, w.Engine.Require(ctx, "newvehicle:create"))

	admin := w.Admin("iam.role:manage", "iam.user:manage", "iam.user:manage")
	// importer cannot make a webmaster, or hand out a system permission
	_, err = admin.Bind(w.As(authz.User(2, 0)), authz.Binding{Subject: user(9), Role: authz.RoleRef{Key: "webmaster"}, Scope: authztest.Org()})
	assert.ErrorIs(t, err, authz.ErrEscalation)
	_, err = admin.SaveRole(w.As(authz.User(2, 0)), authz.Role{Name: "sneaky", Grants: authz.Grants(authz.QualifierAll, "newvehicle:create")})
	assert.ErrorIs(t, err, authz.ErrEscalation)
	// but may bind the salesperson role at a dealer, and hand the distribution centre role out at the organization
	_, err = admin.Bind(w.As(authz.User(2, 0)), authz.Binding{Subject: user(9), Role: authz.RoleRef{Key: "salesused"}, Scope: authztest.Dealer(2)})
	assert.NoError(t, err)
	_, err = admin.Bind(w.As(authz.User(2, 0)), authz.Binding{Subject: user(10), Role: authz.RoleRef{Key: "distribution"}, Scope: authztest.Org()})
	assert.NoError(t, err)
	// a distribution role holds organization-only permissions, so it cannot be bound at a dealer
	_, err = admin.Bind(w.As(authz.User(2, 0)), authz.Binding{Subject: user(10), Role: authz.RoleRef{Key: "distribution"}, Scope: authztest.Dealer(1)})
	var ve *authz.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.True(t, ve.Has(authz.ProblemOrganizationOnly))
}
