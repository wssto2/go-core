package web

import (
	"context"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/auth"
)

// SignedIn is the signed-in person or service account a write is made as; 401 when the context carries none.
func SignedIn(ctx context.Context) (auth.Identifiable, error) { //nolint:ireturn // a person or a service account
	who, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, apperr.Unauthorized("not authenticated")
	}

	return who, nil
}

// SignedInAs is the signed-in person as the application's own user type; 401 when the context carries none or one of
// another type. Name the type the authentication provider puts on the request (SignedIn is the one for code that only
// needs the id).
//
//nolint:ireturn // a generic accessor returns its type parameter
func SignedInAs[T any](ctx context.Context) (T, error) {
	who, ok := auth.UserOf[T](ctx)
	if !ok {
		var zero T

		return zero, apperr.Unauthorized("not authenticated")
	}

	return who, nil
}
