package auth

import (
	"context"
	"strconv"

	coreauth "github.com/wssto2/go-core/auth"
)

// IdentityResolver is a minimal example resolver used by the example app.
// In real applications this should query your users table and map to the
// application's user type. It must return a type implementing
// github.com/wssto2/go-core/auth.Identifiable.
func IdentityResolver(ctx context.Context, id string) (coreauth.Identifiable, error) {
	uid, err := strconv.Atoi(id)
	if err != nil {
		return nil, err
	}
	u := &User{
		ID:       uid,
		Username: "example",
		Policies: []string{"products:create", "products:update", "products:delete"},
	}
	return u, nil
}
