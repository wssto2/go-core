package auth

import (
	"context"
)

func IdentityResolver(ctx context.Context, id string) (User, error) {
	return User{}, nil
}
