package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/wssto2/go-core/apperr"
)

// AdminConfig wires an Admin.
type AdminConfig struct {
	Engine *Engine
	Store  Store
	// ManageRoles is the permission needed (anywhere) to save or delete a custom
	// role. Required.
	ManageRoles string
	// ManageBindings is the permission needed, at the binding's scope, to bind or
	// unbind. Required.
	ManageBindings string
	// Protected are permissions nobody may lose by their own hand: removing your
	// own last binding, or editing a role so that none of your bindings grants
	// one any more, is refused (the last-admin lock-out). Optional.
	Protected []string
}

// Admin is the write side of authorization: it saves roles and manages bindings
// on behalf of the principal in the context, then evicts the cache so the change
// applies on the next request. Writes are audited by the Store.
//
// Delegation differs for roles and bindings:
//
//   - Saving a role is strict: every grant must be held by the actor, at least as
//     widely and broadly (no escalation through a custom role).
//   - Assigning a role is about where and about dangerous permissions: the actor
//     needs ManageBindings at a scope containing the target, and the role may
//     contain a System permission only if the actor holds it, and an
//     OrganizationOnly one only if the actor holds ManageBindings at the root.
//     A dealer administrator can therefore assign a sales role they cannot
//     themselves use. Nobody can assign a role to themselves.
//   - Removing your own binding is allowed, but never your last access to a
//     Protected permission (the last-admin lock-out).
type Admin struct {
	e         *Engine
	store     Store
	roles     string
	bindings  string
	protected []string
}

// NewAdmin validates the configuration.
func NewAdmin(cfg AdminConfig) (*Admin, error) {
	if cfg.Engine == nil || cfg.Store == nil {
		return nil, errors.New("authz: admin needs an engine and a store")
	}
	for _, id := range append([]string{cfg.ManageRoles, cfg.ManageBindings}, cfg.Protected...) {
		if _, ok := cfg.Engine.cat.Lookup(id); !ok {
			return nil, fmt.Errorf("%w: %q (admin configuration)", ErrUnknownPermission, id)
		}
	}
	return &Admin{e: cfg.Engine, store: cfg.Store, roles: cfg.ManageRoles, bindings: cfg.ManageBindings, protected: cfg.Protected}, nil
}

func (a *Admin) actor(ctx context.Context) (Principal, error) {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		return Principal{}, unauthenticated()
	}
	return p, nil
}

// SaveRole creates (ID zero) or replaces a custom role and returns it.
func (a *Admin) SaveRole(ctx context.Context, role Role) (Role, error) {
	actor, err := a.actor(ctx)
	if err != nil {
		return Role{}, err
	}
	if err := a.e.Require(ctx, a.roles); err != nil {
		return Role{}, err
	}
	role.Key = "" // a stored role is identified by ID only
	if err := a.e.CanSaveRole(ctx, actor, role); err != nil {
		return Role{}, err
	}
	if role.ID > 0 {
		if _, err := a.store.Role(ctx, role.ID); err != nil {
			return Role{}, storeErr(err)
		}
		if err := a.e.keepsAccess(ctx, actor.Subject, a.protected, 0, &role); err != nil {
			return Role{}, err
		}
	}
	saved, err := a.store.SaveRole(ctx, actor.Subject, role)
	if err != nil {
		return Role{}, storeErr(err)
	}
	a.e.EvictRole(saved.ID)
	return saved, nil
}

// DeleteRole removes a custom role that has no bindings.
func (a *Admin) DeleteRole(ctx context.Context, id int) error {
	actor, err := a.actor(ctx)
	if err != nil {
		return err
	}
	if err := a.e.Require(ctx, a.roles); err != nil {
		return err
	}
	bound, err := a.store.BindingsForRole(ctx, id)
	if err != nil {
		return storeErr(err)
	}
	if len(bound) > 0 {
		return apperr.Wrap(ErrRoleInUse, "the role is still bound", apperr.CodeAlreadyExists).
			WithReason(ReasonRoleInUse, map[string]any{"bindings": len(bound)})
	}
	if err := a.store.DeleteRole(ctx, actor.Subject, id); err != nil {
		return storeErr(err)
	}
	a.e.EvictRole(id)
	return nil
}

// Bind gives a subject a role at a scope and returns the stored binding.
func (a *Admin) Bind(ctx context.Context, b Binding) (Binding, error) {
	actor, err := a.actor(ctx)
	if err != nil {
		return Binding{}, err
	}
	if !b.Subject.Valid() || !b.Role.Valid() {
		return Binding{}, apperr.BadRequestErr(&ValidationError{Problems: []Problem{
			{Code: ProblemInvalidRole, Detail: "a binding needs a valid subject and exactly one of role ID and key"},
		}})
	}
	if err := a.e.h.Check(b.Scope); err != nil {
		return Binding{}, apperr.BadRequestErr(err)
	}
	if b.Subject == actor.Subject {
		return Binding{}, apperr.Wrap(ErrSelfAssignment, "you cannot assign a role to yourself", apperr.CodePermissionDenied).
			WithLog(apperr.LevelWarn).WithReason(ReasonSelfAssignment)
	}
	if err := a.e.RequireOn(ctx, a.bindings, Resource{Scope: b.Scope}); err != nil {
		return Binding{}, err
	}
	role, err := a.e.lookupRole(ctx, b.Role)
	if err != nil {
		return Binding{}, storeErr(err)
	}
	if err := a.e.CanBind(ctx, actor, role, b.Scope, a.bindings); err != nil {
		return Binding{}, err
	}
	saved, err := a.store.Bind(ctx, actor.Subject, b)
	if err != nil {
		return Binding{}, storeErr(err)
	}
	a.e.Evict(b.Subject)
	return saved, nil
}

// Unbind removes a binding. It needs the same rights as creating it, and refuses
// to remove the actor's own last access to a protected permission (removing your
// own binding is otherwise allowed).
func (a *Admin) Unbind(ctx context.Context, id int) error {
	actor, err := a.actor(ctx)
	if err != nil {
		return err
	}
	b, err := a.store.Binding(ctx, id)
	if err != nil {
		return storeErr(err)
	}
	if err := a.e.RequireOn(ctx, a.bindings, Resource{Scope: b.Scope}); err != nil {
		return err
	}
	if role, err := a.e.lookupRole(ctx, b.Role); err == nil {
		if err := a.e.CanBind(ctx, actor, role, b.Scope, a.bindings); err != nil {
			return err
		}
	} else if !errors.Is(err, ErrRoleNotFound) {
		return apperr.Internal(err)
	}
	if b.Subject == actor.Subject {
		if err := a.e.keepsAccess(ctx, actor.Subject, a.protected, b.ID, nil); err != nil {
			return err
		}
	}
	if err := a.store.Unbind(ctx, actor.Subject, id); err != nil {
		return storeErr(err)
	}
	a.e.Evict(b.Subject)
	return nil
}

// storeErr maps a Store's sentinel errors onto HTTP-ready errors.
func storeErr(err error) error {
	switch {
	case errors.Is(err, ErrRoleNotFound), errors.Is(err, ErrBindingNotFound):
		return apperr.NotFoundErr(err)
	case errors.Is(err, ErrDuplicateBinding), errors.Is(err, ErrRoleInUse):
		return apperr.ConflictErr(err)
	default:
		var ae *apperr.AppError
		if errors.As(err, &ae) {
			return err
		}
		return apperr.Internal(err)
	}
}
