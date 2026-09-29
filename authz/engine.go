package authz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/wssto2/go-core/apperr"
)

// Authorizer is what services depend on. *Engine implements it, and so does
// authztest's fake, which keeps a service's unit tests free of a database.
type Authorizer interface {
	// Require passes when some binding grants the permission anywhere.
	Require(ctx context.Context, permission string) error
	// RequireOn passes when some binding grants the permission on this record.
	RequireOn(ctx context.Context, permission string, res Resource) error
	// Access returns the clauses a list of records must be filtered by.
	Access(ctx context.Context, permission string) (AccessSet, error)
}

// Config wires an Engine.
type Config struct {
	// Catalogue declares the permissions. NewEngine validates and freezes it.
	Catalogue *Catalogue
	// Hierarchy declares the levels.
	Hierarchy *Hierarchy
	// Roles are the predefined (code-defined) roles, each with a Key.
	Roles []Role
	// Store supplies bindings and custom roles.
	Store Reader
	// Resolver finds a scope's parent.
	Resolver ScopeResolver
	// Features answers feature gates. Required when a permission has Feature.
	Features FeatureResolver
	// Cache holds effective access. A default is created when nil.
	Cache *Cache
	// Logger receives warnings about bindings that were skipped. Defaults to slog.Default().
	Logger *slog.Logger
}

// Engine decides access. Deny by default: nothing is allowed unless a binding
// grants it. It is safe for concurrent use.
type Engine struct {
	cat      *Catalogue
	h        *Hierarchy
	roles    map[string]Role
	store    Reader
	resolver ScopeResolver
	features FeatureResolver
	cache    *Cache
	log      *slog.Logger
}

// NewEngine validates the configuration, freezes the catalogue and returns the
// engine. It fails when the catalogue is invalid, a predefined role does not
// validate, or a feature-gated permission has no FeatureResolver.
func NewEngine(cfg Config) (*Engine, error) {
	switch {
	case cfg.Catalogue == nil:
		return nil, errNilCatalogue
	case cfg.Hierarchy == nil:
		return nil, errors.New("authz: hierarchy is required")
	case cfg.Store == nil:
		return nil, errors.New("authz: store is required")
	case cfg.Resolver == nil:
		return nil, errors.New("authz: scope resolver is required")
	case cfg.Catalogue.HasFeatures() && cfg.Features == nil:
		return nil, errors.New("authz: a permission has a Feature but no FeatureResolver is configured")
	}
	if err := cfg.Catalogue.freeze(); err != nil {
		return nil, err
	}
	e := &Engine{
		cat: cfg.Catalogue, h: cfg.Hierarchy, store: cfg.Store, resolver: cfg.Resolver,
		features: cfg.Features, cache: cfg.Cache, log: cfg.Logger,
		roles: make(map[string]Role, len(cfg.Roles)),
	}
	if e.cache == nil {
		e.cache = NewCache(DefaultCacheTTL)
	}
	if e.log == nil {
		e.log = slog.Default()
	}
	for _, r := range cfg.Roles {
		if r.Key == "" || r.ID != 0 {
			return nil, fmt.Errorf("authz: predefined role %q needs a Key and no ID", r.Name)
		}
		if _, dup := e.roles[r.Key]; dup {
			return nil, fmt.Errorf("authz: predefined role %q defined twice", r.Key)
		}
		if err := r.Validate(e.cat); err != nil {
			return nil, fmt.Errorf("authz: predefined role %q: %w", r.Key, err)
		}
		e.roles[r.Key] = r
	}
	return e, nil
}

// Catalogue returns the (frozen) catalogue.
func (e *Engine) Catalogue() *Catalogue { return e.cat }

// Hierarchy returns the hierarchy.
func (e *Engine) Hierarchy() *Hierarchy { return e.h }

// PredefinedRole returns a code-defined role by key.
func (e *Engine) PredefinedRole(key string) (Role, bool) { r, ok := e.roles[key]; return r, ok }

// PredefinedRoles returns the code-defined roles, sorted by key.
func (e *Engine) PredefinedRoles() []Role {
	out := make([]Role, 0, len(e.roles))
	for _, k := range sortedKeys(e.roles) {
		out = append(out, e.roles[k])
	}
	return out
}

// Evict drops one subject's cached access.
func (e *Engine) Evict(s Subject) { e.cache.Evict(s) }

// EvictRole drops the cached access of every holder of a custom role.
func (e *Engine) EvictRole(roleID int) { e.cache.EvictRole(roleID) }

// EvictAll empties the cache (after a predefined role or the hierarchy changes).
func (e *Engine) EvictAll() { e.cache.EvictAll() }

// Effective returns everything a subject may do, from the cache when possible.
func (e *Engine) Effective(ctx context.Context, s Subject) (*Effective, error) {
	if eff, ok := e.cache.get(s); ok {
		return eff, nil
	}
	epoch := e.cache.begin()
	bindings, err := e.store.BindingsFor(ctx, s)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("authz: load bindings of %s: %w", s, err))
	}
	eff, err := e.build(ctx, s, bindings, e.lookupRole)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	e.cache.put(s, eff, epoch)
	return eff, nil
}

type roleLookup func(ctx context.Context, ref RoleRef) (Role, error)

func (e *Engine) lookupRole(ctx context.Context, ref RoleRef) (Role, error) {
	if ref.Key != "" {
		r, ok := e.roles[ref.Key]
		if !ok {
			return Role{}, fmt.Errorf("%w: key %q", ErrRoleNotFound, ref.Key)
		}
		return r, nil
	}
	return e.store.Role(ctx, ref.ID)
}

// build turns bindings into an Effective. A binding that cannot be resolved (an
// unknown role, a scope that no longer exists, a malformed reference) grants
// nothing and is logged: deny by default. Any other error is returned.
func (e *Engine) build(ctx context.Context, s Subject, bindings []Binding, lookup roleLookup) (*Effective, error) {
	eff := &Effective{
		subject: s, grants: map[string][]Clause{},
		roleIDs: map[int]struct{}{}, tenants: map[int]struct{}{},
	}
	roles := map[RoleRef]Role{}
	for _, b := range bindings {
		if !b.Role.Valid() {
			e.skip(b, "role reference must set exactly one of ID and Key", nil)
			continue
		}
		if b.Role.ID > 0 {
			eff.roleIDs[b.Role.ID] = struct{}{}
		}
		role, ok := roles[b.Role]
		if !ok {
			var err error
			role, err = lookup(ctx, b.Role)
			if errors.Is(err, ErrRoleNotFound) {
				e.skip(b, "role not found", err)
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("authz: load role %+v: %w", b.Role, err)
			}
			roles[b.Role] = role
		}
		chain, err := e.h.Chain(ctx, e.resolver, b.Scope)
		if errors.Is(err, ErrScopeNotFound) || errors.Is(err, ErrInvalidScope) {
			e.skip(b, "scope cannot be resolved", err)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("authz: resolve scope %s: %w", b.Scope, err)
		}
		if e.addClauses(eff, b, role, chain) {
			switch tenant, ok := e.h.tenantOf(chain); {
			case e.h.IsRoot(b.Scope):
				eff.hasRoot = true
			case ok:
				eff.tenants[tenant.ID] = struct{}{}
			default:
				eff.unpinned = true
			}
		}
	}
	return eff, nil
}

// addClauses adds the clauses a binding's role yields and reports whether there
// were any. Grants that do not hold up against the catalogue are ignored.
func (e *Engine) addClauses(eff *Effective, b Binding, role Role, chain Chain) bool {
	added := false
	for _, g := range role.Resolve(e.cat) {
		p, ok := e.cat.Lookup(g.Permission)
		if !ok || !g.Qualifier.Valid() || (!p.Ownable() && g.Qualifier != QualifierAll) {
			continue
		}
		if p.OrganizationOnly && !e.h.IsRoot(b.Scope) {
			continue
		}
		eff.grants[p.ID] = append(eff.grants[p.ID], Clause{
			Scope: b.Scope, Chain: chain, Qualifier: g.Qualifier,
			Attrs:  attrsFor(p, role.Attrs),
			Source: Source{BindingID: b.ID, Role: b.Role, RoleName: role.Name},
		})
		added = true
	}
	return added
}

// attrsFor keeps the role's constraints on attributes the permission declares.
func attrsFor(p Permission, roleAttrs map[string][]string) map[string][]string {
	var out map[string][]string
	for _, key := range p.Attributes {
		values, ok := roleAttrs[key]
		if !ok {
			continue
		}
		if out == nil {
			out = map[string][]string{}
		}
		out[key] = append([]string(nil), values...)
	}
	return out
}

func (e *Engine) skip(b Binding, why string, err error) {
	e.log.Warn("authz: binding grants nothing", "binding", b.ID, "subject", b.Subject.String(),
		"role", fmt.Sprintf("%+v", b.Role), "scope", b.Scope.String(), "why", why, "error", err)
}

// prepare resolves what every check needs: the principal, the permission's
// metadata and the principal's effective access.
func (e *Engine) prepare(ctx context.Context, permission string) (Principal, Permission, *Effective, error) {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		return Principal{}, Permission{}, nil, unauthenticated()
	}
	perm, ok := e.cat.Lookup(permission)
	if !ok {
		return Principal{}, Permission{}, nil, apperr.Internal(fmt.Errorf("%w: %q", ErrUnknownPermission, permission))
	}
	eff, err := e.Effective(ctx, p.Subject)
	if err != nil {
		return Principal{}, Permission{}, nil, err
	}
	return p, perm, eff, nil
}
