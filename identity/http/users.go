package http

import (
	"context"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/route"
)

// The permissions of the users routes. They are the ids access/admin uses for
// the same two ideas, seeing people and administering them (access.Install
// defines them in the application's catalogue): iam.user:view reads the list,
// a person, their history, changes and sessions; iam.user:manage writes.
const (
	// ViewUsers is seeing people and what is on their record.
	ViewUsers = "iam.user:view"
	// ManageUsers is creating, editing, unlocking and deactivating people, giving them a
	// new password and ending their sessions.
	ManageUsers = "iam.user:manage"
)

// usersBase and profileBase are where the users and profile routes live (the
// application's prefix goes in front).
const (
	usersBase   = "/v1/iam/users"
	profileBase = "/v1/iam/profile"
)

// The users routes: what an administrator does with other people's accounts.
var (
	// ListUsers lists and searches people, as go-core's datatable page.
	ListUsers = route.Get[ListUsersInput, datatable.DatatableResult[UserRow]](usersBase).Name("identity.users.list").Requires(ViewUsers)
	// ShowUser is one person with their last sign-in and lock.
	ShowUser = route.Get[UserInput, UserDetail](usersBase + "/:id").Name("identity.users.show").Requires(ViewUsers)
	// CreateUser makes an active account.
	CreateUser = route.Post[CreateUserInput, UserDetail](usersBase).Name("identity.users.create").Requires(ManageUsers)
	// UpdateUser writes a person's details.
	UpdateUser = route.Put[UpdateUserInput, UserDetail](usersBase + "/:id").Name("identity.users.update").Requires(ManageUsers)
	// SetUserPassword gives a person a new password, lifting their lock and ending their sessions.
	SetUserPassword = route.Put[SetPasswordInput, route.Empty](usersBase + "/:id/password").Name("identity.users.set-password").Requires(ManageUsers)
	// DeactivateUser makes a person inactive, after the application's hooks agree.
	DeactivateUser = route.Post[UserInput, route.Empty](usersBase + "/:id/deactivate").Name("identity.users.deactivate").Requires(ManageUsers)
	// ActivateUser makes an inactive person active again.
	ActivateUser = route.Post[UserInput, route.Empty](usersBase + "/:id/activate").Name("identity.users.activate").Requires(ManageUsers)
	// UnlockUser lifts the lock after wrong passwords.
	UnlockUser = route.Post[UserInput, UnlockResult](usersBase + "/:id/unlock").Name("identity.users.unlock").Requires(ManageUsers)
	// UserSignIns is a person's sign-in history, newest first.
	UserSignIns = route.Get[HistoryInput, datatable.DatatableResult[SignInRow]](usersBase + "/:id/signins").Name("identity.users.signins").Requires(ViewUsers)
	// UserChanges is the history of changes made to a person, newest first.
	UserChanges = route.Get[HistoryInput, datatable.DatatableResult[ChangeRow]](usersBase + "/:id/changes").Name("identity.users.changes").Requires(ViewUsers)
	// UserSessions lists a person's live sessions.
	UserSessions = route.Get[UserInput, SessionList](usersBase + "/:id/sessions").Name("identity.users.sessions").Requires(ViewUsers)
	// RevokeUserSession ends one of a person's sessions.
	RevokeUserSession = route.Delete[UserSessionInput, route.Empty](usersBase + "/:id/sessions/:session_id").Name("identity.users.revoke-session").Requires(ManageUsers)
	// RevokeUserSessions ends all of a person's sessions.
	RevokeUserSessions = route.Delete[UserInput, route.Empty](usersBase + "/:id/sessions").Name("identity.users.revoke-sessions").Requires(ManageUsers)
)

// UserInput addresses a person.
type UserInput struct {
	ID int `path:"id"`
}

// UserSessionInput addresses one session of a person.
type UserSessionInput struct {
	ID        int `path:"id"`
	SessionID int `path:"session_id"`
}

// ListUsersInput is a page of the list. View is one of active (the default),
// locked, inactive, all; OrderCol one of login (the default), name, email,
// created_at; OrderDir asc (the default) or desc; PerPage at most 100.
type ListUsersInput struct {
	View     string `query:"view" validation:"max:16"`
	Search   string `query:"search" validation:"max:100"`
	OrderCol string `query:"order_col" validation:"max:16"`
	OrderDir string `query:"order_dir" validation:"max:4"`
	Page     int    `query:"page"`
	PerPage  int    `query:"per_page"`
}

// HistoryInput is a page of a person's sign-in history or changes.
type HistoryInput struct {
	ID      int `path:"id"`
	Page    int `query:"page"`
	PerPage int `query:"per_page"`
}

// CreateUserInput is a new person. Locale is a BCP-47 tag such as "hr".
type CreateUserInput struct {
	Login    string `json:"login" validation:"required|max:100"`
	Name     string `json:"name" validation:"required|max:150"`
	Email    string `json:"email" validation:"required|max:255"`
	Phone    string `json:"phone" validation:"max:30"`
	Locale   string `json:"locale" validation:"required|max:16"`
	Password string `json:"password" validation:"required|max:200"`
}

// UpdateUserInput is a person's details as they should be: every field is written.
type UpdateUserInput struct {
	ID     int    `path:"id"`
	Login  string `json:"login" validation:"required|max:100"`
	Name   string `json:"name" validation:"required|max:150"`
	Email  string `json:"email" validation:"required|max:255"`
	Phone  string `json:"phone" validation:"max:30"`
	Locale string `json:"locale" validation:"required|max:16"`
}

// SetPasswordInput is the new password of a person.
type SetPasswordInput struct {
	ID       int    `path:"id"`
	Password string `json:"password" validation:"required|max:200"`
}

// UnlockResult says whether the person was locked.
type UnlockResult struct {
	Unlocked bool `json:"unlocked"`
}

// UserRow is a person in the list. Status is active, locked or inactive.
// LastSignIn is null for a person who never signed in; LockedUntil is null
// unless sign-in is locked.
type UserRow struct {
	ID          int        `json:"id"`
	Login       string     `json:"login"`
	Name        string     `json:"name"`
	Email       string     `json:"email"`
	Phone       string     `json:"phone"`
	Locale      string     `json:"locale"`
	Active      bool       `json:"active"`
	Status      string     `json:"status"`
	LastSignIn  *time.Time `json:"last_sign_in"`
	LockedUntil *time.Time `json:"locked_until"`
	CreatedAt   time.Time  `json:"created_at"`
}

// UserDetail is one person: the same fields as UserRow.
type UserDetail struct {
	ID          int        `json:"id"`
	Login       string     `json:"login"`
	Name        string     `json:"name"`
	Email       string     `json:"email"`
	Phone       string     `json:"phone"`
	Locale      string     `json:"locale"`
	Active      bool       `json:"active"`
	Status      string     `json:"status"`
	LastSignIn  *time.Time `json:"last_sign_in"`
	LockedUntil *time.Time `json:"locked_until"`
	CreatedAt   time.Time  `json:"created_at"`
}

// SignInRow is one row of a sign-in history. Event is signed_in, wrong_password,
// locked_out, refused_inactive, signed_in_as, unlocked, signed_out_everywhere or
// session_revoked; ActorID is who did it when that was somebody else.
type SignInRow struct {
	ID        int       `json:"id"`
	Event     string    `json:"event"`
	IP        string    `json:"ip"`
	Device    string    `json:"device"`
	ActorID   *int      `json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
}

// ChangeRow is one change made to a person. Action is created, updated,
// deactivated, activated, password, email or profile; Fields names what changed,
// Before and After hold the values of the fields that are not secret.
type ChangeRow struct {
	ID        int               `json:"id"`
	Action    string            `json:"action"`
	Fields    []string          `json:"fields"`
	Before    map[string]string `json:"before"`
	After     map[string]string `json:"after"`
	ActorID   *int              `json:"actor_id"`
	CreatedAt time.Time         `json:"created_at"`
}

// SessionItem is one live session. OpenedBy is who opened it by signing in as the
// person; Current is the session the request came with (only the profile knows).
type SessionItem struct {
	ID         int       `json:"id"`
	Device     string    `json:"device"`
	IP         string    `json:"ip"`
	OpenedBy   *int      `json:"opened_by"`
	Current    bool      `json:"current"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// SessionList is a person's live sessions, the latest used first.
type SessionList struct {
	Sessions []SessionItem `json:"sessions"`
}

func (h *Handler) actor(ctx context.Context) (account.Authenticated, error) {
	who, ok := AuthenticatedFrom(ctx)
	if !ok {
		return account.Authenticated{}, apperr.Unauthorized(string(account.ReasonSessionInvalid)).WithReason(account.ReasonSessionInvalid)
	}

	return who, nil
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	t = t.UTC()

	return &t
}

func idOrNil(id int) *int {
	if id <= 0 {
		return nil
	}

	return &id
}

func statusOf(active bool, lockedUntil time.Time) string {
	switch {
	case !active:
		return "inactive"
	case !lockedUntil.IsZero():
		return "locked"
	default:
		return "active"
	}
}

func detailOf(d account.Detail) UserDetail {
	return UserDetail{
		ID: d.ID, Login: d.Login, Name: d.Name, Email: d.Email, Phone: d.Phone, Locale: d.Locale, Active: d.Active,
		Status: statusOf(d.Active, d.LockedUntil), LastSignIn: timeOrNil(d.LastSignIn), LockedUntil: timeOrNil(d.LockedUntil),
		CreatedAt: d.CreatedAt.UTC(),
	}
}

func rowOf(d account.Detail) UserRow { return UserRow(detailOf(d)) }

// pageOf is rows as go-core's datatable page.
func pageOf[T any](rows []T, total, page, perPage int) datatable.DatatableResult[T] {
	if rows == nil {
		rows = []T{}
	}

	out := datatable.DatatableResult[T]{
		Data: rows, Total: int64(total), PerPage: perPage, Page: page, LastPage: max((total+perPage-1)/perPage, 1),
	}

	if len(rows) > 0 {
		out.From = (page-1)*perPage + 1
		out.To = out.From + len(rows) - 1
	}

	return out
}

func pagingOf(page, perPage int) account.Paging { return account.Paging{Page: page, PerPage: perPage} }

func (h *Handler) listUsers(ctx context.Context, in ListUsersInput) (datatable.DatatableResult[UserRow], error) {
	if in.OrderDir != "" && in.OrderDir != "asc" && in.OrderDir != "desc" {
		return datatable.DatatableResult[UserRow]{}, apperr.New(nil, "identity.list.dir_invalid", apperr.CodeValidationError).
			WithReason("identity.list.dir_invalid").WithFields(map[string]string{"order_dir": "identity.list.dir_invalid"})
	}

	listing, err := h.admin.List(ctx, account.ListInput{
		View: account.View(in.View), Search: in.Search, OrderBy: account.Order(in.OrderCol), Desc: in.OrderDir == "desc",
		Paging: pagingOf(in.Page, in.PerPage),
	})
	if err != nil {
		return datatable.DatatableResult[UserRow]{}, err
	}

	rows := make([]UserRow, len(listing.Rows))
	for i, r := range listing.Rows {
		rows[i] = rowOf(r)
	}

	return pageOf(rows, listing.Total, listing.Page, listing.PerPage), nil
}

func (h *Handler) showUser(ctx context.Context, in UserInput) (UserDetail, error) {
	d, err := h.admin.Get(ctx, in.ID)

	return detailOf(d), err
}

func (h *Handler) createUser(ctx context.Context, in CreateUserInput) (UserDetail, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return UserDetail{}, err
	}

	acc, err := h.admin.Create(ctx, account.CreateAccount{
		Login: in.Login, Name: in.Name, Email: in.Email, Phone: in.Phone, Locale: in.Locale, Password: in.Password, ActorID: who.Account.ID,
	})
	if err != nil {
		return UserDetail{}, err
	}

	return detailOf(account.Detail{Account: acc}), nil
}

func (h *Handler) updateUser(ctx context.Context, in UpdateUserInput) (UserDetail, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return UserDetail{}, err
	}

	if _, err := h.admin.Update(ctx, account.UpdateAccount{
		ID: in.ID, Login: in.Login, Name: in.Name, Email: in.Email, Phone: in.Phone, Locale: in.Locale, ActorID: who.Account.ID,
	}); err != nil {
		return UserDetail{}, err
	}

	return h.showUser(ctx, UserInput{ID: in.ID})
}

func (h *Handler) setUserPassword(ctx context.Context, in SetPasswordInput) (route.Empty, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return route.Empty{}, err
	}

	return route.Empty{}, h.admin.SetPassword(ctx, account.SetPassword{ID: in.ID, Password: in.Password, ActorID: who.Account.ID})
}

func (h *Handler) deactivateUser(ctx context.Context, in UserInput) (route.Empty, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return route.Empty{}, err
	}

	return route.Empty{}, h.admin.Deactivate(ctx, account.DeactivateInput{ID: in.ID, ActorID: who.Account.ID})
}

func (h *Handler) activateUser(ctx context.Context, in UserInput) (route.Empty, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return route.Empty{}, err
	}

	return route.Empty{}, h.admin.Activate(ctx, account.DeactivateInput{ID: in.ID, ActorID: who.Account.ID})
}

func (h *Handler) unlockUser(ctx context.Context, in UserInput) (UnlockResult, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return UnlockResult{}, err
	}

	unlocked, err := h.admin.Unlock(ctx, account.UnlockInput{ID: in.ID, ActorID: who.Account.ID})

	return UnlockResult{Unlocked: unlocked}, err
}

func signInRows(rows []account.SignInEntry) []SignInRow {
	out := make([]SignInRow, len(rows))
	for i, r := range rows {
		out[i] = SignInRow{ID: r.ID, Event: string(r.Event), IP: r.IP, Device: r.Device, ActorID: idOrNil(r.ActorID), CreatedAt: r.CreatedAt.UTC()}
	}

	return out
}

func (h *Handler) userSignIns(ctx context.Context, in HistoryInput) (datatable.DatatableResult[SignInRow], error) {
	paging := pagingOf(in.Page, in.PerPage)

	rows, total, err := h.admin.SignIns(ctx, in.ID, paging)
	if err != nil {
		return datatable.DatatableResult[SignInRow]{}, err
	}

	return pageOf(signInRows(rows), total, max(in.Page, 1), resolvedPerPage(in.PerPage)), nil
}

func (h *Handler) userChanges(ctx context.Context, in HistoryInput) (datatable.DatatableResult[ChangeRow], error) {
	rows, total, err := h.admin.Changes(ctx, in.ID, pagingOf(in.Page, in.PerPage))
	if err != nil {
		return datatable.DatatableResult[ChangeRow]{}, err
	}

	out := make([]ChangeRow, len(rows))
	for i, r := range rows {
		out[i] = ChangeRow{
			ID: r.ID, Action: string(r.Action), Fields: nonNil(r.Fields), Before: nonNilMap(r.Before), After: nonNilMap(r.After),
			ActorID: idOrNil(r.ActorID), CreatedAt: r.At.UTC(),
		}
	}

	return pageOf(out, total, max(in.Page, 1), resolvedPerPage(in.PerPage)), nil
}

// resolvedPerPage is the page size the services use for a requested one.
func resolvedPerPage(perPage int) int {
	if perPage < 1 {
		return 20
	}

	return min(perPage, account.MaxPerPage)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}

	return m
}

func sessionItems(sessions []account.Session, current int) SessionList {
	out := SessionList{Sessions: make([]SessionItem, len(sessions))}
	for i, s := range sessions {
		out.Sessions[i] = SessionItem{
			ID: s.ID, Device: s.Label(), IP: s.IP, OpenedBy: idOrNil(s.ActorID), Current: s.ID == current,
			LastUsedAt: s.LastUsedAt.UTC(), ExpiresAt: s.ExpiresAt.UTC(), CreatedAt: s.CreatedAt.UTC(),
		}
	}

	return out
}

func (h *Handler) userSessions(ctx context.Context, in UserInput) (SessionList, error) {
	sessions, err := h.admin.Sessions(ctx, in.ID)
	if err != nil {
		return SessionList{}, err
	}

	return sessionItems(sessions, 0), nil
}

func (h *Handler) revokeUserSession(ctx context.Context, in UserSessionInput) (route.Empty, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return route.Empty{}, err
	}

	return route.Empty{}, h.admin.RevokeSession(ctx, in.ID, in.SessionID, who.Account.ID)
}

func (h *Handler) revokeUserSessions(ctx context.Context, in UserInput) (route.Empty, error) {
	who, err := h.actor(ctx)
	if err != nil {
		return route.Empty{}, err
	}

	return route.Empty{}, h.admin.RevokeSessions(ctx, in.ID, who.Account.ID)
}
