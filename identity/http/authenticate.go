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

// RequestUser makes the application's own user of the signed-in account: what it returns is the request's
// auth.Identifiable (auth.GetIdentifiable, auth.UserFromContext). It runs on every request, so nothing a
// cache holds decides the user.
type RequestUser func(ctx context.Context, who account.Authenticated) (auth.Identifiable, error)

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
	// User makes the request's user of the account; nil leaves the account itself.
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
	return Authenticator{SignIn: signIn, Cookies: cookies, Principal: principal}.Middleware()
}

// Middleware is Authentication with the Authenticator's settings.
func (a Authenticator) Middleware() gin.HandlerFunc {
	cookies, principal := a.Cookies.withDefaults(), a.Principal
	if principal == nil {
		principal = DefaultPrincipal
	}

	return func(c *gin.Context) {
		if a.Keys != nil && c.GetHeader(KeyHeader) != "" {
			a.Keys.authenticate(c)

			return
		}

		ctx := c.Request.Context()

		cookie, _ := c.Cookie(cookies.Access)

		who, err := a.SignIn.Authenticate(ctx, tokenOf(c.GetHeader("Authorization"), cookie))
		if err != nil {
			_ = c.Error(err)
			c.Abort()

			return
		}

		p, err := principal(ctx, who.Account)
		if err != nil {
			_ = c.Error(err)
			c.Abort()

			return
		}

		var user auth.Identifiable = who.Account

		if a.User != nil {
			if user, err = a.User(ctx, who); err != nil || user == nil {
				if err == nil {
					err = apperr.Internal(errNoRequestUser)
				}

				_ = c.Error(err)
				c.Abort()

				return
			}
		}

		ctx = context.WithValue(ctx, authenticatedKey{}, who)
		c.Request = c.Request.WithContext(authz.WithPrincipal(ctx, p))
		auth.SetUser(c, user)

		if !auth.EnforceUserRateLimit(c, user) {
			return
		}

		c.Next()
	}
}

// authenticate is the chain of a request that presents an API key. The key is the only credential looked at: a
// session cookie or a bearer token sent with it is not. The principal is the service account; AuthenticatedFrom
// has nobody, since a service account is no account of identity's.
func (k *Keys) authenticate(c *gin.Context) {
	secret := c.GetHeader(KeyHeader)
	c.Request.Header.Del(KeyHeader) // nothing after this reads, logs or forwards the secret

	if len(secret) > maxKeyLength {
		_ = c.Error(apperr.Unauthorized("invalid API key"))
		c.Abort()

		return
	}

	caller, err := k.Auth.Authenticate(c.Request.Context(), secret)
	if err != nil {
		_ = c.Error(err)
		c.Abort()

		return
	}

	c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.ServiceAccount(caller.AccountID)))
	auth.SetUser(c, caller)

	if !auth.EnforceUserRateLimit(c, caller) {
		return
	}

	c.Next()

	k.record(c, caller)
}

// record leaves a row in the audit trail for a request made with a key that was not a read: which account and
// key, what was asked for, and whether it failed (the error handler renders the response after this runs, so the
// status is not known yet). A failure to record is logged, never returned: the change has been made.
func (k *Keys) record(c *gin.Context, caller serviceaccount.Caller) {
	switch c.Request.Method {
	case nethttp.MethodGet, nethttp.MethodHead, nethttp.MethodOptions:
		return
	}

	if k.Trail == nil {
		return
	}

	entry := audit.NewEntry(serviceaccount.AccountEntity, caller.AccountID, 0, "request").WithAfter(map[string]any{
		"method": c.Request.Method, "route": c.FullPath(), "key_id": caller.KeyID, "failed": len(c.Errors) > 0,
		"at": k.Clock.Now().UTC().Format(time.RFC3339),
	})

	if err := k.Trail.Write(c.Request.Context(), entry); err != nil && k.Log != nil {
		k.Log.ErrorContext(c.Request.Context(), "service account request trail failed", "service_account", caller.AccountID, "error", err)
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
