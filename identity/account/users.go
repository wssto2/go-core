package account

import (
	"context"
	"errors"
	"slices"
	"time"

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
	// set by identity.Install when it has service accounts.
	robots ServiceAccounts
}

// ServiceAccounts is what Users asks of the service accounts identity installs (identity.WithServiceAccounts): their
// names, for access.SubjectDirectory, and whether the actor may see one at all.
type ServiceAccounts interface {
	// Names returns the name of each service account among ids that exists.
	Names(ctx context.Context, ids []int) (map[int]string, error)
	// Reach reports whether the actor may see the service account (false for one of another tenant, or that does not exist).
	Reach(ctx context.Context, id int) (bool, error)
}

// SetServiceAccounts tells Users about the service accounts of the application. identity.Install calls it.
func (u *Users) SetServiceAccounts(s ServiceAccounts) { u.robots = s }

// HasServiceAccounts reports whether the application has service accounts (identity.WithServiceAccounts). access.Install
// asks it, to serve their access routes and name them as subjects.
func (u *Users) HasServiceAccounts() bool { return u.robots != nil }

// ServiceAccountReach says whether the actor may see the service account; false when there are none.
func (u *Users) ServiceAccountReach(ctx context.Context, id int) (bool, error) {
	if u.robots == nil {
		return false, nil
	}

	return u.robots.Reach(ctx, id)
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
// accounts are named when identity has them (WithServiceAccounts), and left out otherwise.
func (u *Users) SubjectNames(ctx context.Context, subjects []authz.Subject) (map[authz.Subject]string, error) {
	var ids, robotIDs []int

	for _, s := range subjects {
		switch s.Kind {
		case authz.KindUser:
			ids = append(ids, s.ID)
		case authz.KindServiceAccount:
			robotIDs = append(robotIDs, s.ID)
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

	if u.robots != nil && len(robotIDs) > 0 {
		robots, err := u.robots.Names(ctx, robotIDs)
		if err != nil {
			return nil, err
		}

		for id, name := range robots {
			out[authz.Subject{Kind: authz.KindServiceAccount, ID: id}] = name
		}
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

func (p pendingNotices) announce(ctx context.Context, hear Notices) {
	for _, e := range p.ended {
		hear.SessionsEnded(ctx, e)
	}

	for _, f := range p.signedOut {
		hear.SignedOutAs(ctx, f)
	}
}

// endedSession is the fact that one session ended, by the person or the system.
func endedSession(session Session, why EndReason) SessionsEnded {
	return SessionsEnded{AccountID: session.AccountID, SessionIDs: []int{session.ID}, Reason: why, ActorID: 0}
}

// keptSession is the id of the live session KeepToken names, when it is the account's; else zero.
func (u *Users) keptSession(ctx context.Context, req RevokeSessionsInput) int {
	if req.KeepToken == "" {
		return 0
	}

	found, err := u.deps.Sessions.Get(ctx, req.KeepToken, u.deps.Clock.Now())
	if err != nil || found.AccountID != req.AccountID {
		return 0
	}

	return found.ID
}

// revokeSessions does RevokeSessions and returns the facts instead of telling them.
func (u *Users) revokeSessions(ctx context.Context, req RevokeSessionsInput) (pendingNotices, error) {
	var out pendingNotices

	if req.ActorID == req.AccountID {
		req.ActorID = 0
	}

	if req.Reason == "" {
		req.Reason = EndedRevoked
	}

	keep, now := u.keptSession(ctx, req), u.deps.Clock.Now()

	// The ids are read just before they are ended: a session opened req between is ended
	// but missing from the fact, which names the account all the same.
	ids, err := u.liveExcept(ctx, req.AccountID, keep, now)
	if err != nil {
		return out, err
	}

	_, err = u.deps.Sessions.End(ctx, req.AccountID, nil, keep, now)
	if err != nil {
		return out, apperr.Internal(err)
	}

	err = u.record(ctx, req.AccountID, SignedOutEverywhere, req.ActorID)
	if err != nil {
		return out, err
	}

	out.ended = append(out.ended, SessionsEnded{
		AccountID: req.AccountID, SessionIDs: ids, Reason: req.Reason, ActorID: req.ActorID,
	})

	return u.endOpenedBy(ctx, req, now, out)
}

// endOpenedBy ends the sessions the account opened by signing req as somebody else, which belong to those
// people, and adds their facts to out.
func (u *Users) endOpenedBy(
	ctx context.Context, req RevokeSessionsInput, now time.Time, out pendingNotices,
) (pendingNotices, error) {
	opened, err := u.deps.Sessions.EndOpenedBy(ctx, req.AccountID, now)
	if err != nil {
		return out, apperr.Internal(err)
	}

	for _, theirs := range opened {
		// The actor of the target's history is who ended it, not the target.
		err = u.record(ctx, theirs.AccountID, SessionRevoked, req.ActorID)
		if err != nil {
			return out, err
		}

		out.signedOut = append(out.signedOut, LoginAs{
			ActorID: req.AccountID, TargetID: theirs.AccountID, Device: theirs.Label(), IP: theirs.IP,
		})
	}

	out.ended = append(out.ended, endedOpenedBy(opened, req.ActorID)...)

	return out, nil
}

// liveExcept is the ids of the account's live sessions but keep, in order.
func (u *Users) liveExcept(ctx context.Context, accountID, keep int, now time.Time) ([]int, error) {
	live, err := u.deps.Sessions.Live(ctx, accountID, now)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	var ids []int

	for _, live := range live {
		if live.ID != keep {
			ids = append(ids, live.ID)
		}
	}

	slices.Sort(ids)

	return ids, nil
}

// endedOpenedBy is one fact for each account whose sessions, opened by somebody, ended.
func endedOpenedBy(opened []Session, actorID int) []SessionsEnded {
	var out []SessionsEnded

	index := map[int]int{} // account -> its place in out

	for _, theirs := range opened {
		if i, ok := index[theirs.AccountID]; ok {
			out[i].SessionIDs = append(out[i].SessionIDs, theirs.ID)

			continue
		}

		index[theirs.AccountID] = len(out)
		out = append(out, SessionsEnded{
			AccountID: theirs.AccountID, SessionIDs: []int{theirs.ID}, Reason: EndedOpenedBy, ActorID: actorID,
		})
	}

	return out
}

// record writes the event; the actor is kept only when it is somebody else than the person.
func (u *Users) record(ctx context.Context, accountID int, e SignInEvent, actorID int) error {
	if actorID == accountID {
		actorID = 0
	}

	return appendEntry(ctx, u.deps, SignInEntry{AccountID: accountID, Event: e, ActorID: actorID})
}
