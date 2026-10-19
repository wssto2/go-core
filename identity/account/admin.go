package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
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

// AdminDeps are what Admin is built on. Users, Search, History, Changes, Activity and
// Transact are required; Policy defaults to Passwords{}. Users carries the
// accounts, the hasher, the clock and the Notices.
type AdminDeps struct {
	Users    *Users
	Search   Searcher
	History  SignInHistory
	Changes  ChangeLog
	Activity ActivityLog
	// Areas name the kinds of record in a person's activity; the zero value has only IdentityArea.
	Areas    ActivityAreas
	Transact Transactor
	Policy   PasswordPolicy
	// Hooks are asked, in order, before an account is deactivated.
	Hooks []DeactivationHook
	// Extension, when set, adds the application's own data to the people (UserExtension).
	Extension UserExtension
	// IdleAfter is how long without signing in makes an account idle (LastSignInIdle); zero is DefaultIdleAfter.
	IdleAfter time.Duration
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
	case d.Activity == nil:
		return nil, errors.New("identity: AdminDeps.Activity is missing: pass an ActivityLog, for example gormstore.NewActivityLog(db)")
	case d.Transact == nil:
		return nil, errors.New("identity: AdminDeps.Transact is missing: pass a Transactor, for example database.NewTransactor(db)")
	}

	if d.Policy == nil {
		d.Policy = Passwords{}
	}

	if d.IdleAfter <= 0 {
		d.IdleAfter = DefaultIdleAfter
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
	// Ext is the application's data for the new person (UserExtension.Create).
	Ext json.RawMessage
}

// The names of the account's fields on its history.
const (
	loginField  = "login"
	nameField   = "name"
	emailField  = "email"
	phoneField  = "phone"
	localeField = "locale"
	activeField = "active"
)

// Create makes an active account (IDENTITY-ADMIN-001). The login and the address are normalised, a
// login or an address that belongs to somebody is refused (identity.login.taken,
// identity.email.taken), the password must satisfy the policy, and the creation
// is on the account's history. Signing in with the password works at once.
func (a *Admin) Create(ctx context.Context, in CreateAccount) (Account, error) {
	created, err := a.CreateWithExt(ctx, in)

	return created.Account, err
}

// CreateWithExt is Create for an application with a UserExtension: it also gives in.Ext to the extension, in the
// transaction of the creation, and returns what the extension answered in Ext.
func (a *Admin) CreateWithExt(ctx context.Context, req CreateAccount) (Detail, error) {
	acc := Account{
		Login: NormalizeLogin(req.Login), Email: NormalizeEmail(req.Email), Name: strings.TrimSpace(req.Name),
		Phone: strings.TrimSpace(req.Phone), Locale: req.Locale, Active: true,
	}

	if err := checkAll(
		checkLogin(acc.Login), checkName(acc.Name), checkEmail("email", acc.Email), checkPhone(acc.Phone),
		a.checkLocale(acc.Locale), checkPassword(a.d.Policy, "password", req.Password),
	); err != nil {
		return Detail{}, err
	}

	if err := a.emailFree(ctx, acc.Email, 0); err != nil {
		return Detail{}, err
	}

	hash, err := a.deps().Hasher.Hash(req.Password)
	if err != nil {
		return Detail{}, apperr.Internal(err)
	}

	acc.PasswordHash, acc.CreatedAt = hash, a.deps().Clock.Now()

	var ext any

	err = a.within(ctx, func(ctx context.Context) error {
		created, err := a.deps().Accounts.Create(ctx, acc)
		if err != nil {
			return stored(err)
		}

		acc = created

		ext, err = a.extendCreate(ctx, acc.ID, req)
		if err != nil {
			return err
		}

		return a.d.Changes.Record(ctx, Change{
			AccountID: acc.ID, ActorID: req.ActorID, Action: ChangeCreated, Before: nil,
			Fields: []string{loginField, nameField, emailField, phoneField, localeField},
			After: map[string]string{
				loginField: acc.Login, nameField: acc.Name, emailField: acc.Email, phoneField: acc.Phone, localeField: acc.Locale,
			},
		})
	})
	if err != nil {
		return Detail{}, err
	}

	a.deps().Notices.AccountCreated(ctx, acc.ID, req.ActorID)

	return Detail{Account: acc, LastSignIn: time.Time{}, LockedUntil: time.Time{}, Ext: ext}, nil
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
	// Ext is the application's data for the person (UserExtension.Update).
	Ext json.RawMessage
}

// Update writes the details that differ (IDENTITY-ADMIN-001) and puts what changed on the account's
// history (names and values; never a secret). Nothing differing is not an error
// and writes nothing, unless the application has a UserExtension: it is given in.Ext, in the same
// transaction, on every update. It returns the account as it is now.
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
		checkLogin(next.Login), checkName(next.Name), checkEmail("email", next.Email), checkPhone(next.Phone),
		a.checkLocale(next.Locale),
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

	if len(fields) == 0 && a.d.Extension == nil {
		return current, nil
	}

	if changes.Email != nil {
		if err := a.emailFree(ctx, next.Email, current.ID); err != nil {
			return Account{}, err
		}
	}

	err = a.within(ctx, func(ctx context.Context) error {
		if len(fields) == 0 { // only the extension has something to write
			return a.extendUpdate(ctx, current.ID, in)
		}

		if err := a.deps().Accounts.Update(ctx, current.ID, changes); err != nil {
			return stored(err)
		}

		err := a.extendUpdate(ctx, current.ID, in)
		if err != nil {
			return err
		}

		return a.d.Changes.Record(ctx, Change{AccountID: current.ID, ActorID: in.ActorID, Action: ChangeUpdated, Fields: fields, Before: before, After: after})
	})
	if err != nil {
		return Account{}, err
	}

	if changes.Email != nil {
		a.deps().Notices.EmailChanged(ctx, current.ID, current.Email, next.Email)
	}

	a.deps().Notices.AccountChanged(ctx, current.ID)

	return a.d.Users.Get(ctx, current.ID)
}

// SetPassword is a new password an administrator gives an account.
type SetPassword struct {
	ID       int
	Password string
	ActorID  int
	// KeepToken, when it is an access token of one of the account's live sessions, keeps that session: an
	// administrator who sets their own password stays signed in.
	KeepToken string
}

// SetPassword gives the account a new password that satisfies the policy
// (IDENTITY-ADMIN-001; IAM-USER-002 item 7: it lifts the lock, so the person can sign in with it at
// once) and ends every session of the account but the one in.KeepToken names (IAM-USER-004), which is on its
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

	var pending pendingNotices

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

		var err error

		pending, err = a.d.Users.revokeSessions(ctx, RevokeSessionsInput{
			AccountID: in.ID, KeepToken: in.KeepToken, ActorID: in.ActorID, Reason: EndedPasswordChanged,
		})

		return err
	})
	if err != nil {
		return err
	}

	pending.announce(ctx, a.deps().Notices)
	a.deps().Notices.PasswordChanged(ctx, in.ID, in.ActorID)
	a.deps().Notices.AccountChanged(ctx, in.ID)

	return nil
}

// DeactivateInput is the account to deactivate and who does it.
type DeactivateInput struct {
	ID      int
	ActorID int
	// Details is the application's instruction for the deactivation (UserExtension.Deactivate).
	Details json.RawMessage
}

// Deactivate makes the account inactive and ends its sessions (IAM-USER-005), after the
// application's hooks have been asked (DeactivationHook): one of them refusing
// refuses the deactivation, and nothing is written. Nobody deactivates themselves
// (identity.account.self_deactivation), and an inactive account is not
// deactivated again (409 identity.account.already_inactive). The hooks, the
// change, the history and the ended sessions are one transaction.
func (a *Admin) Deactivate(ctx context.Context, in DeactivateInput) error {
	_, err := a.DeactivateWithExt(ctx, in)

	return err
}

// DeactivateWithExt is Deactivate for an application with a UserExtension: the extension is given in.Details after the
// hooks, in the same transaction, and what it answered is returned.
func (a *Admin) DeactivateWithExt(ctx context.Context, req DeactivateInput) (any, error) {
	if req.ActorID != 0 && req.ActorID == req.ID {
		return nil, apperr.BadRequest(string(ReasonSelfDeactivate)).WithReason(ReasonSelfDeactivate)
	}

	acc, err := a.d.Users.Get(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	if !acc.Active {
		return nil, apperr.Wrap(nil, string(ReasonAlreadyInactive), apperr.CodeAlreadyExists).WithReason(ReasonAlreadyInactive)
	}

	var (
		pending pendingNotices
		ext     any
	)

	err = a.within(ctx, func(ctx context.Context) error {
		var err error

		ext, err = a.deactivating(ctx, acc, req)
		if err != nil {
			return err
		}

		if err := a.deps().Accounts.SetActive(ctx, acc.ID, false); err != nil {
			return stored(err)
		}

		pending, err = a.d.Users.revokeSessions(ctx, RevokeSessionsInput{
			AccountID: acc.ID, KeepToken: "", ActorID: req.ActorID, Reason: EndedDeactivated,
		})
		if err != nil {
			return err
		}

		return a.d.Changes.Record(ctx, Change{
			AccountID: acc.ID, ActorID: req.ActorID, Action: ChangeDeactivated, Fields: []string{activeField},
			Before: map[string]string{"active": "true"}, After: map[string]string{"active": "false"},
		})
	})
	if err != nil {
		return nil, err
	}

	pending.announce(ctx, a.deps().Notices)
	a.deps().Notices.AccountDeactivated(ctx, acc.ID, req.ActorID)
	a.deps().Notices.AccountChanged(ctx, acc.ID)

	return ext, nil
}

// Activate makes an inactive account active again; its roles were never touched.
// An active account is 409 identity.account.already_active.
func (a *Admin) Activate(ctx context.Context, in DeactivateInput) error {
	acc, err := a.d.Users.Get(ctx, in.ID)
	if err != nil {
		return err
	}

	if acc.Active {
		return apperr.Wrap(nil, string(ReasonAlreadyActive), apperr.CodeAlreadyExists).WithReason(ReasonAlreadyActive)
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
	a.deps().Notices.AccountChanged(ctx, acc.ID)

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
// last signed in (zero: never), until when sign-in is locked (zero: not locked) and the application's own
// data (Ext).
type Detail struct {
	Account
	LastSignIn  time.Time
	LockedUntil time.Time
	// Ext is what the application's UserExtension adds to the person; nil without one.
	Ext any
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

	exts, err := a.exts(ctx, []int{id})
	if err != nil {
		return Detail{}, err
	}

	d.Ext = exts[id]

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
	// Filters are the query keys of the application's own filters (Query.Filters).
	Filters url.Values
	// LastSignIn, when set, lists only the accounts that never signed in, or are idle (Query.LastSignIn).
	LastSignIn LastSignInFilter
	Paging
}

// Row is an account in a list: the details with the last sign-in and the lock.
type Row = Detail

// Listing is one page of the list. Counts is how many accounts each view has under the same
// search, whichever view is shown.
type Listing struct {
	Rows     []Row
	Total    int
	Page     int
	PerPage  int
	LastPage int
	Counts   StatusCounts
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

	switch in.LastSignIn {
	case "", LastSignInNever, LastSignInIdle:
	default:
		return Listing{}, invalid("last_sign_in", ReasonListLastSignInInvalid)
	}

	paging := in.resolved()
	q := Query{Search: strings.TrimSpace(in.Search), OrderBy: order, Desc: in.Desc, Page: paging.Page, PerPage: paging.PerPage}

	q.Filters, q.LastSignIn, q.IdleBefore = in.Filters, in.LastSignIn, a.deps().Clock.Now().Add(-a.d.IdleAfter)

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

	// counts say who is locked whatever the view; the rest of q (status, order, page) they ignore
	q.LockedIDs = ids

	counts, err := a.d.Search.Counts(ctx, q)
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

	out := Listing{Rows: make([]Row, len(page.Accounts)), Total: page.Total, Page: paging.Page, PerPage: paging.PerPage, Counts: counts}
	out.LastPage = max((page.Total+paging.PerPage-1)/paging.PerPage, 1)

	exts, err := a.exts(ctx, rowIDs)
	if err != nil {
		return Listing{}, err
	}

	for i, acc := range page.Accounts {
		out.Rows[i] = Row{Account: acc, LastSignIn: last[acc.ID], LockedUntil: locks[acc.ID], Ext: exts[acc.ID]}
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

// RevokeSession ends one live session of the account, on behalf of actorID: an account that does not exist is
// identity.account.not_found, a session that is not its live one identity.session.not_found.
func (a *Admin) RevokeSession(ctx context.Context, accountID, sessionID, actorID int) error {
	_, err := a.d.Users.Get(ctx, accountID) // like the other methods, so a missing account is not a missing session
	if err != nil {
		return err
	}

	return a.d.Users.RevokeSession(ctx, RevokeSessionInput{AccountID: accountID, SessionID: sessionID, ActorID: actorID})
}

// RevokeSessions ends every session of the account, on behalf of actorID.
func (a *Admin) RevokeSessions(ctx context.Context, accountID, actorID int) error {
	if _, err := a.d.Users.Get(ctx, accountID); err != nil {
		return err
	}

	return a.d.Users.RevokeSessions(ctx, RevokeSessionsInput{AccountID: accountID, ActorID: actorID})
}

// SignIns lists the account's sign-in history (IAM-USER-003) in a view, newest first, with how
// many rows the view has and how many each view has. An empty view is SignInsAll; one that does
// not exist is refused (identity.history.view_invalid).
func (a *Admin) SignIns(ctx context.Context, accountID int, view SignInView, p Paging) (SignInPage, error) {
	if _, err := a.d.Users.Get(ctx, accountID); err != nil {
		return SignInPage{}, err
	}

	return signInPage(ctx, a.d.History, accountID, view, p)
}

func signInPage(ctx context.Context, history SignInHistory, accountID int, view SignInView, p Paging) (SignInPage, error) {
	events, err := view.events()
	if err != nil {
		return SignInPage{}, err
	}

	rows, total, err := history.Entries(ctx, SignInQuery{AccountID: accountID, Events: events, Offset: p.offset(), Limit: p.resolved().PerPage})
	if err != nil {
		return SignInPage{}, apperr.Internal(err)
	}

	byEvent, err := history.EventCounts(ctx, accountID)
	if err != nil {
		return SignInPage{}, apperr.Internal(err)
	}

	return SignInPage{Rows: rows, Total: total, Counts: signInCounts(byEvent)}, nil
}

// Changes lists the changes made to the account (IAM-USER-007) in a view, newest first, with how
// many there are in the view and in each view. An empty view is ChangesAll; one that does not
// exist is refused (identity.history.view_invalid).
func (a *Admin) Changes(ctx context.Context, accountID int, view ChangeView, p Paging) (ChangePage, error) {
	if _, err := a.d.Users.Get(ctx, accountID); err != nil {
		return ChangePage{}, err
	}

	only, except, err := view.narrow()
	if err != nil {
		return ChangePage{}, err
	}

	rows, total, err := a.d.Changes.Changes(ctx, ChangeQuery{AccountID: accountID, Only: only, Except: except, Offset: p.offset(), Limit: p.resolved().PerPage})
	if err != nil {
		return ChangePage{}, apperr.Internal(err)
	}

	byAction, err := a.d.Changes.ChangeCounts(ctx, accountID)
	if err != nil {
		return ChangePage{}, apperr.Internal(err)
	}

	return ChangePage{Rows: rows, Total: total, Counts: changeCounts(byAction)}, nil
}

func (a *Admin) checkLocale(locale string) error {
	if !ValidLocale(locale) {
		return invalid("locale", ReasonLocaleInvalid)
	}

	return a.d.Users.cfg.offers("locale", locale)
}

// exts reads the application's data for the people, none without an extension.
func (a *Admin) exts(ctx context.Context, ids []int) (map[int]any, error) {
	if a.d.Extension == nil {
		return map[int]any{}, nil
	}

	exts, err := a.d.Extension.Ext(ctx, ids)
	if err != nil {
		return nil, stored(err)
	}

	return exts, nil
}

// extendCreate gives the extension, if any, the new account and the body's ext, in the transaction of the creation.
func (a *Admin) extendCreate(ctx context.Context, accountID int, req CreateAccount) (any, error) {
	var none any

	if a.d.Extension == nil {
		return none, nil
	}

	ext, err := a.d.Extension.Create(ctx, accountID, req.Ext, req.ActorID)

	return ext, stored(err)
}

// extendUpdate gives the extension, if any, the body's ext, in the transaction of the update.
func (a *Admin) extendUpdate(ctx context.Context, accountID int, req UpdateAccount) error {
	if a.d.Extension == nil {
		return nil
	}

	return stored(a.d.Extension.Update(ctx, accountID, req.Ext, req.ActorID))
}

// deactivating asks the hooks, then the extension, in the transaction of the deactivation; the extension's answer is
// returned.
func (a *Admin) deactivating(ctx context.Context, acc Account, req DeactivateInput) (any, error) {
	var none any

	for _, hook := range a.d.Hooks {
		err := hook.Deactivating(ctx, acc, req.ActorID)
		if err != nil {
			return none, stored(err)
		}
	}

	if a.d.Extension == nil {
		return none, nil
	}

	ext, err := a.d.Extension.Deactivate(ctx, acc, req.Details, req.ActorID)

	return ext, stored(err)
}
