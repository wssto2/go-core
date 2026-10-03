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

func refs(views []admin.RoleView) []string {
	out := make([]string, len(views))
	for i, v := range views {
		out[i] = v.Ref
	}

	return out
}

func TestRolesAreListedPredefinedFirstThenCustomByName(t *testing.T) {
	f := newFixture(t)
	f.seed(1, "webmaster", authztest.Org())
	f.seed(2, "seller", authztest.Dealer(10))

	zeta, err := f.Roles.Create(as(1), admin.RoleDraft{Name: "Zeta", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")})
	require.NoError(t, err)
	alpha, err := f.Roles.Create(as(1), admin.RoleDraft{Name: "Alpha", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")})
	require.NoError(t, err)
	f.seedCustom(3, alpha.ID, authztest.Dealer(10))
	f.seedCustom(3, alpha.ID, authztest.Dealer(20))

	list, err := f.Roles.List(as(1))
	require.NoError(t, err)
	assert.Equal(t, []string{"webmaster", "dealeradmin", "distribution", "roleadmin", "salesmanager", "seller", alpha.Ref, zeta.Ref}, refs(list),
		"computed first, then predefined by key, then custom by name")

	byRef := map[string]admin.RoleView{}
	for _, v := range list {
		byRef[v.Ref] = v
	}

	assert.Equal(t, 1, byRef["webmaster"].Holders)
	assert.Equal(t, 1, byRef["seller"].Holders)
	assert.Equal(t, 1, byRef[alpha.Ref].Holders, "one subject at two places counts once")
	assert.Equal(t, 0, byRef[zeta.Ref].Holders)
	assert.True(t, byRef["webmaster"].Computed && byRef["webmaster"].Predefined)
	assert.Equal(t, 9, byRef["webmaster"].PermissionCount, "a computed role counts what the catalogue gives it")
	assert.Empty(t, byRef["seller"].Grants, "lists carry no grants")
}

func TestReadingRolesNeedsTheViewPermission(t *testing.T) {
	f := newFixture(t)
	f.seed(2, "seller", authztest.Dealer(10))

	_, err := f.Roles.List(as(2))
	require.ErrorIs(t, err, authz.ErrForbidden)
	_, err = f.Roles.Show(as(2), "seller")
	require.ErrorIs(t, err, authz.ErrForbidden)
	_, err = f.Roles.List(context.Background())
	require.ErrorIs(t, err, authz.ErrNoPrincipal)
}

func TestShowReturnsGrantsAndNotFound(t *testing.T) {
	f := newFixture(t)
	f.seed(1, "webmaster", authztest.Org())

	seller, err := f.Roles.Show(as(1), "seller")
	require.NoError(t, err)
	assert.Equal(t, []authz.Grant{
		{Permission: "crm.customer:view", Qualifier: authz.QualifierAll},
		{Permission: "crm.lead:view", Qualifier: authz.QualifierOwn},
	}, seller.Grants, "sorted by permission")

	webmaster, err := f.Roles.Show(as(1), "webmaster")
	require.NoError(t, err)
	assert.Len(t, webmaster.Grants, 9)

	_, err = f.Roles.Show(as(1), "nonesuch")
	assert.True(t, apperr.HasCode(err, apperr.CodeNotFound))
	_, err = f.Roles.Show(as(1), "999")
	assert.True(t, apperr.HasCode(err, apperr.CodeNotFound))
}

func TestHoldersAreNamedAndPlaced(t *testing.T) {
	f := newFixture(t)
	f.seed(1, "webmaster", authztest.Org())
	f.seed(3, "seller", authztest.Location(101))
	f.seed(2, "seller", authztest.Dealer(20))
	f.seed(9, "seller", authztest.Org())

	got, err := f.Roles.Holders(as(1), "seller")
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, admin.Holder{Subject: user(9), Name: "Ivo", Place: admin.Place{Scope: authztest.Org()}}, got[2], "the root has no name")
	assert.Equal(t, "Boris", got[0].Name, "by name")
	assert.Equal(t, admin.Place{Scope: authztest.Dealer(20), Name: "South"}, got[0].Place)
	assert.Equal(t, admin.Place{Scope: authztest.Location(101), Name: "North main street"}, got[1].Place)
}

func TestCustomRolesAreBuiltChangedAndDeleted(t *testing.T) {
	f := newFixture(t)
	f.seed(1, "webmaster", authztest.Org())
	f.seed(4, "roleadmin", authztest.Org())

	created, err := f.Roles.Create(as(1), admin.RoleDraft{
		Name: "Clerk", Description: "reads", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view"),
		Attrs: nil,
	})
	require.NoError(t, err)
	assert.False(t, created.Predefined)
	assert.Equal(t, 1, created.PermissionCount)

	updated, err := f.Roles.Update(as(1), created.Ref, admin.RoleDraft{Name: "Clerk 2", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view", "crm.lead:view")})
	require.NoError(t, err)
	assert.Equal(t, created.Ref, updated.Ref)
	assert.Equal(t, 2, updated.PermissionCount)

	t.Run("an invalid role names every problem", func(t *testing.T) {
		_, err := f.Roles.Create(as(1), admin.RoleDraft{Name: "", Grants: authz.Grants(authz.QualifierAll, "no.such:perm")})
		require.Error(t, err)
		var problems *authz.ValidationError
		require.ErrorAs(t, err, &problems)
		assert.True(t, problems.Has(authz.ProblemUnknownPermission) && problems.Has(authz.ProblemInvalidRole))
	})

	t.Run("a predefined role is read-only", func(t *testing.T) {
		_, err := f.Roles.Update(as(1), "seller", admin.RoleDraft{Name: "x"})
		assert.True(t, apperr.HasCode(err, apperr.CodePermissionDenied))
		assert.True(t, apperr.HasReason(err, authz.ReasonForbidden))
		assert.True(t, apperr.HasCode(f.Roles.Delete(as(1), "seller"), apperr.CodePermissionDenied))
	})

	t.Run("a role somebody holds is not deleted", func(t *testing.T) {
		f.seedCustom(5, created.ID, authztest.Dealer(10))
		err := f.Roles.Delete(as(4), created.Ref)
		require.ErrorIs(t, err, authz.ErrRoleInUse)
		assert.True(t, apperr.HasReason(err, authz.ReasonRoleInUse))
	})

	t.Run("deleting is its own permission", func(t *testing.T) {
		spare, err := f.Roles.Create(as(1), admin.RoleDraft{Name: "Spare", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")})
		require.NoError(t, err)

		manager, err := f.Roles.Create(as(1), admin.RoleDraft{Name: "Manager", Grants: authz.Grants(authz.QualifierAll, "iam.role:view", "iam.role:manage")})
		require.NoError(t, err)
		f.seedCustom(6, manager.ID, authztest.Org())

		require.ErrorIs(t, f.Roles.Delete(as(6), spare.Ref), authz.ErrForbidden, "managing roles is not deleting them")
		require.NoError(t, f.Roles.Delete(as(4), spare.Ref))

		_, err = f.Roles.Show(as(1), spare.Ref)
		assert.True(t, apperr.HasCode(err, apperr.CodeNotFound))
	})
}

func TestCompareAgainstAPredefinedRole(t *testing.T) {
	f := newFixture(t)
	f.seed(1, "webmaster", authztest.Org())

	custom, err := f.Roles.Create(as(1), admin.RoleDraft{Name: "Mixed", Grants: []authz.Grant{
		{Permission: "crm.lead:view", Qualifier: authz.QualifierAll}, // seller has Own
		{Permission: "crm.customer:view", Qualifier: authz.QualifierAll},
		{Permission: "report.group:view", Qualifier: authz.QualifierAll}, // not in seller
	}})
	require.NoError(t, err)

	got, err := f.Roles.Compare(as(1), custom.Ref, "seller")
	require.NoError(t, err)
	assert.Equal(t, []authz.Grant{{Permission: "report.group:view", Qualifier: authz.QualifierAll}}, got.OnlyInRole)
	assert.Empty(t, got.OnlyInOther)
	assert.Equal(t, []admin.Difference{{Permission: "crm.lead:view", Role: authz.QualifierAll, Other: authz.QualifierOwn}}, got.Different)

	_, err = f.Roles.Compare(as(1), "seller", "seller")
	assert.True(t, apperr.HasCode(err, apperr.CodePermissionDenied), "only a custom role is compared")
	_, err = f.Roles.Compare(as(1), custom.Ref, "nonesuch")
	assert.True(t, apperr.HasCode(err, apperr.CodeNotFound))
}

func TestReplaceRebindsEveryHolderOrNone(t *testing.T) {
	onEveryDatabase(t, func(t *testing.T, f *fixture) {
		f.seed(1, "webmaster", authztest.Org())

		clerk, err := f.Roles.Create(as(1), admin.RoleDraft{Name: "Clerk", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")})
		require.NoError(t, err)
		f.seedCustom(5, clerk.ID, authztest.Dealer(10))
		f.seedCustom(6, clerk.ID, authztest.Dealer(20))
		f.seedCustom(6, clerk.ID, authztest.Location(201))

		t.Run("a holder the actor may not give the role to refuses the whole replacement", func(t *testing.T) {
			// a dealer administrator who may also manage roles, at dealer 10 only
			both, err := f.Roles.Create(as(1), admin.RoleDraft{Name: "Both", Grants: authz.Grants(authz.QualifierAll, "iam.role:view", "iam.role:manage", "iam.user:view", "iam.user:manage")})
			require.NoError(t, err)
			f.seedCustom(7, both.ID, authztest.Dealer(10))
			// binding permission is held at dealer 10 only, role management anywhere it is held: here dealer 10
			_, err = f.Roles.Replace(as(7), clerk.Ref, "salesmanager")
			require.Error(t, err, "dealer 20 is out of reach")

			holders, err := f.Roles.Holders(as(1), clerk.Ref)
			require.NoError(t, err)
			assert.Len(t, holders, 3, "nothing was re-bound")
			bound, err := f.Store.BindingsForRole(context.Background(), clerk.ID)
			require.NoError(t, err)
			assert.Len(t, bound, 3, "the transaction rolled back every binding")
		})

		t.Run("the webmaster replaces it everywhere", func(t *testing.T) {
			n, err := f.Roles.Replace(as(1), clerk.Ref, "salesmanager")
			require.NoError(t, err)
			assert.Equal(t, 3, n)

			bound, err := f.Store.BindingsForRole(context.Background(), clerk.ID)
			require.NoError(t, err)
			assert.Empty(t, bound)

			got, err := f.Roles.Holders(as(1), "salesmanager")
			require.NoError(t, err)
			assert.Len(t, got, 3)

			require.NoError(t, f.Roles.Delete(as(1), clerk.Ref), "unbound, it can now be deleted")
		})

		t.Run("only a predefined role takes its place", func(t *testing.T) {
			_, err := f.Roles.Replace(as(1), "seller", "salesmanager")
			assert.True(t, apperr.HasCode(err, apperr.CodePermissionDenied))
		})
	})
}

func TestAccessExplainsWhatASubjectMayDo(t *testing.T) {
	f := newFixture(t)
	f.seed(1, "webmaster", authztest.Org())
	f.seed(2, "dealeradmin", authztest.Dealer(10))
	f.seed(5, "seller", authztest.Dealer(10))
	f.seed(5, "salesmanager", authztest.Location(201))

	got, err := f.Bindings.Access(as(2), user(5))
	require.NoError(t, err)

	assert.Equal(t, user(5), got.Subject)
	assert.True(t, got.CanManage)
	require.Len(t, got.Bindings, 2)
	assert.Equal(t, "seller", got.Bindings[0].Role.Ref)
	assert.Equal(t, admin.Place{Scope: authztest.Dealer(10), Name: "North"}, got.Bindings[0].Place)
	assert.Equal(t, 2, got.Bindings[0].Role.PermissionCount)

	perms := map[string]admin.Permission{}
	for _, p := range got.Effective {
		perms[p.Permission] = p
	}

	require.Len(t, perms["crm.customer:view"].Grants, 2, "held through both roles, each at its place")
	g := perms["crm.lead:view"].Grants
	require.Len(t, g, 2)
	assert.Equal(t, "seller", g[0].RoleKey)
	assert.Equal(t, authz.QualifierOwn, g[0].Qualifier)
	assert.Equal(t, got.Bindings[0].ID, g[0].BindingID)
	assert.Equal(t, "Sales manager", g[1].RoleName)
	assert.Equal(t, admin.Place{Scope: authztest.Location(201), Name: "South harbour"}, g[1].Place)

	t.Run("effective access equals what the engine says", func(t *testing.T) {
		eff, err := f.Engine.Effective(context.Background(), user(5))
		require.NoError(t, err)
		assert.Equal(t, eff.Permissions(), func() []string {
			ids := make([]string, len(got.Effective))
			for i, p := range got.Effective {
				ids[i] = p.Permission
			}
			return ids
		}())
		for _, p := range got.Effective {
			assert.Len(t, p.Grants, len(eff.Clauses(p.Permission)), p.Permission)
		}
	})

	t.Run("you cannot manage yourself", func(t *testing.T) {
		own, err := f.Bindings.Access(as(2), user(2))
		require.NoError(t, err)
		assert.False(t, own.CanManage)
	})

	t.Run("it needs the view permission and a subject that exists", func(t *testing.T) {
		_, err := f.Bindings.Access(as(5), user(2))
		require.ErrorIs(t, err, authz.ErrForbidden)
		_, err = f.Bindings.Access(as(1), user(42))
		assert.True(t, apperr.HasCode(err, apperr.CodeNotFound))
	})

	t.Run("service accounts are subjects too", func(t *testing.T) {
		bot := authz.Subject{Kind: authz.KindServiceAccount, ID: 1}
		_, err := f.Bindings.Bind(as(1), bot, admin.BindingDraft{Role: "seller", Scope: authztest.Dealer(10)})
		require.NoError(t, err)
		got, err := f.Bindings.Access(as(1), bot)
		require.NoError(t, err)
		assert.Len(t, got.Bindings, 1)
	})
}

func TestBindAndUnbind(t *testing.T) {
	f := newFixture(t)
	f.seed(1, "webmaster", authztest.Org())
	f.seed(2, "dealeradmin", authztest.Dealer(10))

	bound, err := f.Bindings.Bind(as(2), user(5), admin.BindingDraft{Role: "seller", Scope: authztest.Location(101)})
	require.NoError(t, err)
	assert.Equal(t, "seller", bound.Role.Ref)
	assert.Equal(t, 2, bound.CreatedBy)
	assert.Equal(t, "Boris", bound.CreatedByName)
	assert.Equal(t, "North main street", bound.Place.Name)

	_, err = f.Bindings.Bind(as(2), user(5), admin.BindingDraft{Role: "seller", Scope: authztest.Location(101)})
	require.ErrorIs(t, err, authz.ErrDuplicateBinding)

	t.Run("bad requests", func(t *testing.T) {
		for name, draft := range map[string]admin.BindingDraft{
			"no role":                   {Scope: authztest.Dealer(10)},
			"zero role id":              {Role: "0", Scope: authztest.Dealer(10)},
			"unknown level":             {Role: "seller", Scope: authz.Scope{Level: "planet", ID: 1}},
			"a place without an ID":     {Role: "seller", Scope: authz.Scope{Level: "dealer"}},
			"a place that is not there": {Role: "seller", Scope: authztest.Dealer(99)},
		} {
			_, err := f.Bindings.Bind(as(1), user(5), draft)
			assert.True(t, apperr.HasCode(err, apperr.CodeBadRequest), name)
		}
		_, err := f.Bindings.Bind(as(1), user(5), admin.BindingDraft{Role: "nonesuch", Scope: authztest.Org()})
		assert.True(t, apperr.HasCode(err, apperr.CodeNotFound), "an unknown role")
		_, err = f.Bindings.Bind(as(1), user(42), admin.BindingDraft{Role: "seller", Scope: authztest.Org()})
		assert.True(t, apperr.HasCode(err, apperr.CodeNotFound), "an unknown subject")
	})

	t.Run("a binding of somebody else is not found", func(t *testing.T) {
		err := f.Bindings.Unbind(as(2), user(6), bound.ID)
		assert.True(t, apperr.HasCode(err, apperr.CodeNotFound))
	})

	t.Run("unbinding needs the same rights", func(t *testing.T) {
		north := f.seed(6, "seller", authztest.Dealer(20))
		err := f.Bindings.Unbind(as(2), user(6), north.ID)
		require.ErrorIs(t, err, authz.ErrForbidden, "dealer 20 is out of reach")

		require.NoError(t, f.Bindings.Unbind(as(2), user(5), bound.ID))
		err = f.Bindings.Unbind(as(2), user(5), bound.ID)
		assert.True(t, apperr.HasCode(err, apperr.CodeNotFound), "already gone")
	})
}

func TestScopesAreWhereTheActorMayAssign(t *testing.T) {
	f := newFixture(t)
	f.seed(1, "webmaster", authztest.Org())
	f.seed(2, "dealeradmin", authztest.Dealer(10))
	f.seed(3, "dealeradmin", authztest.Location(201))
	f.seed(4, "seller", authztest.Dealer(10))

	scopesOf := func(id int) admin.ScopeOptions {
		got, err := f.Bindings.Scopes(as(id), user(5))
		require.NoError(t, err)

		return got
	}

	root := scopesOf(1)
	assert.True(t, root.Root)
	assert.Len(t, root.Places, 4)
	assert.Equal(t, authztest.Dealer(10), root.Places[0].Scope, "parents before children")

	north := scopesOf(2)
	assert.False(t, north.Root)
	assert.Equal(t, []authz.Scope{authztest.Dealer(10), authztest.Location(101)}, []authz.Scope{north.Places[0].Scope, north.Places[1].Scope})
	assert.Len(t, north.Places, 2)

	harbour := scopesOf(3)
	assert.Equal(t, []admin.ScopeOption{{Scope: authztest.Location(201), Name: "South harbour", Parent: authztest.Dealer(20)}}, harbour.Places)

	_, err := f.Bindings.Scopes(as(4), user(5))
	require.ErrorIs(t, err, authz.ErrForbidden)
}

func TestBindableRolesAreTheOnesDelegationAccepts(t *testing.T) {
	f := newFixture(t)
	f.seed(1, "webmaster", authztest.Org())
	f.seed(2, "dealeradmin", authztest.Dealer(10))

	system, err := f.Roles.Create(as(1), admin.RoleDraft{Name: "System", Grants: authz.Grants(authz.QualifierAll, "system.job:run")})
	require.NoError(t, err)
	plain, err := f.Roles.Create(as(1), admin.RoleDraft{Name: "Plain", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")})
	require.NoError(t, err)

	atDealer, err := f.Bindings.BindableRoles(as(2), authztest.Dealer(10))
	require.NoError(t, err)
	assert.Equal(t, []string{"dealeradmin", "roleadmin", "salesmanager", "seller", plain.Ref}, refs(atDealer),
		"no organization-only, no computed webmaster, no system role below the root")
	assert.NotEmpty(t, atDealer[0].Grants, "the sheet shows what each role grants")

	atOrg, err := f.Bindings.BindableRoles(as(1), authztest.Org())
	require.NoError(t, err)
	assert.Contains(t, refs(atOrg), "webmaster")
	assert.Contains(t, refs(atOrg), "distribution")
	assert.Contains(t, refs(atOrg), system.Ref)

	nowhere, err := f.Bindings.BindableRoles(as(2), authztest.Dealer(20))
	require.NoError(t, err)
	assert.Empty(t, nowhere, "dealer 20 is out of reach")

	_, err = f.Bindings.BindableRoles(as(1), authztest.Dealer(99))
	assert.True(t, apperr.HasCode(err, apperr.CodeNotFound))
	_, err = f.Bindings.BindableRoles(as(1), authz.Scope{Level: "dealer"})
	assert.True(t, apperr.HasCode(err, apperr.CodeBadRequest))
}

func TestMineListsTheSignedInSubjectsOwnBindings(t *testing.T) {
	f := newFixture(t)
	f.seed(5, "seller", authztest.Dealer(10))
	f.seed(5, "salesmanager", authztest.Org())

	got, err := f.Bindings.Mine(as(5))
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "seller", got[0].Role.Ref)
	assert.Equal(t, "North", got[0].Place.Name)
	assert.Zero(t, got[0].Role.Holders, "who else holds it is for the editors")

	_, err = f.Bindings.Mine(context.Background())
	require.ErrorIs(t, err, authz.ErrNoPrincipal)
}

func TestAServiceNeedsEveryPieceAndTheFix(t *testing.T) {
	f := newFixture(t)

	for _, tt := range []struct {
		missing string
		cfg     admin.Config
	}{
		{"engine", admin.Config{}},
		{"store", admin.Config{Engine: f.Engine}},
		{"ScopeCatalog", admin.Config{Engine: f.Engine, Store: f.Store}},
		{"SubjectDirectory", admin.Config{Engine: f.Engine, Store: f.Store, Scopes: admin.NoScopes()}},
		{"Transactor", admin.Config{Engine: f.Engine, Store: f.Store, Scopes: admin.NoScopes(), Subjects: newDirectory()}},
		{"iam.role:viewer", admin.Config{Engine: f.Engine, Store: f.Store, Scopes: admin.NoScopes(), Subjects: newDirectory(), Transactor: noTx{},
			Permissions: admin.Permissions{ViewRoles: "iam.role:viewer"}}},
	} {
		_, _, err := admin.New(tt.cfg)
		require.Error(t, err, tt.missing)
		assert.Contains(t, err.Error(), tt.missing, "the message names the missing piece")
	}
}

type noTx struct{}

func (noTx) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
