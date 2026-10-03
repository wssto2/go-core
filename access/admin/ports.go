package admin

import (
	"context"

	"github.com/wssto2/go-core/authz"
)

// Store is what role administration reads and writes: the authz store plus the
// holder queries. authz/gormstore.Store is one.
type Store interface {
	authz.Store
	authz.HolderStore
}

// Transactor runs fn in one database transaction. database.Transactor is one;
// it is what makes replacing a role all-or-nothing.
type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// ScopeCatalog is what an application knows about the places a role can be
// given at, below the hierarchy's root. It is also the engine's ScopeResolver,
// so one value serves both.
//
// An application without tenancy has only the root and passes NoScopes.
type ScopeCatalog interface {
	authz.ScopeResolver

	// Names returns the display name of each given scope that exists. A scope
	// missing from the result does not exist.
	Names(ctx context.Context, scopes []authz.Scope) (map[authz.Scope]string, error)
	// Options lists every place below the root, parents before their children.
	Options(ctx context.Context) ([]ScopeOption, error)
}

// ScopeOption is one place a role can be given at.
type ScopeOption struct {
	Scope authz.Scope
	Name  string
	// Parent is the place one level up; the root scope for a first-level place.
	Parent authz.Scope
}

// SubjectDirectory is what an application knows about who can hold a role.
type SubjectDirectory interface {
	// SubjectNames returns the display name of each given subject that exists.
	// A subject missing from the result does not exist.
	SubjectNames(ctx context.Context, subjects []authz.Subject) (map[authz.Subject]string, error)
}

// NoScopes is the ScopeCatalog of an application without tenancy: the root is
// the only place.
func NoScopes() ScopeCatalog { return noScopes{} }

type noScopes struct{}

func (noScopes) Parent(context.Context, authz.Scope) (authz.Scope, error) {
	return authz.Scope{}, authz.ErrInvalidScope
}

func (noScopes) Names(context.Context, []authz.Scope) (map[authz.Scope]string, error) {
	return nil, nil
}

func (noScopes) Options(context.Context) ([]ScopeOption, error) { return nil, nil }

// The permissions administration is guarded by. The ids are fixed, so the
// generated TypeScript always matches; Install defines them in the catalogue
// when the application has not.
const (
	// ViewRoles reads roles, their holders and comparisons.
	ViewRoles = "iam.role:view"
	// ManageRoles creates and edits custom roles and replaces one by another.
	ManageRoles = "iam.role:manage"
	// DeleteRoles deletes a custom role nobody holds. It is its own permission:
	// managing roles does not include it.
	DeleteRoles = "iam.role:delete"
	// ViewAccess reads what a subject may do and why.
	ViewAccess = "iam.user:view"
	// ManageBindings gives and takes away roles, at the scope it is held at. It
	// is the permission delegation is keyed on and the one nobody may lose their
	// own last access to.
	ManageBindings = "iam.user:manage"
)

// PermissionIDs returns the five ids.
func PermissionIDs() []string {
	return []string{ViewRoles, ManageRoles, DeleteRoles, ViewAccess, ManageBindings}
}
