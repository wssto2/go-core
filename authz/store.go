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
