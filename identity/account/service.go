package account

import (
	"errors"
	"fmt"
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
