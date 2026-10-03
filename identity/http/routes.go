package http

import (
	"time"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/navigation"
	"github.com/wssto2/go-core/route"
)

// The routes identity serves, declared once. Paths are relative to the prefix
// the application mounts them under (identity.WithPrefix), which the contract
// does not carry: the client adds its base URL.
var (
	// Login signs a person in with a login and a password.
	Login = route.Post[LoginInput, SessionResponse]("/auth/login").Name("identity.login").Public()
	// Refresh swaps the refresh token, from the body or the cookie, for new tokens.
	Refresh = route.Post[RefreshInput, SessionResponse]("/auth/refresh").Name("identity.refresh").Public()
	// Logout ends the session the request came with.
	Logout = route.Post[route.None, route.Empty]("/auth/logout").Name("identity.logout")
	// Me answers the session payload of whoever is signed in.
	Me = route.Get[route.None, SessionResponse]("/auth/me").Name("identity.me")
	// ChangeLocale sets the signed-in person's language.
	ChangeLocale = route.Post[ChangeLocaleInput, route.Empty]("/auth/change-locale").Name("identity.change-locale")
	// LoginAs signs in as somebody else, when the application has said who may.
	LoginAs = route.Post[LoginAsInput, SessionResponse]("/auth/login-as").Name("identity.login-as")

	// Routes is identity's contract: contract.Generate(dir, identity.Routes) writes
	// its TypeScript. User is the default projection of an account, listed because
	// no route names it (the payload's user is whatever the application projects).
	Routes = route.Group("identity", Login, Refresh, Logout, Me, ChangeLocale, LoginAs).Types(User{})
)

// LoginInput is a sign-in attempt.
type LoginInput struct {
	Login    string `json:"login" validation:"required|max:100"`
	Password string `json:"password" validation:"required|max:200"`
}

// RefreshInput carries the refresh token when the client does not send it as a cookie.
type RefreshInput struct {
	RefreshToken string `json:"refresh_token" validation:"max:255"`
}

// ChangeLocaleInput is the language to set, a BCP-47 tag such as "hr".
type ChangeLocaleInput struct {
	Locale string `json:"locale" validation:"required|max:16"`
}

// LoginAsInput names the person to sign in as.
type LoginAsInput struct {
	UserID int `json:"user_id" validation:"required"`
}

// SessionResponse is the session payload: who is signed in, until when, what
// they may do and what the menu offers them.
type SessionResponse struct {
	// User is what the UserProjector made of the account; the default is User.
	User      any       `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
	// Access is how each permission is held, the authz engine's MyAccess; without
	// an engine it holds the subject and no permissions.
	Access authz.MyAccess `json:"access"`
	// Navigation is the application's menu filtered by the permissions held; absent
	// when the application declared none.
	Navigation []navigation.Node `json:"navigation,omitempty"`
}

// User is the default projection of an account into the session payload.
type User struct {
	ID     int    `json:"id"`
	Login  string `json:"login"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Locale string `json:"locale"`
}
