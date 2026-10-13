package http

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/tenancy"
)

type authenticatedKey struct{}

// AuthenticatedFrom returns the account and session the Authentication
// middleware found for the request in ctx.
func AuthenticatedFrom(ctx context.Context) (account.Authenticated, bool) {
	who, ok := ctx.Value(authenticatedKey{}).(account.Authenticated)

	return who, ok
}

// Authentication is the middleware that authenticates every route that is not
// Public: it resolves the access token (bearer header or cookie) to a live
// session of an active account, and leaves in the request context
//
//   - the account and session (AuthenticatedFrom),
//   - the account as go-core's auth identity (auth.UserFromContext),
//   - the authz principal the permission checks act as.
//
// A request without a good token is rejected with 401, reason
// identity.session.invalid.
func Authentication(signIn *account.SignIn, cookies Cookies, principal PrincipalOf) gin.HandlerFunc {
	cookies = cookies.withDefaults()

	if principal == nil {
		principal = DefaultPrincipal
	}

	return func(c *gin.Context) {
		ctx := c.Request.Context()

		cookie, _ := c.Cookie(cookies.Access)

		who, err := signIn.Authenticate(ctx, tokenOf(c.GetHeader("Authorization"), cookie))
		if err != nil {
			_ = c.Error(err)
			c.Abort()

			return
		}

		p, err := principal(ctx, who.Account)
		if err != nil {
			_ = c.Error(err)
			c.Abort()

			return
		}

		ctx = context.WithValue(ctx, authenticatedKey{}, who)
		c.Request = c.Request.WithContext(authz.WithPrincipal(ctx, p))
		auth.SetUser(c, who.Account)

		c.Next()
	}
}

// TenantOf says which tenant an account works in. A tenantID above 0 is that tenant; all true (with no
// tenant) marks the account as crossing tenants, as a super-administrator does; neither leaves the request
// without a tenant, so tenant-scoped reads match nothing.
type TenantOf func(ctx context.Context, a account.Account) (tenantID int, all bool, err error)

// Tenant is the middleware that, behind Authentication, puts the signed-in account's tenant in the request
// context (tenancy.WithTenantID or tenancy.WithAllTenants), so the hooks and handlers of every route read
// tenant-scoped data as that person. An account the authentication did not find is left alone.
func Tenant(tenantOf TenantOf) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		who, ok := AuthenticatedFrom(ctx.Request.Context())
		if !ok {
			ctx.Next()

			return
		}

		tenantID, all, err := tenantOf(ctx.Request.Context(), who.Account)
		if err != nil {
			_ = ctx.Error(err)
			ctx.Abort()

			return
		}

		reqCtx := ctx.Request.Context()

		switch {
		case tenantID > 0:
			reqCtx = tenancy.WithTenantID(reqCtx, tenantID)
		case all:
			reqCtx = tenancy.WithAllTenants(reqCtx)
		}

		ctx.Request = ctx.Request.WithContext(reqCtx)

		ctx.Next()
	}
}
