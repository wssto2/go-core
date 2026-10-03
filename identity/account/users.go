package account

import (
	"context"
	"errors"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
)

// Users is what other features ask of identity: an account by id, a person's
// language, and their sessions.
type Users struct {
	deps Deps
	cfg  Config
	// set by NewAdmin and NewProfile, for code that has only the Users.
	admin   *Admin
	profile *Profile
}

// Admin returns the users module's administration service, which NewAdmin built
// over these Users (identity.Install does): create a person in code, for the
// first administrator of a new installation. It is nil for Users built without one.
func (u *Users) Admin() *Admin { return u.admin }

// Profile returns the profile service NewProfile built over these Users, nil
// for Users built without one.
func (u *Users) Profile() *Profile { return u.profile }

// Get returns the account, active or not.
func (u *Users) Get(ctx context.Context, id int) (Account, error) {
	acc, err := u.deps.Accounts.Find(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Account{}, accountNotFound()
	}

	if err != nil {
		return Account{}, apperr.Internal(err)
	}

	return acc, nil
}

// SubjectNames returns the display name of each person among subjects that
// exists, which is what access.SubjectDirectory asks: *Users satisfies it as it
// is. The name is the account's name, or its login when it has none. Service
// accounts are not identity's, and are left out.
func (u *Users) SubjectNames(ctx context.Context, subjects []authz.Subject) (map[authz.Subject]string, error) {
	var ids []int

	for _, s := range subjects {
		if s.Kind == authz.KindUser {
			ids = append(ids, s.ID)
		}
	}

	out := make(map[authz.Subject]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	found, err := u.deps.Accounts.FindMany(ctx, ids)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	for _, a := range found {
		name := a.Name
		if name == "" {
			name = a.Login
		}

		out[authz.Subject{Kind: authz.KindUser, ID: a.ID}] = name
	}

	return out, nil
}

// ChangeLocaleInput is a person's new language.
type ChangeLocaleInput struct {
	AccountID int
	Locale    string
}

// ChangeLocale sets the account's language, a BCP-47 tag such as "hr".
func (u *Users) ChangeLocale(ctx context.Context, in ChangeLocaleInput) error {
	if !ValidLocale(in.Locale) {
		return apperr.BadRequest(string(ReasonLocaleInvalid)).WithReason(ReasonLocaleInvalid)
	}

	err := u.deps.Accounts.SetLocale(ctx, in.AccountID, in.Locale)
	if errors.Is(err, ErrNotFound) {
		return accountNotFound()
	}

	if err != nil {
		return apperr.Internal(err)
	}

	return nil
}

// Sessions lists the account's live sessions, the latest used first (IAM-USER-004).
func (u *Users) Sessions(ctx context.Context, accountID int) ([]Session, error) {
	sessions, err := u.deps.Sessions.Live(ctx, accountID, u.deps.Clock.Now())
	if err != nil {
		return nil, apperr.Internal(err)
	}

	return sessions, nil
}

// RevokeSessionInput ends one session of an account. Keep, when not zero, is a
// session that may not be ended this way (the request's own: that is signing
// out). ActorID is who does it, zero for the person themselves.
type RevokeSessionInput struct {
	AccountID int
	SessionID int
	Keep      int
	ActorID   int
}

// RevokeSession ends one live session of the account (IAM-USER-004), which is
// on its history as session_revoked. A session that is not the account's live
// one is a not-found.
func (u *Users) RevokeSession(ctx context.Context, in RevokeSessionInput) error {
	if in.ActorID == in.AccountID {
		in.ActorID = 0
	}

	if in.Keep > 0 && in.SessionID == in.Keep {
		return apperr.BadRequest(string(ReasonSessionCurrent)).WithReason(ReasonSessionCurrent)
	}

	n, err := u.deps.Sessions.End(ctx, in.AccountID, []int{in.SessionID}, in.Keep, u.deps.Clock.Now())
	if err != nil {
		return apperr.Internal(err)
	}

	if n == 0 {
		return apperr.NotFound(string(ReasonSessionNotFound)).WithReason(ReasonSessionNotFound)
	}

	if err := u.record(ctx, in.AccountID, SessionRevoked, in.ActorID); err != nil {
		return err
	}

	u.deps.Notices.SessionsRevoked(ctx, in.AccountID, in.ActorID, n)

	return nil
}

// RevokeSessionsInput ends the sessions of an account. KeepToken, when not
// empty, is the access token of a session that stays: somebody who changes
// their own password stays signed in where they did it.
type RevokeSessionsInput struct {
	AccountID int
	KeepToken string
	ActorID   int
}

// RevokeSessions ends every session of the account but the kept one
// (IAM-USER-004), which is on its history as signed_out_everywhere. It also
// ends the sessions the account opened by signing in as somebody else, which
// belong to those people, and puts session_revoked on theirs: a person who is
// gone, or has a new password, leaves no way in as somebody else open.
func (u *Users) RevokeSessions(ctx context.Context, in RevokeSessionsInput) error {
	if in.ActorID == in.AccountID {
		in.ActorID = 0
	}

	keep := 0

	if in.KeepToken != "" {
		if s, err := u.deps.Sessions.Get(ctx, in.KeepToken, u.deps.Clock.Now()); err == nil && s.AccountID == in.AccountID {
			keep = s.ID
		}
	}

	now := u.deps.Clock.Now()

	n, err := u.deps.Sessions.End(ctx, in.AccountID, nil, keep, now)
	if err != nil {
		return apperr.Internal(err)
	}

	if err := u.record(ctx, in.AccountID, SignedOutEverywhere, in.ActorID); err != nil {
		return err
	}

	u.deps.Notices.SessionsRevoked(ctx, in.AccountID, in.ActorID, n)

	opened, err := u.deps.Sessions.EndOpenedBy(ctx, in.AccountID, now)
	if err != nil {
		return apperr.Internal(err)
	}

	for _, s := range opened {
		// The actor of the target's history is who ended it, not the target.
		if err := u.record(ctx, s.AccountID, SessionRevoked, in.ActorID); err != nil {
			return err
		}

		u.deps.Notices.SignedOutAs(ctx, in.AccountID, s.AccountID)
	}

	return nil
}

// record writes the event; the actor is kept only when it is somebody else than the person.
func (u *Users) record(ctx context.Context, accountID int, e Event, actorID int) error {
	if actorID == accountID {
		actorID = 0
	}

	return appendEntry(ctx, u.deps, SignInEntry{AccountID: accountID, Event: e, ActorID: actorID})
}
