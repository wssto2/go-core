package account

import (
	"context"
	"time"
)

// Store keeps accounts. Logins are passed normalised (NormalizeLogin).
// A missing account is ErrNotFound.
type Store interface {
	Find(ctx context.Context, id int) (Account, error)
	FindByLogin(ctx context.Context, login string) (Account, error)
	// FindMany returns the accounts that exist among ids, in no particular order;
	// a missing id is simply absent.
	FindMany(ctx context.Context, ids []int) ([]Account, error)
	// Create stores a new account, assigns its ID and returns it. A login in
	// use is ErrLoginTaken.
	Create(ctx context.Context, a Account) (Account, error)
	SetLocale(ctx context.Context, id int, locale string) error
	SetPasswordHash(ctx context.Context, id int, hash string) error
	SetActive(ctx context.Context, id int, active bool) error
}

// SignInLog keeps the sign-in history the lock is derived from.
type SignInLog interface {
	// Record appends one row.
	Record(ctx context.Context, row SignInEntry) error
	// LockEvents returns the account's latest n lock events (see LockEvent),
	// newest first.
	LockEvents(ctx context.Context, accountID, n int) ([]SignInEntry, error)
}

// SessionStore keeps sessions: an access token, a refresh token and who they
// are for. A session that is revoked, expired or unknown is ErrSessionNotFound.
type SessionStore interface {
	// Open opens a session and returns the tokens, shown once.
	Open(ctx context.Context, s NewSession) (Credentials, error)
	// Get returns the session behind an access token that is live at now.
	Get(ctx context.Context, accessToken string, now time.Time) (Session, error)
	// Touch records that the session was used.
	Touch(ctx context.Context, sessionID int, at time.Time, ip string) error
	// Rotate swaps the tokens of the session whose refresh token this is, in
	// one atomic step: of two calls with one token, exactly one succeeds.
	Rotate(ctx context.Context, r Rotation) (Session, Credentials, error)
	// Live lists the account's live sessions at now, latest used first.
	Live(ctx context.Context, accountID int, now time.Time) ([]Session, error)
	// End revokes the account's live sessions: the ones in ids, or all of them
	// when ids is empty, but never keep (zero keeps none). It returns how many.
	End(ctx context.Context, accountID int, ids []int, keep int, now time.Time) (int, error)
	// EndOpenedBy revokes every live session actorID opened by signing in as
	// somebody else, and returns them.
	EndOpenedBy(ctx context.Context, actorID int, now time.Time) ([]Session, error)
}

// PasswordHasher hashes and checks passwords. Bcrypt is the default.
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Matches reports whether password is the one hash was made from. It must
	// do the same work whatever hash is, so a refusal costs the same for an
	// unknown login as for a wrong password.
	Matches(hash, password string) bool
}

// Impersonation decides who may sign in as somebody else. An application
// supplies it; without one, nobody may.
type Impersonation interface {
	// Permitted refuses an actor who may not sign in as anybody. It runs before
	// the target is looked up, so it does not reveal who exists.
	Permitted(ctx context.Context, actor Account) error
	// Covers refuses a target the actor may not sign in as, for example one who
	// can do more than the actor can.
	Covers(ctx context.Context, actor, target Account) error
}

// Notices hears the facts other features may care about, such as an audit
// trail or a notification. The default does nothing.
type Notices interface {
	// SignedInAs: actor signed in as target.
	SignedInAs(ctx context.Context, actorID, targetID int)
	// SignedOutAs: the session actor opened as target ended.
	SignedOutAs(ctx context.Context, actorID, targetID int)
	// SessionsRevoked: n sessions of accountID were ended by actorID (zero when
	// the person did it themselves).
	SessionsRevoked(ctx context.Context, accountID, actorID, n int)
}

type noNotices struct{}

func (noNotices) SignedInAs(context.Context, int, int)           {}
func (noNotices) SignedOutAs(context.Context, int, int)          {}
func (noNotices) SessionsRevoked(context.Context, int, int, int) {}

// NoNotices is the Notices that does nothing.
var NoNotices Notices = noNotices{}
