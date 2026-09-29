package authz

import (
	"slices"
	"sort"
)

// Source says which binding and role produced a clause: the "why" of an access
// review.
type Source struct {
	BindingID int     `json:"binding_id,omitempty"`
	Role      RoleRef `json:"role"`
	RoleName  string  `json:"role_name,omitempty"`
}

// Clause is one way a permission is held: at a scope, for the records the
// qualifier reaches, narrowed by attribute constraints. A subject's access to a
// permission is the union (OR) of its clauses.
type Clause struct {
	// Scope is the binding's scope; Chain runs from it up to the root.
	Scope Scope `json:"scope"`
	Chain Chain `json:"-"`
	// Qualifier says whose records within the scope.
	Qualifier Qualifier `json:"qualifier"`
	// Attrs constrains attributes: key to allowed values. Absent means any.
	Attrs  map[string][]string `json:"attrs,omitempty"`
	Source Source              `json:"source"`
}

// covers reports whether clause a reaches everything clause b does.
func (a Clause) covers(b Clause) bool {
	if !b.Chain.Within(a.Scope) || !a.Qualifier.Covers(b.Qualifier) {
		return false
	}
	return attrsCover(a.Attrs, b.Attrs)
}

// attrsCover reports whether constraints a allow at least what b allows: every
// key a constrains, b constrains to a subset.
func attrsCover(a, b map[string][]string) bool {
	for key, allowed := range a {
		narrow, ok := b[key]
		if !ok || !subset(narrow, allowed) {
			return false
		}
	}
	return true
}

func subset(narrow, wide []string) bool {
	for _, n := range narrow {
		if !slices.Contains(wide, n) {
			return false
		}
	}
	return true
}

// Effective is everything a subject may do, resolved from its bindings. It is
// immutable once built; the engine caches it.
type Effective struct {
	subject Subject
	grants  map[string][]Clause
	roleIDs map[int]struct{}

	tenants  map[int]struct{}
	hasRoot  bool
	unpinned bool
}

// Subject is who the access belongs to.
func (e *Effective) Subject() Subject { return e.subject }

// Clauses returns every clause holding the permission (nil when none).
func (e *Effective) Clauses(permission string) []Clause {
	return append([]Clause(nil), e.grants[permission]...)
}

// Permissions returns the permissions held at least once, sorted.
func (e *Effective) Permissions() []string {
	out := make([]string, 0, len(e.grants))
	for p := range e.grants {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (e *Effective) usesRole(id int) bool { _, ok := e.roleIDs[id]; return ok }

// Tenants returns the tenant IDs the subject's bindings pin, sorted, and
// whether some binding is at the root (crossing tenants).
func (e *Effective) Tenants() ([]int, bool) {
	ids := make([]int, 0, len(e.tenants))
	for id := range e.tenants {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	return ids, e.hasRoot
}

// Tenant returns the single tenant every binding is pinned to. It is false when
// there is none, several, a root binding, or a binding whose chain has no tenant
// (the hierarchy has no tenant level).
func (e *Effective) Tenant() (int, bool) {
	if e.hasRoot || e.unpinned || len(e.tenants) != 1 {
		return 0, false
	}
	for id := range e.tenants {
		return id, true
	}
	return 0, false
}

// prune drops clauses another clause of the list fully covers ("widest wins"),
// keeping the first of two equal clauses.
func prune(clauses []Clause) []Clause {
	out := make([]Clause, 0, len(clauses))
	for i, c := range clauses {
		redundant := false
		for j, o := range clauses {
			if i == j || !o.covers(c) {
				continue
			}
			if c.covers(o) && i < j { // equal: keep the earlier one
				continue
			}
			redundant = true
			break
		}
		if !redundant {
			out = append(out, c)
		}
	}
	return out
}
