// Package admin is the administration of roles and bindings over the authz
// engine: list, show, build and delete custom roles, who holds a role, compare
// and replace, and what a subject may do and why, give and take away roles at a
// scope. Delegation is authz.Admin's: nobody gives more than they hold, nobody
// assigns a role to themselves, nobody loses their own last access to the
// binding permission.
//
// The package is framework-free: services are plain methods taking a context.
// The access package puts them behind HTTP routes.
package admin

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
)

// Config wires the services. Every field is required.
type Config struct {
	Engine     *authz.Engine
	Store      Store
	Scopes     ScopeCatalog
	Subjects   SubjectDirectory
	Transactor Transactor
}

// New builds the role and binding services around one authz.Admin.
func New(cfg Config) (*Roles, *Bindings, error) {
	switch {
	case cfg.Engine == nil:
		return nil, nil, errors.New("access: the engine is required: build it with authz.NewEngine, or use access.Install")
	case cfg.Store == nil:
		return nil, nil, errors.New("access: a store is required: use gormstore.New(db), which also answers holder queries")
	case cfg.Scopes == nil:
		return nil, nil, errors.New("access: a ScopeCatalog is required: pass admin.NoScopes() for an application without tenancy")
	case cfg.Subjects == nil:
		return nil, nil, errors.New("access: a SubjectDirectory is required: it names the people and service accounts that hold roles")
	case cfg.Transactor == nil:
		return nil, nil, errors.New("access: a Transactor is required: database.NewTransactor(db)")
	}

	for _, id := range PermissionIDs() {
		if _, ok := cfg.Engine.Catalogue().Lookup(id); !ok {
			return nil, nil, fmt.Errorf("access: permission %q is not in the catalogue: define it in the catalogue, or let access.Install define it", id)
		}
	}

	delegation, err := authz.NewAdmin(authz.AdminConfig{
		Engine: cfg.Engine, Store: cfg.Store,
		ManageRoles: ManageRoles, ManageBindings: ManageBindings,
		Protected: []string{ManageBindings},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("access: %w", err)
	}

	b := base{cfg: cfg, delegation: delegation}

	return &Roles{base: b}, &Bindings{base: b}, nil
}

// base is what both services share.
type base struct {
	cfg        Config
	delegation *authz.Admin
}

func (b base) places(ctx context.Context, scopes []authz.Scope) (func(authz.Scope) Place, error) {
	var below []authz.Scope

	h := b.cfg.Engine.Hierarchy()
	for _, s := range scopes {
		if !h.IsRoot(s) {
			below = append(below, s)
		}
	}

	var names map[authz.Scope]string

	if len(below) > 0 {
		var err error
		if names, err = b.cfg.Scopes.Names(ctx, below); err != nil {
			return nil, fmt.Errorf("access: place names: %w", err)
		}
	}

	return func(s authz.Scope) Place { return Place{Scope: s, Name: names[s]} }, nil
}

func (b base) subjectNames(ctx context.Context, subjects []authz.Subject) (map[authz.Subject]string, error) {
	names, err := b.cfg.Subjects.SubjectNames(ctx, subjects)
	if err != nil {
		return nil, fmt.Errorf("access: subject names: %w", err)
	}

	return names, nil
}

// ACCESS-ADMIN-001: the directory names who can hold a role.

// requireSubject answers a not-found for a subject the directory does not know,
// so an id that does not exist is never told apart from one that may not be seen.
func (b base) requireSubject(ctx context.Context, s authz.Subject) error {
	if !s.Valid() {
		return apperr.NotFound("subject not found")
	}

	names, err := b.subjectNames(ctx, []authz.Subject{s})
	if err != nil {
		return err
	}

	if _, ok := names[s]; !ok {
		return apperr.NotFound("subject not found")
	}

	return nil
}

func (b base) actor(ctx context.Context) (authz.Principal, error) {
	p, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return authz.Principal{}, apperr.Wrap(authz.ErrNoPrincipal, "user not authenticated", apperr.CodeUnauthenticated)
	}

	return p, nil
}

// role reads a role by reference: the key of a predefined role or the ID of a
// custom one. A binding to a role that no longer exists yields a stub when
// tolerant, so the editor can still remove it.
func (b base) role(ctx context.Context, ref string) (authz.Role, error) {
	id, err := strconv.Atoi(ref)
	if err != nil {
		return b.predefined(ref)
	}

	role, err := b.cfg.Store.Role(ctx, id)
	if err != nil {
		if errors.Is(err, authz.ErrRoleNotFound) {
			return authz.Role{}, apperr.NotFoundErr(err)
		}

		return authz.Role{}, apperr.Internal(fmt.Errorf("access: read role %d: %w", id, err))
	}

	return role, nil
}

func (b base) predefined(key string) (authz.Role, error) {
	role, ok := b.cfg.Engine.PredefinedRole(key)
	if !ok {
		return authz.Role{}, apperr.NotFound("role " + key + " not found")
	}

	return role, nil
}

// roleOf reads the role a binding refers to; one that no longer exists is shown
// by what is left of it.
func (b base) roleOf(ctx context.Context, ref authz.RoleRef) (authz.Role, error) {
	if ref.ID > 0 {
		role, err := b.cfg.Store.Role(ctx, ref.ID)
		if errors.Is(err, authz.ErrRoleNotFound) {
			return authz.Role{ID: ref.ID, Name: "#" + strconv.Itoa(ref.ID)}, nil
		}

		if err != nil {
			return authz.Role{}, apperr.Internal(fmt.Errorf("access: read role %d: %w", ref.ID, err))
		}

		return role, nil
	}

	role, ok := b.cfg.Engine.PredefinedRole(ref.Key)
	if !ok {
		return authz.Role{Key: ref.Key, Name: ref.Key}, nil
	}

	return role, nil
}

func (b base) holderCounts(ctx context.Context) (authz.HolderCounts, error) {
	counts, err := b.cfg.Store.HolderCounts(ctx)
	if err != nil {
		return authz.HolderCounts{}, apperr.Internal(fmt.Errorf("access: holder counts: %w", err))
	}

	return counts, nil
}
