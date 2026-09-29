package authz

import (
	"fmt"
	"sort"
)

// Grant is one permission a role holds, with the qualifier that says whose
// records it reaches. Only ownable permissions may carry a qualifier other
// than QualifierAll.
type Grant struct {
	Permission string    `json:"permission"`
	Qualifier  Qualifier `json:"qualifier"`
}

// Grants builds grants of one qualifier for several permissions.
func Grants(q Qualifier, permissions ...string) []Grant {
	out := make([]Grant, len(permissions))
	for i, p := range permissions {
		out[i] = Grant{Permission: p, Qualifier: q}
	}
	return out
}

// Role is a named set of grants. A role has no scope and no tenant: those
// belong to the binding.
//
// A predefined role lives in code and is identified by Key. A custom role lives
// in a Store and is identified by ID. A computed role (Computed set) derives its
// grants from the catalogue, so a permission added later is included without
// anyone editing the role.
type Role struct {
	// ID identifies a custom (stored) role; zero for a predefined one.
	ID int `json:"id"`
	// Key identifies a predefined role; empty for a custom one.
	Key         string `json:"key,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Grants are the explicit grants. It must be empty for a computed role.
	Grants []Grant `json:"grants,omitempty"`
	// Attrs constrains attribute-aware permissions: attribute key to the allowed
	// values. An absent key means unconstrained. Example: "vehiclekind": ["used"].
	Attrs map[string][]string `json:"attrs,omitempty"`
	// Computed, when set, replaces Grants: every permission it selects is granted
	// with QualifierAll.
	Computed Selector `json:"-"`
}

// Predefined reports whether the role is defined in code.
func (r Role) Predefined() bool { return r.Key != "" }

// Ref returns the role's reference, as a binding stores it.
func (r Role) Ref() RoleRef { return RoleRef{ID: r.ID, Key: r.Key} }

// Resolve returns the role's grants against the catalogue: the explicit grants,
// or, for a computed role, one QualifierAll grant per selected permission in
// catalogue order.
func (r Role) Resolve(cat *Catalogue) []Grant {
	if r.Computed == nil {
		return append([]Grant(nil), r.Grants...)
	}
	var out []Grant
	for _, p := range cat.All() {
		if r.Computed(p) {
			out = append(out, Grant{Permission: p.ID, Qualifier: QualifierAll})
		}
	}
	return out
}

// Validate checks the role against the catalogue and reports every problem:
// unknown permissions, a qualifier on a permission that is not ownable, an
// unmet Requires (including a required grant that is narrower), duplicates and
// unknown or empty attribute constraints.
func (r Role) Validate(cat *Catalogue) error {
	var problems []Problem
	add := func(code ProblemCode, perm, detail string) {
		problems = append(problems, Problem{Code: code, Permission: perm, Detail: detail})
	}
	if r.Name == "" {
		add(ProblemInvalidRole, "", "name is required")
	}
	if r.Key != "" && !word.MatchString(r.Key) {
		add(ProblemInvalidRole, "", fmt.Sprintf("key %q must be a single lowercase word", r.Key))
	}
	if r.Computed != nil && len(r.Grants) > 0 {
		add(ProblemComputedWithGrants, "", "a computed role has no explicit grants")
	}

	held := map[string]Qualifier{}
	for _, g := range r.Resolve(cat) {
		p, ok := cat.Lookup(g.Permission)
		switch {
		case !ok:
			add(ProblemUnknownPermission, g.Permission, "")
			continue
		case !g.Qualifier.Valid():
			add(ProblemInvalidQualifier, g.Permission, fmt.Sprintf("qualifier %d", int(g.Qualifier)))
			continue
		case !p.Ownable() && g.Qualifier != QualifierAll:
			add(ProblemQualifierNotAllowed, g.Permission, g.Qualifier.String()+" on a permission that is not ownable")
		}
		if _, dup := held[g.Permission]; dup {
			add(ProblemDuplicateGrant, g.Permission, "")
			continue
		}
		held[g.Permission] = g.Qualifier
	}

	for _, id := range sortedKeys(held) {
		p, _ := cat.Lookup(id)
		for _, req := range p.Requires {
			rq, ok := held[req]
			if !ok {
				add(ProblemMissingRequired, id, "requires "+req)
				continue
			}
			rp, _ := cat.Lookup(req)
			if p.Ownable() && rp.Ownable() && !rq.Covers(held[id]) {
				add(ProblemRequiredTooNarrow, id, fmt.Sprintf("%s is %s but %s is %s", req, rq, id, held[id]))
			}
		}
	}

	declared := map[string]bool{}
	for _, k := range cat.AttributeKeys() {
		declared[k] = true
	}
	for _, key := range sortedKeys(r.Attrs) {
		switch {
		case !declared[key]:
			add(ProblemUnknownAttribute, "", key)
		case len(r.Attrs[key]) == 0:
			add(ProblemEmptyAttribute, "", key+": an empty list would match nothing; leave the key out to allow all")
		}
	}
	return asValidation(problems)
}

// RoleRef refers to a role in a binding: by ID for a custom role, by Key for a
// predefined one. Exactly one is set.
type RoleRef struct {
	ID  int    `json:"id,omitempty"`
	Key string `json:"key,omitempty"`
}

// Valid reports whether exactly one of ID and Key is set.
func (r RoleRef) Valid() bool { return (r.ID > 0) != (r.Key != "") }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
