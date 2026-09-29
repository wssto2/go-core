package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/wssto2/go-core/apperr"
)

// CanSaveRole reports whether the actor may store the custom role: the role
// validates against the catalogue, and every grant is one the actor holds, at
// least as widely (qualifier) and at least as broadly (attribute constraints).
// It does not check the actor's right to manage roles at all; Admin does.
//
// The actor's grants are compared one clause at a time, not as a union: an actor
// holding "used vehicles" and "new vehicles" as two separate grants cannot
// create a role for both. That errs on the side of no escalation.
func (e *Engine) CanSaveRole(ctx context.Context, actor Principal, role Role) error {
	if role.Computed != nil {
		return apperr.BadRequestErr(&ValidationError{Problems: []Problem{
			{Code: ProblemInvalidRole, Detail: "a stored role cannot be computed"},
		}})
	}
	if err := role.Validate(e.cat); err != nil {
		return apperr.BadRequestErr(err)
	}
	eff, err := e.Effective(ctx, actor.Subject)
	if err != nil {
		return err
	}
	return e.holdsAll(eff, role)
}

// CanBind reports whether the actor may assign the role at the scope. Assigning
// is not giving away what you hold: a dealer administrator with no sales
// permissions may hand out a sales role at their dealer. So the rule is about
// where and about the dangerous permissions, not about holding every grant:
//
//  1. the actor holds manageBindings at a scope containing the target scope;
//  2. the role contains no System permission the actor does not hold (at a scope
//     containing the target);
//  3. the role contains no OrganizationOnly permission unless the actor holds
//     manageBindings at the root;
//  4. OrganizationOnly permissions can only be bound at the root.
//
// It does not forbid assigning to oneself: Admin does.
func (e *Engine) CanBind(ctx context.Context, actor Principal, role Role, scope Scope, manageBindings string) error {
	if err := role.Validate(e.cat); err != nil {
		return apperr.BadRequestErr(err)
	}
	chain, err := e.h.Chain(ctx, e.resolver, scope)
	if err != nil {
		if errors.Is(err, ErrScopeNotFound) || errors.Is(err, ErrInvalidScope) {
			return apperr.BadRequestErr(err)
		}
		return apperr.Internal(err)
	}
	grants := role.Resolve(e.cat)
	if !e.h.IsRoot(scope) { // (4)
		var problems []Problem
		for _, g := range grants {
			if p, ok := e.cat.Lookup(g.Permission); ok && p.OrganizationOnly {
				problems = append(problems, Problem{Code: ProblemOrganizationOnly, Permission: p.ID,
					Detail: "can only be bound at " + e.h.RootLevel()})
			}
		}
		if err := asValidation(problems); err != nil {
			return apperr.BadRequestErr(err)
		}
	}
	eff, err := e.Effective(ctx, actor.Subject)
	if err != nil {
		return err
	}
	if !holdsWithin(eff, manageBindings, chain.Within) { // (1)
		return forbidden(manageBindings)
	}
	atRoot := holdsWithin(eff, manageBindings, func(s Scope) bool { return e.h.IsRoot(s) })
	for _, g := range grants {
		p, _ := e.cat.Lookup(g.Permission)
		if p.System && !holdsWithin(eff, p.ID, chain.Within) { // (2)
			return escalation(p.ID, "a system permission the actor does not hold")
		}
		if p.OrganizationOnly && !atRoot { // (3)
			return escalation(p.ID, "organization-only permissions need "+manageBindings+" at "+e.h.RootLevel())
		}
	}
	return nil
}

// holdsWithin reports whether eff holds the permission through a clause whose
// scope satisfies the test (qualifier and attributes do not matter).
func holdsWithin(eff *Effective, permission string, within func(Scope) bool) bool {
	for _, c := range eff.grants[permission] {
		if within(c.Scope) {
			return true
		}
	}
	return false
}

// holdsAll checks that eff holds every grant of the role, at least as widely and
// broadly, through a single clause each.
func (e *Engine) holdsAll(eff *Effective, role Role) error {
	for _, g := range role.Resolve(e.cat) {
		perm, _ := e.cat.Lookup(g.Permission)
		wanted := attrsFor(perm, role.Attrs)
		clauses := eff.grants[g.Permission]
		if len(clauses) == 0 {
			return escalation(g.Permission, "not held")
		}
		covered := false
		for _, c := range clauses {
			if c.Qualifier.Covers(g.Qualifier) && attrsCover(c.Attrs, wanted) {
				covered = true
				break
			}
		}
		if !covered {
			return escalation(g.Permission, "held more narrowly")
		}
	}
	return nil
}

// keepsAccess is the last-admin lock-out check: after the change (a binding
// dropped, a role replaced) the subject must still hold every protected
// permission it holds now. drop is a binding ID to leave out (0 for none);
// override replaces a custom role's definition (nil for none).
func (e *Engine) keepsAccess(ctx context.Context, s Subject, protected []string, drop int, override *Role) error {
	if len(protected) == 0 {
		return nil
	}
	before, err := e.Effective(ctx, s)
	if err != nil {
		return err
	}
	bindings, err := e.store.BindingsFor(ctx, s)
	if err != nil {
		return apperr.Internal(fmt.Errorf("authz: load bindings of %s: %w", s, err))
	}
	kept := make([]Binding, 0, len(bindings))
	for _, b := range bindings {
		if b.ID != drop || drop == 0 {
			kept = append(kept, b)
		}
	}
	lookup := func(ctx context.Context, ref RoleRef) (Role, error) {
		if override != nil && ref.ID > 0 && ref.ID == override.ID {
			return *override, nil
		}
		return e.lookupRole(ctx, ref)
	}
	after, err := e.build(ctx, s, kept, lookup)
	if err != nil {
		return apperr.Internal(err)
	}
	for _, perm := range protected {
		if len(before.grants[perm]) > 0 && len(after.grants[perm]) == 0 {
			return apperr.Wrap(fmt.Errorf("%w: %s", ErrLastAdmin, perm), "this would remove your own last access to "+perm,
				apperr.CodePermissionDenied).
				WithLog(apperr.LevelWarn).
				WithReason(ReasonLastAdmin, map[string]any{"permission": perm})
		}
	}
	return nil
}
