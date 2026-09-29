package authz

import "context"

// FeatureResolver answers whether a tenant has a feature switched on: an
// entitlement, not authorization. A permission declared with Feature(name)
// passes only when a binding grants it and the resolver says yes.
//
// tenant is the scope at the hierarchy's tenant level (a dealer) when the
// hierarchy declares one and the check has a scope below it; otherwise it is the
// binding's own scope, which for a root binding is the root scope.
type FeatureResolver interface {
	Enabled(ctx context.Context, tenant Scope, feature string) (bool, error)
}

// FeatureFunc adapts a function to FeatureResolver.
type FeatureFunc func(ctx context.Context, tenant Scope, feature string) (bool, error)

// Enabled calls f.
func (f FeatureFunc) Enabled(ctx context.Context, tenant Scope, feature string) (bool, error) {
	return f(ctx, tenant, feature)
}

// featureOn evaluates the permission's feature gate for a clause. chain is the
// chain to take the tenant from (the resource's, or the clause's).
func (e *Engine) featureOn(ctx context.Context, perm Permission, fallback Scope, chain Chain) (bool, error) {
	if perm.Feature == "" {
		return true, nil
	}
	tenant := fallback
	if t, ok := e.h.tenantOf(chain); ok {
		tenant = t
	}
	return e.features.Enabled(ctx, tenant, perm.Feature)
}
