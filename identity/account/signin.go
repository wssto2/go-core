package account

import (
	"time"
)

// SignInEvent is what happened in a sign-in history row. The values are stored and
// reach the client, which names them.
type SignInEvent string

const (
	// SignedIn is a sign-in with the right password.
	SignedIn SignInEvent = "signed_in"
	// WrongPassword is a sign-in refused for a wrong password.
	WrongPassword SignInEvent = "wrong_password"
	// LockedOut is a sign-in refused while locked; the password was not checked.
	LockedOut SignInEvent = "locked_out"
	// RefusedInactive is the right password for an account that is not active.
	RefusedInactive SignInEvent = "refused_inactive"
	// SignedInAs is somebody (ActorID) signing in as the person.
	SignedInAs SignInEvent = "signed_in_as"
	// Unlocked is an administrator (ActorID) lifting the lock.
	Unlocked SignInEvent = "unlocked"
	// SignedOutEverywhere is every session of the person ended by somebody
	// (ActorID): an administrator, a new password, a deactivation, or the person.
	SignedOutEverywhere SignInEvent = "signed_out_everywhere"
	// SessionRevoked is one session ended by somebody (ActorID).
	SessionRevoked SignInEvent = "session_revoked"
)

// LockEvent reports whether e takes part in the lock: a wrong password counts,
// a sign-in or an unlock starts the count again. A refusal while locked
// (LockedOut) is on the record but neither counts nor extends the lock.
func LockEvent(e SignInEvent) bool {
	return e == WrongPassword || e == SignedIn || e == Unlocked
}

// SignInEntry is one row of the sign-in history (IAM-USER-003). AccountID is who it happened to; ActorID is
// who did it when that was somebody else (signing in as them, unlocking, ending
// their sessions), zero otherwise.
type SignInEntry struct {
	ID        int
	AccountID int
	Event     SignInEvent
	IP        string
	Device    string
	ActorID   int
	CreatedAt time.Time
}

// The widths of the stored columns; longer values are cut.
const (
	DeviceMax = 255
	IPMax     = 45
)

// Lock is the rule that locks sign-in after wrong passwords (IAM-USER-002).
// It is never stored: LockedUntil reads it off the history.
type Lock struct {
	// After is how many wrong passwords in a row lock sign-in. Default 5.
	After int
	// For is how long, from the last of them. Default 15 minutes.
	For time.Duration
}

func (l Lock) withDefaults() Lock {
	if l.After <= 0 {
		l.After = 5
	}

	if l.For <= 0 {
		l.For = 15 * time.Minute
	}

	return l
}

// LockedUntil derives the lock (IAM-USER-002) from the account's latest lock
// events, newest first. It reads the first After of them: when there are After
// and every one is a wrong password, sign-in is locked until the newest of them
// plus For. A sign-in or an unlock among them means no lock. After a lock has
// run out, the next wrong password locks again, until the person signs in or
// is unlocked.
func (l Lock) LockedUntil(latest []SignInEntry, now time.Time) (time.Time, bool) {
	l = l.withDefaults()

	if len(latest) < l.After {
		return time.Time{}, false
	}

	for _, e := range latest[:l.After] {
		if e.Event != WrongPassword {
			return time.Time{}, false
		}
	}

	until := latest[0].CreatedAt.Add(l.For)
	if !now.Before(until) {
		return time.Time{}, false
	}

	return until, true
}
