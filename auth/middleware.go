package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Resolver is implemented by each application to load the full user
// (including app-specific Data) after a token has been validated.
//
// arv-core validates the token. The application loads the rest.
//
// Example implementation in arv-next:
//
//	type AppResolver struct { db *database.Connection }
//
//	func (r *AppResolver) Resolve(ctx *gin.Context, claims *auth.Claims) (auth.Identifiable, error) {
//	    dealer, err := loadDealer(r.db, claims.UserID)
//	    if err != nil { return nil, err }
//	    return &auth.User[AppData]{
//	        ID:    claims.UserID,
//	        Email: claims.Email,
//	        Roles: claims.Roles,
//	        Data:  AppData{Dealer: dealer},
//	    }, nil
//	}
type Resolver interface {
	Resolve(ctx *gin.Context, claims *Claims) (Identifiable, error)
}

// ResolverFunc is a function adapter for the Resolver interface.
// Lets you pass a plain function instead of creating a struct.
//
// Example:
//
//	auth.Authenticated(cfg, auth.ResolverFunc(func(ctx *gin.Context, claims *auth.Claims) (auth.Identifiable, error) {
//	    ...
//	}))
type ResolverFunc func(ctx *gin.Context, claims *Claims) (Identifiable, error)

func (f ResolverFunc) Resolve(ctx *gin.Context, claims *Claims) (Identifiable, error) {
	return f(ctx, claims)
}

// Authenticated returns a gin middleware that:
//  1. Extracts the Bearer token from the Authorization header
//  2. Validates and parses the token using the provided TokenConfig
//  3. Calls the app's Resolver to load the full user (including app-specific data)
//  4. Stores the user in the gin context via SetUser
//
// If any step fails the request is aborted with 401.
func Authenticated(cfg TokenConfig, resolver Resolver) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		tokenString := extractBearerToken(ctx)
		if tokenString == "" {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": ErrMissingToken.Error(),
			})
			return
		}

		claims, err := ParseToken(tokenString, cfg)
		if err != nil {
			status := http.StatusUnauthorized
			ctx.AbortWithStatusJSON(status, gin.H{
				"error": err.Error(),
			})
			return
		}

		user, err := resolver.Resolve(ctx, claims)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": ErrUnauthorized.Error(),
			})
			return
		}

		SetUser(ctx, user)
		ctx.Next()
	}
}

// Authorized returns a gin middleware that checks whether the authenticated
// user has permission to perform the given policy action.
//
// Must be used after the Authenticated middleware.
// Uses DefaultAuthorizer unless you replace it at startup.
//
// Usage:
//
//	router.GET("/customers",
//	    guards.Authenticated(...),
//	    auth.Authorized(auth.GeneratePolicy("customers.customers", "view")),
//	    handler,
//	)
func Authorized(policy Policy) gin.HandlerFunc {
	return AuthorizedWith(policy, DefaultAuthorizer)
}

// AuthorizedWith is like Authorized but lets you supply a custom Authorizer.
// Useful when different route groups need different authorization strategies.
func AuthorizedWith(policy Policy, authorizer Authorizer) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		user, ok := GetIdentifiable(ctx)
		if !ok {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": ErrUnauthorized.Error(),
			})
			return
		}

		if !authorizer(user, policy) {
			ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": ErrForbidden.Error(),
			})
			return
		}

		ctx.Next()
	}
}

// extractBearerToken pulls the token string from "Authorization: Bearer <token>".
// Returns empty string if the header is missing or malformed.
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
