package account

import (
	"context"
	"net/url"
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
	// FindByEmail returns the account with the address, compared without case.
	// A missing account is ErrNotFound.
	FindByEmail(ctx context.Context, email string) (Account, error)
	// Update writes the fields of c that are set and leaves the others alone, so a
	// stale form cannot overwrite what somebody else changed meanwhile. A login in
	// use is ErrLoginTaken; a missing account is ErrNotFound.
	Update(ctx context.Context, id int, c Changes) error
}

// Changes are the fields of an account to write; a nil field is left as it is.
type Changes struct {
	Login  *string
	Email  *string
	Name   *string
	Phone  *string
	Locale *string
}

// Order is the column a list is sorted by.
type Order string

// The columns a list can be sorted by.
const (
	OrderLogin   Order = "login"
	OrderName    Order = "name"
	OrderEmail   Order = "email"
	OrderCreated Order = "created_at"
)

// LastSignInFilter is a filter on when the account last signed in.
type LastSignInFilter string

// The sign-in filters of the list.
const (
	// LastSignInNever is the accounts that never signed in.
	LastSignInNever LastSignInFilter = "never"
	// LastSignInIdle is the accounts that signed in before, but not within the idle time
	// (AdminDeps.IdleAfter, 60 days unless set): those who never did are LastSignInNever's.
	LastSignInIdle LastSignInFilter = "idle"
)

// DefaultIdleAfter is how long without signing in makes an account idle (LastSignInIdle).
const DefaultIdleAfter = 60 * 24 * time.Hour

// Query is a page of accounts.
type Query struct {
	// Search matches the login, the name and the e-mail address, without case.
	Search string
	// Active, when set, restricts the list to active or inactive accounts.
	Active *bool
	// Locked, when set, restricts the list to the accounts in LockedIDs (true) or
	// leaves them out (false). The lock is derived from the sign-in history, so
	// the service works it out and hands the store the ids.
	Locked    *bool
	LockedIDs []int
	// Filters are the application's own filters, the query keys of the list that identity has
	// no field for (an application's dealer or role, say): its Searcher reads the ones it knows
	// and leaves the others. The default store knows none and ignores them.
	Filters url.Values
	// LastSignIn, when set, restricts the list to the accounts that never signed in, or that
	// did but not since IdleBefore. A store answers it from the sign-in history (rows of the
	// event signed_in), as in "NOT EXISTS (a signed_in row)" and "EXISTS (one) AND NOT EXISTS
	// (one at or after IdleBefore)".
	LastSignIn LastSignInFilter
	IdleBefore time.Time
	OrderBy    Order
	Desc       bool
	// Page counts from 1.
	Page, PerPage int
}

// Page is one page of accounts and how many there are in all.
type Page struct {
	Accounts []Account
	Total    int
}

// StatusCounts is how many accounts are in each status: active (not locked), locked (active
// and in Query.LockedIDs) and inactive.
type StatusCounts struct {
	Active, Locked, Inactive int
}

// All is every account.
func (c StatusCounts) All() int { return c.Active + c.Locked + c.Inactive }

// Searcher lists and searches accounts.
type Searcher interface {
	Search(ctx context.Context, q Query) (Page, error)
	// Counts says how many accounts match q.Search, q.Filters and q.LastSignIn in each status, in one
	// query; q.LockedIDs are the locked ones, and the rest of q (the status, the order, the page)
	// is ignored.
	Counts(ctx context.Context, q Query) (StatusCounts, error)
}

// SignInHistory is the sign-in log together with what the users module reads
// from it. A store that keeps the log implements both.
type SignInHistory interface {
	SignInLog
	// Entries lists the account's history, newest first, with how many rows match the query.
	Entries(ctx context.Context, q SignInQuery) ([]SignInEntry, int, error)
	// EventCounts counts the account's history rows per event, in one query; an event that
	// never happened is absent.
	EventCounts(ctx context.Context, accountID int) (map[SignInEvent]int, error)
	// LastSignIns returns, for each id that has one, the time of the latest signed_in.
	LastSignIns(ctx context.Context, ids []int) (map[int]time.Time, error)
	// WrongPasswordsSince returns the accounts that had a wrong password after since:
	// the only ones that can be locked at since plus the lock's length.
	WrongPasswordsSince(ctx context.Context, since time.Time) ([]int, error)
}

// SignInQuery asks for a page of an account's sign-in history, newest first. Events narrows it
// to those events (empty: every one); Limit is the page size.
type SignInQuery struct {
	AccountID int
	Events    []SignInEvent
	Offset    int
	Limit     int
}

// ChangeQuery asks for a page of the changes made to an account, newest first. Only narrows it to
// those actions (empty: every one) and Except leaves some out; Limit is the page size.
type ChangeQuery struct {
	AccountID int
	Only      []ChangeAction
	Except    []ChangeAction
	Offset    int
	Limit     int
}

// Change is one change to an account, for its history (IAM-USER-007).
type Change struct {
	AccountID int
	// ActorID is who did it, zero for the person themselves.
	ActorID int
	Action  ChangeAction
	// Fields names what was changed. Before and After hold the values of the
	// fields that are not secret; a password is named but never recorded.
	Fields []string
	Before map[string]string
	After  map[string]string
}

// ChangeAction is what kind of change a history row records.
type ChangeAction string

// The changes on an account's history.
const (
	ChangeCreated     ChangeAction = "created"
	ChangeUpdated     ChangeAction = "updated"
	ChangeDeactivated ChangeAction = "deactivated"
	ChangeActivated   ChangeAction = "activated"
	ChangePassword    ChangeAction = "password"
	ChangeEmail       ChangeAction = "email"
	ChangeProfile     ChangeAction = "profile"
)

// ChangeEntry is a recorded Change.
type ChangeEntry struct {
	ID int
	Change
	At time.Time
	// Ext is what a ChangeLog of the application's own adds to a row (WithChangeLog), such as the
	// records a deactivation handed over; the response carries it as "ext". The default log sets none.
	Ext any
}

// ChangeLog keeps the history of changes to accounts. The default is go-core's
// audit trail (the audit_logs table).
type ChangeLog interface {
	// Record writes a change, in the transaction of the context when it has one.
	Record(ctx context.Context, c Change) error
	// Changes lists the account's history, newest first, with how many rows match the query.
	Changes(ctx context.Context, q ChangeQuery) ([]ChangeEntry, int, error)
	// ChangeCounts counts the account's changes per action, in one query; an action that never
	// happened is absent.
	ChangeCounts(ctx context.Context, accountID int) (map[ChangeAction]int, error)
}

// Transactor runs a function in one database transaction: what it writes
// commits together or not at all, and a store reads the transaction from the
// context it is given. database.Transactor satisfies it.
type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
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

// LoginAs is the fact of signing in as somebody, or of that session ending. Device is
// the label (DeviceLabel of the User-Agent, "Chrome · macOS") and IP the caller's address.
type LoginAs struct {
	ActorID  int
	TargetID int
	Device   string
	IP       string
}

// EndReason says why sessions ended.
type EndReason string

// Why sessions end.
const (
	// EndedLogout: the person signed out of the session.
	EndedLogout EndReason = "logout"
	// EndedRotation: a refresh replaced the session's access token; the old one is dead.
	EndedRotation EndReason = "rotation"
	// EndedRevoked: the person (own session list, "sign out everywhere") or an administrator ended them.
	EndedRevoked EndReason = "revoke"
	// EndedPasswordChanged: a password change ended the other sessions.
	EndedPasswordChanged EndReason = "password_changed"
	// EndedDeactivated: the account was deactivated.
	EndedDeactivated EndReason = "deactivated"
	// EndedLoginAsReturn: the person returned from signing in as somebody.
	EndedLoginAsReturn EndReason = "login_as_return"
	// EndedOpenedBy: the sessions an account had opened as somebody ended with the account's own.
	EndedOpenedBy EndReason = "opened_by_ended"
)

// SessionsEnded is the fact that sessions of one account stopped working. SessionIDs are the
// ones ended; for ending all of an account's sessions they are the ones that were live a
// moment before. ActorID is the administrator who did it, zero for the person themselves
// or when the system did.
type SessionsEnded struct {
	AccountID  int
	SessionIDs []int
	Reason     EndReason
	ActorID    int
}

// Notices hears the facts other features may care about, such as an audit
// trail or a notification. The default does nothing.
type Notices interface {
	// SignedInAs: actor signed in as target, from the request's device and address.
	SignedInAs(ctx context.Context, fact LoginAs)
	// SignedOutAs: the session actor opened as target ended: by returning (the request's device
	// and address), by signing out or by an administrator ending it (those of the ended session).
	SignedOutAs(ctx context.Context, fact LoginAs)
	// SessionsEnded: sessions of one account stopped working, after the change is
	// committed. A host that caches verified tokens drops the account's entries here.
	SessionsEnded(ctx context.Context, fact SessionsEnded)
	// AccountChanged: the account's data (locale, name, phone, e-mail address, password or
	// active flag) changed, after the change is committed. A host that caches the
	// resolved user drops it here.
	AccountChanged(ctx context.Context, accountID int)
	// AccountCreated: actorID made the account.
	AccountCreated(ctx context.Context, accountID, actorID int)
	// AccountDeactivated: actorID deactivated the account.
	AccountDeactivated(ctx context.Context, accountID, actorID int)
	// AccountActivated: actorID activated the account again.
	AccountActivated(ctx context.Context, accountID, actorID int)
	// PasswordChanged: the account's password was changed, by the person
	// (actorID zero) or by an administrator.
	PasswordChanged(ctx context.Context, accountID, actorID int)
	// EmailChanged: the account's address changed from old to new, after a code
	// sent to the new address was confirmed (or by an administrator).
	EmailChanged(ctx context.Context, accountID int, oldEmail, newEmail string)
}

// DiscardNotices is a Notices that does nothing. Embed it in your own to hear
// only some of the facts:
//
//	type audit struct{ account.DiscardNotices }
//
//	func (audit) AccountDeactivated(ctx context.Context, id, actor int) { ... }
type DiscardNotices struct{}

// SignedInAs hears the fact and does nothing.
func (DiscardNotices) SignedInAs(context.Context, LoginAs) {}

// SignedOutAs hears the fact and does nothing.
func (DiscardNotices) SignedOutAs(context.Context, LoginAs) {}

// SessionsEnded hears the fact and does nothing.
func (DiscardNotices) SessionsEnded(context.Context, SessionsEnded) {}

// AccountChanged hears the fact and does nothing.
func (DiscardNotices) AccountChanged(context.Context, int) {}

// AccountCreated hears the fact and does nothing.
func (DiscardNotices) AccountCreated(context.Context, int, int) {}

// AccountDeactivated hears the fact and does nothing.
func (DiscardNotices) AccountDeactivated(context.Context, int, int) {}

// AccountActivated hears the fact and does nothing.
func (DiscardNotices) AccountActivated(context.Context, int, int) {}

// PasswordChanged hears the fact and does nothing.
func (DiscardNotices) PasswordChanged(context.Context, int, int) {}

// EmailChanged hears the fact and does nothing.
func (DiscardNotices) EmailChanged(context.Context, int, string, string) {}

// NoNotices is the Notices that does nothing.
var NoNotices Notices = DiscardNotices{}
