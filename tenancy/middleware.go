// go-core/tenancy/middleware.go
package tenancy

import (
	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/auth"
)

// FromAuthenticatedUser is a middleware that extracts the tenant ID from
// the authenticated user and stores it in the context.
//
// This assumes auth.Authenticated has already run (i.e. a user is in the context).
// The user must implement TenantAware.
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

		// Check if the user implements TenantAware
		if ta, ok := user.(TenantAware); ok && ta.HasTenant() {
			ctx.Request = ctx.Request.WithContext(
				WithTenantID(ctx.Request.Context(), ta.GetTenantID()),
			)
		}

		ctx.Next()
	}
}
