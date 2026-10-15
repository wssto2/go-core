package authz

import (
	"context"
	"fmt"
	"slices"
)

// roleLister is the part of a Store that lists the custom roles; with HolderStore it is what HoldersOf needs.
type roleLister interface {
	ListRoles(ctx context.Context) ([]Role, error)
}

// HoldersOf returns every subject bound to a role that grants the permission, each once, in the order of their
// first binding: the people (and service accounts, with their Kind; callers filter) an alert about it, or a review of
// who can do it, concerns. The roles are the predefined ones of the engine and the custom ones of the store; a
// binding counts at any scope, as Require does.
//
// It needs a store that lists custom roles and their holders (Store and HolderStore: authz/gormstore and
// authztest.MemoryStore are), else it answers an error that says so. An unknown permission is ErrUnknownPermission.
func (e *Engine) HoldersOf(ctx context.Context, permission string) ([]Subject, error) {
	if _, ok := e.cat.Lookup(permission); !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownPermission, permission)
	}

	lister, listsRoles := e.store.(roleLister)
	holders, hasHolders := e.store.(HolderStore)

	if !listsRoles || !hasHolders {
		//nolint:err113 // names the store
		return nil, fmt.Errorf("authz: HoldersOf needs a store that lists custom roles and their holders, got %T", e.store)
	}

	custom, err := lister.ListRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("authz: list custom roles: %w", err)
	}

	var bindings []Binding

	for _, role := range append(e.PredefinedRoles(), custom...) {
		if !grants(role, e.cat, permission) {
			continue
		}

		held, err := holders.HoldersOf(ctx, role.Ref())
		if err != nil {
			return nil, fmt.Errorf("authz: holders of role %s: %w", role.Name, err)
		}

		bindings = append(bindings, held...)
	}

	slices.SortStableFunc(bindings, func(a, b Binding) int { return a.ID - b.ID })

	var out []Subject

	for _, b := range bindings {
		if !slices.Contains(out, b.Subject) {
			out = append(out, b.Subject)
		}
	}

	return out, nil
}

// grants reports whether the role grants the permission, whatever the qualifier.
func grants(role Role, cat *Catalogue, permission string) bool {
	return slices.ContainsFunc(role.Resolve(cat), func(g Grant) bool { return g.Permission == permission })
}
