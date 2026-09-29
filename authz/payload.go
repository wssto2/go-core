package authz

import (
	"context"
	"fmt"
	"sort"

	"github.com/wssto2/go-core/apperr"
)

// PermissionAccess is one permission in a MyAccess payload.
type PermissionAccess struct {
	// Scope is the widest scope the permission is held at, Qualifier the widest
	// qualifier. They can come from different clauses; Clauses has the detail.
	Scope     Scope     `json:"scope"`
	Qualifier Qualifier `json:"qualifier"`
	// Clauses are the pruned ways the permission is held.
	Clauses []ClauseInfo `json:"clauses"`
}

// ClauseInfo is the serializable form of a Clause, with the "why".
type ClauseInfo struct {
	Scope     Scope               `json:"scope"`
	Qualifier Qualifier           `json:"qualifier"`
	Attrs     map[string][]string `json:"attrs,omitempty"`
	Role      string              `json:"role,omitempty"`
	BindingID int                 `json:"binding_id,omitempty"`
}

// MyAccess is the /me/access payload: every permission the caller holds, with
// its widest scope and qualifier. The frontend's can() and scopeOf() read it.
type MyAccess struct {
	Subject Subject `json:"subject"`
	// Root is true when some binding is at the root scope (crosses tenants).
	Root bool `json:"root"`
	// Permissions maps permission identifier to how it is held.
	Permissions map[string]PermissionAccess `json:"permissions"`
	// Unavailable lists permissions a role grants but whose feature the tenant
	// does not have; they are not in Permissions.
	Unavailable []string `json:"unavailable,omitempty"`
}

// MyAccess computes the payload for the principal in ctx.
func (e *Engine) MyAccess(ctx context.Context) (MyAccess, error) {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		return MyAccess{}, unauthenticated()
	}
	eff, err := e.Effective(ctx, p.Subject)
	if err != nil {
		return MyAccess{}, err
	}
	out := MyAccess{Subject: p.Subject, Permissions: map[string]PermissionAccess{}}
	_, out.Root = eff.Tenants()
	for _, id := range eff.Permissions() {
		perm, _ := e.cat.Lookup(id)
		var open []Clause
		for _, c := range eff.grants[id] {
			on, err := e.featureOn(ctx, perm, c.Scope, c.Chain)
			if err != nil {
				return MyAccess{}, apperr.Internal(fmt.Errorf("authz: feature %q: %w", perm.Feature, err))
			}
			if on {
				open = append(open, c)
			}
		}
		if len(open) == 0 {
			out.Unavailable = append(out.Unavailable, id)
			continue
		}
		out.Permissions[id] = e.describe(prune(open))
	}
	sort.Strings(out.Unavailable)
	return out, nil
}

func (e *Engine) describe(clauses []Clause) PermissionAccess {
	pa := PermissionAccess{Scope: clauses[0].Scope}
	for _, c := range clauses {
		if e.h.Wider(c.Scope.Level, pa.Scope.Level) {
			pa.Scope = c.Scope
		}
		if c.Qualifier > pa.Qualifier {
			pa.Qualifier = c.Qualifier
		}
		pa.Clauses = append(pa.Clauses, ClauseInfo{
			Scope: c.Scope, Qualifier: c.Qualifier, Attrs: c.Attrs,
			Role: c.Source.RoleName, BindingID: c.Source.BindingID,
		})
	}
	return pa
}
