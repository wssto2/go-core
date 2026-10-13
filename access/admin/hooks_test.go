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

func noLeads(_ context.Context, role authz.Role) []authz.Problem {
	var out []authz.Problem

	for _, g := range role.Grants {
		if g.Permission == "crm.lead:view" {
			out = append(out, authz.Problem{Code: problemLeadsForbidden, Permission: g.Permission})
		}
	}

	return out
}

func TestRoleCheckRefusesCreateAndUpdate(t *testing.T) {
	f := newFixture(t, func(c *admin.Config) { c.RoleCheck = noLeads })
	f.seed(1, "webmaster", authztest.Org())
	f.seed(2, "dealeradmin", authztest.Org())

	leads := admin.RoleDraft{Name: "Leads", Grants: authz.Grants(authz.QualifierAll, "crm.lead:view")}
	ok := admin.RoleDraft{Name: "Customers", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")}

	_, err := f.Roles.Create(as(1), leads)
	require.Error(t, err)
	assert.True(t, apperr.HasCode(err, apperr.CodeBadRequest))
	assert.True(t, apperr.HasReason(err, authz.ReasonInvalid))

	var invalid *authz.ValidationError

	require.ErrorAs(t, err, &invalid)
	assert.True(t, invalid.Has(problemLeadsForbidden))

	created, err := f.Roles.Create(as(1), ok)
	require.NoError(t, err)

	leads.Name = "Customers"
	_, err = f.Roles.Update(as(1), created.Ref, leads)
	require.Error(t, err, "an update is judged too")
	assert.True(t, apperr.HasReason(err, authz.ReasonInvalid))

	_, err = f.Roles.Create(as(2), leads)
	require.ErrorIs(t, err, authz.ErrForbidden, "who may not manage roles is told so, not the problems")
}

func TestBindingCheckGuardsBindUnbindAndReplace(t *testing.T) {
	errNo := apperr.Forbidden("not in this dealer")
	deny := false
	f := newFixture(t, func(c *admin.Config) {
		c.BindingCheck = func(context.Context, authz.Subject, authz.Binding) error {
			if deny {
				return errNo
			}

			return nil
		}
	})
	f.seed(1, "webmaster", authztest.Org())

	bound, err := f.Bindings.Bind(as(1), user(5), admin.BindingDraft{Role: "seller", Scope: authztest.Dealer(10)})
	require.NoError(t, err)

	clerk, err := f.Roles.Create(as(1), admin.RoleDraft{Name: "Clerk", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")})
	require.NoError(t, err)
	f.seedCustom(6, clerk.ID, authztest.Dealer(10))

	deny = true

	_, err = f.Bindings.Bind(as(1), user(7), admin.BindingDraft{Role: "seller", Scope: authztest.Dealer(10)})
	require.ErrorIs(t, err, errNo)
	require.ErrorIs(t, f.Bindings.Unbind(as(1), user(5), bound.ID), errNo)
	_, err = f.Roles.Replace(as(1), clerk.Ref, "seller")
	require.ErrorIs(t, err, errNo)

	held, err := f.Store.BindingsForRole(context.Background(), clerk.ID)
	require.NoError(t, err)
	assert.Len(t, held, 1, "a refused replacement re-binds nobody")

	deny = false

	n, err := f.Roles.Replace(as(1), clerk.Ref, "seller")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestSubjectReachAnswersNotFoundForTheRest(t *testing.T) {
	// the dealer administrator (2) reaches person 5 only; the webmaster (1) everyone
	f := newFixture(t, func(c *admin.Config) {
		c.Reach = func(ctx context.Context, s authz.Subject) (bool, error) {
			p, _ := authz.PrincipalFrom(ctx)

			return p.ID == 1 || s.ID == 5, nil
		}
	})
	f.seed(1, "webmaster", authztest.Org())
	f.seed(2, "dealeradmin", authztest.Dealer(10))

	otherDealers, err := f.Bindings.Bind(as(1), user(6), admin.BindingDraft{Role: "seller", Scope: authztest.Dealer(20)})
	require.NoError(t, err)

	notFound := func(err error) { assert.True(t, apperr.HasCode(err, apperr.CodeNotFound), "%v", err) }

	_, err = f.Bindings.Access(as(2), user(5))
	require.NoError(t, err)
	_, err = f.Bindings.Scopes(as(2), user(5))
	require.NoError(t, err)

	_, err = f.Bindings.Access(as(2), user(6))
	notFound(err)
	_, err = f.Bindings.Scopes(as(2), user(6))
	notFound(err)
	_, err = f.Bindings.Bind(as(2), user(6), admin.BindingDraft{Role: "seller", Scope: authztest.Dealer(10)})
	notFound(err)
	notFound(f.Bindings.Unbind(as(2), user(6), otherDealers.ID))

	_, err = f.Bindings.Access(as(1), user(6))
	require.NoError(t, err, "the webmaster reaches everyone")
}
