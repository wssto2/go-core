package account

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Deps are what the services are built on. Accounts, SignIns, Sessions and
// Clock are required; the rest have defaults.
type Deps struct {
	Accounts Store
	SignIns  SignInLog
	Sessions SessionStore
	Clock    Clock
	// Hasher defaults to Bcrypt.
	Hasher PasswordHasher
	// Impersonation defaults to nobody may.
	Impersonation Impersonation
	// Notices defaults to NoNotices.
	Notices Notices
}

// Config tunes the rules. The zero value is the defaults.
type Config struct {
	// TokenTTL is how long a session lasts, from sign-in and from each refresh. Default 24 hours.
	TokenTTL time.Duration
	// Lock is the lock after wrong passwords. Default 5 wrong passwords, 15 minutes.
	Lock Lock
	// AttemptsPerMinute is how many sign-in attempts of one login are let
	// through a minute, whatever the answers. Default 10.
	AttemptsPerMinute int
	// Codes are the limits of one-time codes (NewCodes takes them).
	Codes CodeRules
	// ReauthLock is the lock after wrong passwords of re-confirmation (NewReauth takes it).
	ReauthLock Lock
	// Locales are the languages an account may have (change-locale, and creating or editing an account):
	// a locale outside them is refused with identity.locale.unsupported (422). Empty accepts any
	// BCP-47 tag. Compared as given: the store maps them to its own codes.
	Locales []string
	// Limits are the widths the host's store keeps, at most go-core's own (LoginMax ..).
	Limits Limits
}

// Limits are the longest values, in characters, the host's store keeps. Zero is
// go-core's own width (LoginMax, NameMax, EmailMax, PhoneMax, the ceiling of the
// input tags): a host limit may only be smaller, so New refuses a larger one.
type Limits struct {
	Login, Name, Email, Phone int
}

// WithDefaults fills the zero limits with go-core's widths.
func (l Limits) WithDefaults() Limits {
	for _, f := range []struct {
		v *int
		d int
	}{{&l.Login, LoginMax}, {&l.Name, NameMax}, {&l.Email, EmailMax}, {&l.Phone, PhoneMax}} {
		if *f.v == 0 {
			*f.v = f.d
		}
	}

	return l
}

func (l Limits) check() error {
	d := l.WithDefaults()
	if l.Login < 0 || l.Name < 0 || l.Email < 0 || l.Phone < 0 ||
		d.Login > LoginMax || d.Name > NameMax || d.Email > EmailMax || d.Phone > PhoneMax {
		return fmt.Errorf("identity: Limits %+v must be positive and no larger than go-core's (login %d, name %d, e-mail %d, phone %d)",
			l, LoginMax, NameMax, EmailMax, PhoneMax)
	}

	return nil
}

// Services is what New builds.
type Services struct {
	SignIn *SignIn
	Users  *Users
}

// New builds the services, or says which dependency is missing.
func New(d Deps, cfg Config) (Services, error) {
	switch {
	case d.Accounts == nil:
		return Services{}, errors.New("identity: Deps.Accounts is missing: pass a Store, for example gormstore.New(db).Accounts")
	case d.SignIns == nil:
		return Services{}, errors.New("identity: Deps.SignIns is missing: pass a SignInLog, for example gormstore.New(db).SignIns")
	case d.Sessions == nil:
		return Services{}, errors.New("identity: Deps.Sessions is missing: pass a SessionStore, for example gormstore.New(db).Sessions")
	case d.Clock == nil:
		return Services{}, errors.New("identity: Deps.Clock is missing: pass the application's clock, app.Clock()")
	}

	if d.Hasher == nil {
		d.Hasher = Bcrypt{}
	}

	if d.Impersonation == nil {
		d.Impersonation = noImpersonation{}
	}

	if d.Notices == nil {
		d.Notices = NoNotices
	}

	if err := cfg.Limits.check(); err != nil {
		return Services{}, err
	}

	cfg.Limits = cfg.Limits.WithDefaults()

	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = 24 * time.Hour
	}

	if cfg.AttemptsPerMinute <= 0 {
		cfg.AttemptsPerMinute = 10
	}

	cfg.Lock = cfg.Lock.withDefaults()

	// A placeholder hash for the password of a login that does not exist, made
	// by the same hasher, so refusing it costs what refusing a wrong password does.
	placeholder, err := d.Hasher.Hash("placeholder")
	if err != nil {
		return Services{}, fmt.Errorf("identity: the password hasher failed: %w", err)
	}

	return Services{
		SignIn: &SignIn{deps: d, cfg: cfg, placeholder: placeholder, attempts: newAttempts(d.Clock, cfg.AttemptsPerMinute)},
		Users:  &Users{deps: d, cfg: cfg},
	}, nil
}

// offers refuses a locale Config.Locales does not list, as a 422 on the field.
func (c Config) offers(field, locale string) error {
	if len(c.Locales) == 0 || slices.Contains(c.Locales, locale) {
		return nil
	}

	return invalid(field, ReasonLocaleUnsupported)
}
