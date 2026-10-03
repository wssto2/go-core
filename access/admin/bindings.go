package admin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
)

// BindingDraft gives a subject a role at a scope. Role is the key of a
// predefined role or the ID of a custom one as text (RoleView.Ref).
type BindingDraft struct {
	Role  string
	Scope authz.Scope
}

// IAM-AUTHZ-005 items 5 to 7: bindings through the delegation rules, effective access with the why,
// scope options and bindable roles that offer only what the delegation accepts.

// Bindings reads what subjects may do and why, and gives and takes away roles.
type Bindings struct {
	base
}

// Access describes the subject's bindings and, for each permission they hold,
// how they hold it.
func (b *Bindings) Access(ctx context.Context, subject authz.Subject) (SubjectAccess, error) {
	if err := b.cfg.Engine.Require(ctx, ViewAccess); err != nil {
		return SubjectAccess{}, err
	}

	if err := b.requireSubject(ctx, subject); err != nil {
		return SubjectAccess{}, err
	}

	stored, err := b.cfg.Store.BindingsFor(ctx, subject)
	if err != nil {
		return SubjectAccess{}, apperr.Internal(fmt.Errorf("access: bindings of %s: %w", subject, err))
	}

	eff, err := b.cfg.Engine.Effective(ctx, subject)
	if err != nil {
		return SubjectAccess{}, err
	}

	mine, err := b.cfg.Engine.MyAccess(authz.WithPrincipal(ctx, authz.Principal{Subject: subject}))
	if err != nil {
		return SubjectAccess{}, err
	}

	scopes := make([]authz.Scope, 0, len(stored))
	for _, s := range stored {
		scopes = append(scopes, s.Scope)
	}

	for _, id := range eff.Permissions() {
		for _, c := range eff.Clauses(id) {
			scopes = append(scopes, c.Scope)
		}
	}

	placeOf, err := b.places(ctx, scopes)
	if err != nil {
		return SubjectAccess{}, err
	}

	bindings, err := b.describe(ctx, stored, placeOf)
	if err != nil {
		return SubjectAccess{}, err
	}

	off := map[string]bool{}
	for _, id := range mine.Unavailable {
		off[id] = true
	}

	effective := make([]Permission, 0, len(eff.Permissions()))

	for _, id := range eff.Permissions() {
		perm := Permission{Permission: id, Unavailable: off[id]}

		for _, c := range eff.Clauses(id) {
			perm.Grants = append(perm.Grants, Grant{
				Qualifier: c.Qualifier, Place: placeOf(c.Scope), Attrs: c.Attrs, BindingID: c.Source.BindingID,
				RoleKey: c.Source.Role.Key, RoleName: c.Source.RoleName,
			})
		}

		effective = append(effective, perm)
	}

	actor, _ := authz.PrincipalFrom(ctx)
	canManage := actor.Subject != subject && b.cfg.Engine.Require(ctx, ManageBindings) == nil

	return SubjectAccess{Subject: subject, Bindings: bindings, Effective: effective, CanManage: canManage}, nil
}

// Mine lists the signed-in subject's own bindings with their roles and places:
// what a profile page shows. Anyone may read their own; no permission is asked.
func (b *Bindings) Mine(ctx context.Context) ([]Binding, error) {
	actor, err := b.actor(ctx)
	if err != nil {
		return nil, err
	}

	stored, err := b.cfg.Store.BindingsFor(ctx, actor.Subject)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("access: own bindings: %w", err))
	}

	scopes := make([]authz.Scope, 0, len(stored))
	for _, s := range stored {
		scopes = append(scopes, s.Scope)
	}

	placeOf, err := b.places(ctx, scopes)
	if err != nil {
		return nil, err
	}

	cat := b.cfg.Engine.Catalogue()
	out := make([]Binding, 0, len(stored))

	for _, s := range stored {
		role, err := b.roleOf(ctx, s.Role)
		if err != nil {
			return nil, err
		}

		out = append(out, Binding{ID: s.ID, Role: viewOf(role, cat, 0, false), Place: placeOf(s.Scope), CreatedAt: s.CreatedAt})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out, nil
}

// describe turns stored bindings into views, naming the roles and who made them.
func (b *Bindings) describe(ctx context.Context, stored []authz.Binding, placeOf func(authz.Scope) Place) ([]Binding, error) {
	counts, err := b.holderCounts(ctx)
	if err != nil {
		return nil, err
	}

	authors := make([]authz.Subject, 0, len(stored))
	for _, s := range stored {
		if s.CreatedBy > 0 {
			authors = append(authors, authz.Subject{Kind: authz.KindUser, ID: s.CreatedBy})
		}
	}

	names, err := b.subjectNames(ctx, authors)
	if err != nil {
		return nil, err
	}

	cat := b.cfg.Engine.Catalogue()
	out := make([]Binding, 0, len(stored))

	for _, s := range stored {
		role, err := b.roleOf(ctx, s.Role)
		if err != nil {
			return nil, err
		}

		view := Binding{ID: s.ID, Role: viewOf(role, cat, counts.Of(role.Ref()), false), Place: placeOf(s.Scope), CreatedAt: s.CreatedAt}

		if name, known := names[authz.Subject{Kind: authz.KindUser, ID: s.CreatedBy}]; known {
			view.CreatedBy, view.CreatedByName = s.CreatedBy, name
		}

		out = append(out, view)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out, nil
}

// Bind gives the subject a role at a scope. Delegation decides whether the
// actor may: ManageBindings at a scope containing the target, no escalation,
// never to themselves.
func (b *Bindings) Bind(ctx context.Context, subject authz.Subject, draft BindingDraft) (Binding, error) {
	if err := b.cfg.Engine.Require(ctx, ManageBindings); err != nil {
		return Binding{}, err
	}

	if err := b.requireSubject(ctx, subject); err != nil {
		return Binding{}, err
	}

	ref, err := roleRef(draft.Role)
	if err != nil {
		return Binding{}, err
	}

	saved, err := b.delegation.Bind(ctx, authz.Binding{Subject: subject, Role: ref, Scope: draft.Scope})
	if err != nil {
		return Binding{}, err
	}

	placeOf, err := b.places(ctx, []authz.Scope{saved.Scope})
	if err != nil {
		return Binding{}, err
	}

	views, err := b.describe(ctx, []authz.Binding{saved}, placeOf)
	if err != nil {
		return Binding{}, err
	}

	return views[0], nil
}

// Unbind removes one of the subject's bindings; a binding of somebody else is a
// not-found.
func (b *Bindings) Unbind(ctx context.Context, subject authz.Subject, bindingID int) error {
	if err := b.cfg.Engine.Require(ctx, ManageBindings); err != nil {
		return err
	}

	if err := b.requireSubject(ctx, subject); err != nil {
		return err
	}

	found, err := b.cfg.Store.Binding(ctx, bindingID)
	if err != nil {
		if errors.Is(err, authz.ErrBindingNotFound) {
			return apperr.NotFoundErr(err)
		}

		return apperr.Internal(fmt.Errorf("access: read binding %d: %w", bindingID, err))
	}

	if found.Subject != subject {
		return apperr.NotFoundErr(authz.ErrBindingNotFound)
	}

	return b.delegation.Unbind(ctx, bindingID)
}

// Scopes are the places the actor may give this subject roles at: where they
// hold ManageBindings, with the names of the places below the root.
func (b *Bindings) Scopes(ctx context.Context, subject authz.Subject) (ScopeOptions, error) {
	actor, err := b.actor(ctx)
	if err != nil {
		return ScopeOptions{}, err
	}

	if err := b.cfg.Engine.Require(ctx, ManageBindings); err != nil {
		return ScopeOptions{}, err
	}

	if err := b.requireSubject(ctx, subject); err != nil {
		return ScopeOptions{}, err
	}

	eff, err := b.cfg.Engine.Effective(ctx, actor.Subject)
	if err != nil {
		return ScopeOptions{}, err
	}

	options, err := b.cfg.Scopes.Options(ctx)
	if err != nil {
		return ScopeOptions{}, apperr.Internal(fmt.Errorf("access: scope options: %w", err))
	}

	h := b.cfg.Engine.Hierarchy()
	clauses := eff.Clauses(ManageBindings)

	out := ScopeOptions{RootLevel: h.RootLevel(), Places: []ScopeOption{}}

	for _, c := range clauses {
		out.Root = out.Root || h.IsRoot(c.Scope)
	}

	for _, option := range options {
		chain, err := h.Chain(ctx, b.cfg.Scopes, option.Scope)
		if err != nil {
			continue // a place the hierarchy cannot resolve cannot be bound at
		}

		for _, c := range clauses {
			if chain.Within(c.Scope) {
				out.Places = append(out.Places, option)
				break
			}
		}
	}

	return out, nil
}

// BindableRoles are the roles the actor may give at the scope: every role for
// which delegation would accept the binding, so a sheet only offers allowed ones.
func (b *Bindings) BindableRoles(ctx context.Context, scope authz.Scope) ([]RoleView, error) {
	actor, err := b.actor(ctx)
	if err != nil {
		return nil, err
	}

	h := b.cfg.Engine.Hierarchy()
	if err := h.Check(scope); err != nil {
		return nil, apperr.BadRequestErr(err)
	}

	if !h.IsRoot(scope) {
		names, err := b.cfg.Scopes.Names(ctx, []authz.Scope{scope})
		if err != nil {
			return nil, apperr.Internal(fmt.Errorf("access: place %s: %w", scope, err))
		}

		if _, ok := names[scope]; !ok {
			return nil, apperr.NotFound("place not found")
		}
	}

	custom, err := b.cfg.Store.ListRoles(ctx)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("access: list roles: %w", err))
	}

	counts, err := b.holderCounts(ctx)
	if err != nil {
		return nil, err
	}

	sort.SliceStable(custom, func(i, j int) bool { return custom[i].Name < custom[j].Name })

	cat := b.cfg.Engine.Catalogue()
	out := []RoleView{}

	for _, role := range append(b.cfg.Engine.PredefinedRoles(), custom...) {
		if b.cfg.Engine.CanBind(ctx, actor, role, scope, ManageBindings) == nil {
			out = append(out, viewOf(role, cat, counts.Of(role.Ref()), true))
		}
	}

	return out, nil
}

// roleRef reads a role reference: the key of a predefined role, or the ID of a
// custom one as text.
func roleRef(ref string) (authz.RoleRef, error) {
	if id, err := strconv.Atoi(ref); err == nil {
		if id <= 0 {
			return authz.RoleRef{}, apperr.BadRequest("role is not a role reference")
		}

		return authz.RoleRef{ID: id}, nil
	}

	if ref == "" {
		return authz.RoleRef{}, apperr.BadRequest("role is required")
	}

	return authz.RoleRef{Key: ref}, nil
}
