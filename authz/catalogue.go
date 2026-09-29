package authz

import (
	"errors"
	"fmt"
	"sort"
)

// Catalogue is the set of permissions an application declares at start-up. It
// is a value, never a global: build one, Define every permission, then hand it
// to NewEngine, which validates and freezes it.
//
// A Catalogue is not safe for concurrent Define calls; once frozen it is
// read-only and safe for concurrent use.
type Catalogue struct {
	perms  map[string]*Permission
	order  []string
	frozen bool
}

// NewCatalogue returns an empty catalogue.
func NewCatalogue() *Catalogue {
	return &Catalogue{perms: make(map[string]*Permission)}
}

// Define registers a permission. It rejects a malformed identifier, a duplicate
// and inconsistent options at once; Requires targets are checked by Validate
// (they may be defined later).
func (c *Catalogue) Define(id string, opts ...DefineOption) error {
	if c.frozen {
		return fmt.Errorf("authz: catalogue is frozen, cannot define %q", id)
	}
	if err := checkIdentifier(id); err != nil {
		return &ValidationError{Problems: []Problem{{Code: ProblemInvalidIdentifier, Permission: id, Detail: err.Error()}}}
	}
	if _, dup := c.perms[id]; dup {
		return &ValidationError{Problems: []Problem{{Code: ProblemDuplicatePermission, Permission: id}}}
	}
	p := &Permission{ID: id}
	for _, opt := range opts {
		opt(p)
	}
	if problems := checkPermission(p); len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	c.perms[id] = p
	c.order = append(c.order, id)
	return nil
}

// MustDefine is Define for start-up code: it panics on error.
func (c *Catalogue) MustDefine(id string, opts ...DefineOption) {
	if err := c.Define(id, opts...); err != nil {
		panic(err)
	}
}

func checkPermission(p *Permission) []Problem {
	var problems []Problem
	bad := func(detail string) {
		problems = append(problems, Problem{Code: ProblemInconsistentMetadata, Permission: p.ID, Detail: detail})
	}
	if p.UnownedIsOwn && p.RecordType == "" {
		bad("UnownedIsOwn needs Ownable")
	}
	if p.Feature != "" && !word.MatchString(p.Feature) {
		bad(fmt.Sprintf("feature %q must be a single lowercase word", p.Feature))
	}
	seen := map[string]bool{}
	for _, a := range p.Attributes {
		if !word.MatchString(a) {
			bad(fmt.Sprintf("attribute %q must be a single lowercase word", a))
		}
		if seen[a] {
			bad(fmt.Sprintf("attribute %q listed twice", a))
		}
		seen[a] = true
	}
	for _, r := range p.Requires {
		if err := checkIdentifier(r); err != nil {
			bad("requires: " + err.Error())
		}
		if r == p.ID {
			bad("requires itself")
		}
	}
	return problems
}

// Lookup returns the permission with the given identifier.
func (c *Catalogue) Lookup(id string) (Permission, bool) {
	p, ok := c.perms[id]
	if !ok {
		return Permission{}, false
	}
	return *p, true
}

// All returns every permission in definition order.
func (c *Catalogue) All() []Permission {
	out := make([]Permission, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, *c.perms[id])
	}
	return out
}

// Len is the number of permissions.
func (c *Catalogue) Len() int { return len(c.order) }

// AttributeKeys returns the role attribute keys any permission declares, sorted.
func (c *Catalogue) AttributeKeys() []string {
	set := map[string]bool{}
	for _, p := range c.perms {
		for _, a := range p.Attributes {
			set[a] = true
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// HasFeatures reports whether any permission is feature-gated.
func (c *Catalogue) HasFeatures() bool {
	for _, p := range c.perms {
		if p.Feature != "" {
			return true
		}
	}
	return false
}

// Validate checks the whole catalogue: every Requires target exists and the
// Requires graph has no cycle.
func (c *Catalogue) Validate() error {
	var problems []Problem
	for _, id := range c.order {
		for _, r := range c.perms[id].Requires {
			if _, ok := c.perms[r]; !ok {
				problems = append(problems, Problem{Code: ProblemUnknownRequirement, Permission: id, Detail: "requires unknown " + r})
			}
		}
	}
	if len(problems) == 0 {
		problems = append(problems, c.requirementCycles()...)
	}
	return asValidation(problems)
}

// requirementCycles finds Requires cycles with a depth-first search.
func (c *Catalogue) requirementCycles() []Problem {
	const (
		unseen = iota
		visiting
		done
	)
	state := make(map[string]int, len(c.perms))
	var problems []Problem
	var visit func(id string)
	visit = func(id string) {
		state[id] = visiting
		for _, r := range c.perms[id].Requires {
			switch state[r] {
			case visiting:
				problems = append(problems, Problem{Code: ProblemRequirementCycle, Permission: id, Detail: "requires " + r})
			case unseen:
				visit(r)
			}
		}
		state[id] = done
	}
	for _, id := range c.order {
		if state[id] == unseen {
			visit(id)
		}
	}
	return problems
}

// freeze validates the catalogue and makes it read-only.
func (c *Catalogue) freeze() error {
	if err := c.Validate(); err != nil {
		return err
	}
	c.frozen = true
	return nil
}

// errNilCatalogue is returned by constructors given no catalogue.
var errNilCatalogue = errors.New("authz: catalogue is required")
