package account

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/wssto2/go-core/apperr"
)

// DeactivationHook lets the application take part in deactivating an account:
// hand its records over, or refuse because it still owns something. It runs in
// the transaction of the deactivation, before anything is written; returning an
// error vetoes it and nothing changes, so return an *apperr.AppError whose reason
// the client can explain. What a hook writes with the context it is given is in
// the same transaction.
type DeactivationHook interface {
	Deactivating(ctx context.Context, a Account, actorID int) error
}

// DeactivationHookFunc is a DeactivationHook made of a function.
type DeactivationHookFunc func(ctx context.Context, a Account, actorID int) error

// Deactivating calls f.
func (f DeactivationHookFunc) Deactivating(ctx context.Context, a Account, actorID int) error {
	return f(ctx, a, actorID)
}

// AdminDeps are what Admin is built on. Users, Search, History, Changes and
// Transact are required; Policy defaults to Passwords{}. Users carries the
// accounts, the hasher, the clock and the Notices.
type AdminDeps struct {
	Users    *Users
	Search   Searcher
	History  SignInHistory
	Changes  ChangeLog
	Transact Transactor
	Policy   PasswordPolicy
	// Hooks are asked, in order, before an account is deactivated.
	Hooks []DeactivationHook
}

// Admin is the users module: what an administrator does with other people's
// accounts. Who may is the caller's to decide: the routes require the
// permissions, the services trust their caller. Every method takes the actor,
// zero for the person themselves.
type Admin struct {
	d AdminDeps
}

// NewAdmin builds the service, or says which dependency is missing.
func NewAdmin(d AdminDeps) (*Admin, error) {
	switch {
	case d.Users == nil:
		return nil, errors.New("identity: AdminDeps.Users is missing: pass the Users service account.New built")
	case d.Search == nil:
		return nil, errors.New("identity: AdminDeps.Search is missing: pass a Searcher, for example gormstore.New(db).Accounts")
	case d.History == nil:
		return nil, errors.New("identity: AdminDeps.History is missing: pass a SignInHistory, for example gormstore.New(db).SignIns")
	case d.Changes == nil:
		return nil, errors.New("identity: AdminDeps.Changes is missing: pass a ChangeLog, for example gormstore.NewChangeLog(db)")
	case d.Transact == nil:
		return nil, errors.New("identity: AdminDeps.Transact is missing: pass a Transactor, for example database.NewTransactor(db)")
	}

	if d.Policy == nil {
		d.Policy = Passwords{}
	}

	a := &Admin{d: d}
	d.Users.admin = a

	return a, nil
}

func (a *Admin) deps() Deps { return a.d.Users.deps }

// within runs fn in one transaction; what is not already an application error is an internal one.
func (a *Admin) within(ctx context.Context, fn func(ctx context.Context) error) error {
	err := a.d.Transact.WithinTransaction(ctx, fn)

	var ae *apperr.AppError
	if err == nil || errors.As(err, &ae) {
		return err
	}

	return apperr.Internal(err)
}

// stored maps what a store says to what a caller can act on.
func stored(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return accountNotFound()
	case errors.Is(err, ErrLoginTaken):
		return conflict("login", ReasonLoginTaken)
	case errors.Is(err, ErrEmailTaken):
		return conflict("email", ReasonEmailTaken)
	}

	var ae *apperr.AppError
	if errors.As(err, &ae) {
		return err
	}

	return apperr.Internal(err)
}

// emailFree refuses an address that belongs to another account (the one with
// the id except, which is the person's own).
func (a *Admin) emailFree(ctx context.Context, email string, except int) error {
	return emailFree(ctx, a.deps().Accounts, email, except)
}

func emailFree(ctx context.Context, accounts Store, email string, except int) error {
	owner, err := accounts.FindByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		return nil
	}

	if err != nil {
		return apperr.Internal(err)
	}

	if owner.ID != except {
		return conflict("email", ReasonEmailTaken)
	}

	return nil
}

// CreateAccount is a new person. Locale is a BCP-47 tag; Phone is optional.
type CreateAccount struct {
	Login    string
	Name     string
	Email    string
	Phone    string
	Locale   string
	Password string
	ActorID  int
}

// Create makes an active account (IDENTITY-ADMIN-001). The login and the address are normalised, a
// login or an address that belongs to somebody is refused (identity.login.taken,
// identity.email.taken), the password must satisfy the policy, and the creation
// is on the account's history. Signing in with the password works at once.
func (a *Admin) Create(ctx context.Context, in CreateAccount) (Account, error) {
	acc := Account{
		Login: NormalizeLogin(in.Login), Email: NormalizeEmail(in.Email), Name: strings.TrimSpace(in.Name),
		Phone: strings.TrimSpace(in.Phone), Locale: in.Locale, Active: true,
	}

	if err := checkAll(
		checkLogin(acc.Login), checkName(acc.Name), checkEmail("email", acc.Email), checkPhone(acc.Phone),
		checkLocale(acc.Locale), checkPassword(a.d.Policy, "password", in.Password),
	); err != nil {
		return Account{}, err
	}

	if err := a.emailFree(ctx, acc.Email, 0); err != nil {
		return Account{}, err
	}

	hash, err := a.deps().Hasher.Hash(in.Password)
	if err != nil {
		return Account{}, apperr.Internal(err)
	}

	acc.PasswordHash, acc.CreatedAt = hash, a.deps().Clock.Now()

	err = a.within(ctx, func(ctx context.Context) error {
		created, err := a.deps().Accounts.Create(ctx, acc)
		if err != nil {
			return stored(err)
		}

		acc = created

		return a.d.Changes.Record(ctx, Change{
			AccountID: acc.ID, ActorID: in.ActorID, Action: ChangeCreated, Fields: []string{"login", "name", "email", "phone", "locale"},
			After: map[string]string{"login": acc.Login, "name": acc.Name, "email": acc.Email, "phone": acc.Phone, "locale": acc.Locale},
		})
	})
	if err != nil {
		return Account{}, err
	}

	a.deps().Notices.AccountCreated(ctx, acc.ID, in.ActorID)

	return acc, nil
}

// checkAll returns the first error, or nil. A field error names its field, so a
// form shows them one at a time.
func checkAll(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	return nil
}

func checkLocale(locale string) error {
	if !ValidLocale(locale) {
		return invalid("locale", ReasonLocaleInvalid)
	}

	return nil
}

// UpdateAccount is the new values of an account's details: every field is
// written as given, so a form posts them all. The password, the status and the
// language of the person's own choosing are not here: see SetPassword,
// Deactivate and Activate.
type UpdateAccount struct {
	ID      int
	Login   string
	Name    string
	Email   string
	Phone   string
	Locale  string
	ActorID int
}

// Update writes the details that differ (IDENTITY-ADMIN-001) and puts what changed on the account's
// history (names and values; never a secret). Nothing differing is not an error
// and writes nothing. It returns the account as it is now.
func (a *Admin) Update(ctx context.Context, in UpdateAccount) (Account, error) {
	current, err := a.d.Users.Get(ctx, in.ID)
	if err != nil {
		return Account{}, err
	}

	next := Account{
		Login: NormalizeLogin(in.Login), Email: NormalizeEmail(in.Email), Name: strings.TrimSpace(in.Name),
		Phone: strings.TrimSpace(in.Phone), Locale: in.Locale,
	}

	if err := checkAll(
		checkLogin(next.Login), checkName(next.Name), checkEmail("email", next.Email), checkPhone(next.Phone), checkLocale(next.Locale),
	); err != nil {
		return Account{}, err
	}

	var (
		changes       Changes
		fields        []string
		before, after = map[string]string{}, map[string]string{}
	)

	diff := func(field string, old, now string, set **string) {
		if old == now {
			return
		}

		fields = append(fields, field)
		before[field], after[field] = old, now
		*set = &now
	}

	diff("login", current.Login, next.Login, &changes.Login)
	diff("name", current.Name, next.Name, &changes.Name)
	diff("email", current.Email, next.Email, &changes.Email)
	diff("phone", current.Phone, next.Phone, &changes.Phone)
	diff("locale", current.Locale, next.Locale, &changes.Locale)

	if len(fields) == 0 {
		return current, nil
	}

	if changes.Email != nil {
		if err := a.emailFree(ctx, next.Email, current.ID); err != nil {
			return Account{}, err
		}
	}

	err = a.within(ctx, func(ctx context.Context) error {
		if err := a.deps().Accounts.Update(ctx, current.ID, changes); err != nil {
			return stored(err)
		}

		return a.d.Changes.Record(ctx, Change{AccountID: current.ID, ActorID: in.ActorID, Action: ChangeUpdated, Fields: fields, Before: before, After: after})
	})
	if err != nil {
		return Account{}, err
	}

	if changes.Email != nil {
		a.deps().Notices.EmailChanged(ctx, current.ID, current.Email, next.Email)
	}

	return a.d.Users.Get(ctx, current.ID)
}

// SetPassword is a new password an administrator gives an account.
type SetPassword struct {
	ID       int
	Password string
	ActorID  int
}

// SetPassword gives the account a new password that satisfies the policy
// (IDENTITY-ADMIN-001; IAM-USER-002 item 7: it lifts the lock, so the person can sign in with it at
// once) and ends every session of the account (IAM-USER-004), which is on its
// history. The change is on its history by name, never by value.
func (a *Admin) SetPassword(ctx context.Context, in SetPassword) error {
	if _, err := a.d.Users.Get(ctx, in.ID); err != nil {
		return err
	}

	if err := checkPassword(a.d.Policy, "password", in.Password); err != nil {
		return err
	}

	hash, err := a.deps().Hasher.Hash(in.Password)
	if err != nil {
		return apperr.Internal(err)
	}

	err = a.within(ctx, func(ctx context.Context) error {
		// The history first, so a failure there writes nothing; a database rolls back the rest.
		if err := a.d.Changes.Record(ctx, Change{AccountID: in.ID, ActorID: in.ActorID, Action: ChangePassword, Fields: []string{"password"}}); err != nil {
			return err
		}

		if err := a.deps().Accounts.SetPasswordHash(ctx, in.ID, hash); err != nil {
			return stored(err)
		}

		if err := appendEntry(ctx, a.deps(), SignInEntry{AccountID: in.ID, Event: Unlocked, ActorID: in.ActorID}); err != nil {
			return err
		}

		return a.d.Users.RevokeSessions(ctx, RevokeSessionsInput{AccountID: in.ID, ActorID: in.ActorID})
	})
	if err != nil {
		return err
	}

	a.deps().Notices.PasswordChanged(ctx, in.ID, in.ActorID)

	return nil
}

// DeactivateInput is the account to deactivate and who does it.
type DeactivateInput struct {
	ID      int
	ActorID int
}

// Deactivate makes the account inactive and ends its sessions (IAM-USER-005), after the
// application's hooks have been asked (DeactivationHook): one of them refusing
// refuses the deactivation, and nothing is written. Nobody deactivates themselves
// (identity.account.self_deactivation), and an inactive account is not
// deactivated again (409 identity.account.already_inactive). The hooks, the
// change, the history and the ended sessions are one transaction.
func (a *Admin) Deactivate(ctx context.Context, in DeactivateInput) error {
	if in.ActorID != 0 && in.ActorID == in.ID {
		return apperr.BadRequest(string(ReasonSelfDeactivate)).WithReason(ReasonSelfDeactivate)
	}

	acc, err := a.d.Users.Get(ctx, in.ID)
	if err != nil {
		return err
	}

	if !acc.Active {
		return apperr.New(nil, string(ReasonAlreadyInactive), apperr.CodeAlreadyExists).WithReason(ReasonAlreadyInactive)
	}

	err = a.within(ctx, func(ctx context.Context) error {
		for _, hook := range a.d.Hooks {
			if err := hook.Deactivating(ctx, acc, in.ActorID); err != nil {
				return err
			}
		}

		if err := a.deps().Accounts.SetActive(ctx, acc.ID, false); err != nil {
			return stored(err)
		}

		if err := a.d.Users.RevokeSessions(ctx, RevokeSessionsInput{AccountID: acc.ID, ActorID: in.ActorID}); err != nil {
			return err
		}

		return a.d.Changes.Record(ctx, Change{
			AccountID: acc.ID, ActorID: in.ActorID, Action: ChangeDeactivated, Fields: []string{"active"},
			Before: map[string]string{"active": "true"}, After: map[string]string{"active": "false"},
		})
	})
	if err != nil {
		return err
	}

	a.deps().Notices.AccountDeactivated(ctx, acc.ID, in.ActorID)

	return nil
}

// Activate makes an inactive account active again; its roles were never touched.
// An active account is 409 identity.account.already_active.
func (a *Admin) Activate(ctx context.Context, in DeactivateInput) error {
	acc, err := a.d.Users.Get(ctx, in.ID)
	if err != nil {
		return err
	}

	if acc.Active {
		return apperr.New(nil, string(ReasonAlreadyActive), apperr.CodeAlreadyExists).WithReason(ReasonAlreadyActive)
	}

	err = a.within(ctx, func(ctx context.Context) error {
		if err := a.deps().Accounts.SetActive(ctx, acc.ID, true); err != nil {
			return stored(err)
		}

		return a.d.Changes.Record(ctx, Change{
			AccountID: acc.ID, ActorID: in.ActorID, Action: ChangeActivated, Fields: []string{"active"},
			Before: map[string]string{"active": "false"}, After: map[string]string{"active": "true"},
		})
	})
	if err != nil {
		return err
	}

	a.deps().Notices.AccountActivated(ctx, acc.ID, in.ActorID)

	return nil
}

// UnlockInput is the account to unlock and who does it.
type UnlockInput struct {
	ID      int
	ActorID int
}

// Unlock lifts the lock after wrong passwords (IAM-USER-002 item 5) by writing
// unlocked, with the actor, on the account's history. It reports whether the
// account was locked; one that is not is left alone.
func (a *Admin) Unlock(ctx context.Context, in UnlockInput) (bool, error) {
	if _, err := a.d.Users.Get(ctx, in.ID); err != nil {
		return false, err
	}

	if _, locked, err := a.d.Users.lockedUntil(ctx, in.ID); err != nil || !locked {
		return false, err
	}

	if err := appendEntry(ctx, a.deps(), SignInEntry{AccountID: in.ID, Event: Unlocked, ActorID: in.ActorID}); err != nil {
		return false, err
	}

	return true, nil
}

// lockedUntil reads the lock off the account's history (IAM-USER-002).
func (u *Users) lockedUntil(ctx context.Context, accountID int) (time.Time, bool, error) {
	latest, err := u.deps.SignIns.LockEvents(ctx, accountID, u.cfg.Lock.After)
	if err != nil {
		return time.Time{}, false, apperr.Internal(err)
	}

	until, locked := u.cfg.Lock.LockedUntil(latest, u.deps.Clock.Now())

	return until, locked, nil
}

// Detail is an account with what the users module adds to it: when the person
// last signed in (zero: never) and until when sign-in is locked (zero: not locked).
type Detail struct {
	Account
	LastSignIn  time.Time
	LockedUntil time.Time
}

// Get returns the account, active or not, with its last sign-in and its lock.
func (a *Admin) Get(ctx context.Context, id int) (Detail, error) {
	acc, err := a.d.Users.Get(ctx, id)
	if err != nil {
		return Detail{}, err
	}

	d := Detail{Account: acc}

	last, err := a.d.History.LastSignIns(ctx, []int{id})
	if err != nil {
		return Detail{}, apperr.Internal(err)
	}

	d.LastSignIn = last[id]

	if until, locked, err := a.d.Users.lockedUntil(ctx, id); err != nil {
		return Detail{}, err
	} else if locked {
		d.LockedUntil = until
	}

	return d, nil
}

// View is which accounts a list shows.
type View string

// The views of the list (IAM-USER-006).
const (
	// ViewActive is the active accounts that are not locked.
	ViewActive View = "active"
	// ViewLocked is the active accounts that are locked.
	ViewLocked View = "locked"
	// ViewInactive is the inactive accounts.
	ViewInactive View = "inactive"
	// ViewAll is every account.
	ViewAll View = "all"
)

// Paging is a page of a list; the zero value is the first page of 20.
type Paging struct {
	Page, PerPage int
}

// MaxPerPage is the most rows a page may have.
const MaxPerPage = 100

func (p Paging) resolved() Paging {
	if p.Page < 1 {
		p.Page = 1
	}

	if p.PerPage < 1 {
		p.PerPage = 20
	}

	p.PerPage = min(p.PerPage, MaxPerPage)

	return p
}

func (p Paging) offset() int { r := p.resolved(); return (r.Page - 1) * r.PerPage }

// ListInput is a page of the list. An empty View is ViewActive and an empty
// OrderBy is OrderLogin; an unknown view or column is refused.
type ListInput struct {
	View    View
	Search  string
	OrderBy Order
	Desc    bool
	Paging
}

// Row is an account in a list: the details with the last sign-in and the lock.
type Row = Detail

// Listing is one page of the list.
type Listing struct {
	Rows     []Row
	Total    int
	Page     int
	PerPage  int
	LastPage int
}

// List searches the accounts (IAM-USER-006). The views split them by status and
// lock, counted under the same search; each row carries its last sign-in and
// its lock. The lock is derived from the history, so the list asks it only
// about the accounts that had a wrong password within the lock's length.
func (a *Admin) List(ctx context.Context, in ListInput) (Listing, error) {
	view := in.View
	if view == "" {
		view = ViewActive
	}

	order := in.OrderBy
	if order == "" {
		order = OrderLogin
	}

	switch order {
	case OrderLogin, OrderName, OrderEmail, OrderCreated:
	default:
		return Listing{}, invalid("order_col", ReasonListOrderInvalid)
	}

	paging := in.resolved()
	q := Query{Search: strings.TrimSpace(in.Search), OrderBy: order, Desc: in.Desc, Page: paging.Page, PerPage: paging.PerPage}

	locks, err := a.locks(ctx)
	if err != nil {
		return Listing{}, err
	}

	ids := make([]int, 0, len(locks))
	for id := range locks {
		ids = append(ids, id)
	}

	yes, no := true, false

	switch view {
	case ViewActive:
		q.Active, q.Locked, q.LockedIDs = &yes, &no, ids
	case ViewLocked:
		q.Active, q.Locked, q.LockedIDs = &yes, &yes, ids
	case ViewInactive:
		q.Active = &no
	case ViewAll:
	default:
		return Listing{}, invalid("view", ReasonListViewInvalid)
	}

	page, err := a.d.Search.Search(ctx, q)
	if err != nil {
		return Listing{}, apperr.Internal(err)
	}

	rowIDs := make([]int, len(page.Accounts))
	for i, acc := range page.Accounts {
		rowIDs[i] = acc.ID
	}

	last, err := a.d.History.LastSignIns(ctx, rowIDs)
	if err != nil {
		return Listing{}, apperr.Internal(err)
	}

	out := Listing{Rows: make([]Row, len(page.Accounts)), Total: page.Total, Page: paging.Page, PerPage: paging.PerPage}
	out.LastPage = max((page.Total+paging.PerPage-1)/paging.PerPage, 1)

	for i, acc := range page.Accounts {
		out.Rows[i] = Row{Account: acc, LastSignIn: last[acc.ID], LockedUntil: locks[acc.ID]}
	}

	return out, nil
}

// locks is who is locked now and until when: of the accounts that had a wrong
// password within the lock's length, the ones whose latest lock events say so.
func (a *Admin) locks(ctx context.Context) (map[int]time.Time, error) {
	lock := a.d.Users.cfg.Lock
	now := a.deps().Clock.Now()

	candidates, err := a.d.History.WrongPasswordsSince(ctx, now.Add(-lock.For))
	if err != nil {
		return nil, apperr.Internal(err)
	}

	locked := map[int]time.Time{}

	for _, id := range candidates {
		until, ok, err := a.d.Users.lockedUntil(ctx, id)
		if err != nil {
			return nil, err
		}

		if ok {
			locked[id] = until
		}
	}

	return locked, nil
}

// Sessions lists the account's live sessions, the latest used first (IAM-USER-004).
func (a *Admin) Sessions(ctx context.Context, accountID int) ([]Session, error) {
	if _, err := a.d.Users.Get(ctx, accountID); err != nil {
		return nil, err
	}

	return a.d.Users.Sessions(ctx, accountID)
}

// RevokeSession ends one live session of the account, on behalf of actorID.
func (a *Admin) RevokeSession(ctx context.Context, accountID, sessionID, actorID int) error {
	return a.d.Users.RevokeSession(ctx, RevokeSessionInput{AccountID: accountID, SessionID: sessionID, ActorID: actorID})
}

// RevokeSessions ends every session of the account, on behalf of actorID.
func (a *Admin) RevokeSessions(ctx context.Context, accountID, actorID int) error {
	if _, err := a.d.Users.Get(ctx, accountID); err != nil {
		return err
	}

	return a.d.Users.RevokeSessions(ctx, RevokeSessionsInput{AccountID: accountID, ActorID: actorID})
}

// SignIns lists the account's sign-in history (IAM-USER-003), newest first, with
// how many rows it has.
func (a *Admin) SignIns(ctx context.Context, accountID int, p Paging) ([]SignInEntry, int, error) {
	if _, err := a.d.Users.Get(ctx, accountID); err != nil {
		return nil, 0, err
	}

	rows, total, err := a.d.History.Entries(ctx, accountID, p.offset(), p.resolved().PerPage)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}

	return rows, total, nil
}

// Changes lists the changes made to the account (IAM-USER-007), newest first,
// with how many there are.
func (a *Admin) Changes(ctx context.Context, accountID int, p Paging) ([]ChangeEntry, int, error) {
	if _, err := a.d.Users.Get(ctx, accountID); err != nil {
		return nil, 0, err
	}

	rows, total, err := a.d.Changes.Changes(ctx, accountID, p.offset(), p.resolved().PerPage)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}

	return rows, total, nil
}
