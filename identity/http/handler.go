package http

import (
	"context"
	"strings"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/navigation"
	"github.com/wssto2/go-core/route"
)

// Handler serves the routes. Build it with NewHandler.
type Handler struct {
	signIn     *account.SignIn
	users      *account.Users
	admin      *account.Admin
	profile    *account.Profile
	clock      account.Clock
	cookies    Cookies
	project    UserProjector
	principal  PrincipalOf
	access     AccessProvider
	navigation NavigationProvider
}

// Config is what the handler is built from. Services, Admin, Profile and Clock
// are required; everything else has a default.
type Config struct {
	Services account.Services
	// Admin serves the users routes, Profile the profile routes.
	Admin   *account.Admin
	Profile *account.Profile
	Clock   account.Clock
	Cookies Cookies
	// Project defaults to DefaultUser.
	Project UserProjector
	// Principal defaults to DefaultPrincipal.
	Principal PrincipalOf
	// Access, when nil, gives the payload an access block with no permissions.
	Access AccessProvider
	// Navigation, when nil, leaves the payload without a menu.
	Navigation NavigationProvider
}

// NewHandler returns the handler for the configuration.
func NewHandler(cfg Config) *Handler {
	h := &Handler{
		signIn: cfg.Services.SignIn, users: cfg.Services.Users, admin: cfg.Admin, profile: cfg.Profile, clock: cfg.Clock,
		cookies: cfg.Cookies.withDefaults(), project: cfg.Project, principal: cfg.Principal,
		access: cfg.Access, navigation: cfg.Navigation,
	}

	if h.project == nil {
		h.project = DefaultUser
	}

	if h.principal == nil {
		h.principal = DefaultPrincipal
	}

	return h
}

// Routes binds every declared route to its handler.
func (h *Handler) Routes() []route.Handled {
	return []route.Handled{
		Login.To(h.login),
		Refresh.To(h.refresh),
		Logout.To(h.logout),
		Me.To(h.me),
		ChangeLocale.To(h.changeLocale),
		LoginAs.To(h.loginAs),
		ReturnToOwn.To(h.returnToOwn),

		ListUsers.To(h.listUsers),
		ShowUser.To(h.showUser),
		CreateUser.To(h.createUser),
		UpdateUser.To(h.updateUser),
		SetUserPassword.To(h.setUserPassword),
		DeactivateUser.To(h.deactivateUser),
		ActivateUser.To(h.activateUser),
		UnlockUser.To(h.unlockUser),
		UserSignIns.To(h.userSignIns),
		UserChanges.To(h.userChanges),
		UserSessions.To(h.userSessions),
		RevokeUserSession.To(h.revokeUserSession),
		RevokeUserSessions.To(h.revokeUserSessions),

		ShowProfile.To(h.showProfile),
		UpdateProfile.To(h.updateProfile),
		ChangeOwnPassword.To(h.changeOwnPassword),
		RequestEmailChange.To(h.requestEmailChange),
		ResendEmailCode.To(h.resendEmailCode),
		ConfirmEmailChange.To(h.confirmEmailChange),
		CancelEmailChange.To(h.cancelEmailChange),
		OwnSignIns.To(h.ownSignIns),
		OwnSessions.To(h.ownSessions),
		RevokeOwnSession.To(h.revokeOwnSession),
	}
}

func (h *Handler) login(ctx context.Context, in LoginInput) (SessionResponse, error) {
	x := route.ExchangeOf(ctx)

	signed, err := h.signIn.Login(ctx, account.LoginInput{
		Login: in.Login, Password: in.Password, Device: x.UserAgent(), IP: x.ClientIP(),
	})
	if err != nil {
		return SessionResponse{}, err
	}

	return h.session(ctx, Login.Spec().Path, signed)
}

func (h *Handler) refresh(ctx context.Context, in RefreshInput) (SessionResponse, error) {
	x := route.ExchangeOf(ctx)

	token := in.RefreshToken
	if token == "" {
		token = x.Cookie(h.cookies.Refresh)
	}

	if token == "" {
		return SessionResponse{}, apperr.Unauthorized(string(account.ReasonSessionInvalid)).WithReason(account.ReasonSessionInvalid)
	}

	signed, err := h.signIn.Refresh(ctx, account.RefreshInput{Token: token, Device: x.UserAgent(), IP: x.ClientIP()})
	if err != nil {
		return SessionResponse{}, err
	}

	return h.session(ctx, Refresh.Spec().Path, signed)
}

func (h *Handler) logout(ctx context.Context, _ route.None) (route.Empty, error) {
	x := route.ExchangeOf(ctx)

	// The cookies go whether or not the token was still good.
	h.cookies.clear(x, Logout.Spec().Path)

	return route.Empty{}, h.signIn.Logout(ctx, tokenOf(x.Header("Authorization"), x.Cookie(h.cookies.Access)))
}

func (h *Handler) me(ctx context.Context, _ route.None) (SessionResponse, error) {
	who, ok := AuthenticatedFrom(ctx)
	if !ok {
		return SessionResponse{}, apperr.Unauthorized(string(account.ReasonSessionInvalid)).WithReason(account.ReasonSessionInvalid)
	}

	return h.payload(ctx, who.Account, who.Actor, who.Session.ExpiresAt)
}

func (h *Handler) changeLocale(ctx context.Context, in ChangeLocaleInput) (route.Empty, error) {
	who, ok := AuthenticatedFrom(ctx)
	if !ok {
		return route.Empty{}, apperr.Unauthorized(string(account.ReasonSessionInvalid)).WithReason(account.ReasonSessionInvalid)
	}

	return route.Empty{}, h.users.ChangeLocale(ctx, account.ChangeLocaleInput{AccountID: who.Account.ID, Locale: in.Locale})
}

func (h *Handler) loginAs(ctx context.Context, in LoginAsInput) (SessionResponse, error) {
	who, ok := AuthenticatedFrom(ctx)
	if !ok {
		return SessionResponse{}, apperr.Unauthorized(string(account.ReasonSessionInvalid)).WithReason(account.ReasonSessionInvalid)
	}

	x := route.ExchangeOf(ctx)

	signed, err := h.signIn.LoginAs(ctx, account.LoginAsInput{
		ActorID: who.Account.ID, TargetID: in.UserID, Device: x.UserAgent(), IP: x.ClientIP(),
	})
	if err != nil {
		return SessionResponse{}, err
	}

	return h.session(ctx, LoginAs.Spec().Path, signed)
}

func (h *Handler) returnToOwn(ctx context.Context, _ route.None) (SessionResponse, error) {
	who, ok := AuthenticatedFrom(ctx)
	if !ok {
		return SessionResponse{}, apperr.Unauthorized(string(account.ReasonSessionInvalid)).WithReason(account.ReasonSessionInvalid)
	}

	x := route.ExchangeOf(ctx)

	signed, err := h.signIn.Return(ctx, account.ReturnInput{Session: who.Session, Device: x.UserAgent(), IP: x.ClientIP()})
	if err != nil {
		return SessionResponse{}, err
	}

	return h.session(ctx, ReturnToOwn.Spec().Path, signed)
}

// session sets the cookies for a fresh session, served at the declared path,
// and answers its payload.
func (h *Handler) session(ctx context.Context, declared string, signed account.Signed) (SessionResponse, error) {
	h.cookies.set(route.ExchangeOf(ctx), declared, signed.Credentials.Access, signed.Credentials.Refresh, h.clock.Now(), signed.Credentials.ExpiresAt)

	return h.payload(ctx, signed.Account, signed.Actor, signed.Credentials.ExpiresAt)
}

// payload builds {user, expires_at, impersonator, access, navigation} for an account. The
// engine and the projector act as the account: a sign-in has no principal in
// its request yet, so it is put in the context here.
func (h *Handler) payload(ctx context.Context, acc account.Account, actor *account.Account, expires time.Time) (SessionResponse, error) {
	principal, err := h.principal(ctx, acc)
	if err != nil {
		return SessionResponse{}, apperr.Internal(err)
	}

	ctx = authz.WithPrincipal(ctx, principal)

	user, err := h.project(ctx, acc)
	if err != nil {
		return SessionResponse{}, apperr.Internal(err)
	}

	held := authz.MyAccess{Subject: principal.Subject, Permissions: map[string]authz.PermissionAccess{}}

	if h.access != nil {
		if held, err = h.access(ctx); err != nil {
			return SessionResponse{}, err
		}
	}

	out := SessionResponse{User: user, ExpiresAt: expires.UTC(), Access: held}

	if actor != nil {
		out.Impersonator = &Impersonator{ID: actor.ID, Name: actor.Name}
	}

	if h.navigation != nil {
		tree, err := h.navigation(ctx, acc)
		if err != nil {
			return SessionResponse{}, apperr.Internal(err)
		}

		out.Navigation = navigation.Filter(tree, func(permission string) bool { _, ok := held.Permissions[permission]; return ok })
	}

	return out, nil
}

// tokenOf is the access token a request carries: an "Authorization: Bearer"
// header, else the access cookie. It is empty for none.
func tokenOf(authorization, cookie string) string {
	if token, ok := strings.CutPrefix(authorization, "Bearer "); ok {
		return strings.TrimSpace(token)
	}

	return cookie
}
