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

const (
	manageRoles    = "iam.role:manage"
	manageBindings = "iam.user:manage"
)

func newAdminWorld(t *testing.T) (*authztest.World, *authz.Admin) {
	t.Helper()
	w := newSmallWorld(t)
	return w, w.Admin(manageRoles, manageBindings, manageBindings)
}

// dealerAdmin makes user id the user administrator of dealer 1: it manages
// bindings there and holds no sales permission at all.
func dealerAdmin(w *authztest.World, id int) authz.Principal {
	role := w.SaveRole(authz.Role{Name: "dealer admin", Grants: authz.Grants(authz.QualifierAll, manageBindings)})
	w.BindCustom(user(id), role.ID, authztest.Dealer(1))
	return authz.User(id, 0)
}

func TestSaveRoleDelegation(t *testing.T) {
	w, admin := newAdminWorld(t)
	w.Bind(user(1), "importer", authztest.Org()) // everything but System, manages roles

	// a role for a lead-limited manager: leads only up to OwnLocation, used vehicles only
	limited := w.SaveRole(authz.Role{
		Name: "limited",
		Grants: []authz.Grant{
			{Permission: manageRoles, Qualifier: authz.QualifierAll},
			{Permission: "crm.lead:view", Qualifier: authz.QualifierOwnLocation},
			{Permission: "crm.lead:update", Qualifier: authz.QualifierOwn},
			{Permission: "vehicle.stock:view", Qualifier: authz.QualifierAll},
		},
		Attrs: map[string][]string{"vehiclekind": {"used"}},
	})
	w.BindCustom(user(2), limited.ID, authztest.Dealer(1))

	tests := []struct {
		name    string
		who     authz.Principal
		role    authz.Role
		wantErr error
		problem authz.ProblemCode // set: a validation problem instead of wantErr
	}{
		{"importer creates a subset", authz.User(1, 0), authz.Role{Name: "r", Grants: authz.Grants(authz.QualifierAll, "vehicle.stock:view")}, nil, ""},
		{"importer cannot hand out a system permission", authz.User(1, 0), authz.Role{Name: "r", Grants: authz.Grants(authz.QualifierAll, "system.job:run")}, authz.ErrEscalation, ""},
		{"limited: what it holds", authz.User(2, 0), authz.Role{Name: "r", Grants: []authz.Grant{{Permission: "crm.lead:view", Qualifier: authz.QualifierOwnLocation}}, Attrs: map[string][]string{"vehiclekind": {"used"}}}, nil, ""},
		{"limited: narrower qualifier", authz.User(2, 0), authz.Role{Name: "r", Grants: []authz.Grant{{Permission: "crm.lead:view", Qualifier: authz.QualifierOwn}}, Attrs: map[string][]string{"vehiclekind": {"used"}}}, nil, ""},
		{"limited: wider qualifier", authz.User(2, 0), authz.Role{Name: "r", Grants: []authz.Grant{{Permission: "crm.lead:view", Qualifier: authz.QualifierAll}}, Attrs: map[string][]string{"vehiclekind": {"used"}}}, authz.ErrEscalation, ""},
		{"limited: drops the attribute constraint", authz.User(2, 0), authz.Role{Name: "r", Grants: []authz.Grant{{Permission: "crm.lead:view", Qualifier: authz.QualifierOwn}}}, authz.ErrEscalation, ""},
		{"limited: another attribute value", authz.User(2, 0), authz.Role{Name: "r", Grants: []authz.Grant{{Permission: "crm.lead:view", Qualifier: authz.QualifierOwn}}, Attrs: map[string][]string{"vehiclekind": {"new"}}}, authz.ErrEscalation, ""},
		{"limited: widening the set of allowed values", authz.User(2, 0), authz.Role{Name: "r", Grants: []authz.Grant{{Permission: "crm.lead:view", Qualifier: authz.QualifierOwn}}, Attrs: map[string][]string{"vehiclekind": {"used", "new"}}}, authz.ErrEscalation, ""},
		{"limited: a permission it does not hold", authz.User(2, 0), authz.Role{Name: "r", Grants: authz.Grants(authz.QualifierAll, "crm.offer:view")}, authz.ErrEscalation, ""},
		{"limited: cannot hand out a computed role", authz.User(2, 0), authz.ComputedRole("", "everything", authz.All()), nil, authz.ProblemInvalidRole},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saved, err := admin.SaveRole(w.As(tt.who), tt.role)
			if tt.problem != "" {
				var ve *authz.ValidationError
				require.ErrorAs(t, err, &ve)
				assert.True(t, ve.Has(tt.problem))
				return
			}
			if tt.wantErr == nil {
				require.NoError(t, err)
				assert.NotZero(t, saved.ID)
				stored, err := w.Store.Role(context.Background(), saved.ID)
				require.NoError(t, err)
				assert.Equal(t, tt.role.Grants, stored.Grants)
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.True(t, apperr.HasCode(err, apperr.CodePermissionDenied) || apperr.HasCode(err, apperr.CodeBadRequest))
		})
	}
}

func TestSaveRoleNeedsTheManageRolesPermission(t *testing.T) {
	w, admin := newAdminWorld(t)
	w.Bind(user(1), "manager", authztest.Dealer(1)) // manages users, not roles
	_, err := admin.SaveRole(w.As(authz.User(1, 0)), authz.Role{Name: "r", Grants: authz.Grants(authz.QualifierAll, "crm.lead:assign", "crm.lead:view")})
	assert.ErrorIs(t, err, authz.ErrForbidden)
	_, err = admin.SaveRole(context.Background(), authz.Role{Name: "r"})
	assert.ErrorIs(t, err, authz.ErrNoPrincipal)
}

func TestSaveRoleRejectsInvalidRoles(t *testing.T) {
	w, admin := newAdminWorld(t)
	w.Bind(user(1), "webmaster", authztest.Org())
	ctx := w.As(authz.User(1, 0))
	_, err := admin.SaveRole(ctx, authz.Role{Name: "r", Grants: []authz.Grant{{Permission: "vehicle.stock:view", Qualifier: authz.QualifierOwn}}})
	require.Error(t, err)
	assert.True(t, apperr.HasCode(err, apperr.CodeBadRequest))
	assert.True(t, apperr.HasReason(err, authz.ReasonInvalid))
	var ve *authz.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.True(t, ve.Has(authz.ProblemQualifierNotAllowed))

	_, err = admin.SaveRole(ctx, authz.Role{ID: 4242, Name: "ghost", Grants: authz.Grants(authz.QualifierAll, "vehicle.stock:view")})
	assert.ErrorIs(t, err, authz.ErrRoleNotFound)
	assert.True(t, apperr.HasCode(err, apperr.CodeNotFound))
}

func TestSavingARoleAppliesToHoldersImmediately(t *testing.T) {
	w, admin := newAdminWorld(t)
	w.Bind(user(1), "webmaster", authztest.Org())
	role, err := admin.SaveRole(w.As(authz.User(1, 0)), authz.Role{Name: "reader", Grants: authz.Grants(authz.QualifierAll, "vehicle.stock:view")})
	require.NoError(t, err)
	w.BindCustom(user(2), role.ID, authztest.Dealer(1))
	ctx := w.As(authz.User(2, 0))

	require.NoError(t, w.Engine.Require(ctx, "vehicle.stock:view")) // now cached
	assert.ErrorIs(t, w.Engine.Require(ctx, "crm.offer:view"), authz.ErrForbidden)

	role.Grants = authz.Grants(authz.QualifierAll, "crm.offer:view")
	_, err = admin.SaveRole(w.As(authz.User(1, 0)), role)
	require.NoError(t, err)
	assert.NoError(t, w.Engine.Require(ctx, "crm.offer:view"), "no staleness after a role change")
	assert.ErrorIs(t, w.Engine.Require(ctx, "vehicle.stock:view"), authz.ErrForbidden)
}

func TestBindDelegation(t *testing.T) {
	w, admin := newAdminWorld(t)
	w.Bind(user(1), "importer", authztest.Org())  // everything except System
	w.Bind(user(2), "webmaster", authztest.Org()) // everything
	da := dealerAdmin(w, 9)                       // users at dealer 1, no sales permissions
	// a system-permission holder who also manages bindings, at dealer 1
	sysAdmin := w.SaveRole(authz.Role{Name: "sys admin", Grants: authz.Grants(authz.QualifierAll, manageBindings, "system.job:run")})
	w.BindCustom(user(8), sysAdmin.ID, authztest.Dealer(1))
	// a custom role that contains a System permission, and one that is plain sales
	sysRole := w.SaveRole(authz.Role{Name: "job runner", Grants: authz.Grants(authz.QualifierAll, "system.job:run", "vehicle.stock:view")})
	leadRole := w.SaveRole(authz.Role{Name: "lead reader", Grants: authz.Grants(authz.QualifierAll, "crm.lead:view")})

	bind := func(subject int, role authz.RoleRef, scope authz.Scope) authz.Binding {
		return authz.Binding{Subject: user(subject), Role: role, Scope: scope}
	}
	key := func(k string) authz.RoleRef { return authz.RoleRef{Key: k} }
	tests := []struct {
		name    string
		actor   authz.Principal
		binding authz.Binding
		wantErr error
		problem authz.ProblemCode
	}{
		// a dealer administrator assigns roles it cannot itself use
		{"dealer admin binds a sales role at its dealer", da, bind(50, key("seller"), authztest.Dealer(1)), nil, ""},
		{"dealer admin binds a sales manager role at its dealer", da, bind(51, key("manager"), authztest.Dealer(1)), nil, ""},
		{"dealer admin binds at a location of its dealer", da, bind(52, key("seller"), authztest.Location(10)), nil, ""},
		{"dealer admin binds a service account", da, authz.Binding{Subject: authz.Subject{Kind: authz.KindServiceAccount, ID: 7}, Role: leadRole.Ref(), Scope: authztest.Dealer(1)}, nil, ""},
		{"dealer admin cannot bind at another dealer", da, bind(53, key("seller"), authztest.Dealer(2)), authz.ErrForbidden, ""},
		{"dealer admin cannot bind at another dealer's location", da, bind(54, key("seller"), authztest.Location(20)), authz.ErrForbidden, ""},
		{"dealer admin cannot bind at the organization", da, bind(55, key("seller"), authztest.Org()), authz.ErrForbidden, ""},
		{"dealer admin cannot bind a role with a system permission", da, bind(56, sysRole.Ref(), authztest.Dealer(1)), authz.ErrEscalation, ""},
		{"dealer admin cannot make a webmaster (organization-only below the root)", da, bind(57, key("webmaster"), authztest.Dealer(1)), nil, authz.ProblemOrganizationOnly},
		// Uvoznik: everything except System, at the root
		{"importer binds any non-system role anywhere", authz.User(1, 0), bind(60, key("manager"), authztest.Dealer(2)), nil, ""},
		{"importer binds at the organization", authz.User(1, 0), bind(61, leadRole.Ref(), authztest.Org()), nil, ""},
		{"importer binds an organization-only role at the root", authz.User(1, 0), bind(62, key("reporter"), authztest.Org()), nil, ""},
		{"importer cannot bind an organization-only role below the root", authz.User(1, 0), bind(63, key("reporter"), authztest.Dealer(1)), nil, authz.ProblemOrganizationOnly},
		{"importer cannot make a webmaster", authz.User(1, 0), bind(64, key("webmaster"), authztest.Org()), authz.ErrEscalation, ""},
		{"importer cannot bind a custom role with a system permission", authz.User(1, 0), bind(65, sysRole.Ref(), authztest.Dealer(1)), authz.ErrEscalation, ""},
		// only a System holder binds a System role
		{"webmaster binds a webmaster", authz.User(2, 0), bind(70, key("webmaster"), authztest.Org()), nil, ""},
		{"webmaster binds a system role", authz.User(2, 0), bind(71, sysRole.Ref(), authztest.Dealer(2)), nil, ""},
		{"a system holder binds a system role where it holds it", authz.User(8, 0), bind(72, sysRole.Ref(), authztest.Dealer(1)), nil, ""},
		{"a system holder cannot bind it where it does not manage bindings", authz.User(8, 0), bind(73, sysRole.Ref(), authztest.Dealer(2)), authz.ErrForbidden, ""},
		// nobody assigns themselves
		{"dealer admin cannot bind itself", da, bind(9, key("seller"), authztest.Dealer(1)), authz.ErrSelfAssignment, ""},
		{"importer cannot bind itself", authz.User(1, 0), bind(1, leadRole.Ref(), authztest.Dealer(1)), authz.ErrSelfAssignment, ""},
		{"webmaster cannot bind itself", authz.User(2, 0), bind(2, key("seller"), authztest.Org()), authz.ErrSelfAssignment, ""},
		// malformed requests
		{"unknown scope", authz.User(1, 0), bind(80, leadRole.Ref(), authztest.Dealer(99)), authz.ErrInvalidScope, ""},
		{"malformed scope", authz.User(1, 0), bind(81, leadRole.Ref(), authz.Scope{Level: "dealer"}), authz.ErrInvalidScope, ""},
		{"no such role", authz.User(1, 0), bind(82, authz.RoleRef{ID: 999}, authztest.Dealer(1)), authz.ErrRoleNotFound, ""},
		{"invalid subject", authz.User(1, 0), authz.Binding{Subject: authz.Subject{Kind: "robot", ID: 1}, Role: leadRole.Ref(), Scope: authztest.Dealer(1)}, authz.ErrInvalidScope, ""},
	}
	badRequest := map[string]bool{"unknown scope": true, "malformed scope": true, "invalid subject": true}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := admin.Bind(w.As(tt.actor), tt.binding)
			switch {
			case tt.problem != "":
				var ve *authz.ValidationError
				require.ErrorAs(t, err, &ve)
				assert.True(t, ve.Has(tt.problem))
			case tt.wantErr == nil:
				require.NoError(t, err)
				assert.NotZero(t, b.ID)
				assert.Equal(t, tt.actor.ID, b.CreatedBy)
			case badRequest[tt.name]:
				require.Error(t, err)
				assert.True(t, apperr.HasCode(err, apperr.CodeBadRequest), err)
			default:
				assert.ErrorIs(t, err, tt.wantErr)
				if tt.wantErr == authz.ErrSelfAssignment {
					assert.True(t, apperr.HasReason(err, authz.ReasonSelfAssignment))
				}
			}
		})
	}
}

func TestBindingAppliesAtOnce(t *testing.T) {
	w, admin := newAdminWorld(t)
	w.Bind(user(1), "webmaster", authztest.Org())
	root := w.As(authz.User(1, 0))
	target := w.As(authz.User(7, 0))

	require.ErrorIs(t, w.Engine.Require(target, "vehicle.stock:view"), authz.ErrForbidden) // cached as "nothing"
	b, err := admin.Bind(root, authz.Binding{Subject: user(7), Role: authz.RoleRef{Key: "seller"}, Scope: authztest.Dealer(1)})
	require.NoError(t, err)
	assert.NoError(t, w.Engine.Require(target, "vehicle.stock:view"))

	_, err = admin.Bind(root, authz.Binding{Subject: user(7), Role: authz.RoleRef{Key: "seller"}, Scope: authztest.Dealer(1)})
	assert.ErrorIs(t, err, authz.ErrDuplicateBinding)
	assert.True(t, apperr.HasCode(err, apperr.CodeAlreadyExists))

	require.NoError(t, admin.Unbind(root, b.ID))
	assert.ErrorIs(t, w.Engine.Require(target, "vehicle.stock:view"), authz.ErrForbidden)
	assert.ErrorIs(t, admin.Unbind(root, b.ID), authz.ErrBindingNotFound)
}

func TestUnbindNeedsTheSameRightsAsBind(t *testing.T) {
	w, admin := newAdminWorld(t)
	w.Bind(user(1), "webmaster", authztest.Org())
	w.Bind(user(3), "importer", authztest.Org())
	da := dealerAdmin(w, 9)

	seller := w.Bind(user(7), "seller", authztest.Dealer(1))
	manager := w.Bind(user(6), "manager", authztest.Dealer(1))
	other := w.Bind(user(8), "seller", authztest.Dealer(2))
	system := w.Bind(user(5), "webmaster", authztest.Org())
	sysRole := w.SaveRole(authz.Role{Name: "job runner", Grants: authz.Grants(authz.QualifierAll, "system.job:run")})
	sysBinding := w.BindCustom(user(4), sysRole.ID, authztest.Dealer(1))

	// the dealer admin removes roles it cannot itself use, at its own dealer
	assert.NoError(t, admin.Unbind(w.As(da), seller.ID))
	assert.NoError(t, admin.Unbind(w.As(da), manager.ID))
	// but not another dealer's, and not one carrying a system permission
	assert.ErrorIs(t, admin.Unbind(w.As(da), other.ID), authz.ErrForbidden)
	assert.ErrorIs(t, admin.Unbind(w.As(da), sysBinding.ID), authz.ErrEscalation)
	// Uvoznik cannot take away a webmaster, the webmaster can
	assert.ErrorIs(t, admin.Unbind(w.As(authz.User(3, 0)), system.ID), authz.ErrEscalation)
	assert.NoError(t, admin.Unbind(w.As(authz.User(1, 0)), system.ID))
	assert.NoError(t, admin.Unbind(w.As(authz.User(3, 0)), other.ID))
}

func TestRemovingYourOwnBindingIsAllowedUnlessItIsYourLastAdminAccess(t *testing.T) {
	w := newSmallWorld(t)
	admin := w.Admin(manageRoles, manageBindings, manageBindings)
	da := dealerAdmin(w, 9)
	own := w.Bind(da.Subject, "seller", authztest.Dealer(1))
	assert.NoError(t, admin.Unbind(w.As(da), own.ID), "a binding that is not the last admin access")

	bindings, err := w.Store.BindingsFor(context.Background(), da.Subject)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	assert.ErrorIs(t, admin.Unbind(w.As(da), bindings[0].ID), authz.ErrLastAdmin)
}

func TestLastAdminLockOut(t *testing.T) {
	t.Run("removing your own last binding that grants the protected permission", func(t *testing.T) {
		w, admin := newAdminWorld(t)
		b := w.Bind(user(1), "manager", authztest.Dealer(1)) // grants iam.user:manage
		err := admin.Unbind(w.As(authz.User(1, 0)), b.ID)
		assert.ErrorIs(t, err, authz.ErrLastAdmin)
		assert.True(t, apperr.HasReason(err, authz.ReasonLastAdmin))
		// still bound
		assert.NoError(t, w.Engine.Require(w.As(authz.User(1, 0)), manageBindings))
	})
	t.Run("allowed while another binding still grants it", func(t *testing.T) {
		w, admin := newAdminWorld(t)
		first := w.Bind(user(1), "manager", authztest.Dealer(1))
		w.Bind(user(1), "webmaster", authztest.Org())
		assert.NoError(t, admin.Unbind(w.As(authz.User(1, 0)), first.ID))
	})
	t.Run("removing a binding that does not grant it is fine", func(t *testing.T) {
		w, admin := newAdminWorld(t)
		w.Bind(user(1), "webmaster", authztest.Org())
		seller := w.Bind(user(1), "seller", authztest.Dealer(1))
		assert.NoError(t, admin.Unbind(w.As(authz.User(1, 0)), seller.ID))
	})
	t.Run("removing someone else's last admin binding is not the actor's lock-out", func(t *testing.T) {
		w, admin := newAdminWorld(t)
		w.Bind(user(1), "webmaster", authztest.Org())
		theirs := w.Bind(user(2), "manager", authztest.Dealer(1))
		assert.NoError(t, admin.Unbind(w.As(authz.User(1, 0)), theirs.ID))
	})
	t.Run("editing your only role so it no longer grants it", func(t *testing.T) {
		w, admin := newAdminWorld(t)
		role := w.SaveRole(authz.Role{Name: "admins", Grants: authz.Grants(authz.QualifierAll, manageRoles, manageBindings, "vehicle.stock:view")})
		w.BindCustom(user(1), role.ID, authztest.Org())
		ctx := w.As(authz.User(1, 0))

		role.Grants = authz.Grants(authz.QualifierAll, manageRoles, "vehicle.stock:view") // drops iam.user:manage
		_, err := admin.SaveRole(ctx, role)
		assert.ErrorIs(t, err, authz.ErrLastAdmin)

		role.Grants = authz.Grants(authz.QualifierAll, manageRoles, manageBindings) // keeps it
		_, err = admin.SaveRole(ctx, role)
		assert.NoError(t, err)
	})
}

func TestDeleteRole(t *testing.T) {
	w, admin := newAdminWorld(t)
	w.Bind(user(1), "webmaster", authztest.Org())
	ctx := w.As(authz.User(1, 0))
	role := w.SaveRole(authz.Role{Name: "temp", Grants: authz.Grants(authz.QualifierAll, "vehicle.stock:view")})
	b := w.BindCustom(user(2), role.ID, authztest.Dealer(1))

	err := admin.DeleteRole(ctx, role.ID)
	assert.ErrorIs(t, err, authz.ErrRoleInUse)
	assert.True(t, apperr.HasCode(err, apperr.CodeAlreadyExists))

	require.NoError(t, admin.Unbind(ctx, b.ID))
	assert.NoError(t, admin.DeleteRole(ctx, role.ID))
	assert.ErrorIs(t, admin.DeleteRole(ctx, role.ID), authz.ErrRoleNotFound)

	// a dealer administrator does not build or delete roles
	w.Bind(user(3), "manager", authztest.Dealer(1))
	assert.ErrorIs(t, admin.DeleteRole(w.As(authz.User(3, 0)), 1), authz.ErrForbidden)
}

func TestNewAdminValidatesPermissions(t *testing.T) {
	w := newSmallWorld(t)
	_, err := authz.NewAdmin(authz.AdminConfig{Engine: w.Engine, Store: w.Store, ManageRoles: "nope:view", ManageBindings: manageBindings})
	assert.ErrorIs(t, err, authz.ErrUnknownPermission)
	_, err = authz.NewAdmin(authz.AdminConfig{Engine: w.Engine, Store: w.Store, ManageRoles: manageRoles, ManageBindings: manageBindings, Protected: []string{"nope:view"}})
	assert.ErrorIs(t, err, authz.ErrUnknownPermission)
	_, err = authz.NewAdmin(authz.AdminConfig{ManageRoles: manageRoles, ManageBindings: manageBindings})
	assert.Error(t, err)
}
