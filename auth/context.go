package auth

import (
	"context"

	"github.com/gin-gonic/gin"
)

type ctxKey struct{}

var userCtxKey = ctxKey{}

// SetUser puts the authenticated user on the request context, where UserFromContext and UserOf read it.
func SetUser(ctx *gin.Context, user Identifiable) {
	ctx.Request = ctx.Request.WithContext(
		context.WithValue(ctx.Request.Context(), userCtxKey, user),
	)
}

// UserFromContext retrieves the Identifiable user from a standard context.Context.
func UserFromContext(ctx context.Context) (Identifiable, bool) {
	user, ok := ctx.Value(userCtxKey).(Identifiable)
	return user, ok
}

// UserOf is the authenticated user as the application's own type: what a typed route handler
// calls with its ctx, after the authentication middleware (Authenticated or the app's own, which
// calls SetUser) put the provider's user on the request. False when there is none or it is another type.
//
//nolint:ireturn // a generic accessor returns its type parameter
func UserOf[T any](ctx context.Context) (T, bool) {
	user, ok := ctx.Value(userCtxKey).(T)

	return user, ok
}

// ActorID is the id of the authenticated user in ctx, false when there is none: the actor function
// audit.NewRecorder takes.
func ActorID(ctx context.Context) (int, bool) {
	user, ok := UserFromContext(ctx)
	if !ok || user == nil {
		return 0, false
	}

	return user.GetID(), true
}
