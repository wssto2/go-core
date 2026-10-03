package identitytest

import (
	"sync"
	"testing"
	"time"

	"github.com/wssto2/go-core/identity/account"
	"golang.org/x/crypto/bcrypt"
)

// Epoch is the time a Kit's clock starts at.
var Epoch = time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

// Clock is a clock the test moves.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a clock showing at.
func NewClock(at time.Time) *Clock { return &Clock{now: at} }

// Now implements account.Clock.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

// Hasher is a bcrypt at its cheapest cost, so tests that hash stay fast.
var Hasher account.PasswordHasher = account.Bcrypt{Cost: bcrypt.MinCost}

// Account is an active account with the given id, login and password, whose
// language is "en" and whose name is its login.
func Account(id int, login, password string) account.Account {
	hash, _ := Hasher.Hash(password) // bcrypt fails only on a password over 72 bytes or a bad cost; the cost is fixed

	return account.Account{ID: id, Login: login, Name: login, Locale: "en", Active: true, PasswordHash: hash, CreatedAt: Epoch}
}

// Kit is the services over memory stores, with the stores and the clock a test
// reads and moves.
type Kit struct {
	Clock    *Clock
	Accounts *Accounts
	SignIns  *SignIns
	Sessions *Sessions
	SignIn   *account.SignIn
	Users    *account.Users
}

// Option adjusts New.
type Option func(*options)

type options struct {
	cfg  account.Config
	deps func(*account.Deps)
}

// WithConfig sets the rules, such as a shorter lock.
func WithConfig(cfg account.Config) Option { return func(o *options) { o.cfg = cfg } }

// WithImpersonation lets the Impersonation decide who may sign in as whom.
func WithImpersonation(i account.Impersonation) Option {
	return func(o *options) {
		prev := o.deps
		o.deps = func(d *account.Deps) {
			if prev != nil {
				prev(d)
			}

			d.Impersonation = i
		}
	}
}

// WithHasher replaces the password hasher.
func WithHasher(h account.PasswordHasher) Option {
	return func(o *options) {
		prev := o.deps
		o.deps = func(d *account.Deps) {
			if prev != nil {
				prev(d)
			}

			d.Hasher = h
		}
	}
}

// WithNotices hears the facts the services publish.
func WithNotices(n account.Notices) Option {
	return func(o *options) {
		prev := o.deps
		o.deps = func(d *account.Deps) {
			if prev != nil {
				prev(d)
			}

			d.Notices = n
		}
	}
}

// New builds the services over memory stores seeded with the accounts, on a
// clock that shows Epoch.
func New(t testing.TB, seed []account.Account, opts ...Option) Kit {
	t.Helper()

	var o options
	for _, opt := range opts {
		opt(&o)
	}

	kit := Kit{Clock: NewClock(Epoch), Accounts: NewAccounts(), SignIns: NewSignIns(), Sessions: NewSessions()}

	for _, a := range seed {
		if _, err := kit.Accounts.Create(t.Context(), a); err != nil {
			t.Fatalf("identitytest: seeding %q: %v", a.Login, err)
		}
	}

	deps := account.Deps{
		Accounts: kit.Accounts, SignIns: kit.SignIns, Sessions: kit.Sessions, Clock: kit.Clock, Hasher: Hasher,
	}
	if o.deps != nil {
		o.deps(&deps)
	}

	svc, err := account.New(deps, o.cfg)
	if err != nil {
		t.Fatalf("identitytest: %v", err)
	}

	kit.SignIn, kit.Users = svc.SignIn, svc.Users

	return kit
}

// Users is the Users service over memory stores holding the accounts: what a
// feature that takes identity's Users is given in its own tests.
func Users(t testing.TB, seed ...account.Account) *account.Users { return New(t, seed).Users }
