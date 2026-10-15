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
