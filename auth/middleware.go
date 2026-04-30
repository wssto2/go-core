package auth

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
)

// Authenticated returns a gin middleware that extracts and validates a token.
func Authenticated(provider Provider) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		tokenString := extractBearerToken(ctx)
		if tokenString == "" {
			_ = ctx.Error(apperr.Unauthorized("authorization token is missing"))
			ctx.Abort()
			return
		}

		user, err := provider.Verify(ctx, tokenString)
		if err != nil {
			_ = ctx.Error(apperr.Wrap(err, "failed to resolve user", apperr.CodeUnauthenticated))
			ctx.Abort()
			return
		}

		SetUser(ctx, user)

		ctx.Next()
	}
}

// AuthorizedWith uses a custom authorizer.
func AuthorizedWith(policy Policy, authorizer Authorizer) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		user, ok := GetIdentifiable(ctx)
		if !ok {
			_ = ctx.Error(apperr.Unauthorized("user not authenticated"))
			ctx.Abort()
			return
		}

		if !authorizer(user, policy) {
			_ = ctx.Error(apperr.Forbidden("access denied"))
			ctx.Abort()
			return
		}

		ctx.Next()
	}
}

func extractBearerToken(ctx *gin.Context) string {
	header := ctx.GetHeader("Authorization")
	if header != "" && strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(header[len("Bearer "):])
	}

	// Fall back to HttpOnly cookie-based auth.
	if cookie, err := ctx.Cookie("access_token"); err == nil && cookie != "" {
		return cookie
	}

	return ""
}
