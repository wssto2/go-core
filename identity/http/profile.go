package http

import (
	"context"
	"time"

	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/route"
)

// The profile routes: what a signed-in person does with their own account. They
// need a signed-in person and no permission.
var (
	// ShowProfile is the person's own account, with an e-mail change that waits for its code.
	ShowProfile = route.Get[route.None, ProfileResponse](profileBase).Name("identity.profile.show")
	// UpdateProfile writes the person's name and phone.
	UpdateProfile = route.Put[UpdateProfileInput, ProfileResponse](profileBase).Name("identity.profile.update")
	// ChangeOwnPassword changes the password and signs out every other session.
	ChangeOwnPassword = route.Put[ChangePasswordInput, route.Empty](profileBase + "/password").Name("identity.profile.change-password")
	// RequestEmailChange mails a code to a new address after the password is confirmed.
	RequestEmailChange = route.Post[RequestEmailInput, PendingEmail](profileBase + "/email").Name("identity.profile.request-email")
	// ResendEmailCode mails a fresh code to the pending address.
	ResendEmailCode = route.Post[route.None, PendingEmail](profileBase + "/email/resend").Name("identity.profile.resend-email")
	// ConfirmEmailChange verifies the code and makes the new address the person's.
	ConfirmEmailChange = route.Post[ConfirmEmailInput, ProfileResponse](profileBase + "/email/confirm").Name("identity.profile.confirm-email")
	// CancelEmailChange drops the pending change.
	CancelEmailChange = route.Delete[route.None, route.Empty](profileBase + "/email").Name("identity.profile.cancel-email")
	// OwnSignIns is the person's own sign-in history, newest first.
	OwnSignIns = route.Get[PageInput, datatable.DatatableResult[SignInRow]](profileBase + "/signins").Name("identity.profile.signins")
	// OwnSessions lists the person's live sessions; the one the request came with is marked current.
	OwnSessions = route.Get[route.None, SessionList](profileBase + "/sessions").Name("identity.profile.sessions")
	// RevokeOwnSession ends one of the person's sessions, not the current one (that is signing out).
	RevokeOwnSession = route.Delete[OwnSessionInput, route.Empty](profileBase + "/sessions/:session_id").Name("identity.profile.revoke-session")
)

// PageInput is a page of a list.
type PageInput struct {
	Page    int `query:"page"`
	PerPage int `query:"per_page"`
}

// OwnSessionInput addresses one of the person's own sessions.
type OwnSessionInput struct {
	SessionID int `path:"session_id"`
}

// UpdateProfileInput is the person's own details.
type UpdateProfileInput struct {
	Name  string `json:"name" validation:"required|max:150"`
	Phone string `json:"phone" validation:"max:30"`
}

// ChangePasswordInput is the current password and the new one, twice.
type ChangePasswordInput struct {
	CurrentPassword         string `json:"current_password" validation:"required|max:200"`
	NewPassword             string `json:"new_password" validation:"required|max:200"`
	NewPasswordConfirmation string `json:"new_password_confirmation" validation:"required|max:200"`
}

// RequestEmailInput is the new address and the current password.
type RequestEmailInput struct {
	Email           string `json:"email" validation:"required|max:255"`
	CurrentPassword string `json:"current_password" validation:"required|max:200"`
}

// ConfirmEmailInput is the code that was mailed to the new address.
type ConfirmEmailInput struct {
	Code string `json:"code" validation:"required|max:12"`
}

// PendingEmail is an e-mail change that waits for its code. It never carries the code.
type PendingEmail struct {
	Email             string    `json:"email"`
	ExpiresAt         time.Time `json:"expires_at"`
	ResendAvailableAt time.Time `json:"resend_available_at"`
	AttemptsLeft      int       `json:"attempts_left"`
}

// ProfileResponse is the person's own account. PendingEmail is null unless a change waits for its code.
type ProfileResponse struct {
	ID           int           `json:"id"`
	Login        string        `json:"login"`
	Name         string        `json:"name"`
	Email        string        `json:"email"`
	Phone        string        `json:"phone"`
	Locale       string        `json:"locale"`
	CreatedAt    time.Time     `json:"created_at"`
	PendingEmail *PendingEmail `json:"pending_email"`
}

func pendingOf(p account.PendingCode) PendingEmail {
	return PendingEmail{Email: p.Target, ExpiresAt: p.ExpiresAt.UTC(), ResendAvailableAt: p.ResendAvailableAt.UTC(), AttemptsLeft: p.AttemptsLeft}
}

func profileOf(v account.ProfileView) ProfileResponse {
	out := ProfileResponse{
		ID: v.ID, Login: v.Login, Name: v.Name, Email: v.Email, Phone: v.Phone, Locale: v.Locale, CreatedAt: v.CreatedAt.UTC(),
	}

	if v.PendingEmail != nil {
		p := pendingOf(*v.PendingEmail)
		out.PendingEmail = &p
	}

	return out
}

func (h *Handler) showProfile(ctx context.Context, _ route.None) (ProfileResponse, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return ProfileResponse{}, err
	}

	view, err := h.profile.Get(ctx, who.Account.ID)

	return profileOf(view), err
}

func (h *Handler) updateProfile(ctx context.Context, in UpdateProfileInput) (ProfileResponse, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return ProfileResponse{}, err
	}

	view, err := h.profile.UpdateDetails(ctx, account.ProfileDetails{AccountID: who.Account.ID, Name: in.Name, Phone: in.Phone})

	return profileOf(view), err
}

func (h *Handler) changeOwnPassword(ctx context.Context, in ChangePasswordInput) (route.Empty, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return route.Empty{}, err
	}

	x := route.ExchangeOf(ctx)

	return route.Empty{}, h.profile.ChangePassword(ctx, account.PasswordChange{
		AccountID: who.Account.ID, CurrentPassword: in.CurrentPassword, NewPassword: in.NewPassword, Confirmation: in.NewPasswordConfirmation,
		KeepToken: tokenOf(x.Header("Authorization"), x.Cookie(h.cookies.Access)),
	})
}

func (h *Handler) requestEmailChange(ctx context.Context, in RequestEmailInput) (PendingEmail, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return PendingEmail{}, err
	}

	pending, err := h.profile.RequestEmailChange(ctx, account.RequestEmail{
		AccountID: who.Account.ID, Email: in.Email, CurrentPassword: in.CurrentPassword, IP: route.ExchangeOf(ctx).ClientIP(),
	})

	return pendingOf(pending), err
}

func (h *Handler) resendEmailCode(ctx context.Context, _ route.None) (PendingEmail, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return PendingEmail{}, err
	}

	pending, err := h.profile.ResendEmailChange(ctx, who.Account.ID, route.ExchangeOf(ctx).ClientIP())

	return pendingOf(pending), err
}

func (h *Handler) confirmEmailChange(ctx context.Context, in ConfirmEmailInput) (ProfileResponse, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return ProfileResponse{}, err
	}

	view, err := h.profile.ConfirmEmailChange(ctx, account.ConfirmEmail{AccountID: who.Account.ID, Code: in.Code})

	return profileOf(view), err
}

func (h *Handler) cancelEmailChange(ctx context.Context, _ route.None) (route.Empty, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return route.Empty{}, err
	}

	return route.Empty{}, h.profile.CancelEmailChange(ctx, who.Account.ID)
}

func (h *Handler) ownSignIns(ctx context.Context, in PageInput) (datatable.DatatableResult[SignInRow], error) {
	who, err := h.actor(ctx)
	if err != nil {
		return datatable.DatatableResult[SignInRow]{}, err
	}

	rows, total, err := h.profile.SignIns(ctx, who.Account.ID, pagingOf(in.Page, in.PerPage))
	if err != nil {
		return datatable.DatatableResult[SignInRow]{}, err
	}

	return pageOf(signInRows(rows), total, max(in.Page, 1), resolvedPerPage(in.PerPage)), nil
}

func (h *Handler) ownSessions(ctx context.Context, _ route.None) (SessionList, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return SessionList{}, err
	}

	sessions, err := h.profile.Sessions(ctx, who.Account.ID)
	if err != nil {
		return SessionList{}, err
	}

	return sessionItems(sessions, who.Session.ID), nil
}

func (h *Handler) revokeOwnSession(ctx context.Context, in OwnSessionInput) (route.Empty, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return route.Empty{}, err
	}

	return route.Empty{}, h.profile.RevokeSession(ctx, who.Account.ID, in.SessionID, who.Session.ID)
}
