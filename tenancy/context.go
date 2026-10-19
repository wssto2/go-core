// go-core/tenancy/context.go
package tenancy

import "context"

type contextKey string

const (
	tenantIDKey   contextKey = "tenant_id"
	allTenantsKey contextKey = "all_tenants"
)

// WithTenantID stores the tenant ID in context (e.g. set by middleware after auth).
func WithTenantID(ctx context.Context, tenantID int) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

// WithAllTenants marks the context as deliberately crossing tenants (a
// super-admin, or a principal bound at the organization root). It is the only
// way ScopeByTenant returns rows of every tenant: a context with neither a
// tenant ID nor this marker sees nothing. A tenant ID in the same context wins,
// so the narrower scope applies.
func WithAllTenants(ctx context.Context) context.Context {
	return context.WithValue(ctx, allTenantsKey, true)
}

// AllTenantsFromContext reports whether WithAllTenants was applied.
func AllTenantsFromContext(ctx context.Context) bool {
	all, _ := ctx.Value(allTenantsKey).(bool)
	return all
}

// TenantIDFromContext retrieves the tenant ID. Returns 0 and false if not set.
func TenantIDFromContext(ctx context.Context) (int, bool) {
	id, ok := ctx.Value(tenantIDKey).(int)
	return id, ok && id > 0
}
