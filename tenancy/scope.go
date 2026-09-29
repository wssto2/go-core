// go-core/tenancy/scope.go
package tenancy

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// ScopeByTenant returns a GORM scope that filters by a tenant column. It fails
// closed:
//
//   - a tenant ID in the context (WithTenantID) restricts to that tenant;
//   - the all-tenants marker (WithAllTenants) and no tenant ID means no
//     restriction: the caller deliberately crosses tenants;
//   - neither matches nothing (WHERE 1 = 0). A request whose tenant was never
//     established must not read every tenant's rows.
//
// Usage:
//
//	db.Scopes(tenancy.ScopeByTenant(ctx, "dealer_id")).Find(&vehicles)
//
// Column must be a hardcoded string literal, not user-provided input.
// Example: tenancy.ScopeByTenant(ctx, "dealer_id")
func ScopeByTenant(ctx context.Context, column string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if tenantID, ok := TenantIDFromContext(ctx); ok {
			return db.Where(db.Statement.Quote(column)+" = ?", tenantID)
		}
		if AllTenantsFromContext(ctx) {
			return db // deliberate cross-tenant access (super-admin, organization root)
		}
		return db.Where("1 = 0") // tenant never established: match nothing
	}
}

// RequireTenantScope is like ScopeByTenant but returns an error unless a tenant ID
// is present. The all-tenants marker does not satisfy it: use it on paths that must
// never run across tenants.
// Use this in services where being unscoped would be a security problem.
// column must be a hardcoded string literal, not user-provided input.
// Example: tenancy.RequireTenantScope(ctx, "dealer_id")
func RequireTenantScope(ctx context.Context, column string) (func(*gorm.DB) *gorm.DB, error) {
	tenantID, ok := TenantIDFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("tenancy: operation requires a tenant scope but no tenant ID is in context")
	}
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(db.Statement.Quote(column)+" = ?", tenantID)
	}, nil
}
