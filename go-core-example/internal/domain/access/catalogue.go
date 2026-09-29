// Package access shows go-core's authz package end to end: a permission
// catalogue, two roles, an organization > dealer > location hierarchy with the
// dealer as tenant, bindings in the database, a lead list filtered by what the
// caller may see, and record-level checks on single leads.
package access

import (
	"github.com/wssto2/go-core/authz"
)

// Permissions of the example.
const (
	LeadView   = "sales.lead:view"
	LeadUpdate = "sales.lead:update"
	UserManage = "iam.user:manage"
	RoleManage = "iam.role:manage"
)

// Levels of the example's hierarchy.
const (
	LevelOrganization = "organization"
	LevelDealer       = "dealer"
	LevelLocation     = "location"
)

// AttrVehicleKind is the role attribute that limits leads to used or new cars.
const AttrVehicleKind = "vehiclekind"

// Keys of the predefined roles.
const (
	RoleSalesperson = "salesperson"
	RoleManager     = "manager"
	RoleWebmaster   = "webmaster"
)

// NewCatalogue declares the permissions. Ownable ones carry a "whose" qualifier
// (Own, OwnLocation, All) in a role; unassigned leads count as everyone's.
func NewCatalogue() (*authz.Catalogue, error) {
	c := authz.NewCatalogue()
	for _, def := range []struct {
		id   string
		opts []authz.DefineOption
	}{
		{LeadView, []authz.DefineOption{authz.Label("perm.sales.lead.view"), authz.Ownable("sales.lead"), authz.UnownedIsOwn(), authz.Attributes(AttrVehicleKind)}},
		{LeadUpdate, []authz.DefineOption{authz.Label("perm.sales.lead.update"), authz.Ownable("sales.lead"), authz.Attributes(AttrVehicleKind), authz.Requires(LeadView)}},
		{UserManage, []authz.DefineOption{authz.Label("perm.iam.user.manage"), authz.Sensitive()}},
		{RoleManage, []authz.DefineOption{authz.Label("perm.iam.role.manage"), authz.Sensitive(), authz.System()}},
	} {
		if err := c.Define(def.id, def.opts...); err != nil {
			return nil, err
		}
	}
	return c, c.Validate()
}

// NewHierarchy declares organization > dealer > location; the dealer is the
// tenant, so a binding below the organization can never reach another dealer.
func NewHierarchy() (*authz.Hierarchy, error) {
	h, err := authz.NewHierarchy(LevelOrganization, LevelDealer, LevelLocation)
	if err != nil {
		return nil, err
	}
	return h.WithTenantLevel(LevelDealer)
}

// Roles returns the predefined roles: a salesperson who sees and edits their
// own leads, a manager who sees and edits every lead in the binding's scope,
// and a computed webmaster that holds everything (and so picks up permissions
// added later).
func Roles() []authz.Role {
	return []authz.Role{
		{
			Key: RoleSalesperson, Name: "Salesperson",
			Grants: authz.Grants(authz.QualifierOwn, LeadView, LeadUpdate),
		},
		{
			Key: RoleManager, Name: "Manager",
			Grants: append(authz.Grants(authz.QualifierAll, LeadView, LeadUpdate), authz.Grants(authz.QualifierAll, UserManage)...),
		},
		authz.ComputedRole(RoleWebmaster, "Webmaster", authz.All()),
	}
}
