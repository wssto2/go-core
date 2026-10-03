package http

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/identity/account"
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
	cookies = cookies.withDefaults("")

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
