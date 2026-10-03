package authz

import "context"

// Reader is the part of a Store the engine reads on every cache miss.
type Reader interface {
	// BindingsFor returns every binding of the subject.
	BindingsFor(ctx context.Context, s Subject) ([]Binding, error)
	// Role returns a custom role by ID, or an error wrapping ErrRoleNotFound.
	Role(ctx context.Context, id int) (Role, error)
}

// Store persists custom roles and bindings. Every write takes the acting
// subject, so an implementation can audit who changed what. Implementations
// must be safe for concurrent use and pass the conformance suite in
// authz/storetest.
//
// Store does not enforce delegation: use Admin, which checks it, evicts the
// cache and then calls the Store.
type Store interface {
	Reader

	// ListRoles returns every custom role.
	ListRoles(ctx context.Context) ([]Role, error)
	// SaveRole creates the role when ID is zero and replaces it otherwise. It
	// returns the stored role (with its ID). A role that does not exist yields
	// ErrRoleNotFound.
	SaveRole(ctx context.Context, actor Subject, role Role) (Role, error)
	// DeleteRole removes a role. It fails with ErrRoleInUse while bindings
	// reference it and ErrRoleNotFound when there is no such role.
	DeleteRole(ctx context.Context, actor Subject, id int) error

	// Binding returns one binding, or an error wrapping ErrBindingNotFound.
	Binding(ctx context.Context, id int) (Binding, error)
	// BindingsForRole returns every binding of a custom role: the users a role
	// change affects.
	BindingsForRole(ctx context.Context, roleID int) ([]Binding, error)
	// Bind stores a binding and returns it with its ID. An identical binding
	// yields ErrDuplicateBinding.
	Bind(ctx context.Context, actor Subject, b Binding) (Binding, error)
	// Unbind removes a binding.
	Unbind(ctx context.Context, actor Subject, id int) error
}

// HolderCounts is how many subjects hold each role: custom roles by ID,
// predefined ones by key. A subject bound to a role at several scopes counts once.
type HolderCounts struct {
	ByID  map[int]int
	ByKey map[string]int
}

// Of returns the count for a role reference.
func (c HolderCounts) Of(ref RoleRef) int {
	if ref.ID > 0 {
		return c.ByID[ref.ID]
	}
	return c.ByKey[ref.Key]
}

// HolderStore is the part of a Store that role administration needs to show who
// holds a role. It is separate from Store so that an existing Store keeps
// compiling; the gormstore and the in-memory store of authztest implement it and
// pass storetest.RunHolders.
type HolderStore interface {
	// HolderCounts counts the holders of every role that has any.
	HolderCounts(ctx context.Context) (HolderCounts, error)
	// HoldersOf returns every binding of the role, custom or predefined, in
	// binding order. An invalid reference yields none.
	HoldersOf(ctx context.Context, ref RoleRef) ([]Binding, error)
}
