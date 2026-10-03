// Package account is the framework-free core of go-core's identity module:
// accounts, password sign-in, sessions and the lock after wrong passwords. It
// imports no web framework and no database; the ports in ports.go are what
// the stores (identity/gormstore, identity/identitytest, or an application's
// own) implement.
//
// Two services do the work, both built on the ports:
//
//   - [SignIn]: Login, Refresh, Logout, LoginAs and Authenticate;
//   - [Users]: Get, ChangeLocale and the sessions of an account.
//
// Failures a caller can act on are *apperr.AppError values whose Reason is one
// of the Reason* constants, with params where they help; the sign-in screen
// translates the reason, the module never writes a sentence for a person.
package account

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// Clock tells the time. gocore.Clock satisfies it.
type Clock interface {
	Now() time.Time
}

// Account is a person who signs in.
type Account struct {
	ID int
	// Login is what the person types to sign in, stored lower-case.
	Login string
	Email string
	// Phone is optional, free text of at most PhoneMax characters.
	Phone string
	Name  string
	// Locale is a BCP-47 language tag such as "hr" or "en".
	Locale string
	Active bool
	// PasswordHash is the output of a PasswordHasher. It never leaves the module:
	// no DTO carries it.
	PasswordHash string
	CreatedAt    time.Time
}

// GetID makes an Account an auth.Identifiable, so go-core's rate limiting and
// audit read who is signed in.
func (a Account) GetID() int { return a.ID }

var (
	// ErrNotFound is what a Store returns for an account that does not exist.
	ErrNotFound = errors.New("identity: account not found")
	// ErrLoginTaken is what Store.Create and Store.Update return when the login is in use.
	ErrLoginTaken = errors.New("identity: login already in use")
	// ErrEmailTaken is what Store.Create and Store.Update return when the store
	// itself refuses an e-mail address that belongs to another account (a store
	// with a unique index). The services check before writing, so a store
	// without one still gets one account per address, except under a race.
	ErrEmailTaken = errors.New("identity: e-mail address already in use")
)

// The widths of the stored fields; the services refuse longer values.
const (
	LoginMax = 100
	NameMax  = 150
	EmailMax = 255
	PhoneMax = 30
)

// NormalizeLogin is how a login is compared and stored: without surrounding
// space and without case. Services pass logins through it; stores keep and
// match the normalised form.
func NormalizeLogin(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}

var locale = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// ValidLocale reports whether s is a BCP-47 language tag: "hr", "en", "pt-BR".
func ValidLocale(s string) bool { return locale.MatchString(s) }
