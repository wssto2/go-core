// go-core/tenancy/middleware.go
package tenancy

import (
	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/auth"
)

// FromAuthenticatedUser is a middleware that establishes the tenant of the
// authenticated user in the context.
//
// This assumes auth.Authenticated has already run (i.e. a user is in the context).
// The user must implement TenantAware:
//
//   - HasTenant() true: the tenant ID is stored (WithTenantID);
//   - HasTenant() false: the user is a super-admin without a tenant, and the
//     context is marked as crossing tenants (WithAllTenants);
//   - a user that is not TenantAware, or no user at all: nothing is set, so
//     ScopeByTenant matches nothing.
//
// Usage in your router:
//
//	api := engine.Group("/api")
//	api.Use(auth.Authenticated(...))
//	api.Use(tenancy.FromAuthenticatedUser())
func FromAuthenticatedUser() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		user, ok := auth.GetIdentifiable(ctx)
		if !ok {
			ctx.Next()
			return
		}

		if ta, ok := user.(TenantAware); ok {
			if ta.HasTenant() {
				ctx.Request = ctx.Request.WithContext(WithTenantID(ctx.Request.Context(), ta.GetTenantID()))
			} else {
				ctx.Request = ctx.Request.WithContext(WithAllTenants(ctx.Request.Context()))
			}
		}
		ctx.Next()
	}
}
