//nolint:exhaustruct,exhaustruct_v5,gochecknoglobals,goconst,lll,paralleltest,varnamelen // tests in the style of the package's others
package admin_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/database"
)

var bot = authz.Subject{Kind: authz.KindServiceAccount, ID: 1}

// robotsFixture is the fixture with the permissions of service accounts defined: iam.serviceaccount:view and :manage,
// and a role that holds only those.
func robotsFixture(t *testing.T) *fixture {
	t.Helper()

	cat := catalogue()
	cat.MustDefine(admin.ViewServiceAccountAccess)
	cat.MustDefine(admin.ManageServiceAccountBindings, authz.Sensitive(), authz.OrganizationOnly(), authz.Requires(admin.ViewServiceAccountAccess))

	roles := append(predefined(), authz.Role{
		Key: "robotadmin", Name: "Service account administrator",
		Grants: authz.Grants(authz.QualifierAll, admin.ViewServiceAccountAccess, admin.ManageServiceAccountBindings),
	})

	db, cleanup := database.MustPrepareTestDB()
	t.Cleanup(cleanup)
	require.NoError(t, gormstore.Migrate(db))

	f, err := fixtureWith(db, cat, roles)
	require.NoError(t, err)

	f.t = t

	return f
}

// A catalogue without the service account permissions has no service account subjects.
func TestServiceAccountsAreNotSubjectsWithoutTheirPermissions(t *testing.T) {
	f := newFixture(t)
	f.seed(1, "webmaster", authztest.Org())

	_, err := f.Bindings.Bind(as(1), bot, admin.BindingDraft{Role: "seller", Scope: authztest.Dealer(10)})
	assert.True(t, apperr.HasCode(err, apperr.CodeNotFound))
	_, err = f.Bindings.Access(as(1), bot)
	assert.True(t, apperr.HasCode(err, apperr.CodeNotFound))
}

// The access of a service account is guarded by iam.serviceaccount:*, a person's by iam.user:*, and neither opens the other.
func TestEachKindOfSubjectHasItsOwnPermissions(t *testing.T) {
	f := robotsFixture(t)
	f.seed(1, "webmaster", authztest.Org())
	f.seed(2, "robotadmin", authztest.Org())  // nothing of iam.user:*
	f.seed(3, "dealeradmin", authztest.Org()) // nothing of iam.serviceaccount:*

	draft := admin.BindingDraft{Role: "seller", Scope: authztest.Dealer(10)}

	t.Run("a manager of service accounts, without iam.user, binds a service account", func(t *testing.T) {
		bound, err := f.Bindings.Bind(as(2), bot, draft)
		require.NoError(t, err)
		assert.Equal(t, "seller", bound.Role.Ref)

		got, err := f.Bindings.Access(as(2), bot)
		require.NoError(t, err)
		assert.Len(t, got.Bindings, 1)
		assert.True(t, got.CanManage)

		scopes, err := f.Bindings.Scopes(as(2), bot)
		require.NoError(t, err)
		assert.NotEmpty(t, scopes.Places)

		roles, err := f.Bindings.BindableRolesFor(as(2), authz.KindServiceAccount, authztest.Dealer(10))
		require.NoError(t, err)
		assert.NotEmpty(t, roles)

		require.NoError(t, f.Bindings.Unbind(as(2), bot, bound.ID))
	})

	t.Run("and cannot touch a person", func(t *testing.T) {
		_, err := f.Bindings.Bind(as(2), user(5), draft)
		require.ErrorIs(t, err, authz.ErrForbidden)
		_, err = f.Bindings.Access(as(2), user(5))
		require.ErrorIs(t, err, authz.ErrForbidden)
		_, err = f.Bindings.Scopes(as(2), user(5))
		require.ErrorIs(t, err, authz.ErrForbidden)

		roles, err := f.Bindings.BindableRoles(as(2), authztest.Dealer(10))
		require.NoError(t, err)
		assert.Empty(t, roles, "nothing a person may be given, by somebody who may give none")
	})

	t.Run("a manager of people, without iam.serviceaccount, binds a person and not a service account", func(t *testing.T) {
		_, err := f.Bindings.Bind(as(3), user(5), draft)
		require.NoError(t, err)

		_, err = f.Bindings.Bind(as(3), bot, draft)
		require.ErrorIs(t, err, authz.ErrForbidden)
		_, err = f.Bindings.Access(as(3), bot)
		require.ErrorIs(t, err, authz.ErrForbidden)
		_, err = f.Bindings.Scopes(as(3), bot)
		require.ErrorIs(t, err, authz.ErrForbidden)

		roles, err := f.Bindings.BindableRolesFor(as(3), authz.KindServiceAccount, authztest.Dealer(10))
		require.NoError(t, err)
		assert.Empty(t, roles)
	})

	t.Run("a subject of no kind is not found", func(t *testing.T) {
		_, err := f.Bindings.Access(as(1), authz.Subject{Kind: "robot", ID: 1})
		assert.True(t, apperr.HasCode(err, apperr.CodeNotFound))
	})
}
