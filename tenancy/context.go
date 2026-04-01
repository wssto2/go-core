// go-core/tenancy/context.go
package tenancy

import "context"

type contextKey string

const (
	tenantIDKey   contextKey = "tenant_id"
	tenantNameKey contextKey = "tenant_name"
)

// WithTenantID stores the tenant ID in context (e.g. set by middleware after auth).
func WithTenantID(ctx context.Context, tenantID int) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

// WithTenantName stores the tenant name in context (optional, useful for logging).
func WithTenantName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, tenantNameKey, name)
}

// TenantIDFromContext retrieves the tenant ID. Returns 0 and false if not set.
func TenantIDFromContext(ctx context.Context) (int, bool) {
	id, ok := ctx.Value(tenantIDKey).(int)
	return id, ok && id > 0
}

// MustTenantIDFromContext retrieves the tenant ID. Panics if not set.
// Use only in code paths that are guaranteed to have a tenant (i.e. after TenantMiddleware).
func MustTenantIDFromContext(ctx context.Context) int {
	id, ok := TenantIDFromContext(ctx)
	if !ok {
		panic("tenancy: tenant ID not in context — is TenantMiddleware applied?")
	}
	return id
}

// TenantNameFromContext retrieves the tenant name. Returns "" if not set.
func TenantNameFromContext(ctx context.Context) string {
	name, _ := ctx.Value(tenantNameKey).(string)
	return name
}
