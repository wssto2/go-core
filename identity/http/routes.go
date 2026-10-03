package http

import (
	"time"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/navigation"
	"github.com/wssto2/go-core/route"
)

// base is the path every route of this version lives under. The application's
// own prefix (gocore.WithPrefix("/api")) goes in front of it, so these are
// served at /api/v1/auth/login. A v2 is new declarations in another group, not
// an edit of these.
const base = "/v1/auth"

// The routes identity serves, declared once.
var (
	// Login signs a person in with a login and a password.
	Login = route.Post[LoginInput, SessionResponse](base + "/login").Name("identity.login").Public()
	// Refresh swaps the refresh token, from the body or the cookie, for new tokens.
	Refresh = route.Post[RefreshInput, SessionResponse](base + "/refresh").Name("identity.refresh").Public()
	// Logout ends the session the request came with.
	Logout = route.Post[route.None, route.Empty](base + "/logout").Name("identity.logout")
	// Me answers the session payload of whoever is signed in.
	Me = route.Get[route.None, SessionResponse](base + "/me").Name("identity.me")
	// ChangeLocale sets the signed-in person's language.
	ChangeLocale = route.Post[ChangeLocaleInput, route.Empty](base + "/change-locale").Name("identity.change-locale")
	// LoginAs signs in as somebody else, when the application has said who may.
	LoginAs = route.Post[LoginAsInput, SessionResponse](base + "/login-as").Name("identity.login-as")
	// ReturnToOwn ends a session opened by signing in as somebody and signs the
	// person in as themselves again, with no password; any other session is refused
	// with identity.impersonation.not_active.
	ReturnToOwn = route.Post[route.None, SessionResponse](base + "/login-as/return").Name("identity.login-as.return")

	// Routes is identity's contract: contract.Generate(dir, identity.Routes) writes
	// its TypeScript. User is the default projection of an account, listed because
	// no route names it (the payload's user is whatever the application projects).
	Routes = route.Group("identity", Login, Refresh, Logout, Me, ChangeLocale, LoginAs, ReturnToOwn,
		ListUsers, ShowUser, CreateUser, UpdateUser, SetUserPassword, DeactivateUser, ActivateUser, UnlockUser,
		UserSignIns, UserChanges, UserSessions, RevokeUserSession, RevokeUserSessions,
		ShowProfile, UpdateProfile, ChangeOwnPassword, RequestEmailChange, ResendEmailCode, ConfirmEmailChange,
		CancelEmailChange, OwnSignIns, OwnSessions, RevokeOwnSession,
	).Types(User{}, Statuses, SignInEvents, ChangeActions)
)

// Status is where an account stands: usable, locked after wrong passwords, or deactivated.
type Status string

// The statuses an account reports.
const (
	StatusActive   Status = "active"
	StatusLocked   Status = "locked"
	StatusInactive Status = "inactive"
)

// The fixed sets of values the responses carry, listed in the contract so the
// generated TypeScript has them as unions.
var (
	Statuses      = route.Enum(StatusActive, StatusLocked, StatusInactive)
	SignInEvents  = route.Enum(account.SignedIn, account.WrongPassword, account.LockedOut, account.RefusedInactive, account.SignedInAs, account.Unlocked, account.SignedOutEverywhere, account.SessionRevoked)
	ChangeActions = route.Enum(account.ChangeCreated, account.ChangeUpdated, account.ChangeDeactivated, account.ChangeActivated, account.ChangePassword, account.ChangeEmail, account.ChangeProfile)
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
	// Impersonator is who is really signed in when the session was opened by
	// signing in as the user (the actor); absent for a person's own session.
	Impersonator *Impersonator `json:"impersonator,omitempty"`
	// Access is how each permission is held, the authz engine's MyAccess; without
	// an engine it holds the subject and no permissions.
	Access authz.MyAccess `json:"access"`
	// Navigation is the application's menu filtered by the permissions held; absent
	// when the application declared none.
	Navigation []navigation.Node `json:"navigation,omitempty"`
}

// Impersonator is the person behind a session opened by signing in as somebody
// else: who to return to.
type Impersonator struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// User is the default projection of an account into the session payload.
type User struct {
	ID     int    `json:"id"`
	Login  string `json:"login"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Locale string `json:"locale"`
}
