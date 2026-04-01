package auth

import (
	"context"

	"github.com/gin-gonic/gin"
)

type ctxKey struct{}

var userCtxKey = ctxKey{}

// SetUser stores the authenticated user in the gin context.
func SetUser(ctx *gin.Context, user Identifiable) {
	ctx.Set(userCtxKey, user)

	// Also inject into the standard context so services can read it
	ctx.Request = ctx.Request.WithContext(
		context.WithValue(ctx.Request.Context(), userCtxKey, user),
	)
}

// GetUser retrieves the authenticated user from the gin context.
func GetUser[T any](ctx *gin.Context) (T, bool) {
	raw, exists := ctx.Get(userCtxKey)
	if !exists {
		var zero T
		return zero, false
	}

	user, ok := raw.(T)
	return user, ok
}

// MustGetUser retrieves the authenticated user from the gin context.
func MustGetUser[T any](ctx *gin.Context) T {
	user, ok := GetUser[T](ctx)
	if !ok {
		panic("auth.MustGetUser: no user in context")
	}
	return user
}

// GetIdentifiable retrieves the user as the Identifiable interface.
func GetIdentifiable(ctx *gin.Context) (Identifiable, bool) {
	raw, exists := ctx.Get(userCtxKey)
	if !exists {
		return nil, false
	}
	user, ok := raw.(Identifiable)
	return user, ok
}

// UserFromContext retrieves the Identifiable user from a standard context.Context.
func UserFromContext(ctx context.Context) (Identifiable, bool) {
	user, ok := ctx.Value(userCtxKey).(Identifiable)
	return user, ok
}
