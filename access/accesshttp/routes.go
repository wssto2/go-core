// Package accesshttp is the HTTP side of role and binding administration: the
// typed routes, their inputs and response types, and the handlers over the
// admin services. The routes are declared as values (Declare) so the TypeScript
// contract is generated from them without starting anything, and bound to the
// services with To.
package accesshttp

import (
	"context"
	"strings"

	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authzhttp"
	"github.com/wssto2/go-core/route"
)

// RoleInput addresses a role: the key of a predefined role, the ID of a custom
// one.
type RoleInput struct {
	Ref string `path:"ref" validation:"required|max:64"`
}

// CompareInput names the custom role and the predefined one to compare it with.
type CompareInput struct {
	Ref  string `path:"ref" validation:"required|max:64"`
	With string `query:"with" validation:"required|max:64"`
}

// GrantInput is one permission of a custom role. The qualifier only means
// something for an ownable permission; every other permission is all.
type GrantInput struct {
	Permission string `json:"permission" validation:"required|max:100"`
	Qualifier  string `json:"qualifier" validation:"required|max:16"`
}

// CreateRoleInput builds a custom role.
type CreateRoleInput struct {
	Name        string       `json:"name" validation:"required|max:100"`
	Description string       `json:"description" validation:"max:255"`
	Grants      []GrantInput `json:"grants"`
	Attrs       []Constraint `json:"attrs"`
}

// UpdateRoleInput replaces a custom role.
type UpdateRoleInput struct {
	Ref         string       `path:"ref" validation:"required|max:64"`
	Name        string       `json:"name" validation:"required|max:100"`
	Description string       `json:"description" validation:"max:255"`
	Grants      []GrantInput `json:"grants"`
	Attrs       []Constraint `json:"attrs"`
}

// ReplaceRoleInput names the predefined role that takes the place of a custom
// one for every holder.
type ReplaceRoleInput struct {
	Ref  string `path:"ref" validation:"required|max:64"`
	With string `json:"with" validation:"required|max:64"`
}

// SubjectInput addresses a person.
type SubjectInput struct {
	ID int `path:"id"`
}

// BindInput gives a person a role at a place: RoleRef is a role's Ref, Level the
// hierarchy level, ScopeID the place's ID (absent for the root).
type BindInput struct {
	ID      int    `path:"id"`
	RoleRef string `json:"role_ref" validation:"required|max:64"`
	Level   string `json:"level" validation:"required|max:32"`
	ScopeID *int   `json:"scope_id"`
}

// UnbindInput addresses one binding of a person.
type UnbindInput struct {
	ID        int `path:"id"`
	BindingID int `path:"binding_id"`
}

// BindableInput names the place the roles are wanted for.
type BindableInput struct {
	Level   string `query:"level" validation:"required|max:32"`
	ScopeID int    `query:"scope_id"`
}

// Routes is the declared HTTP contract of the access module. Declare builds it;
// Contract is what the TypeScript generator reads.
type Routes struct {
	ListRoles     route.Route[route.None, RoleList]
	ShowRole      route.Route[RoleInput, Role]
	RoleHolders   route.Route[RoleInput, RoleHolders]
	CompareRole   route.Route[CompareInput, RoleComparison]
	CreateRole    route.Route[CreateRoleInput, Role]
	UpdateRole    route.Route[UpdateRoleInput, Role]
	DeleteRole    route.Route[RoleInput, route.Empty]
	ReplaceRole   route.Route[ReplaceRoleInput, Replaced]
	BindableRoles route.Route[BindableInput, BindableRoles]
	SubjectAccess route.Route[SubjectInput, SubjectAccess]
	SubjectScopes route.Route[SubjectInput, ScopeOptions]
	Bind          route.Route[BindInput, Binding]
	Unbind        route.Route[UnbindInput, route.Empty]
	MyAccess      route.RawRoute

	group *route.Contract
}

// base is the version the routes are declared under. A v2 is new declarations
// next to these, in a group of their own.
const base = "/v1"

// Declare declares the routes, guarded by the module's fixed permissions. The
// application's prefix (gocore.WithPrefix) goes in front of the declared paths.
func Declare() *Routes {
	roles, users := base+"/iam/roles", base+"/iam/users/:id"

	r := &Routes{
		ListRoles:     route.Get[route.None, RoleList](roles).Name("access.roles.list").Requires(admin.ViewRoles),
		ShowRole:      route.Get[RoleInput, Role](roles + "/:ref").Name("access.roles.show").Requires(admin.ViewRoles),
		RoleHolders:   route.Get[RoleInput, RoleHolders](roles + "/:ref/holders").Name("access.roles.holders").Requires(admin.ViewRoles),
		CompareRole:   route.Get[CompareInput, RoleComparison](roles + "/:ref/compare").Name("access.roles.compare").Requires(admin.ViewRoles),
		CreateRole:    route.Post[CreateRoleInput, Role](roles).Name("access.roles.create").Requires(admin.ManageRoles),
		UpdateRole:    route.Put[UpdateRoleInput, Role](roles + "/:ref").Name("access.roles.update").Requires(admin.ManageRoles),
		DeleteRole:    route.Delete[RoleInput, route.Empty](roles + "/:ref").Name("access.roles.delete").Requires(admin.DeleteRoles),
		ReplaceRole:   route.Post[ReplaceRoleInput, Replaced](roles + "/:ref/replace").Name("access.roles.replace").Requires(admin.ManageRoles),
		BindableRoles: route.Get[BindableInput, BindableRoles](base + "/iam/bindable-roles").Name("access.bindable"),
		SubjectAccess: route.Get[SubjectInput, SubjectAccess](users + "/access").Name("access.subjects.access").Requires(admin.ViewAccess),
		SubjectScopes: route.Get[SubjectInput, ScopeOptions](users + "/scopes").Name("access.subjects.scopes").Requires(admin.ManageBindings),
		Bind:          route.Post[BindInput, Binding](users + "/bindings").Name("access.subjects.bind").Requires(admin.ManageBindings),
		Unbind:        route.Delete[UnbindInput, route.Empty](users + "/bindings/:binding_id").Name("access.subjects.unbind").Requires(admin.ManageBindings),
		MyAccess:      route.Raw("GET", base+"/me/access").Name("access.me"),
	}

	r.group = route.Group("access",
		r.ListRoles, r.ShowRole, r.RoleHolders, r.CompareRole, r.CreateRole, r.UpdateRole, r.DeleteRole, r.ReplaceRole,
		r.BindableRoles, r.SubjectAccess, r.SubjectScopes, r.Bind, r.Unbind, r.MyAccess,
	).Types(SubjectKinds)

	return r
}

// SubjectKinds is the fixed set of who a role can be given to.
var SubjectKinds = route.Enum(authz.KindUser, authz.KindServiceAccount).As("SubjectKind")

// Contract is the routes as a group: what contract.Generate reads.
func (r *Routes) Contract() *route.Contract { return r.group }

// To binds a handler to every route; hand the result to app.Routes. The engine
// answers GET /me/access: the signed-in subject's own effective access.
func (r *Routes) To(roles *admin.Roles, bindings *admin.Bindings, engine *authz.Engine) []route.Handled {
	h := handlers{roles: roles, bindings: bindings}

	return []route.Handled{
		r.ListRoles.To(h.listRoles),
		r.ShowRole.To(h.showRole),
		r.RoleHolders.To(h.roleHolders),
		r.CompareRole.To(h.compareRole),
		r.CreateRole.To(h.createRole),
		r.UpdateRole.To(h.updateRole),
		r.DeleteRole.To(h.deleteRole),
		r.ReplaceRole.To(h.replaceRole),
		r.BindableRoles.To(h.bindableRoles),
		r.SubjectAccess.To(h.subjectAccess),
		r.SubjectScopes.To(h.subjectScopes),
		r.Bind.To(h.bind),
		r.Unbind.To(h.unbind),
		r.MyAccess.To(authzhttp.MeAccess(engine)),
	}
}

type handlers struct {
	roles    *admin.Roles
	bindings *admin.Bindings
}

func (h handlers) listRoles(ctx context.Context, _ route.None) (RoleList, error) {
	views, err := h.roles.List(ctx)
	if err != nil {
		return RoleList{}, err
	}

	out := RoleList{Roles: make([]RoleSummary, len(views))}
	for i, v := range views {
		out.Roles[i] = summaryOf(v)
	}

	return out, nil
}

func (h handlers) showRole(ctx context.Context, in RoleInput) (Role, error) {
	view, err := h.roles.Show(ctx, in.Ref)
	if err != nil {
		return Role{}, err
	}

	return roleOf(view), nil
}

func (h handlers) roleHolders(ctx context.Context, in RoleInput) (RoleHolders, error) {
	holders, err := h.roles.Holders(ctx, in.Ref)
	if err != nil {
		return RoleHolders{}, err
	}

	return holdersOf(holders), nil
}

func (h handlers) compareRole(ctx context.Context, in CompareInput) (RoleComparison, error) {
	c, err := h.roles.Compare(ctx, in.Ref, in.With)
	if err != nil {
		return RoleComparison{}, err
	}

	return comparisonOf(c), nil
}

func (h handlers) createRole(ctx context.Context, in CreateRoleInput) (Role, error) {
	draft, err := draftOf(in.Name, in.Description, in.Grants, in.Attrs)
	if err != nil {
		return Role{}, err
	}

	view, err := h.roles.Create(ctx, draft)
	if err != nil {
		return Role{}, err
	}

	return roleOf(view), nil
}

func (h handlers) updateRole(ctx context.Context, in UpdateRoleInput) (Role, error) {
	draft, err := draftOf(in.Name, in.Description, in.Grants, in.Attrs)
	if err != nil {
		return Role{}, err
	}

	view, err := h.roles.Update(ctx, in.Ref, draft)
	if err != nil {
		return Role{}, err
	}

	return roleOf(view), nil
}

func (h handlers) deleteRole(ctx context.Context, in RoleInput) (route.Empty, error) {
	return route.Empty{}, h.roles.Delete(ctx, in.Ref)
}

func (h handlers) replaceRole(ctx context.Context, in ReplaceRoleInput) (Replaced, error) {
	n, err := h.roles.Replace(ctx, in.Ref, strings.TrimSpace(in.With))
	if err != nil {
		return Replaced{}, err
	}

	return Replaced{Rebound: n}, nil
}

func (h handlers) bindableRoles(ctx context.Context, in BindableInput) (BindableRoles, error) {
	views, err := h.bindings.BindableRoles(ctx, authz.Scope{Level: strings.ToLower(strings.TrimSpace(in.Level)), ID: in.ScopeID})
	if err != nil {
		return BindableRoles{}, err
	}

	out := BindableRoles{Roles: make([]Role, len(views))}
	for i, v := range views {
		out.Roles[i] = roleOf(v)
	}

	return out, nil
}

func person(id int) authz.Subject { return authz.Subject{Kind: authz.KindUser, ID: id} }

func (h handlers) subjectAccess(ctx context.Context, in SubjectInput) (SubjectAccess, error) {
	a, err := h.bindings.Access(ctx, person(in.ID))
	if err != nil {
		return SubjectAccess{}, err
	}

	return accessOf(a), nil
}

func (h handlers) subjectScopes(ctx context.Context, in SubjectInput) (ScopeOptions, error) {
	o, err := h.bindings.Scopes(ctx, person(in.ID))
	if err != nil {
		return ScopeOptions{}, err
	}

	return scopesOf(o), nil
}

func (h handlers) bind(ctx context.Context, in BindInput) (Binding, error) {
	scope := authz.Scope{Level: strings.ToLower(strings.TrimSpace(in.Level))}
	if in.ScopeID != nil {
		scope.ID = *in.ScopeID
	}

	b, err := h.bindings.Bind(ctx, person(in.ID), admin.BindingDraft{Role: strings.TrimSpace(in.RoleRef), Scope: scope})
	if err != nil {
		return Binding{}, err
	}

	return bindingOf(b), nil
}

func (h handlers) unbind(ctx context.Context, in UnbindInput) (route.Empty, error) {
	return route.Empty{}, h.bindings.Unbind(ctx, person(in.ID), in.BindingID)
}

func draftOf(name, description string, grants []GrantInput, attrs []Constraint) (admin.RoleDraft, error) {
	out := admin.RoleDraft{Name: strings.TrimSpace(name), Description: strings.TrimSpace(description)}

	for _, c := range attrs {
		if out.Attrs == nil {
			out.Attrs = map[string][]string{}
		}

		out.Attrs[strings.TrimSpace(c.Attribute)] = c.Values
	}

	for _, g := range grants {
		q, err := authz.ParseQualifier(strings.ToLower(strings.TrimSpace(g.Qualifier)))
		if err != nil {
			return admin.RoleDraft{}, apperr.BadRequest("qualifier must be own, own_location or all")
		}

		out.Grants = append(out.Grants, authz.Grant{Permission: strings.TrimSpace(g.Permission), Qualifier: q})
	}

	return out, nil
}
