package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// Scope is a place in the application's hierarchy: a level and an ID. The root
// level has a single scope whose ID is zero (Hierarchy.Root).
type Scope struct {
	Level string `json:"level"`
	ID    int    `json:"id,omitempty"`
}

// IsZero reports whether the scope is unset.
func (s Scope) IsZero() bool { return s.Level == "" && s.ID == 0 }

func (s Scope) String() string {
	if s.ID == 0 {
		return s.Level
	}
	return fmt.Sprintf("%s:%d", s.Level, s.ID)
}

// Hierarchy declares the levels of an application, widest first. For example
// NewHierarchy("organization", "dealer", "location"). The first level is the
// root: a binding there crosses tenants.
type Hierarchy struct {
	levels []string
	depth  map[string]int
	tenant string
}

// NewHierarchy declares the levels, root first. It needs at least the root
// (an application without tenancy has only "organization"), each level a single
// lowercase word, all distinct.
func NewHierarchy(levels ...string) (*Hierarchy, error) {
	if len(levels) < 1 {
		return nil, errors.New("authz: a hierarchy needs at least a root level, for example NewHierarchy(\"organization\")")
	}
	h := &Hierarchy{levels: append([]string(nil), levels...), depth: make(map[string]int, len(levels))}
	for i, l := range levels {
		if !word.MatchString(l) {
			return nil, fmt.Errorf("authz: level %q must be a single lowercase word", l)
		}
		if _, dup := h.depth[l]; dup {
			return nil, fmt.Errorf("authz: level %q declared twice", l)
		}
		h.depth[l] = i
	}
	return h, nil
}

// WithTenantLevel names the level that is a tenant (for example "dealer"). A
// binding below the root pins the tenant found at that level of its chain;
// FeatureResolver is asked about that tenant, and PinTenant middleware puts it
// in the tenancy context. It returns a copy.
func (h *Hierarchy) WithTenantLevel(level string) (*Hierarchy, error) {
	d, ok := h.depth[level]
	if !ok {
		return nil, fmt.Errorf("authz: tenant level %q is not a level", level)
	}
	if d == 0 {
		return nil, errors.New("authz: the root level cannot be the tenant level")
	}
	cp := *h
	cp.tenant = level
	return &cp, nil
}

// Levels returns the levels, root first.
func (h *Hierarchy) Levels() []string { return append([]string(nil), h.levels...) }

// RootLevel is the widest level.
func (h *Hierarchy) RootLevel() string { return h.levels[0] }

// LeafLevel is the narrowest level. The OwnLocation qualifier compares the
// principal's Location against a record's location at this level.
func (h *Hierarchy) LeafLevel() string { return h.levels[len(h.levels)-1] }

// TenantLevel is the level set by WithTenantLevel, or "".
func (h *Hierarchy) TenantLevel() string { return h.tenant }

// Root is the single scope of the root level.
func (h *Hierarchy) Root() Scope { return Scope{Level: h.levels[0]} }

// Depth returns the level's position, 0 for the root; false for an unknown level.
func (h *Hierarchy) Depth(level string) (int, bool) { d, ok := h.depth[level]; return d, ok }

// IsRoot reports whether the scope is the root scope.
func (h *Hierarchy) IsRoot(s Scope) bool { return s.Level == h.levels[0] }

// Check validates a scope against the hierarchy: a known level, an ID of zero
// at the root and a positive ID below it.
func (h *Hierarchy) Check(s Scope) error {
	d, ok := h.depth[s.Level]
	switch {
	case !ok:
		return fmt.Errorf("%w: unknown level %q", ErrInvalidScope, s.Level)
	case d == 0 && s.ID != 0:
		return fmt.Errorf("%w: the %s scope has no ID", ErrInvalidScope, s.Level)
	case d > 0 && s.ID <= 0:
		return fmt.Errorf("%w: %s needs a positive ID", ErrInvalidScope, s.Level)
	}
	return nil
}

// Wider reports whether level a is strictly wider than level b.
func (h *Hierarchy) Wider(a, b string) bool { return h.depth[a] < h.depth[b] }

// ScopeResolver is implemented by the application: it knows how its places
// nest. Parent returns the scope one level up (a location's dealer, a dealer's
// organization). It is never called for the root scope. It must return an error
// wrapping ErrScopeNotFound when the scope does not exist.
type ScopeResolver interface {
	Parent(ctx context.Context, s Scope) (Scope, error)
}

// Chain is a scope followed by its ancestors up to and including the root:
// location 7, dealer 3, organization. It is how containment is decided.
type Chain []Scope

// Scope is the chain's first (most specific) element.
func (c Chain) Scope() Scope {
	if len(c) == 0 {
		return Scope{}
	}
	return c[0]
}

// Within reports whether the chain's scope lies inside s: s is the scope itself
// or one of its ancestors. The root contains everything.
func (c Chain) Within(s Scope) bool {
	return slices.Contains(c, s)
}

// At returns the chain's element at the given level.
func (c Chain) At(level string) (Scope, bool) {
	for _, x := range c {
		if x.Level == level {
			return x, true
		}
	}
	return Scope{}, false
}

// Chain resolves the scope's ancestors using the resolver. Ancestors the
// caller already knows can be passed to skip the resolver.
func (h *Hierarchy) Chain(ctx context.Context, res ScopeResolver, s Scope, known ...Scope) (Chain, error) {
	if err := h.Check(s); err != nil {
		return nil, err
	}
	chain := Chain{s}
	for _, k := range known {
		if err := h.Check(k); err != nil {
			return nil, err
		}
	}
	cur, curKnown := s, false
	for !h.IsRoot(cur) {
		want := h.levels[h.depth[cur.Level]-1]
		next, ok := findLevel(known, want)
		if !ok && curKnown && want == h.levels[0] {
			next, ok = h.Root(), true // a known scope below the root: its parent is the root
		}
		if !ok {
			var err error
			if next, err = res.Parent(ctx, cur); err != nil {
				return nil, fmt.Errorf("authz: parent of %s: %w", cur, err)
			}
			if next.Level != want {
				return nil, fmt.Errorf("%w: parent of %s is %s, expected level %q", ErrInvalidScope, cur, next, want)
			}
			if err := h.Check(next); err != nil {
				return nil, err
			}
		}
		chain = append(chain, next)
		cur, curKnown = next, ok
	}
	return chain, nil
}

func findLevel(scopes []Scope, level string) (Scope, bool) {
	for _, s := range scopes {
		if s.Level == level {
			return s, true
		}
	}
	return Scope{}, false
}

// tenantOf returns the tenant scope in the chain, if the hierarchy has a tenant
// level and the chain reaches it.
func (h *Hierarchy) tenantOf(c Chain) (Scope, bool) {
	if h.tenant == "" {
		return Scope{}, false
	}
	return c.At(h.tenant)
}
