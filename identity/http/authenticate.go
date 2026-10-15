package http

import (
	"context"
	"errors"
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/serviceaccount"
	"github.com/wssto2/go-core/tenancy"
)

type authenticatedKey struct{}

var errNoRequestUser = errors.New("identity: the request user function returned no user")

// AuthenticatedFrom returns the account and session the Authentication
// middleware found for the request in ctx.
func AuthenticatedFrom(ctx context.Context) (account.Authenticated, bool) {
	who, ok := ctx.Value(authenticatedKey{}).(account.Authenticated)

	return who, ok
}

// Authenticated is who a request was made by, as RequestUser is given it: exactly one of the two is set. A person's
// request has the Account (and its Session and Actor) the access token belongs to, and no ServiceAccount; a request
// made with an API key has the ServiceAccount (the account and the key), and the embedded account.Authenticated is
// its zero value.
type Authenticated struct {
	account.Authenticated

	// ServiceAccount is the account and key of a request made with an API key, nil for a person's.
	ServiceAccount *serviceaccount.Caller
}

// RequestUser makes the application's own user of the signed-in account, or of the service account behind an API
// key: what it returns is the request's auth.Identifiable (auth.GetIdentifiable, auth.UserFromContext) and what the
// rate limit counts. It runs on every request, so nothing a cache holds decides the user. The authz principal is not
// its to change: it stays the account's, or authz.ServiceAccount for a key.
type RequestUser func(ctx context.Context, who Authenticated) (auth.Identifiable, error)

// KeyHeader is the header a service account sends its API key in.
const KeyHeader = "X-API-Key"

// maxKeyLength bounds what is looked at: a secret is 48 characters.
const maxKeyLength = 256

// KeyAuthenticator is what authentication asks of the service accounts: which account and key a secret
// belongs to. *serviceaccount.Service is one.
type KeyAuthenticator interface {
	Authenticate(ctx context.Context, secret string) (serviceaccount.Caller, error)
}

// Keys is the authentication of API keys: a request with the X-API-Key header is a service account's.
type Keys struct {
	Auth KeyAuthenticator
	// Trail records what a request made with a key changed: one row per request that is not a read.
	Trail serviceaccount.Trail
	Clock interface{ Now() time.Time }
	Log   *slog.Logger
}

// Authenticator is the authentication of every route that is not Public.
type Authenticator struct {
	SignIn  *account.SignIn
	Cookies Cookies
	// Principal says which authz principal an account acts as; nil is DefaultPrincipal.
	Principal PrincipalOf
	// User makes the request's user of the account or the service account; nil leaves the account itself
	// (a person's request) or the serviceaccount.Caller (a key's).
	User RequestUser
	// Keys, when set, authenticates the requests that carry an API key; without it the header is ignored.
	Keys *Keys
}

// Authentication is the middleware that authenticates every route that is not
// Public: it resolves the access token (bearer header or cookie) to a live
// session of an active account, and leaves in the request context
//
//   - the account and session (AuthenticatedFrom),
//   - the account as go-core's auth identity (auth.UserFromContext),
//   - the authz principal the permission checks act as.
//
// A request without a good token is rejected with 401, reason
// identity.session.invalid. When the application armed a per-user rate limit
// (bootstrap's WithRateLimit, auth.DeferUserRateLimit), the account is counted
// in it, and the request after the limit is answered 429.
func Authentication(signIn *account.SignIn, cookies Cookies, principal PrincipalOf) gin.HandlerFunc {
	return Authenticator{SignIn: signIn, Cookies: cookies, Principal: principal, User: nil, Keys: nil}.Middleware()
}

// Middleware is Authentication with the Authenticator's settings.
func (a Authenticator) Middleware() gin.HandlerFunc {
	cookies, principal := a.Cookies.withDefaults(), a.Principal
	if principal == nil {
		principal = DefaultPrincipal
	}

	return func(ctx *gin.Context) {
		if a.Keys != nil && ctx.GetHeader(KeyHeader) != "" {
			a.byKey(ctx)

			return
		}

		a.bySession(ctx, cookies, principal)
	}
}

// refuse ends the request with the error, which the error handler answers.
func refuse(ctx *gin.Context, err error) {
	_ = ctx.Error(err)
	ctx.Abort()
}

// bySession is the chain of a request that carries an access token.
func (a Authenticator) bySession(ctx *gin.Context, cookies Cookies, principal PrincipalOf) {
	reqCtx := ctx.Request.Context()

	cookie, _ := ctx.Cookie(cookies.Access)

	signed, err := a.SignIn.Authenticate(reqCtx, tokenOf(ctx.GetHeader("Authorization"), cookie))
	if err != nil {
		refuse(ctx, err)

		return
	}

	who := Authenticated{Authenticated: signed, ServiceAccount: nil}

	acting, err := principal(reqCtx, who.Account)
	if err != nil {
		refuse(ctx, err)

		return
	}

	user, err := a.requestUser(reqCtx, who, who.Account)
	if err != nil {
		refuse(ctx, err)

		return
	}

	reqCtx = context.WithValue(reqCtx, authenticatedKey{}, signed)
	ctx.Request = ctx.Request.WithContext(authz.WithPrincipal(reqCtx, acting))

	a.admit(ctx, user)
}

// byKey is the chain of a request that presents an API key. The key is the only credential looked at: a session
// cookie or a bearer token sent with it is not. The principal is the service account; AuthenticatedFrom has
// nobody, since a service account is no account of identity's.
func (a Authenticator) byKey(ctx *gin.Context) {
	secret := ctx.GetHeader(KeyHeader)
	delete(ctx.Request.Header, nethttp.CanonicalHeaderKey(KeyHeader)) // nothing after this reads, logs or forwards the secret

	if len(secret) > maxKeyLength {
		refuse(ctx, apperr.Unauthorized("invalid API key"))

		return
	}

	reqCtx := ctx.Request.Context()

	caller, err := a.Keys.Auth.Authenticate(reqCtx, secret)
	if err != nil {
		refuse(ctx, err)

		return
	}

	user, err := a.requestUser(reqCtx, Authenticated{Authenticated: account.Authenticated{}, ServiceAccount: &caller}, caller)
	if err != nil {
		refuse(ctx, err)

		return
	}

	ctx.Request = ctx.Request.WithContext(authz.WithPrincipal(reqCtx, authz.ServiceAccount(caller.AccountID)))

	if a.admit(ctx, user) {
		a.Keys.record(ctx, caller)
	}
}

// requestUser is the request's user: the application's own when it made one, else own, the account or the caller.
func (a Authenticator) requestUser(ctx context.Context, who Authenticated, own auth.Identifiable) (auth.Identifiable, error) {
	if a.User == nil {
		return own, nil
	}

	user, err := a.User(ctx, who)
	if err != nil {
		return nil, err
	}

	if user == nil {
		return nil, apperr.Internal(errNoRequestUser)
	}

	return user, nil
}

// admit makes the user the request's, counts it in the application's per-user rate limit and hands the request on.
// It reports whether the request went on.
func (Authenticator) admit(ctx *gin.Context, user auth.Identifiable) bool {
	auth.SetUser(ctx, user)

	if !auth.EnforceUserRateLimit(ctx, user) {
		return false
	}

	ctx.Next()

	return true
}

// record leaves a row in the audit trail for a request made with a key that was not a read: which account and
// key, what was asked for, and whether it failed (the error handler renders the response after this runs, so the
// status is not known yet). A failure to record is logged, never returned: the change has been made.
func (k *Keys) record(ctx *gin.Context, caller serviceaccount.Caller) {
	switch ctx.Request.Method {
	case nethttp.MethodGet, nethttp.MethodHead, nethttp.MethodOptions:
		return
	}

	if k.Trail == nil {
		return
	}

	entry := audit.NewEntry(serviceaccount.AccountEntity, caller.AccountID, 0, "request").WithAfter(map[string]any{
		"method": ctx.Request.Method, "route": ctx.FullPath(), "key_id": caller.KeyID, "failed": len(ctx.Errors) > 0,
		"at": k.Clock.Now().UTC().Format(time.RFC3339),
	})

	err := k.Trail.Write(ctx.Request.Context(), entry)
	if err != nil && k.Log != nil {
		k.Log.ErrorContext(ctx.Request.Context(), "service account request trail failed", "service_account", caller.AccountID, "error", err)
	}
}

// TenantOf says which tenant an account works in. A tenantID above 0 is that tenant; all true (with no
// tenant) marks the account as crossing tenants, as a super-administrator does; neither leaves the request
// without a tenant, so tenant-scoped reads match nothing.
type TenantOf func(ctx context.Context, a account.Account) (tenantID int, all bool, err error)

// Tenant is the middleware that, behind Authentication, puts the signed-in account's tenant in the request
// context (tenancy.WithTenantID or tenancy.WithAllTenants), so the hooks and handlers of every route read
// tenant-scoped data as that person. An account the authentication did not find is left alone.
func Tenant(tenantOf TenantOf) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		who, ok := AuthenticatedFrom(ctx.Request.Context())
		if !ok {
			ctx.Next()

			return
		}

		tenantID, all, err := tenantOf(ctx.Request.Context(), who.Account)
		if err != nil {
			_ = ctx.Error(err)
			ctx.Abort()

			return
		}

		reqCtx := ctx.Request.Context()

		switch {
		case tenantID > 0:
			reqCtx = tenancy.WithTenantID(reqCtx, tenantID)
		case all:
			reqCtx = tenancy.WithAllTenants(reqCtx)
		}

		ctx.Request = ctx.Request.WithContext(reqCtx)

		ctx.Next()
	}
}
