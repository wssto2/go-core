package identitytest

import (
	"context"
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
	// Codes and Reauth are the one-time code and password re-confirmation
	// services; Mailbox holds what Codes "sent", where a test reads the code.
	Codes   *account.Codes
	Reauth  *account.Reauth
	Mailbox *Mailbox
	// Admin is the users module; Changes is the history it writes.
	Admin   *account.Admin
	Changes *ChangeLog
	// Activity reads what people did: the changes they made and the sessions opened as them.
	Activity *ActivityLog
	// Profile is what a person does with their own account.
	Profile *account.Profile
}

// CodeSecret is the secret the Kit's codes are hashed with.
const CodeSecret = "identitytest-secret-of-32-characters!"

// Mailbox is a CodeSender that keeps what it is given, so a test reads the code
// a person would have been mailed.
type Mailbox struct {
	mu   sync.Mutex
	sent []account.CodeMessage
	// Fail, when set, is returned by SendCode instead of recording: a mail
	// that cannot be delivered.
	Fail error
}

// SendCode implements account.CodeSender.
func (m *Mailbox) SendCode(_ context.Context, msg account.CodeMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Fail != nil {
		return m.Fail
	}

	m.sent = append(m.sent, msg)

	return nil
}

// Sent returns every message, oldest first.
func (m *Mailbox) Sent() []account.CodeMessage {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]account.CodeMessage(nil), m.sent...)
}

// Last returns the latest message; its Code is what the person types. It is the
// zero value when nothing was sent.
func (m *Mailbox) Last() account.CodeMessage {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.sent) == 0 {
		return account.CodeMessage{}
	}

	return m.sent[len(m.sent)-1]
}

// Option adjusts New.
type Option func(*options)

type options struct {
	cfg   account.Config
	deps  func(*account.Deps)
	codes account.CodeRules
	lock  account.Lock
	// policy and hooks are the users module's.
	policy account.PasswordPolicy
	hooks  []account.DeactivationHook
	noMail bool
}

// WithoutMail builds the Profile without codes: changing the e-mail address is
// refused, as in an application that runs without a mail sender.
func WithoutMail() Option { return func(o *options) { o.noMail = true } }

// WithPasswordPolicy replaces the rules for new passwords.
func WithPasswordPolicy(p account.PasswordPolicy) Option { return func(o *options) { o.policy = p } }

// WithDeactivationHooks asks the hooks before an account is deactivated.
func WithDeactivationHooks(h ...account.DeactivationHook) Option {
	return func(o *options) { o.hooks = append(o.hooks, h...) }
}

// WithCodeRules sets the limits of one-time codes, such as a shorter cooldown.
func WithCodeRules(r account.CodeRules) Option { return func(o *options) { o.codes = r } }

// WithReauthLock sets the lock after wrong passwords of re-confirmation.
func WithReauthLock(l account.Lock) Option { return func(o *options) { o.lock = l } }

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
	kit.Mailbox = &Mailbox{}

	codes, err := account.NewCodes(account.CodesDeps{Store: NewCodes(), Sender: kit.Mailbox, Clock: kit.Clock, Secret: CodeSecret}, o.codes)
	if err != nil {
		t.Fatalf("identitytest: %v", err)
	}

	reauth, err := account.NewReauth(account.ReauthDeps{Store: NewReauth(), Hasher: deps.Hasher, Clock: kit.Clock}, o.lock)
	if err != nil {
		t.Fatalf("identitytest: %v", err)
	}

	kit.Codes, kit.Reauth = codes, reauth

	kit.Changes = NewChangeLog(kit.Clock)
	kit.Activity = NewActivityLog(kit.Changes, kit.Sessions)

	kit.Admin, err = account.NewAdmin(account.AdminDeps{
		Users: svc.Users, Search: kit.Accounts, History: kit.SignIns, Changes: kit.Changes, Activity: kit.Activity, Transact: Transactor{},
		Policy: o.policy, Hooks: o.hooks,
	})
	if err != nil {
		t.Fatalf("identitytest: %v", err)
	}

	profileCodes := kit.Codes
	if o.noMail {
		profileCodes = nil
	}

	kit.Profile, err = account.NewProfile(account.ProfileDeps{
		Users: svc.Users, Reauth: kit.Reauth, History: kit.SignIns, Changes: kit.Changes, Transact: Transactor{},
		Policy: o.policy, Codes: profileCodes,
	})
	if err != nil {
		t.Fatalf("identitytest: %v", err)
	}

	return kit
}

// Users is the Users service over memory stores holding the accounts: what a
// feature that takes identity's Users is given in its own tests.
func Users(t testing.TB, seed ...account.Account) *account.Users { return New(t, seed).Users }
