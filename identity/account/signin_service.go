package account

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wssto2/go-core/apperr"
)

// SignIn signs people in and out, and knows who a token belongs to.
type SignIn struct {
	deps        Deps
	cfg         Config
	placeholder string
	attempts    *attempts
}

// LoginInput is a sign-in attempt. Device is the User-Agent, IP the caller's address.
type LoginInput struct {
	Login    string
	Password string
	Device   string
	IP       string
}

// Signed is a signed-in account and its session's tokens.
type Signed struct {
	Account     Account
	Credentials Credentials
}

// Login signs a person in (IAM-USER-001, IAM-USER-002). More than the allowed
// attempts of one login in a minute are refused like a lock, whoever they are;
// the lock is checked before the password, so a locked person's password is
// never compared; every other refusal (an unknown login, a wrong password) is
// the same ReasonSignInFailed, after the same work. An unknown login is not
// recorded: there is nobody to attach it to. Whether the account is active is
// said only to whoever knows its password.
func (s *SignIn) Login(ctx context.Context, in LoginInput) (Signed, error) {
	login := NormalizeLogin(in.Login)

	// Attempts made at the same moment cannot all get past the lock before it is written.
	if until, ok := s.attempts.allow(login); !ok {
		return Signed{}, signInLocked(until)
	}

	acc, err := s.deps.Accounts.FindByLogin(ctx, login)
	if errors.Is(err, ErrNotFound) {
		s.deps.Hasher.Matches(s.placeholder, in.Password)

		return Signed{}, signInFailed()
	}

	if err != nil {
		return Signed{}, apperr.Internal(err)
	}

	record := func(e Event) error { return s.record(ctx, acc.ID, e, 0, in.Device, in.IP) }

	latest, err := s.deps.SignIns.LockEvents(ctx, acc.ID, s.cfg.Lock.After)
	if err != nil {
		return Signed{}, apperr.Internal(err)
	}

	if until, locked := s.cfg.Lock.LockedUntil(latest, s.deps.Clock.Now()); locked {
		if err := record(LockedOut); err != nil {
			return Signed{}, err
		}

		return Signed{}, signInLocked(until)
	}

	if !s.deps.Hasher.Matches(acc.PasswordHash, in.Password) {
		if err := record(WrongPassword); err != nil {
			return Signed{}, err
		}

		return Signed{}, signInFailed()
	}

	if !acc.Active {
		if err := record(RefusedInactive); err != nil {
			return Signed{}, err
		}

		return Signed{}, signInInactive()
	}

	creds, err := s.open(ctx, NewSession{AccountID: acc.ID, Device: in.Device, IP: in.IP})
	if err != nil {
		return Signed{}, err
	}

	if err := record(SignedIn); err != nil {
		return Signed{}, err
	}

	return Signed{Account: acc, Credentials: creds}, nil
}

// RefreshInput is a request for new tokens. Token is the refresh token.
type RefreshInput struct {
	Token  string
	Device string
	IP     string
}

// Refresh swaps a refresh token for new tokens. The old pair stops working; of
// two requests with one token, one succeeds. A session opened by signing in as
// somebody stays marked as one.
func (s *SignIn) Refresh(ctx context.Context, in RefreshInput) (Signed, error) {
	now := s.deps.Clock.Now()

	session, creds, err := s.deps.Sessions.Rotate(ctx, Rotation{
		Refresh: in.Token, At: now, ExpiresAt: now.Add(s.cfg.TokenTTL),
		Device: cut(strings.TrimSpace(in.Device), DeviceMax), IP: cut(strings.TrimSpace(in.IP), IPMax),
	})
	if errors.Is(err, ErrSessionNotFound) {
		return Signed{}, sessionInvalid()
	}

	if err != nil {
		return Signed{}, apperr.Internal(err)
	}

	acc, err := s.deps.Accounts.Find(ctx, session.AccountID)
	if errors.Is(err, ErrNotFound) || (err == nil && !acc.Active) {
		return Signed{}, sessionInvalid()
	}

	if err != nil {
		return Signed{}, apperr.Internal(err)
	}

	return Signed{Account: acc, Credentials: creds}, nil
}

// Logout ends the session behind an access token. A session opened by signing
// in as somebody says so to the Notices when it ends.
func (s *SignIn) Logout(ctx context.Context, accessToken string) error {
	session, err := s.deps.Sessions.Get(ctx, accessToken, s.deps.Clock.Now())
	if errors.Is(err, ErrSessionNotFound) {
		return sessionInvalid()
	}

	if err != nil {
		return apperr.Internal(err)
	}

	if _, err := s.deps.Sessions.End(ctx, session.AccountID, []int{session.ID}, 0, s.deps.Clock.Now()); err != nil {
		return apperr.Internal(err)
	}

	if session.ActorID > 0 {
		s.deps.Notices.SignedOutAs(ctx, session.ActorID, session.AccountID)
	}

	return nil
}

// LoginAsInput is somebody (ActorID) signing in as somebody else (TargetID).
type LoginAsInput struct {
	ActorID  int
	TargetID int
	Device   string
	IP       string
}

// LoginAs opens a session for TargetID on ActorID's behalf. The Impersonation
// says whether the actor may at all (before the target is looked up, so it does
// not reveal who exists) and whether this target is within reach. The session
// carries the actor, and the target's history says who signed in as them.
func (s *SignIn) LoginAs(ctx context.Context, in LoginAsInput) (Signed, error) {
	actor, err := s.deps.Accounts.Find(ctx, in.ActorID)
	if errors.Is(err, ErrNotFound) || (err == nil && !actor.Active) {
		return Signed{}, sessionInvalid()
	}

	if err != nil {
		return Signed{}, apperr.Internal(err)
	}

	if err := s.deps.Impersonation.Permitted(ctx, actor); err != nil {
		return Signed{}, err
	}

	target, err := s.deps.Accounts.Find(ctx, in.TargetID)
	if errors.Is(err, ErrNotFound) {
		return Signed{}, accountNotFound()
	}

	if err != nil {
		return Signed{}, apperr.Internal(err)
	}

	if !target.Active {
		return Signed{}, accountInactive()
	}

	if err := s.deps.Impersonation.Covers(ctx, actor, target); err != nil {
		return Signed{}, err
	}

	creds, err := s.open(ctx, NewSession{AccountID: target.ID, ActorID: actor.ID, Device: in.Device, IP: in.IP})
	if err != nil {
		return Signed{}, err
	}

	if err := s.record(ctx, target.ID, SignedInAs, actor.ID, in.Device, in.IP); err != nil {
		return Signed{}, err
	}

	s.deps.Notices.SignedInAs(ctx, actor.ID, target.ID)

	return Signed{Account: target, Credentials: creds}, nil
}

// touchEvery is how often a session's last use is written: once a minute at
// most, not on every request.
const touchEvery = time.Minute

// Authenticated is the account and session behind an access token.
type Authenticated struct {
	Account Account
	Session Session
}

// Authenticate resolves an access token to its live session and its account,
// which must be active. Anything else is ReasonSessionInvalid.
func (s *SignIn) Authenticate(ctx context.Context, accessToken string) (Authenticated, error) {
	if accessToken == "" {
		return Authenticated{}, sessionInvalid()
	}

	session, err := s.deps.Sessions.Get(ctx, accessToken, s.deps.Clock.Now())
	if errors.Is(err, ErrSessionNotFound) {
		return Authenticated{}, sessionInvalid()
	}

	if err != nil {
		return Authenticated{}, apperr.Internal(err)
	}

	acc, err := s.deps.Accounts.Find(ctx, session.AccountID)
	if errors.Is(err, ErrNotFound) || (err == nil && !acc.Active) {
		return Authenticated{}, sessionInvalid()
	}

	if err != nil {
		return Authenticated{}, apperr.Internal(err)
	}

	if now := s.deps.Clock.Now(); now.Sub(session.LastUsedAt) >= touchEvery {
		if err := s.deps.Sessions.Touch(ctx, session.ID, now, session.IP); err != nil {
			return Authenticated{}, apperr.Internal(err)
		}
	}

	return Authenticated{Account: acc, Session: session}, nil
}

func (s *SignIn) open(ctx context.Context, n NewSession) (Credentials, error) {
	n.At = s.deps.Clock.Now()
	n.ExpiresAt = n.At.Add(s.cfg.TokenTTL)
	n.Device = cut(strings.TrimSpace(n.Device), DeviceMax)
	n.IP = cut(strings.TrimSpace(n.IP), IPMax)

	creds, err := s.deps.Sessions.Open(ctx, n)
	if err != nil {
		return Credentials{}, apperr.Internal(err)
	}

	return creds, nil
}

func (s *SignIn) record(ctx context.Context, accountID int, e Event, actorID int, device, ip string) error {
	return appendEntry(ctx, s.deps, SignInEntry{AccountID: accountID, Event: e, ActorID: actorID, Device: device, IP: ip})
}

// appendEntry appends a history row stamped with now, its text cut to the columns.
func appendEntry(ctx context.Context, d Deps, row SignInEntry) error {
	row.CreatedAt = d.Clock.Now()
	row.Device = cut(strings.TrimSpace(row.Device), DeviceMax)
	row.IP = cut(strings.TrimSpace(row.IP), IPMax)

	if err := d.SignIns.Record(ctx, row); err != nil {
		return apperr.Internal(err)
	}

	return nil
}

// cut keeps the first width characters of s.
func cut(s string, width int) string {
	if utf8.RuneCountInString(s) <= width {
		return s
	}

	return string([]rune(s)[:width])
}

type noImpersonation struct{}

func (noImpersonation) Permitted(context.Context, Account) error {
	return apperr.Forbidden(string(ReasonImpersonationDisabled)).WithReason(ReasonImpersonationDisabled)
}

func (noImpersonation) Covers(context.Context, Account, Account) error { return nil }
