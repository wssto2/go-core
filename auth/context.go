package auth

import (
	"context"

	"github.com/gin-gonic/gin"
)

const userContextKey = "user"

// SetUser stores the authenticated user in the gin context.
// Called by the Authenticated middleware after successful resolution.
func SetUser(ctx *gin.Context, user Identifiable) {
	ctx.Set(userContextKey, user)
}

// GetUser retrieves the authenticated user from the gin context.
// Returns the user and true if found, nil and false if not present.
//
// Usage in handlers:
//
//	user, ok := auth.GetUser[AppData](ctx)
//	if !ok { ... }
func GetUser[T any](ctx *gin.Context) (*User[T], bool) {
	raw, exists := ctx.Get(userContextKey)
	if !exists {
		return nil, false
	}

	user, ok := raw.(*User[T])
	return user, ok
}

// MustGetUser retrieves the authenticated user from the gin context.
// Panics if the user is not present — use only in handlers protected
// by the Authenticated middleware.
func MustGetUser[T any](ctx *gin.Context) *User[T] {
	user, ok := GetUser[T](ctx)
	if !ok {
		panic("auth.MustGetUser: no user in context — is the Authenticated middleware applied?")
	}
	return user
}

// GetIdentifiable retrieves the user as the Identifiable interface.
// Useful in middleware that doesn't know the concrete app data type.
func GetIdentifiable(ctx *gin.Context) (Identifiable, bool) {
	raw, exists := ctx.Get(userContextKey)
	if !exists {
		return nil, false
	}
	user, ok := raw.(Identifiable)
	return user, ok
}

// UserFromContext retrieves the Identifiable user from a standard context.Context.
// Useful in service/repository layers that don't have access to *gin.Context.
func UserFromContext(ctx context.Context) (Identifiable, bool) {
	user, ok := ctx.Value(userContextKey).(Identifiable)
	return user, ok
}
