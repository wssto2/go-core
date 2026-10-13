package account

import (
	"context"
	"errors"
	"slices"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/mail"
)

// Mail is where identity's mail goes: the sender it was given and the renderer of its mails (the
// application's copy, with identity's English defaults behind it). A feature that mails the same people,
// such as notification, sends through it, so there is nothing more to configure.
type Mail struct {
	Sender   mail.Sender
	Renderer mail.Renderer
}

// Users is what other features ask of identity: an account by id, a person's
// language, and their sessions.
type Users struct {
	deps Deps
	cfg  Config
	// set by NewAdmin and NewProfile, for code that has only the Users.
	admin   *Admin
	profile *Profile
	mail    *Mail
}

// Mail returns where identity's mail goes, and false when identity runs WithoutMail (or Users was built
// without mail): such a person cannot be mailed through identity, and a feature that would mail them says
// e-mail is unavailable.
func (u *Users) Mail() (Mail, bool) {
	if u.mail == nil {
		return Mail{}, false
	}

	return *u.mail, true
}

// SetMail sets where the mail goes. identity.Install calls it; a test of a feature that mails calls it with a
// mail.Sink.
func (u *Users) SetMail(m Mail) { u.mail = &m }

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

// Find returns the accounts that exist among ids, active or not, in one query and in no particular
// order; an id that is not an account (deleted, or never one) is simply absent. It is what a feature
// that notifies or addresses many people asks, such as notification.Install.
func (u *Users) Find(ctx context.Context, ids []int) ([]Account, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	found, err := u.deps.Accounts.FindMany(ctx, ids)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	return found, nil
}

// Names returns the display name of each account among ids that exists, in one query: the
// account's name, or its login when it has none. An id that is not an account (deleted, or
// never one) is simply absent.
func (u *Users) Names(ctx context.Context, ids []int) (map[int]string, error) {
	out := make(map[int]string, len(ids))
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

		out[a.ID] = name
	}

	return out, nil
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

	names, err := u.Names(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := make(map[authz.Subject]string, len(names))
	for id, name := range names {
		out[authz.Subject{Kind: authz.KindUser, ID: id}] = name
	}

	return out, nil
}

// ChangeLocaleInput is a person's new language.
type ChangeLocaleInput struct {
	AccountID int
	Locale    string
}

// ChangeLocale sets the account's language, a BCP-47 tag such as "hr" that Config.Locales offers.
func (u *Users) ChangeLocale(ctx context.Context, in ChangeLocaleInput) error {
	if !ValidLocale(in.Locale) {
		return apperr.BadRequest(string(ReasonLocaleInvalid)).WithReason(ReasonLocaleInvalid)
	}

	err := u.cfg.offers("locale", in.Locale)
	if err != nil {
		return err
	}

	err = u.deps.Accounts.SetLocale(ctx, in.AccountID, in.Locale)
	if errors.Is(err, ErrNotFound) {
		return accountNotFound()
	}

	if err != nil {
		return apperr.Internal(err)
	}

	u.deps.Notices.AccountChanged(ctx, in.AccountID)

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

	u.deps.Notices.SessionsEnded(ctx, SessionsEnded{
		AccountID: in.AccountID, SessionIDs: []int{in.SessionID}, Reason: EndedRevoked, ActorID: in.ActorID,
	})

	return nil
}

// RevokeSessionsInput ends the sessions of an account. KeepToken, when not
// empty, is the access token of a session that stays: somebody who changes
// their own password stays signed in where they did it.
type RevokeSessionsInput struct {
	AccountID int
	KeepToken string
	ActorID   int
	// Reason is told to the Notices; EndedRevoked when empty.
	Reason EndReason
}

// RevokeSessions ends every session of the account but the kept one
// (IAM-USER-004), which is on its history as signed_out_everywhere. It also
// ends the sessions the account opened by signing in as somebody else, which
// belong to those people, and puts session_revoked on theirs: a person who is
// gone, or has a new password, leaves no way in as somebody else open.
func (u *Users) RevokeSessions(ctx context.Context, in RevokeSessionsInput) error {
	pending, err := u.revokeSessions(ctx, in)
	if err != nil {
		return err
	}

	pending.announce(ctx, u.deps.Notices)

	return nil
}

// pendingNotices are facts to tell the Notices once the change they describe is
// committed: a caller inside a transaction announces them after it.
type pendingNotices struct {
	ended     []SessionsEnded
	signedOut []LoginAs
}

func (p pendingNotices) announce(ctx context.Context, n Notices) {
	for _, e := range p.ended {
		n.SessionsEnded(ctx, e)
	}

	for _, f := range p.signedOut {
		n.SignedOutAs(ctx, f)
	}
}

// revokeSessions does RevokeSessions and returns the facts instead of telling them.
func (u *Users) revokeSessions(ctx context.Context, in RevokeSessionsInput) (pendingNotices, error) {
	var out pendingNotices

	if in.ActorID == in.AccountID {
		in.ActorID = 0
	}

	if in.Reason == "" {
		in.Reason = EndedRevoked
	}

	keep := 0

	if in.KeepToken != "" {
		if s, err := u.deps.Sessions.Get(ctx, in.KeepToken, u.deps.Clock.Now()); err == nil && s.AccountID == in.AccountID {
			keep = s.ID
		}
	}

	now := u.deps.Clock.Now()

	// The ids are read just before they are ended: a session opened in between is ended
	// but missing from the fact, which names the account all the same.
	live, err := u.deps.Sessions.Live(ctx, in.AccountID, now)
	if err != nil {
		return out, apperr.Internal(err)
	}

	var ids []int

	for _, s := range live {
		if s.ID != keep {
			ids = append(ids, s.ID)
		}
	}

	slices.Sort(ids)

	if _, err := u.deps.Sessions.End(ctx, in.AccountID, nil, keep, now); err != nil {
		return out, apperr.Internal(err)
	}

	if err := u.record(ctx, in.AccountID, SignedOutEverywhere, in.ActorID); err != nil {
		return out, err
	}

	// An empty list still goes out: a session opened after the read was ended too.
	out.ended = append(out.ended, SessionsEnded{AccountID: in.AccountID, SessionIDs: ids, Reason: in.Reason, ActorID: in.ActorID})

	opened, err := u.deps.Sessions.EndOpenedBy(ctx, in.AccountID, now)
	if err != nil {
		return out, apperr.Internal(err)
	}

	byTarget := map[int]int{} // target account -> index in out.ended

	for _, s := range opened {
		// The actor of the target's history is who ended it, not the target.
		if err := u.record(ctx, s.AccountID, SessionRevoked, in.ActorID); err != nil {
			return out, err
		}

		out.signedOut = append(out.signedOut, LoginAs{
			ActorID: in.AccountID, TargetID: s.AccountID, Device: s.Label(), IP: s.IP,
		})

		if i, ok := byTarget[s.AccountID]; ok {
			out.ended[i].SessionIDs = append(out.ended[i].SessionIDs, s.ID)
		} else {
			byTarget[s.AccountID] = len(out.ended)
			out.ended = append(out.ended, SessionsEnded{AccountID: s.AccountID, SessionIDs: []int{s.ID}, Reason: EndedOpenedBy, ActorID: in.ActorID})
		}
	}

	return out, nil
}

// record writes the event; the actor is kept only when it is somebody else than the person.
func (u *Users) record(ctx context.Context, accountID int, e SignInEvent, actorID int) error {
	if actorID == accountID {
		actorID = 0
	}

	return appendEntry(ctx, u.deps, SignInEntry{AccountID: accountID, Event: e, ActorID: actorID})
}
