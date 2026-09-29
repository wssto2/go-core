package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/wssto2/go-core/apperr"
)

// Resource describes one record for RequireOn.
type Resource struct {
	// Scope is where the record lives, as specifically as known: a location, or
	// a dealer. The zero scope means "organization-wide", which only a root
	// binding reaches.
	Scope Scope
	// Ancestors are scopes above Scope the caller already knows (the record's
	// dealer), so the ScopeResolver is not asked.
	Ancestors []Scope
	// Owner is the ID of the user owning the record; zero means unowned.
	Owner int
	// OwnerLocation is the ID, at the hierarchy's leaf level, of the record's
	// location, for the OwnLocation qualifier.
	OwnerLocation int
	// Attrs are the record's attribute values (for example "vehiclekind": "used").
	Attrs map[string]string
}

// AccessSet is what a list must be filtered by: the clauses through which the
// principal holds the permission. It has no clauses when access is denied; an
// empty set matches nothing. Use authzgorm.Filter to turn it into a WHERE.
type AccessSet struct {
	Permission string
	// RecordType is the permission's ownable record type, if any.
	RecordType string
	// UnownedIsOwn is the permission's flag: unowned records count as Own.
	UnownedIsOwn bool
	Principal    Principal
	// Clauses are pruned: none is covered by another.
	Clauses []Clause
}

// Empty reports whether nothing is accessible.
func (a AccessSet) Empty() bool { return len(a.Clauses) == 0 }

// Unrestricted reports whether some clause reaches every record: root scope,
// all records, no attribute constraint.
func (a AccessSet) Unrestricted(h *Hierarchy) bool {
	for _, c := range a.Clauses {
		if h.IsRoot(c.Scope) && c.Qualifier == QualifierAll && len(c.Attrs) == 0 {
			return true
		}
	}
	return false
}

// Widest returns the widest qualifier among the clauses (zero when empty).
func (a AccessSet) Widest() Qualifier {
	var w Qualifier
	for _, c := range a.Clauses {
		if c.Qualifier > w {
			w = c.Qualifier
		}
	}
	return w
}

// Require passes when some binding of the principal grants the permission
// anywhere. Row-level decisions belong to RequireOn and Access.
func (e *Engine) Require(ctx context.Context, permission string) error {
	_, perm, eff, err := e.prepare(ctx, permission)
	if err != nil {
		return err
	}
	for _, c := range eff.grants[permission] {
		ok, err := e.featureOn(ctx, perm, c.Scope, c.Chain)
		if err != nil {
			return apperr.Internal(fmt.Errorf("authz: feature %q: %w", perm.Feature, err))
		}
		if ok {
			return nil
		}
	}
	return forbidden(permission)
}

// RequireOn passes when some binding grants the permission on this record: the
// record lies within the binding's scope, the qualifier reaches the record's
// owner or location, the role's attribute constraints allow the record's
// attributes and any feature gate is open.
func (e *Engine) RequireOn(ctx context.Context, permission string, res Resource) error {
	p, perm, eff, err := e.prepare(ctx, permission)
	if err != nil {
		return err
	}
	clauses := eff.grants[permission]
	var resChain Chain
	chainDone := false
	for _, c := range clauses {
		if !e.h.IsRoot(c.Scope) || perm.Feature != "" {
			if !chainDone {
				chainDone = true
				if resChain, err = e.resourceChain(ctx, res); err != nil {
					return err
				}
			}
			if !resChain.Within(c.Scope) {
				continue
			}
		}
		if !matchRecord(c, p, perm, res) {
			continue
		}
		fallback, chain := c.Scope, c.Chain
		if resChain != nil && !res.Scope.IsZero() {
			fallback, chain = res.Scope, resChain
		}
		ok, err := e.featureOn(ctx, perm, fallback, chain)
		if err != nil {
			return apperr.Internal(fmt.Errorf("authz: feature %q: %w", perm.Feature, err))
		}
		if ok {
			return nil
		}
	}
	return forbidden(permission)
}

// resourceChain resolves the record's ancestors. A zero scope yields a nil chain
// that lies within no non-root scope.
func (e *Engine) resourceChain(ctx context.Context, res Resource) (Chain, error) {
	if res.Scope.IsZero() {
		return Chain{e.h.Root()}, nil
	}
	chain, err := e.h.Chain(ctx, e.resolver, res.Scope, res.Ancestors...)
	if err != nil {
		if errors.Is(err, ErrScopeNotFound) || errors.Is(err, ErrInvalidScope) {
			return Chain{}, nil // an unknown place is within nothing: deny
		}
		return nil, apperr.Internal(err)
	}
	return chain, nil
}

// matchRecord checks qualifier and attributes of a clause against a record.
func matchRecord(c Clause, p Principal, perm Permission, res Resource) bool {
	switch c.Qualifier {
	case QualifierAll:
	case QualifierOwnLocation:
		if p.Location == 0 || res.OwnerLocation != p.Location {
			return false
		}
	case QualifierOwn:
		if p.Kind != KindUser {
			return false
		}
		if res.Owner == p.ID {
			break
		}
		if !perm.UnownedIsOwn || res.Owner != 0 {
			return false
		}
	default:
		return false
	}
	for key, allowed := range c.Attrs {
		v, ok := res.Attrs[key]
		if !ok || !slices.Contains(allowed, v) {
			return false
		}
	}
	return true
}

// Access returns the clauses through which the principal holds the permission,
// with feature-gated ones removed and covered ones pruned (widest wins). It
// never fails for lack of access: a denied principal gets an empty set, which
// filters to nothing. An unauthenticated context is an error.
func (e *Engine) Access(ctx context.Context, permission string) (AccessSet, error) {
	p, perm, eff, err := e.prepare(ctx, permission)
	if err != nil {
		return AccessSet{}, err
	}
	set := AccessSet{Permission: permission, RecordType: perm.RecordType, UnownedIsOwn: perm.UnownedIsOwn, Principal: p}
	var open []Clause
	for _, c := range eff.grants[permission] {
		ok, err := e.featureOn(ctx, perm, c.Scope, c.Chain)
		if err != nil {
			return AccessSet{}, apperr.Internal(fmt.Errorf("authz: feature %q: %w", perm.Feature, err))
		}
		if ok {
			open = append(open, c)
		}
	}
	set.Clauses = prune(open)
	return set, nil
}
