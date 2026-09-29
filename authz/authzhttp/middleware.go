// Package authzhttp connects authz to gin: middleware that puts the acting
// principal in the request context, gates routes on a permission, pins the
// tenancy context, authenticates service accounts by API key, and serves the
// /me/access payload.
//
// Middleware reports failures with ctx.Error and Abort, like go-core's auth
// package, so middlewares.ErrorHandler turns them into 401 and 403 responses.
package authzhttp

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/tenancy"
	"github.com/wssto2/go-core/web"
)

func abort(ctx *gin.Context, err error) {
	_ = ctx.Error(err)
	ctx.Abort()
}

// Principals turns the authenticated identity (set by auth.Authenticated) into
// the authz principal stored in the request context. resolve maps the
// application's user type to a Principal (it may look up the user's own
// location, hence the context); return false when the identity is not one.
// Without an identity the request is rejected with 401.
func Principals(resolve func(context.Context, auth.Identifiable) (authz.Principal, bool)) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		identity, ok := auth.GetIdentifiable(ctx)
		if !ok {
			abort(ctx, apperr.Unauthorized("user not authenticated"))
			return
		}
		p, ok := resolve(ctx.Request.Context(), identity)
		if !ok || !p.Valid() {
			abort(ctx, apperr.Unauthorized("user not authenticated"))
			return
		}
		ctx.Request = ctx.Request.WithContext(authz.WithPrincipal(ctx.Request.Context(), p))
		ctx.Next()
	}
}

// Require gates a route on a permission held anywhere: a coarse check. Services
// still decide row-level access with RequireOn and Access.
func Require(a authz.Authorizer, permission string) gin.HandlerFunc {
	return RequireAny(a, permission)
}

// RequireAny passes when the principal holds at least one of the permissions.
// Any failure other than "not granted" (no principal, an unknown permission, a
// storage error) rejects the request with that error.
func RequireAny(a authz.Authorizer, permissions ...string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var denied error
		for _, permission := range permissions {
			err := a.Require(ctx.Request.Context(), permission)
			if err == nil {
				ctx.Next()
				return
			}
			if !errors.Is(err, authz.ErrForbidden) {
				abort(ctx, err)
				return
			}
			denied = err
		}
		if denied == nil {
			denied = authz.Deny("")
		}
		abort(ctx, denied)
	}
}

// PinTenant puts the principal's tenant in the tenancy context when every one of
// its bindings is pinned to the same tenant, so tenancy.ScopeByTenant keeps
// working as a second wall. It does nothing for a principal at the root or with
// several tenants. Run it after Principals.
func PinTenant(e *authz.Engine) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		p, ok := authz.PrincipalFrom(ctx.Request.Context())
		if !ok {
			abort(ctx, apperr.Unauthorized("user not authenticated"))
			return
		}
		eff, err := e.Effective(ctx.Request.Context(), p.Subject)
		if err != nil {
			abort(ctx, err)
			return
		}
		if tenant, ok := eff.Tenant(); ok {
			ctx.Request = ctx.Request.WithContext(tenancy.WithTenantID(ctx.Request.Context(), tenant))
		}
		ctx.Next()
	}
}

// MeAccess serves the caller's access as authz.MyAccess: every permission held,
// with its widest scope and qualifier. Mount it at /me/access behind
// Principals.
func MeAccess(e *authz.Engine) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		access, err := e.MyAccess(ctx.Request.Context())
		if err != nil {
			abort(ctx, err)
			return
		}
		web.JSON(ctx, 200, access)
	}
}
