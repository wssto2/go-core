package admin

import (
	"sort"
	"strconv"
	"time"

	"github.com/wssto2/go-core/authz"
)

// RoleView is a role as the editors show it.
type RoleView struct {
	// Ref is how the role is addressed: the key of a predefined role, the ID of
	// a custom one as text.
	Ref         string
	ID          int
	Key         string
	Name        string
	Description string
	// Predefined roles live in code and cannot be changed or deleted.
	Predefined bool
	// Computed roles derive their grants from the catalogue.
	Computed bool
	// Attrs constrain attribute-aware permissions (attribute key to values).
	Attrs map[string][]string
	// Holders is how many subjects hold the role.
	Holders         int
	PermissionCount int
	// Grants are set when the view is of one role, not of a list.
	Grants []authz.Grant
}

// RefOf is the reference a role is addressed by.
func RefOf(role authz.Role) string {
	if role.Predefined() {
		return role.Key
	}
	return strconv.Itoa(role.ID)
}

func viewOf(role authz.Role, cat *authz.Catalogue, holders int, withGrants bool) RoleView {
	resolved := role.Resolve(cat)
	view := RoleView{
		Ref: RefOf(role), ID: role.ID, Key: role.Key, Name: role.Name, Description: role.Description,
		Predefined: role.Predefined(), Computed: role.Computed != nil, Attrs: role.Attrs,
		Holders: holders, PermissionCount: len(resolved),
	}
	if withGrants {
		view.Grants = resolved
		sort.SliceStable(view.Grants, func(i, j int) bool { return view.Grants[i].Permission < view.Grants[j].Permission })
	}
	return view
}

// Place is a scope with its name, when the application knows one.
type Place struct {
	Scope authz.Scope
	Name  string
}

// Holder is one subject that holds a role, and where.
type Holder struct {
	Subject authz.Subject
	Name    string
	Place   Place
}

// Difference is a permission two roles grant at different qualifiers.
type Difference struct {
	Permission string
	Role       authz.Qualifier
	Other      authz.Qualifier
}

// Comparison is how a custom role differs from a predefined one.
type Comparison struct {
	OnlyInRole  []authz.Grant
	OnlyInOther []authz.Grant
	Different   []Difference
}

// Binding is one role a subject holds, and where.
type Binding struct {
	ID   int
	Role RoleView
	// Place is where the role applies.
	Place Place
	// CreatedBy names who gave the role; zero when unknown or not a person.
	CreatedBy     int
	CreatedByName string
	CreatedAt     time.Time
}

// Grant is one way an effective permission is held: through which binding and
// role, at which place, for whose records.
type Grant struct {
	Qualifier authz.Qualifier
	Place     Place
	Attrs     map[string][]string
	BindingID int
	// RoleKey is the key of a predefined role, empty for a custom one.
	RoleKey  string
	RoleName string
}

// Permission is one permission a subject holds and why. Unavailable is set for
// a permission a role grants but a feature switch turns off: held on paper,
// doing nothing.
type Permission struct {
	Permission  string
	Grants      []Grant
	Unavailable bool
}

// SubjectAccess is everything about what a subject may do.
type SubjectAccess struct {
	Subject   authz.Subject
	Bindings  []Binding
	Effective []Permission
	// CanManage is whether the actor may change this subject's bindings: they
	// hold the manage permission somewhere and the subject is not themselves.
	CanManage bool
}

// ScopeOptions are the places the actor may give roles at.
type ScopeOptions struct {
	// RootLevel names the hierarchy's root level ("organization"), the level a
	// binding at the root carries.
	RootLevel string
	// Root is whether the actor may give roles at the root (the whole organization).
	Root bool
	// Places are the places below the root they may give roles at, parents first.
	Places []ScopeOption
}
