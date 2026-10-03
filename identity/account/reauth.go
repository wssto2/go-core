package account

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/wssto2/go-core/apperr"
)

// ReasonReauthLocked is re-confirming the password while locked (IAM-REAUTH-001);
// params.retry_after is seconds and params.locked_until an RFC 3339 time.
const ReasonReauthLocked apperr.Reason = "identity.reauth.locked"

// ErrWrongPassword is what Reauth.Confirm returns for a password that was checked
// and did not match: the caller reports it on its own field.
var ErrWrongPassword = errors.New("identity: the password does not match")

// ErrReauthNotFound is what ReauthStore.Find returns for an account with no counter yet.
var ErrReauthNotFound = errors.New("identity: no re-confirmation attempts yet")

// ErrReauthConflict is what ReauthStore.Create and Save return when the counter
// changed since it was read, or was created meanwhile; nothing was written.
var ErrReauthConflict = errors.New("identity: the re-confirmation attempts changed since they were read")

// Attempts is one account's re-confirmation counter: the attempts since the last
// correct password, counted before the password is checked (IAM-REAUTH-001).
type Attempts struct {
	AccountID   int
	Failures    int
	LockedUntil *time.Time
	UpdatedAt   time.Time
}

// Locked reports whether re-confirmation is refused at now.
func (a Attempts) Locked(now time.Time) bool {
	return a.LockedUntil != nil && now.Before(*a.LockedUntil)
}

// ReauthStore keeps the counters, one per account.
type ReauthStore interface {
	// Find returns the account's counter, or ErrReauthNotFound.
	Find(ctx context.Context, accountID int) (Attempts, error)
	// Create stores an account's first counter, or returns ErrReauthConflict
	// when it already has one.
	Create(ctx context.Context, a Attempts) error
	// Save writes a only while the stored counter still equals read (same
	// failures and locked_until), so two requests never count from one read;
	// otherwise it writes nothing and returns ErrReauthConflict.
	Save(ctx context.Context, a, read Attempts) error
}

// ReauthDeps are what Reauth is built on; all are required.
type ReauthDeps struct {
	Store  ReauthStore
	Hasher PasswordHasher
	Clock  Clock
}

// Reauth re-confirms an account's current password inside a signed-in session
// (IAM-REAUTH-001), before a password or an e-mail change. Every such check goes
// through it, so one counter limits the guesses across all of them. Sign-in is
// not a re-confirmation and has its own lock.
type Reauth struct {
	deps ReauthDeps
	lock Lock
}

// NewReauth builds the service, or says which dependency is missing. lock is
// the rule: After attempts without a correct password lock re-confirmation For
// a time; the zero value is 5 and 15 minutes.
func NewReauth(d ReauthDeps, lock Lock) (*Reauth, error) {
	switch {
	case d.Store == nil:
		return nil, errors.New("identity: ReauthDeps.Store is missing: pass a ReauthStore, for example gormstore.New(db).Reauth")
	case d.Hasher == nil:
		return nil, errors.New("identity: ReauthDeps.Hasher is missing: pass the PasswordHasher the accounts use")
	case d.Clock == nil:
		return nil, errors.New("identity: ReauthDeps.Clock is missing: pass the application's clock, app.Clock()")
	}

	return &Reauth{deps: d, lock: lock.withDefaults()}, nil
}

// Confirm counts the attempt first, then checks the password against hash: the
// account's stored hash. Parallel requests each have to win a counted attempt,
// so no more than After passwords are checked per lock. The attempt that
// reaches After is still checked and a right password lifts the lock at once;
// while locked the password is not checked at all, so the lock tells an
// attacker nothing. A match clears the count. It returns nil for a match,
// ErrWrongPassword for a mismatch, and an error with ReasonReauthLocked when
// locked, including by this attempt.
func (r *Reauth) Confirm(ctx context.Context, accountID int, hash, password string) error {
	counted, err := r.begin(ctx, accountID)
	if err != nil {
		return err
	}

	if r.deps.Hasher.Matches(hash, password) {
		return r.succeed(ctx, accountID)
	}

	if now := r.deps.Clock.Now(); counted.Locked(now) {
		return reauthLocked(counted, now)
	}

	return ErrWrongPassword
}

// Every conflict means another request's write landed; wrong guesses stop
// writing once the lock is set, so a request loses at most After rounds to
// them, plus one lost create.
func (r *Reauth) rounds() int { return r.lock.After + 2 }

func (r *Reauth) begin(ctx context.Context, accountID int) (Attempts, error) {
	for range r.rounds() {
		read, found, err := r.find(ctx, accountID)
		if err != nil {
			return Attempts{}, err
		}

		now := r.deps.Clock.Now()
		next := read

		if next.Locked(now) {
			return Attempts{}, reauthLocked(next, now)
		}

		if next.LockedUntil != nil { // the lock ran out: a fresh count
			next.Failures, next.LockedUntil = 0, nil
		}

		next.Failures++
		next.UpdatedAt = now

		if next.Failures >= r.lock.After {
			until := now.Add(r.lock.For)
			next.LockedUntil = &until
		}

		if found {
			err = r.deps.Store.Save(ctx, next, read)
		} else {
			err = r.deps.Store.Create(ctx, next)
		}

		switch {
		case err == nil:
			return next, nil
		case !errors.Is(err, ErrReauthConflict):
			return Attempts{}, apperr.Internal(err)
		}
	}

	return Attempts{}, apperr.Internal(ErrReauthConflict)
}

func (r *Reauth) succeed(ctx context.Context, accountID int) error {
	for range r.rounds() {
		read, found, err := r.find(ctx, accountID)
		if err != nil {
			return err
		}

		if !found || (read.Failures == 0 && read.LockedUntil == nil) {
			return nil
		}

		next := Attempts{AccountID: accountID, UpdatedAt: r.deps.Clock.Now()}

		err = r.deps.Store.Save(ctx, next, read)

		switch {
		case err == nil:
			return nil
		case !errors.Is(err, ErrReauthConflict):
			return apperr.Internal(err)
		}
	}

	return apperr.Internal(ErrReauthConflict)
}

func (r *Reauth) find(ctx context.Context, accountID int) (Attempts, bool, error) {
	a, err := r.deps.Store.Find(ctx, accountID)

	switch {
	case err == nil:
		return a, true, nil
	case errors.Is(err, ErrReauthNotFound):
		return Attempts{AccountID: accountID}, false, nil
	default:
		return Attempts{}, false, apperr.Internal(err)
	}
}

func reauthLocked(a Attempts, now time.Time) error {
	until := *a.LockedUntil

	return apperr.New(nil, string(ReasonReauthLocked), apperr.CodeBadRequest).
		WithReason(ReasonReauthLocked, map[string]any{
			"retry_after":  max(int(math.Ceil(until.Sub(now).Seconds())), 1),
			"locked_until": until.UTC().Format(time.RFC3339),
		}).WithLog(apperr.LevelWarn)
}
