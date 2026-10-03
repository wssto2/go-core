package accesshttp

import (
	"maps"
	"slices"
	"time"

	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/authz"
)

// Constraint limits an attribute-aware permission to some values of an
// attribute, for example the vehicle kind to used. An attribute without a
// constraint is unrestricted.
type Constraint struct {
	Attribute string   `json:"attribute" validation:"required|max:64"`
	Values    []string `json:"values"`
}

// Scope is a place a role applies at. ID and Name are null for the root.
type Scope struct {
	Level string  `json:"level"`
	ID    *int    `json:"id"`
	Name  *string `json:"name"`
}

// Grant is one permission of a role and whose records it reaches
// (own, own_location or all).
type Grant struct {
	Permission string `json:"permission"`
	Qualifier  string `json:"qualifier"`
}

// RoleSummary is a role in a list. Key is set for a predefined role, ID for a
// custom one.
type RoleSummary struct {
	Ref             string       `json:"ref"`
	ID              *int         `json:"id"`
	Key             *string      `json:"key"`
	Name            string       `json:"name"`
	Description     string       `json:"description"`
	Predefined      bool         `json:"predefined"`
	Computed        bool         `json:"computed"`
	Attrs           []Constraint `json:"attrs"`
	Holders         int          `json:"holders"`
	PermissionCount int          `json:"permission_count"`
}

// Role is one role with its grants.
type Role struct {
	Ref             string       `json:"ref"`
	ID              *int         `json:"id"`
	Key             *string      `json:"key"`
	Name            string       `json:"name"`
	Description     string       `json:"description"`
	Predefined      bool         `json:"predefined"`
	Computed        bool         `json:"computed"`
	Attrs           []Constraint `json:"attrs"`
	Holders         int          `json:"holders"`
	PermissionCount int          `json:"permission_count"`
	Grants          []Grant      `json:"grants"`
}

// RoleList is GET /iam/roles.
type RoleList struct {
	Roles []RoleSummary `json:"roles"`
}

// BindableRoles is GET /iam/bindable-roles: the roles the signed-in subject may
// give at a place, with their grants.
type BindableRoles struct {
	Roles []Role `json:"roles"`
}

// SubjectRef names a person or a service account.
type SubjectRef struct {
	Kind authz.Kind `json:"kind"`
	ID   int        `json:"id"`
}

// RoleHolder is one binding of a role: who holds it, and where.
type RoleHolder struct {
	Subject SubjectRef `json:"subject"`
	Name    string     `json:"name"`
	Scope   Scope      `json:"scope"`
}

// RoleHolders is GET /iam/roles/:ref/holders.
type RoleHolders struct {
	Holders []RoleHolder `json:"holders"`
}

// GrantDifference is a permission both roles grant, at different qualifiers.
type GrantDifference struct {
	Permission string `json:"permission"`
	Role       string `json:"role"`
	Other      string `json:"other"`
}

// RoleComparison is GET /iam/roles/:ref/compare?with=: how a custom role differs
// from a predefined one.
type RoleComparison struct {
	OnlyInRole  []Grant           `json:"only_in_role"`
	OnlyInOther []Grant           `json:"only_in_other"`
	Different   []GrantDifference `json:"different"`
}

// Replaced is the result of POST /iam/roles/:ref/replace.
type Replaced struct {
	Rebound int `json:"rebound"`
}

// PersonRef is who gave a role.
type PersonRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Binding is one role a subject holds, and where.
type Binding struct {
	ID        int         `json:"id"`
	Role      RoleSummary `json:"role"`
	Scope     Scope       `json:"scope"`
	CreatedBy *PersonRef  `json:"created_by"`
	CreatedAt time.Time   `json:"created_at"`
}

// EffectiveGrant is one way a permission is held: through which binding and
// role, at which place, for whose records.
type EffectiveGrant struct {
	Qualifier string       `json:"qualifier"`
	Scope     Scope        `json:"scope"`
	Attrs     []Constraint `json:"attrs"`
	BindingID int          `json:"binding_id"`
	RoleKey   string       `json:"role_key"`
	RoleName  string       `json:"role_name"`
}

// EffectivePermission is a permission a subject holds and why. Unavailable is
// set when a feature switch turns a held permission off.
type EffectivePermission struct {
	Permission  string           `json:"permission"`
	Grants      []EffectiveGrant `json:"grants"`
	Unavailable bool             `json:"unavailable"`
}

// SubjectAccess is GET /iam/users/:id/access: bindings, effective permissions
// and whether the signed-in subject may change them.
type SubjectAccess struct {
	Subject   SubjectRef            `json:"subject"`
	Bindings  []Binding             `json:"bindings"`
	Effective []EffectivePermission `json:"effective"`
	CanManage bool                  `json:"can_manage"`
}

// ScopeOption is a place a role can be given at. The parent is null for a place
// directly below the root.
type ScopeOption struct {
	Level       string `json:"level"`
	ID          int    `json:"id"`
	Name        string `json:"name"`
	ParentLevel string `json:"parent_level"`
	ParentID    *int   `json:"parent_id"`
}

// ScopeOptions is GET /iam/users/:id/scopes: whether roles may be given at the
// root, and at which places below it, parents first.
type ScopeOptions struct {
	Root   bool          `json:"root"`
	Places []ScopeOption `json:"places"`
}

func scopeOf(p admin.Place) Scope {
	out := Scope{Level: p.Scope.Level}
	if p.Scope.ID > 0 {
		out.ID = &p.Scope.ID
	}

	if p.Name != "" {
		out.Name = &p.Name
	}

	return out
}

func present[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}

	return &v
}

func attrsOf(a map[string][]string) []Constraint {
	out := make([]Constraint, 0, len(a))
	for _, key := range slices.Sorted(maps.Keys(a)) {
		out = append(out, Constraint{Attribute: key, Values: a[key]})
	}

	return out
}

func summaryOf(v admin.RoleView) RoleSummary {
	return RoleSummary{
		Ref: v.Ref, ID: idOf(v), Key: present(v.Key), Name: v.Name, Description: v.Description,
		Predefined: v.Predefined, Computed: v.Computed, Attrs: attrsOf(v.Attrs),
		Holders: v.Holders, PermissionCount: v.PermissionCount,
	}
}

func roleOf(v admin.RoleView) Role {
	s := summaryOf(v)

	return Role{
		Ref: s.Ref, ID: s.ID, Key: s.Key, Name: s.Name, Description: s.Description, Predefined: s.Predefined,
		Computed: s.Computed, Attrs: s.Attrs, Holders: s.Holders, PermissionCount: s.PermissionCount,
		Grants: grantsOf(v.Grants),
	}
}

func idOf(v admin.RoleView) *int {
	if v.Predefined {
		return nil
	}

	return &v.ID
}

func grantsOf(grants []authz.Grant) []Grant {
	out := make([]Grant, len(grants))
	for i, g := range grants {
		out[i] = Grant{Permission: g.Permission, Qualifier: g.Qualifier.String()}
	}

	return out
}

func subjectOf(s authz.Subject) SubjectRef { return SubjectRef{Kind: s.Kind, ID: s.ID} }

func bindingOf(b admin.Binding) Binding {
	out := Binding{ID: b.ID, Role: summaryOf(b.Role), Scope: scopeOf(b.Place), CreatedAt: b.CreatedAt}
	if b.CreatedBy > 0 {
		out.CreatedBy = &PersonRef{ID: b.CreatedBy, Name: b.CreatedByName}
	}

	return out
}

func accessOf(a admin.SubjectAccess) SubjectAccess {
	out := SubjectAccess{
		Subject: subjectOf(a.Subject), CanManage: a.CanManage,
		Bindings: make([]Binding, len(a.Bindings)), Effective: make([]EffectivePermission, len(a.Effective)),
	}

	for i, b := range a.Bindings {
		out.Bindings[i] = bindingOf(b)
	}

	for i, p := range a.Effective {
		perm := EffectivePermission{Permission: p.Permission, Unavailable: p.Unavailable, Grants: make([]EffectiveGrant, len(p.Grants))}
		for j, g := range p.Grants {
			perm.Grants[j] = EffectiveGrant{
				Qualifier: g.Qualifier.String(), Scope: scopeOf(g.Place), Attrs: attrsOf(g.Attrs),
				BindingID: g.BindingID, RoleKey: g.RoleKey, RoleName: g.RoleName,
			}
		}

		out.Effective[i] = perm
	}

	return out
}

func holdersOf(holders []admin.Holder) RoleHolders {
	out := RoleHolders{Holders: make([]RoleHolder, len(holders))}
	for i, h := range holders {
		out.Holders[i] = RoleHolder{Subject: subjectOf(h.Subject), Name: h.Name, Scope: scopeOf(h.Place)}
	}

	return out
}

func comparisonOf(c admin.Comparison) RoleComparison {
	out := RoleComparison{
		OnlyInRole: grantsOf(c.OnlyInRole), OnlyInOther: grantsOf(c.OnlyInOther),
		Different: make([]GrantDifference, len(c.Different)),
	}

	for i, d := range c.Different {
		out.Different[i] = GrantDifference{Permission: d.Permission, Role: d.Role.String(), Other: d.Other.String()}
	}

	return out
}

func scopesOf(o admin.ScopeOptions) ScopeOptions {
	out := ScopeOptions{Root: o.Root, Places: make([]ScopeOption, len(o.Places))}
	for i, p := range o.Places {
		out.Places[i] = ScopeOption{
			Level: p.Scope.Level, ID: p.Scope.ID, Name: p.Name,
			ParentLevel: p.Parent.Level, ParentID: present(p.Parent.ID),
		}
	}

	return out
}
