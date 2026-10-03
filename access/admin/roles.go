package admin

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
)

// RoleDraft is what building a custom role takes.
type RoleDraft struct {
	Name        string
	Description string
	Grants      []authz.Grant
	Attrs       map[string][]string
}

func (d RoleDraft) role(id int) authz.Role {
	return authz.Role{ID: id, Name: d.Name, Description: d.Description, Grants: d.Grants, Attrs: d.Attrs}
}

// Roles reads roles and their holders and builds custom ones. Predefined roles
// live in code and are read-only.
type Roles struct {
	base
}

// errPredefined is the refusal to change or delete a role that is defined in code.
var errPredefined = errors.New("access: a predefined role is defined in code and cannot be changed")

// List returns every role: the predefined ones (computed first, then by key),
// then the custom ones by name.
func (r *Roles) List(ctx context.Context) ([]RoleView, error) {
	if err := r.cfg.Engine.Require(ctx, r.cfg.Permissions.ViewRoles); err != nil {
		return nil, err
	}

	counts, err := r.holderCounts(ctx)
	if err != nil {
		return nil, err
	}

	custom, err := r.cfg.Store.ListRoles(ctx)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("access: list roles: %w", err))
	}

	cat := r.cfg.Engine.Catalogue()
	predefined := r.cfg.Engine.PredefinedRoles()
	sort.SliceStable(predefined, func(i, j int) bool {
		return predefined[i].Computed != nil && predefined[j].Computed == nil
	})
	sort.SliceStable(custom, func(i, j int) bool { return custom[i].Name < custom[j].Name })

	out := make([]RoleView, 0, len(predefined)+len(custom))

	for _, role := range append(predefined, custom...) {
		out = append(out, viewOf(role, cat, counts.Of(role.Ref()), false))
	}

	return out, nil
}

// Show returns one role with its grants; a computed role's are the ones the
// catalogue gives it.
func (r *Roles) Show(ctx context.Context, ref string) (RoleView, error) {
	if err := r.cfg.Engine.Require(ctx, r.cfg.Permissions.ViewRoles); err != nil {
		return RoleView{}, err
	}

	role, err := r.role(ctx, ref)
	if err != nil {
		return RoleView{}, err
	}

	return r.describe(ctx, role)
}

// Holders lists who holds the role and where, by name.
func (r *Roles) Holders(ctx context.Context, ref string) ([]Holder, error) {
	if err := r.cfg.Engine.Require(ctx, r.cfg.Permissions.ViewRoles); err != nil {
		return nil, err
	}

	role, err := r.role(ctx, ref)
	if err != nil {
		return nil, err
	}

	bindings, err := r.cfg.Store.HoldersOf(ctx, role.Ref())
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("access: holders of %s: %w", ref, err))
	}

	scopes := make([]authz.Scope, len(bindings))
	subjects := make([]authz.Subject, len(bindings))

	for i, b := range bindings {
		scopes[i], subjects[i] = b.Scope, b.Subject
	}

	placeOf, err := r.places(ctx, scopes)
	if err != nil {
		return nil, err
	}

	names, err := r.subjectNames(ctx, subjects)
	if err != nil {
		return nil, err
	}

	out := make([]Holder, len(bindings))
	for i, b := range bindings {
		out[i] = Holder{Subject: b.Subject, Name: names[b.Subject], Place: placeOf(b.Scope)}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}

		return out[i].Subject.ID < out[j].Subject.ID
	})

	return out, nil
}

// Create builds a custom role. Delegation decides whether the actor may: they
// hold ManageRoles and every grant of the role is one they hold at least as
// widely (no escalation through a custom role).
func (r *Roles) Create(ctx context.Context, draft RoleDraft) (RoleView, error) {
	return r.save(ctx, draft.role(0))
}

// Update replaces a custom role; a predefined role is refused.
func (r *Roles) Update(ctx context.Context, ref string, draft RoleDraft) (RoleView, error) {
	current, err := r.custom(ctx, ref)
	if err != nil {
		return RoleView{}, err
	}

	return r.save(ctx, draft.role(current.ID))
}

func (r *Roles) save(ctx context.Context, role authz.Role) (RoleView, error) {
	saved, err := r.delegation.SaveRole(ctx, role)
	if err != nil {
		return RoleView{}, err
	}

	return r.describe(ctx, saved)
}

// Delete removes a custom role nobody holds (authz.role_in_use otherwise). It
// is its own permission, DeleteRoles, so it does not go through the delegation
// admin, which asks for ManageRoles.
func (r *Roles) Delete(ctx context.Context, ref string) error {
	role, err := r.custom(ctx, ref)
	if err != nil {
		return err
	}

	if err := r.cfg.Engine.Require(ctx, r.cfg.Permissions.DeleteRoles); err != nil {
		return err
	}

	actor, err := r.actor(ctx)
	if err != nil {
		return err
	}

	bound, err := r.cfg.Store.BindingsForRole(ctx, role.ID)
	if err != nil {
		return apperr.Internal(fmt.Errorf("access: bindings of role %d: %w", role.ID, err))
	}

	if len(bound) > 0 {
		return apperr.Wrap(authz.ErrRoleInUse, "the role is still bound", apperr.CodeAlreadyExists).
			WithReason(authz.ReasonRoleInUse, map[string]any{"bindings": len(bound)})
	}

	if err := r.cfg.Store.DeleteRole(ctx, actor.Subject, role.ID); err != nil {
		switch {
		case errors.Is(err, authz.ErrRoleNotFound):
			return apperr.NotFoundErr(err)
		case errors.Is(err, authz.ErrRoleInUse):
			return apperr.Wrap(err, "the role is still bound", apperr.CodeAlreadyExists).WithReason(authz.ReasonRoleInUse)
		}

		return apperr.Internal(fmt.Errorf("access: delete role %d: %w", role.ID, err))
	}

	r.cfg.Engine.EvictRole(role.ID)

	return nil
}

// Compare says how a custom role differs from a predefined one, by permission
// and qualifier.
func (r *Roles) Compare(ctx context.Context, ref, with string) (Comparison, error) {
	if err := r.cfg.Engine.Require(ctx, r.cfg.Permissions.ViewRoles); err != nil {
		return Comparison{}, err
	}

	custom, err := r.custom(ctx, ref)
	if err != nil {
		return Comparison{}, err
	}

	other, err := r.predefined(with)
	if err != nil {
		return Comparison{}, err
	}

	cat := r.cfg.Engine.Catalogue()

	return compare(custom.Resolve(cat), other.Resolve(cat)), nil
}

func compare(role, other []authz.Grant) Comparison {
	have := map[string]authz.Qualifier{}
	for _, g := range role {
		have[g.Permission] = g.Qualifier
	}

	want := map[string]authz.Qualifier{}
	for _, g := range other {
		want[g.Permission] = g.Qualifier
	}

	var out Comparison

	for _, g := range role {
		q, both := want[g.Permission]

		switch {
		case !both:
			out.OnlyInRole = append(out.OnlyInRole, g)
		case q != g.Qualifier:
			out.Different = append(out.Different, Difference{Permission: g.Permission, Role: g.Qualifier, Other: q})
		}
	}

	for _, g := range other {
		if _, both := have[g.Permission]; !both {
			out.OnlyInOther = append(out.OnlyInOther, g)
		}
	}

	byPermission := func(grants []authz.Grant) {
		sort.SliceStable(grants, func(i, j int) bool { return grants[i].Permission < grants[j].Permission })
	}

	byPermission(out.OnlyInRole)
	byPermission(out.OnlyInOther)
	sort.SliceStable(out.Different, func(i, j int) bool { return out.Different[i].Permission < out.Different[j].Permission })

	return out
}

// Replace gives every holder of a custom role the predefined role at the same
// scope instead, through the delegation rules: a holder the actor may not give
// the predefined role to refuses the whole replacement. It is one transaction,
// so either every holder is re-bound or none is. The custom role stays, unbound,
// and can then be deleted. It returns how many bindings were re-bound.
func (r *Roles) Replace(ctx context.Context, ref, with string) (int, error) {
	custom, err := r.custom(ctx, ref)
	if err != nil {
		return 0, err
	}

	other, err := r.predefined(with)
	if err != nil {
		return 0, err
	}

	if err := r.cfg.Engine.Require(ctx, r.cfg.Permissions.ManageRoles); err != nil {
		return 0, err
	}

	bindings, err := r.cfg.Store.BindingsForRole(ctx, custom.ID)
	if err != nil {
		return 0, apperr.Internal(fmt.Errorf("access: bindings of role %d: %w", custom.ID, err))
	}

	rebound := 0

	err = r.cfg.Transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		for _, b := range bindings {
			_, err := r.delegation.Bind(ctx, authz.Binding{Subject: b.Subject, Role: other.Ref(), Scope: b.Scope})
			if err != nil && !errors.Is(err, authz.ErrDuplicateBinding) {
				return err
			}

			if err := r.delegation.Unbind(ctx, b.ID); err != nil {
				return err
			}

			rebound++
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	// what was read while the transaction ran may predate its commit
	r.cfg.Engine.EvictRole(custom.ID)

	for _, b := range bindings {
		r.cfg.Engine.Evict(b.Subject)
	}

	return rebound, nil
}

func (r *Roles) describe(ctx context.Context, role authz.Role) (RoleView, error) {
	counts, err := r.holderCounts(ctx)
	if err != nil {
		return RoleView{}, err
	}

	return viewOf(role, r.cfg.Engine.Catalogue(), counts.Of(role.Ref()), true), nil
}

// custom is a role that can be changed: a predefined role is refused, an
// unknown one not found.
func (r *Roles) custom(ctx context.Context, ref string) (authz.Role, error) {
	role, err := r.role(ctx, ref)
	if err != nil {
		return authz.Role{}, err
	}

	if role.Predefined() {
		return authz.Role{}, apperr.ForbiddenErr(errPredefined).
			WithReason(authz.ReasonForbidden, map[string]any{"why": "predefined"})
	}

	return role, nil
}
