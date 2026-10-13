package admin_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
)

const problemLeadsForbidden authz.ProblemCode = "leads_forbidden"

func customers() []authz.Grant { return authz.Grants(authz.QualifierAll, "crm.customer:view") }

func seller20() admin.BindingDraft {
	return admin.BindingDraft{Role: roleSeller, Scope: authztest.Dealer(20)}
}

const (
	permLead   = "crm.lead:view"
	roleSeller = "seller"
	nameClerk  = "Clerk"
)

func noLeads(_ context.Context, role authz.Role) []authz.Problem {
	var out []authz.Problem

	for _, g := range role.Grants {
		if g.Permission == permLead {
			out = append(out, authz.Problem{Code: problemLeadsForbidden, Permission: g.Permission, Detail: ""})
		}
	}

	return out
}

func TestRoleCheckRefusesCreateAndUpdate(t *testing.T) {
	t.Parallel()

	world := newFixture(t, func(c *admin.Config) { c.RoleCheck = noLeads })
	world.seed(1, "webmaster", authztest.Org())
	world.seed(2, "dealeradmin", authztest.Org())

	leads := admin.RoleDraft{Name: "Leads", Description: "", Attrs: nil,
		Grants: authz.Grants(authz.QualifierAll, permLead)}
	plain := admin.RoleDraft{Name: "Customers", Description: "", Attrs: nil,
		Grants: customers()}

	_, err := world.Roles.Create(as(1), leads)
	require.Error(t, err)
	assert.True(t, apperr.HasCode(err, apperr.CodeBadRequest))
	assert.True(t, apperr.HasReason(err, authz.ReasonInvalid))

	var invalid *authz.ValidationError

	require.ErrorAs(t, err, &invalid)
	assert.True(t, invalid.Has(problemLeadsForbidden))

	created, err := world.Roles.Create(as(1), plain)
	require.NoError(t, err)

	leads.Name = "Customers"
	_, err = world.Roles.Update(as(1), created.Ref, leads)
	require.Error(t, err, "an update is judged too")
	assert.True(t, apperr.HasReason(err, authz.ReasonInvalid))

	_, err = world.Roles.Create(as(2), leads)
	require.ErrorIs(t, err, authz.ErrForbidden, "who may not manage roles is told so, not the problems")
}

func TestBindingCheckGuardsBindUnbindAndReplace(t *testing.T) {
	t.Parallel()

	errNo := apperr.Forbidden("not in this dealer")
	deny := false
	world := newFixture(t, func(c *admin.Config) {
		c.BindingCheck = func(context.Context, authz.Subject, authz.Binding) error {
			if deny {
				return errNo
			}

			return nil
		}
	})
	world.seed(1, "webmaster", authztest.Org())

	bound, err := world.Bindings.Bind(as(1), user(5), admin.BindingDraft{Role: roleSeller, Scope: authztest.Dealer(10)})
	require.NoError(t, err)

	clerk, err := world.Roles.Create(as(1), admin.RoleDraft{Name: nameClerk, Description: "", Attrs: nil,
		Grants: customers()})
	require.NoError(t, err)
	world.seedCustom(6, clerk.ID, authztest.Dealer(10))

	deny = true

	_, err = world.Bindings.Bind(as(1), user(7), admin.BindingDraft{Role: roleSeller, Scope: authztest.Dealer(10)})
	require.ErrorIs(t, err, errNo)
	require.ErrorIs(t, world.Bindings.Unbind(as(1), user(5), bound.ID), errNo)
	_, err = world.Roles.Replace(as(1), clerk.Ref, roleSeller)
	require.ErrorIs(t, err, errNo)

	held, err := world.Store.BindingsForRole(context.Background(), clerk.ID)
	require.NoError(t, err)
	assert.Len(t, held, 1, "a refused replacement re-binds nobody")

	deny = false

	n, err := world.Roles.Replace(as(1), clerk.Ref, roleSeller)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestSubjectReachAnswersNotFoundForTheRest(t *testing.T) {
	t.Parallel()

	// the dealer administrator (2) reaches person 5 only; the webmaster (1) everyone
	world := newFixture(t, func(c *admin.Config) {
		c.Reach = func(ctx context.Context, s authz.Subject) (bool, error) {
			p, _ := authz.PrincipalFrom(ctx)

			return p.ID == 1 || s.ID == 5, nil
		}
	})
	world.seed(1, "webmaster", authztest.Org())
	world.seed(2, "dealeradmin", authztest.Dealer(10))

	otherDealers, err := world.Bindings.Bind(as(1), user(6), seller20())
	require.NoError(t, err)

	notFound := func(err error) { assert.True(t, apperr.HasCode(err, apperr.CodeNotFound), "%v", err) }

	_, err = world.Bindings.Access(as(2), user(5))
	require.NoError(t, err)
	_, err = world.Bindings.Scopes(as(2), user(5))
	require.NoError(t, err)

	_, err = world.Bindings.Access(as(2), user(6))
	notFound(err)
	_, err = world.Bindings.Scopes(as(2), user(6))
	notFound(err)
	_, err = world.Bindings.Bind(as(2), user(6), admin.BindingDraft{Role: roleSeller, Scope: authztest.Dealer(10)})
	notFound(err)
	notFound(world.Bindings.Unbind(as(2), user(6), otherDealers.ID))

	_, err = world.Bindings.Access(as(1), user(6))
	require.NoError(t, err, "the webmaster reaches everyone")
}
