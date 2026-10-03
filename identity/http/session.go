package http

import (
	"context"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/navigation"
)

// UserProjector makes the payload's user of an account. The result must
// serialise with an "id". The default is DefaultUser.
type UserProjector func(ctx context.Context, a account.Account) (any, error)

// DefaultUser projects the account's public fields: never its password hash.
func DefaultUser(_ context.Context, a account.Account) (any, error) {
	return User{ID: a.ID, Login: a.Login, Name: a.Name, Email: a.Email, Locale: a.Locale}, nil
}

// NavigationProvider returns the application's whole menu for an account; the
// payload carries the part of it the account's permissions reach.
type NavigationProvider func(ctx context.Context, a account.Account) ([]navigation.Node, error)

// AccessProvider tells how the person in ctx holds each permission. The
// authz engine's MyAccess is one: identity.WithAccess(engine) wires it.
type AccessProvider func(ctx context.Context) (authz.MyAccess, error)

// PrincipalOf is the authz principal of an account, which the engine and the
// routes' permission checks act as. The default is authz.User(a.ID, 0).
type PrincipalOf func(ctx context.Context, a account.Account) (authz.Principal, error)

// DefaultPrincipal is the person with no location of their own.
func DefaultPrincipal(_ context.Context, a account.Account) (authz.Principal, error) {
	return authz.User(a.ID, 0), nil
}
