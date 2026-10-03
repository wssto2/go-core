package account_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
)

func newPerson() account.CreateAccount {
	return account.CreateAccount{
		Login: " Dora ", Name: " Dora Horvat ", Email: " Dora@Example.test ", Phone: "+385 1 555", Locale: "hr", Password: "long enough", ActorID: 1,
	}
}

func fieldOf(t *testing.T, err error) (apperr.Reason, string) {
	t.Helper()

	var ae *apperr.AppError
	require.ErrorAs(t, err, &ae)

	for field := range ae.Fields {
		return ae.Reason, field
	}

	return ae.Reason, ""
}

// IAM-USER-008: a created account is active, normalised, can sign in at once and is on its history.
func TestCreateMakesAnActiveAccountThatCanSignIn(t *testing.T) {
	n := &notices{}
	k := seeded(t, identitytest.WithNotices(n))

	acc, err := k.Admin.Create(t.Context(), newPerson())
	require.NoError(t, err)
	require.Greater(t, acc.ID, 3)
	require.Equal(t, "dora", acc.Login)
	require.Equal(t, "Dora Horvat", acc.Name)
	require.Equal(t, "dora@example.test", acc.Email)
	require.Equal(t, "+385 1 555", acc.Phone)
	require.True(t, acc.Active)
	require.Equal(t, identitytest.Epoch, acc.CreatedAt)
	require.NotContains(t, acc.PasswordHash, "long enough")

	signed, err := k.SignIn.Login(t.Context(), account.LoginInput{Login: "DORA", Password: "long enough"})
	require.NoError(t, err)
	require.Equal(t, acc.ID, signed.Account.ID)

	changes, total, err := k.Admin.Changes(t.Context(), acc.ID, account.Paging{})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, account.ChangeCreated, changes[0].Action)
	require.Equal(t, 1, changes[0].ActorID)
	require.Equal(t, "dora@example.test", changes[0].After["email"])
	require.NotContains(t, changes[0].After, "password")

	require.Contains(t, n.events, "created 4 by 1")
}

func TestCreateRefusesWhatCannotBeStored(t *testing.T) {
	k := seeded(t)
	require.NoError(t, k.Accounts.Update(t.Context(), 1, account.Changes{Email: ptr("ana@example.test")}))

	for name, tc := range map[string]struct {
		change func(*account.CreateAccount)
		reason apperr.Reason
		field  string
	}{
		"no login":             {func(c *account.CreateAccount) { c.Login = "  " }, account.ReasonLoginInvalid, "login"},
		"a login with a space": {func(c *account.CreateAccount) { c.Login = "a b" }, account.ReasonLoginInvalid, "login"},
		"a long login":         {func(c *account.CreateAccount) { c.Login = string(make([]rune, 101)) }, account.ReasonLoginInvalid, "login"},
		"a taken login":        {func(c *account.CreateAccount) { c.Login = "ANA" }, account.ReasonLoginTaken, "login"},
		"no name":              {func(c *account.CreateAccount) { c.Name = "" }, account.ReasonNameInvalid, "name"},
		"not an address":       {func(c *account.CreateAccount) { c.Email = "dora" }, account.ReasonEmailInvalid, "email"},
		"a display name":       {func(c *account.CreateAccount) { c.Email = "Dora <dora@example.test>" }, account.ReasonEmailInvalid, "email"},
		"a taken address":      {func(c *account.CreateAccount) { c.Email = "ana@example.test" }, account.ReasonEmailTaken, "email"},
		"a long phone":         {func(c *account.CreateAccount) { c.Phone = "1234567890123456789012345678901" }, account.ReasonPhoneInvalid, "phone"},
		"a bad locale":         {func(c *account.CreateAccount) { c.Locale = "Croatian" }, account.ReasonLocaleInvalid, "locale"},
		"a short password":     {func(c *account.CreateAccount) { c.Password = "short" }, account.ReasonPasswordWeak, "password"},
	} {
		in := newPerson()
		tc.change(&in)

		_, err := k.Admin.Create(t.Context(), in)

		var ae *apperr.AppError
		require.ErrorAs(t, err, &ae, name)

		reason, field := fieldOf(t, err)
		require.Equal(t, tc.reason, reason, name)
		require.Equal(t, tc.field, field, name)
	}

	_, err := k.Admin.Create(t.Context(), func() account.CreateAccount { in := newPerson(); in.Email = "ANA@example.test"; return in }())
	reason, _ := fieldOf(t, err)
	require.Equal(t, account.ReasonEmailTaken, reason, "an address is compared without case")

	var ae *apperr.AppError
	require.ErrorAs(t, err, &ae)
	require.Equal(t, apperr.CodeAlreadyExists, ae.Code)
}

func ptr(s string) *string { return &s }

func TestThePasswordPolicyIsTheApplications(t *testing.T) {
	k := seeded(t, identitytest.WithPasswordPolicy(digitsPolicy{}))

	in := newPerson()
	in.Password = "no digits here"

	_, err := k.Admin.Create(t.Context(), in)

	var ae *apperr.AppError
	require.ErrorAs(t, err, &ae)
	require.Equal(t, account.ReasonPasswordWeak, ae.Reason)
	require.Equal(t, []string{"digits"}, ae.Params["rules"])

	in.Password = "has 2 digits 3"
	_, err = k.Admin.Create(t.Context(), in)
	require.NoError(t, err)
}

type digitsPolicy struct{}

func (digitsPolicy) Violations(p string) []string {
	for _, r := range p {
		if r >= '0' && r <= '9' {
			return nil
		}
	}

	return []string{"digits"}
}

func TestThePasswordRulesOfTheDefaultPolicy(t *testing.T) {
	require.Empty(t, account.Passwords{}.Violations("12345678"))
	require.Equal(t, []string{"min_length"}, account.Passwords{}.Violations("1234567"))
	require.Equal(t, []string{"min_length"}, account.Passwords{MinLength: 12}.Violations("12345678901"))
	require.Equal(t, []string{"max_length"}, account.Passwords{}.Violations(string(make([]byte, 73))[:0]+repeat("a", 73)))
	require.Empty(t, account.Passwords{}.Violations(repeat("a", 72)))
	require.Equal(t, []string{"min_length"}, account.Passwords{}.Violations("čćšđž"), "characters, not bytes")
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}

	return out
}

// IAM-USER-007: what changed is on the history, by name and value, and nothing differing writes nothing.
func TestUpdateWritesOnlyWhatDiffersAndRecordsIt(t *testing.T) {
	k := seeded(t)

	acc, err := k.Admin.Update(t.Context(), account.UpdateAccount{
		ID: 2, Login: "boris", Name: "Boris Babić", Email: "boris@example.test", Phone: "", Locale: "en", ActorID: 1,
	})
	require.NoError(t, err)
	require.Equal(t, "Boris Babić", acc.Name)
	require.Equal(t, "boris@example.test", acc.Email)
	require.Equal(t, "boris", acc.Login)

	changes, total, err := k.Admin.Changes(t.Context(), 2, account.Paging{})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, account.ChangeUpdated, changes[0].Action)
	require.Equal(t, []string{"name", "email"}, changes[0].Fields, "the locale and login did not differ")
	require.Equal(t, map[string]string{"name": "boris", "email": ""}, changes[0].Before)
	require.Equal(t, map[string]string{"name": "Boris Babić", "email": "boris@example.test"}, changes[0].After)

	_, err = k.Admin.Update(t.Context(), account.UpdateAccount{ID: 2, Login: "boris", Name: "Boris Babić", Email: "boris@example.test", Locale: "en"})
	require.NoError(t, err)

	_, total, _ = k.Admin.Changes(t.Context(), 2, account.Paging{})
	require.Equal(t, 1, total, "an update that changes nothing is not on the history")
}

func TestUpdateRefusesATakenLoginOrAddressAndAMissingAccount(t *testing.T) {
	k := seeded(t)
	require.NoError(t, k.Accounts.Update(t.Context(), 1, account.Changes{Email: ptr("ana@example.test")}))

	_, err := k.Admin.Update(t.Context(), account.UpdateAccount{ID: 2, Login: "ana", Name: "B", Email: "b@example.test", Locale: "en"})
	reason, field := fieldOf(t, err)
	require.Equal(t, account.ReasonLoginTaken, reason)
	require.Equal(t, "login", field)

	_, err = k.Admin.Update(t.Context(), account.UpdateAccount{ID: 2, Login: "boris", Name: "B", Email: "ANA@example.test", Locale: "en"})
	reason, _ = fieldOf(t, err)
	require.Equal(t, account.ReasonEmailTaken, reason)

	_, err = k.Admin.Update(t.Context(), account.UpdateAccount{ID: 1, Login: "ana", Name: "Ana", Email: "ana@example.test", Locale: "en"})
	require.NoError(t, err, "an account keeps its own address")

	_, err = k.Admin.Update(t.Context(), account.UpdateAccount{ID: 99, Login: "x", Name: "x", Email: "x@example.test", Locale: "en"})
	require.True(t, apperr.HasReason(err, account.ReasonAccountNotFound))
}

// IAM-USER-002 item 7, IAM-USER-004: a new password lifts the lock and ends the sessions.
func TestSetPasswordLiftsTheLockAndEndsTheSessions(t *testing.T) {
	n := &notices{}
	k := seeded(t, identitytest.WithNotices(n))

	signed, err := k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2"})
	require.NoError(t, err)

	for range 5 {
		_, _ = k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "nope"})
	}

	require.NoError(t, k.Admin.SetPassword(t.Context(), account.SetPassword{ID: 2, Password: "a brand new one", ActorID: 1}))

	_, err = k.SignIn.Authenticate(t.Context(), signed.Credentials.Access)
	require.True(t, apperr.HasReason(err, account.ReasonSessionInvalid), "the old session is gone")

	_, err = k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2"})
	require.True(t, apperr.HasReason(err, account.ReasonSignInFailed), "the old password no longer works")

	_, err = k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "a brand new one"})
	require.NoError(t, err, "the lock is lifted, the new password works at once")

	changes, _, _ := k.Admin.Changes(t.Context(), 2, account.Paging{})
	require.Equal(t, account.ChangePassword, changes[0].Action)
	require.Equal(t, []string{"password"}, changes[0].Fields)
	require.Empty(t, changes[0].After, "never the value")
	require.Contains(t, n.events, "password 2 by 1")

	_, _, err = k.Admin.SignIns(t.Context(), 2, account.Paging{})
	require.NoError(t, err)

	require.True(t, apperr.HasReason(k.Admin.SetPassword(t.Context(), account.SetPassword{ID: 2, Password: "short", ActorID: 1}), account.ReasonPasswordWeak))
	require.True(t, apperr.HasReason(k.Admin.SetPassword(t.Context(), account.SetPassword{ID: 99, Password: "long enough", ActorID: 1}), account.ReasonAccountNotFound))
}

// IAM-USER-005: nobody deactivates themselves, nor an inactive account again, and the account is signed out.
func TestDeactivateEndsTheSessionsAndIsOnTheHistory(t *testing.T) {
	n := &notices{}
	k := seeded(t, identitytest.WithNotices(n))

	signed, err := k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2"})
	require.NoError(t, err)

	require.NoError(t, k.Admin.Deactivate(t.Context(), account.DeactivateInput{ID: 2, ActorID: 1}))

	boris, err := k.Users.Get(t.Context(), 2)
	require.NoError(t, err)
	require.False(t, boris.Active)

	_, err = k.SignIn.Authenticate(t.Context(), signed.Credentials.Access)
	require.True(t, apperr.HasReason(err, account.ReasonSessionInvalid))

	changes, _, _ := k.Admin.Changes(t.Context(), 2, account.Paging{})
	require.Equal(t, account.ChangeDeactivated, changes[0].Action)
	require.Contains(t, n.events, "deactivated 2 by 1")

	err = k.Admin.Deactivate(t.Context(), account.DeactivateInput{ID: 2, ActorID: 1})
	require.True(t, apperr.HasReason(err, account.ReasonAlreadyInactive))

	var ae *apperr.AppError
	require.ErrorAs(t, err, &ae)
	require.Equal(t, apperr.CodeAlreadyExists, ae.Code)

	err = k.Admin.Deactivate(t.Context(), account.DeactivateInput{ID: 1, ActorID: 1})
	require.True(t, apperr.HasReason(err, account.ReasonSelfDeactivate))

	require.True(t, apperr.HasReason(k.Admin.Deactivate(t.Context(), account.DeactivateInput{ID: 99, ActorID: 1}), account.ReasonAccountNotFound))
}

func TestActivateBringsAnAccountBack(t *testing.T) {
	n := &notices{}
	k := seeded(t, identitytest.WithNotices(n))

	require.True(t, apperr.HasReason(k.Admin.Activate(t.Context(), account.DeactivateInput{ID: 2, ActorID: 1}), account.ReasonAlreadyActive))

	require.NoError(t, k.Admin.Activate(t.Context(), account.DeactivateInput{ID: 3, ActorID: 1}))

	_, err := k.SignIn.Login(t.Context(), account.LoginInput{Login: "ines", Password: "secret"})
	require.NoError(t, err)

	changes, _, _ := k.Admin.Changes(t.Context(), 3, account.Paging{})
	require.Equal(t, account.ChangeActivated, changes[0].Action)
	require.Contains(t, n.events, "activated 3 by 1")
}

// Two applications' hooks: one vetoes, so nothing changes; the other is asked in order and
// sees who is deactivated and by whom.
func TestADeactivationHookCanVeto(t *testing.T) {
	var asked []string

	owns := apperr.BadRequest("owns open leads").WithReason("crm.owns_leads", map[string]any{"count": 3})

	k := seeded(t, identitytest.WithDeactivationHooks(
		account.DeactivationHookFunc(func(_ context.Context, a account.Account, actor int) error {
			asked = append(asked, "first:"+a.Login)
			require.Equal(t, 1, actor)

			return nil
		}),
		account.DeactivationHookFunc(func(_ context.Context, a account.Account, _ int) error {
			asked = append(asked, "second:"+a.Login)

			if a.Login == "boris" {
				return owns
			}

			return nil
		}),
	))

	signed, err := k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2"})
	require.NoError(t, err)

	err = k.Admin.Deactivate(t.Context(), account.DeactivateInput{ID: 2, ActorID: 1})
	require.True(t, apperr.HasReason(err, "crm.owns_leads"), "the hook's own error reaches the caller")
	require.Equal(t, []string{"first:boris", "second:boris"}, asked)

	boris, _ := k.Users.Get(t.Context(), 2)
	require.True(t, boris.Active, "nothing changed")

	_, err = k.SignIn.Authenticate(t.Context(), signed.Credentials.Access)
	require.NoError(t, err, "the session is intact")

	_, total, _ := k.Admin.Changes(t.Context(), 2, account.Paging{})
	require.Zero(t, total, "nothing on the history")

	dora, err := k.Admin.Create(t.Context(), newPerson())
	require.NoError(t, err)

	require.NoError(t, k.Admin.Deactivate(t.Context(), account.DeactivateInput{ID: dora.ID, ActorID: 1}), "hooks that agree let it through")
	require.Equal(t, []string{"first:boris", "second:boris", "first:dora", "second:dora"}, asked)
}

// A hook's own failure rolls the transaction back, as a real Transactor does.
func TestADeactivationRunsInOneTransaction(t *testing.T) {
	tx := &recordingTransactor{}
	activity := identitytest.NewActivityLog(identitytest.NewChangeLog(identitytest.NewClock(identitytest.Epoch)), identitytest.NewSessions())
	svc, err := account.NewAdmin(account.AdminDeps{
		Users: seeded(t).Users, Search: identitytest.NewAccounts(), History: identitytest.NewSignIns(),
		Changes: identitytest.NewChangeLog(identitytest.NewClock(identitytest.Epoch)), Activity: activity, Transact: tx,
		Hooks: []account.DeactivationHook{account.DeactivationHookFunc(func(ctx context.Context, _ account.Account, _ int) error {
			require.True(t, tx.inside(ctx), "the hook runs in the transaction")

			return nil
		})},
	})
	require.NoError(t, err)

	_ = svc.Deactivate(t.Context(), account.DeactivateInput{ID: 2, ActorID: 1})
	require.Equal(t, 1, tx.runs, "one transaction for the hooks, the change, the sessions and the history")
}

type recordingTransactor struct {
	runs int
}

type txKey struct{}

func (r *recordingTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	r.runs++

	return fn(context.WithValue(ctx, txKey{}, true))
}

func (r *recordingTransactor) inside(ctx context.Context) bool { return ctx.Value(txKey{}) == true }

// IAM-USER-002 item 5: unlock writes unlocked with the actor, and only for who is locked.
func TestUnlock(t *testing.T) {
	k := seeded(t)

	for range 5 {
		_, _ = k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "nope"})
	}

	unlocked, err := k.Admin.Unlock(t.Context(), account.UnlockInput{ID: 2, ActorID: 1})
	require.NoError(t, err)
	require.True(t, unlocked)

	_, err = k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2"})
	require.NoError(t, err)

	unlocked, err = k.Admin.Unlock(t.Context(), account.UnlockInput{ID: 2, ActorID: 1})
	require.NoError(t, err)
	require.False(t, unlocked, "a person who is not locked is left alone")

	rows, _, err := k.Admin.SignIns(t.Context(), 2, account.Paging{})
	require.NoError(t, err)

	var events []account.SignInEvent
	for _, r := range rows {
		events = append(events, r.Event)
	}

	require.Equal(t, []account.SignInEvent{account.SignedIn, account.Unlocked, account.WrongPassword, account.WrongPassword, account.WrongPassword, account.WrongPassword, account.WrongPassword}, events)

	require.Equal(t, 1, rows[1].ActorID, "unlocked by the administrator")

	_, err = k.Admin.Unlock(t.Context(), account.UnlockInput{ID: 99, ActorID: 1})
	require.True(t, apperr.HasReason(err, account.ReasonAccountNotFound))
}

// IAM-USER-006: the views, with the lock derived from the history.
func TestListViews(t *testing.T) {
	k := seeded(t)
	ctx := t.Context()

	_, err := k.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "secret"})
	require.NoError(t, err)

	for range 5 {
		_, _ = k.SignIn.Login(ctx, account.LoginInput{Login: "boris", Password: "nope"})
	}

	logins := func(in account.ListInput) []string {
		t.Helper()

		page, err := k.Admin.List(ctx, in)
		require.NoError(t, err)

		var out []string
		for _, r := range page.Rows {
			out = append(out, r.Login)
		}

		return out
	}

	require.Equal(t, []string{"ana"}, logins(account.ListInput{}), "the default view is active and not locked")
	require.Equal(t, []string{"ana"}, logins(account.ListInput{View: account.ViewActive}))
	require.Equal(t, []string{"boris"}, logins(account.ListInput{View: account.ViewLocked}))
	require.Equal(t, []string{"ines"}, logins(account.ListInput{View: account.ViewInactive}))
	require.Equal(t, []string{"ana", "boris", "ines"}, logins(account.ListInput{View: account.ViewAll}))
	require.Equal(t, []string{"ines", "boris", "ana"}, logins(account.ListInput{View: account.ViewAll, OrderBy: account.OrderLogin, Desc: true}))
	require.Equal(t, []string{"boris"}, logins(account.ListInput{View: account.ViewAll, Search: "BOR"}))

	page, err := k.Admin.List(ctx, account.ListInput{View: account.ViewAll})
	require.NoError(t, err)
	require.Equal(t, 3, page.Total)
	require.Equal(t, 1, page.Page)
	require.Equal(t, 20, page.PerPage)
	require.Equal(t, 1, page.LastPage)

	require.Equal(t, identitytest.Epoch, page.Rows[0].LastSignIn, "ana signed in")
	require.True(t, page.Rows[1].LastSignIn.IsZero(), "boris never did")
	require.Equal(t, identitytest.Epoch.Add(15*time.Minute), page.Rows[1].LockedUntil, "boris is locked")
	require.True(t, page.Rows[0].LockedUntil.IsZero())

	k.Clock.Advance(15 * time.Minute)
	require.Equal(t, []string{"ana", "boris"}, logins(account.ListInput{View: account.ViewActive}), "the lock ran out")
	require.Empty(t, logins(account.ListInput{View: account.ViewLocked}))

	paged, err := k.Admin.List(ctx, account.ListInput{View: account.ViewAll, Paging: account.Paging{Page: 2, PerPage: 2}})
	require.NoError(t, err)
	require.Len(t, paged.Rows, 1)
	require.Equal(t, 2, paged.LastPage)

	capped, err := k.Admin.List(ctx, account.ListInput{View: account.ViewAll, Paging: account.Paging{PerPage: 5000}})
	require.NoError(t, err)
	require.Equal(t, account.MaxPerPage, capped.PerPage)

	_, err = k.Admin.List(ctx, account.ListInput{View: "everyone"})
	require.True(t, apperr.HasReason(err, account.ReasonListViewInvalid))

	_, err = k.Admin.List(ctx, account.ListInput{OrderBy: "password_hash"})
	require.True(t, apperr.HasReason(err, account.ReasonListOrderInvalid))
}

func TestGetShowsTheLockAndTheLastSignIn(t *testing.T) {
	k := seeded(t)

	_, err := k.SignIn.Login(t.Context(), account.LoginInput{Login: "ana", Password: "secret"})
	require.NoError(t, err)

	d, err := k.Admin.Get(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "ana", d.Login)
	require.Equal(t, identitytest.Epoch, d.LastSignIn)
	require.True(t, d.LockedUntil.IsZero())

	_, err = k.Admin.Get(t.Context(), 99)
	require.True(t, apperr.HasReason(err, account.ReasonAccountNotFound))
}

// IAM-USER-004: an administrator lists and ends sessions, on the person's history.
func TestSessionsAdmin(t *testing.T) {
	k := seeded(t)

	first, err := k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2", Device: "Firefox"})
	require.NoError(t, err)

	_, err = k.SignIn.Login(t.Context(), account.LoginInput{Login: "boris", Password: "hunter2", Device: "Chrome"})
	require.NoError(t, err)

	sessions, err := k.Admin.Sessions(t.Context(), 2)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	_, err = k.Admin.Sessions(t.Context(), 99)
	require.True(t, apperr.HasReason(err, account.ReasonAccountNotFound))

	var chrome int

	for _, s := range sessions {
		if s.Device == "Chrome" {
			chrome = s.ID
		}
	}

	require.NoError(t, k.Admin.RevokeSession(t.Context(), 2, chrome, 1))

	_, err = k.SignIn.Authenticate(t.Context(), first.Credentials.Access)
	require.NoError(t, err, "only the revoked one is gone")

	require.NoError(t, k.Admin.RevokeSessions(t.Context(), 2, 1))

	sessions, _ = k.Admin.Sessions(t.Context(), 2)
	require.Empty(t, sessions)

	rows, _, _ := k.Admin.SignIns(t.Context(), 2, account.Paging{})
	require.Equal(t, account.SignedOutEverywhere, rows[0].Event)
	require.Equal(t, 1, rows[0].ActorID)
	require.Equal(t, account.SessionRevoked, rows[1].Event)
}

func TestSignInsAndChangesPage(t *testing.T) {
	k := seeded(t)

	for range 3 {
		_, err := k.SignIn.Login(t.Context(), account.LoginInput{Login: "ana", Password: "secret"})
		require.NoError(t, err)
	}

	rows, total, err := k.Admin.SignIns(t.Context(), 1, account.Paging{Page: 2, PerPage: 2})
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, rows, 1)

	_, _, err = k.Admin.SignIns(t.Context(), 99, account.Paging{})
	require.True(t, apperr.HasReason(err, account.ReasonAccountNotFound))

	_, _, err = k.Admin.Changes(t.Context(), 99, account.Paging{})
	require.True(t, apperr.HasReason(err, account.ReasonAccountNotFound))
}

func TestNewAdminNamesWhatIsMissing(t *testing.T) {
	k := seeded(t)
	full := account.AdminDeps{
		Users: k.Users, Search: k.Accounts, History: k.SignIns, Changes: k.Changes, Activity: k.Activity, Transact: identitytest.Transactor{},
	}

	for want, mutate := range map[string]func(*account.AdminDeps){
		"AdminDeps.Users":    func(d *account.AdminDeps) { d.Users = nil },
		"AdminDeps.Search":   func(d *account.AdminDeps) { d.Search = nil },
		"AdminDeps.History":  func(d *account.AdminDeps) { d.History = nil },
		"AdminDeps.Changes":  func(d *account.AdminDeps) { d.Changes = nil },
		"AdminDeps.Activity": func(d *account.AdminDeps) { d.Activity = nil },
		"AdminDeps.Transact": func(d *account.AdminDeps) { d.Transact = nil },
	} {
		d := full
		mutate(&d)

		_, err := account.NewAdmin(d)
		require.ErrorContains(t, err, want)
	}

	_, err := account.NewAdmin(full)
	require.NoError(t, err)
}

var _ = errors.New
