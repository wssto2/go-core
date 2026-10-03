package account

import (
	"time"

	"github.com/wssto2/go-core/apperr"
)

// The reasons of the failures a caller can act on. They are part of the HTTP
// contract: the client translates "errors.<reason>".
const (
	// ReasonSignInFailed is every refused sign-in that is not the lock (IAM-USER-001):
	// an unknown login and a wrong password answer exactly alike.
	ReasonSignInFailed apperr.Reason = "identity.signin.failed"
	// ReasonSignInLocked is the lock after wrong passwords; params.locked_until
	// says until when, as an RFC 3339 time.
	ReasonSignInLocked apperr.Reason = "identity.signin.locked"
	// ReasonSignInInactive is the right password for an account that is not active.
	ReasonSignInInactive apperr.Reason = "identity.signin.inactive"
	// ReasonSessionInvalid is a token that is unknown, revoked or expired.
	ReasonSessionInvalid apperr.Reason = "identity.session.invalid"
	// ReasonSessionCurrent refuses ending, from a list, the session the request
	// came with: that is signing out.
	ReasonSessionCurrent apperr.Reason = "identity.session.current"
	// ReasonSessionNotFound is a session that is not the account's live one.
	ReasonSessionNotFound apperr.Reason = "identity.session.not_found"
	// ReasonAccountNotFound is an account that does not exist.
	ReasonAccountNotFound apperr.Reason = "identity.account.not_found"
	// ReasonAccountInactive is an account that is not active, where it must be.
	ReasonAccountInactive apperr.Reason = "identity.account.inactive"
	// ReasonLocaleInvalid is a locale that is not a BCP-47 language tag.
	ReasonLocaleInvalid apperr.Reason = "identity.locale.invalid"
	// ReasonImpersonationDisabled is signing in as somebody when the application
	// has not said who may.
	ReasonImpersonationDisabled apperr.Reason = "identity.impersonation.disabled"
	// ReasonImpersonationNotActive is returning to one's own account from a
	// session that was not opened by signing in as somebody.
	ReasonImpersonationNotActive apperr.Reason = "identity.impersonation.not_active"
)

// The errors are built with the reason as their message: the person reads the
// client's translation of the reason, never a sentence from here.

func signInFailed() error {
	return apperr.New(nil, string(ReasonSignInFailed), apperr.CodeInvalidArgument).
		WithLog(apperr.LevelInfo).WithReason(ReasonSignInFailed)
}

func signInLocked(until time.Time) error {
	return apperr.New(nil, string(ReasonSignInLocked), apperr.CodeInvalidArgument).
		WithLog(apperr.LevelInfo).
		WithReason(ReasonSignInLocked, map[string]any{"locked_until": until.UTC().Format(time.RFC3339)})
}

func signInInactive() error {
	return apperr.BadRequest(string(ReasonSignInInactive)).WithReason(ReasonSignInInactive)
}

func sessionInvalid() error {
	return apperr.Unauthorized(string(ReasonSessionInvalid)).WithReason(ReasonSessionInvalid)
}

func accountNotFound() error {
	return apperr.NotFound(string(ReasonAccountNotFound)).WithReason(ReasonAccountNotFound)
}

func accountInactive() error {
	return apperr.BadRequest(string(ReasonAccountInactive)).WithReason(ReasonAccountInactive)
}
