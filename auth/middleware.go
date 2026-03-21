package auth

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
)

// Resolver is implemented by each application to load the full user
// (including app-specific Data) after a token has been validated.
type Resolver interface {
	Resolve(ctx *gin.Context, claims *Claims) (Identifiable, error)
}

type ResolverFunc func(ctx *gin.Context, claims *Claims) (Identifiable, error)

func (f ResolverFunc) Resolve(ctx *gin.Context, claims *Claims) (Identifiable, error) {
	return f(ctx, claims)
}

// Authenticated returns a gin middleware that extracts and validates a JWT token.
func Authenticated(cfg TokenConfig, resolver Resolver) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		tokenString := extractBearerToken(ctx)
		if tokenString == "" {
			_ = ctx.Error(apperr.Unauthorized("authorization token is missing"))
			ctx.Abort()
			return
		}

		claims, err := ParseToken(tokenString, cfg)
		if err != nil {
			// ParseToken returns ErrMissingToken, ErrInvalidToken, ErrExpiredToken
			_ = ctx.Error(apperr.Wrap(err, "invalid or expired token", 401))
			ctx.Abort()
			return
		}

		user, err := resolver.Resolve(ctx, claims)
		if err != nil {
			_ = ctx.Error(apperr.Wrap(err, "failed to resolve user", 401))
			ctx.Abort()
			return
		}

		SetUser(ctx, user)

		// Also store in the standard context.Context for service layers
		ctx.Request = ctx.Request.WithContext(
			context.WithValue(ctx.Request.Context(), userCtxKey, user),
		)

		ctx.Next()
	}
}

// Authorized checks whether the authenticated user has permission for the policy.
func Authorized(policy Policy) gin.HandlerFunc {
	return AuthorizedWith(policy, DefaultAuthorizer)
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
	if header == "" {
		return ""
	}

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}

	return strings.TrimSpace(parts[1])
}
