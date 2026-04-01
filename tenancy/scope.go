// go-core/tenancy/scope.go
package tenancy

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// ScopeByTenant returns a GORM scope that filters by a tenant column.
//
// Usage:
//
//	db.Scopes(tenancy.ScopeByTenant(ctx, "dealer_id")).Find(&vehicles)
//
// If the context has no tenant ID, the scope is a no-op.
// This is intentional: system admins (no tenant) see all data.
// Column must be a hardcoded string literal, not user-provided input.
// Example: tenancy.ScopeByTenant(ctx, "dealer_id")
func ScopeByTenant(ctx context.Context, column string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		tenantID, ok := TenantIDFromContext(ctx)
		if !ok {
			return db // no tenant scope — caller sees all rows (super-admin)
		}
		return db.Where(fmt.Sprintf("`%s` = ?", column), tenantID)
	}
}

// RequireTenantScope is like ScopeByTenant but returns an error if no tenant ID is present.
// Use this in services where being unscoped would be a security problem.
// column must be a hardcoded string literal, not user-provided input.
// Example: tenancy.RequireTenantScope(ctx, "dealer_id")
func RequireTenantScope(ctx context.Context, column string) (func(*gorm.DB) *gorm.DB, error) {
	tenantID, ok := TenantIDFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("tenancy: operation requires a tenant scope but no tenant ID is in context")
	}
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(fmt.Sprintf("`%s` = ?", column), tenantID)
	}, nil
}
