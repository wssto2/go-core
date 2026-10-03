package gormstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/gormstore"
	"github.com/wssto2/go-core/identity/identitytest"
	"gorm.io/gorm"
)

// failingEnd is a session store whose End fails: the last write of a password
// change or a deactivation.
type failingEnd struct{ account.SessionStore }

func (failingEnd) End(context.Context, int, []int, int, time.Time) (int, error) {
	return 0, errors.New("the sessions table is down")
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC().Truncate(time.Second) }

// services builds the identity services over the real stores and a real
// transactor, with sessions that cannot be ended when failing is set.
func services(t *testing.T, db *gorm.DB, failing bool, hooks ...account.DeactivationHook) (*account.SignIn, *account.Admin, *account.Profile, gormstore.Stores) {
	t.Helper()

	tables(t, db)
	empty(t, db)

	stores := gormstore.New(db)

	var sessions account.SessionStore = stores.Sessions
	if failing {
		sessions = failingEnd{stores.Sessions}
	}

	svc, err := account.New(account.Deps{
		Accounts: stores.Accounts, SignIns: stores.SignIns, Sessions: sessions, Clock: realClock{}, Hasher: identitytest.Hasher,
	}, account.Config{})
	require.NoError(t, err)

	tx := database.NewTransactor(db)
	changes := gormstore.NewChangeLog(db)

	reauth, err := account.NewReauth(account.ReauthDeps{Store: stores.Reauth, Hasher: identitytest.Hasher, Clock: realClock{}}, account.Lock{})
	require.NoError(t, err)

	admin, err := account.NewAdmin(account.AdminDeps{
		Users: svc.Users, Search: stores.Accounts, History: stores.SignIns, Changes: changes, Activity: gormstore.NewActivityLog(db), Transact: tx, Hooks: hooks,
	})
	require.NoError(t, err)

	profile, err := account.NewProfile(account.ProfileDeps{
		Users: svc.Users, Reauth: reauth, History: stores.SignIns, Changes: changes, Transact: tx,
	})
	require.NoError(t, err)

	return svc.SignIn, admin, profile, stores
}

func person(t *testing.T, admin *account.Admin, login string) account.Account {
	t.Helper()

	acc, err := admin.Create(t.Context(), account.CreateAccount{
		Login: login, Name: login, Email: login + "@example.test", Locale: "en", Password: "a long password",
	})
	require.NoError(t, err)

	return acc
}

// What an administrator does is real SQL end to end: the account, its history in
// audit_logs, the list with its lock, on every database.
func TestTheUsersModuleOverRealTables(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		signIn, admin, profile, _ := services(t, db, false)
		ctx := t.Context()

		ana := person(t, admin, "ana")
		boris := person(t, admin, "boris")

		_, err := admin.Create(ctx, account.CreateAccount{Login: "ANA", Name: "x", Email: "x@example.test", Locale: "en", Password: "a long password"})
		require.True(t, apperr.HasReason(err, account.ReasonLoginTaken))

		_, err = admin.Create(ctx, account.CreateAccount{Login: "cvita", Name: "x", Email: "BORIS@example.test", Locale: "en", Password: "a long password"})
		require.True(t, apperr.HasReason(err, account.ReasonEmailTaken))

		_, err = signIn.Login(ctx, account.LoginInput{Login: "ana", Password: "a long password"})
		require.NoError(t, err)

		for range 5 {
			_, _ = signIn.Login(ctx, account.LoginInput{Login: "boris", Password: "nope"})
		}

		locked, err := admin.List(ctx, account.ListInput{View: account.ViewLocked})
		require.NoError(t, err)
		require.Len(t, locked.Rows, 1)
		require.Equal(t, boris.ID, locked.Rows[0].ID)
		require.False(t, locked.Rows[0].LockedUntil.IsZero())

		active, err := admin.List(ctx, account.ListInput{})
		require.NoError(t, err)
		require.Len(t, active.Rows, 1)
		require.Equal(t, ana.ID, active.Rows[0].ID)
		require.False(t, active.Rows[0].LastSignIn.IsZero())

		unlocked, err := admin.Unlock(ctx, account.UnlockInput{ID: boris.ID, ActorID: ana.ID})
		require.NoError(t, err)
		require.True(t, unlocked)

		_, err = admin.Update(ctx, account.UpdateAccount{ID: boris.ID, Login: "boris", Name: "Boris B", Email: "boris@example.test", Phone: "1", Locale: "hr", ActorID: ana.ID})
		require.NoError(t, err)

		require.NoError(t, admin.Deactivate(ctx, account.DeactivateInput{ID: boris.ID, ActorID: ana.ID}))

		inactive, err := admin.List(ctx, account.ListInput{View: account.ViewInactive})
		require.NoError(t, err)
		require.Len(t, inactive.Rows, 1)

		pg20, err := admin.Changes(ctx, boris.ID, account.ChangesAll, account.Paging{})
		changes, total := pg20.Rows, pg20.Total
		require.NoError(t, err)
		require.Equal(t, 3, total)
		require.Equal(t, account.ChangeDeactivated, changes[0].Action)
		require.Equal(t, ana.ID, changes[0].ActorID)
		require.Equal(t, account.ChangeUpdated, changes[1].Action)
		require.Equal(t, map[string]string{"name": "boris", "phone": "", "locale": "en"}, changes[1].Before)
		require.Equal(t, account.ChangeCreated, changes[2].Action)

		view, err := profile.UpdateDetails(ctx, account.ProfileDetails{AccountID: ana.ID, Name: "Ana A", Phone: "42"})
		require.NoError(t, err)
		require.Equal(t, "Ana A", view.Name)
	})
}

// A password change that fails on its last write (ending the sessions) rolls
// the new hash back: the old password still works, the sessions live.
func TestAFailedPasswordChangeRollsBack(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		_, admin, _, _ := services(t, db, false)
		boris := person(t, admin, "boris")

		signIn, _, profile, stores := services2(t, db)
		_ = signIn

		session, err := stores.Sessions.Open(t.Context(), account.NewSession{AccountID: boris.ID, At: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
		require.NoError(t, err)

		err = profile.ChangePassword(t.Context(), account.PasswordChange{
			AccountID: boris.ID, CurrentPassword: "a long password", NewPassword: "a better password", Confirmation: "a better password",
		})
		require.ErrorContains(t, err, "sessions table is down", "it failed on the last write, after the hash was written")

		after, err := stores.Accounts.Find(t.Context(), boris.ID)
		require.NoError(t, err)
		require.True(t, identitytest.Hasher.Matches(after.PasswordHash, "a long password"), "the old password is still the password")

		_, err = stores.Sessions.Get(t.Context(), session.Access, time.Now())
		require.NoError(t, err, "the session lives")

		_, total, err := gormstore.NewChangeLog(db).Changes(t.Context(), account.ChangeQuery{AccountID: boris.ID, Limit: 10})
		require.NoError(t, err)
		require.Equal(t, 1, total, "only the creation: the password change left no history either")
	})
}

// services2 is services with sessions that fail to end, over the tables services made.
func services2(t *testing.T, db *gorm.DB) (*account.SignIn, *account.Admin, *account.Profile, gormstore.Stores) {
	t.Helper()

	stores := gormstore.New(db)

	svc, err := account.New(account.Deps{
		Accounts: stores.Accounts, SignIns: stores.SignIns, Sessions: failingEnd{stores.Sessions}, Clock: realClock{}, Hasher: identitytest.Hasher,
	}, account.Config{})
	require.NoError(t, err)

	reauth, err := account.NewReauth(account.ReauthDeps{Store: stores.Reauth, Hasher: identitytest.Hasher, Clock: realClock{}}, account.Lock{})
	require.NoError(t, err)

	profile, err := account.NewProfile(account.ProfileDeps{
		Users: svc.Users, Reauth: reauth, History: stores.SignIns, Changes: gormstore.NewChangeLog(db), Transact: database.NewTransactor(db),
	})
	require.NoError(t, err)

	return svc.SignIn, nil, profile, stores
}

// A hook's writes are in the transaction of the deactivation: when a later
// hook refuses, the first one's write is rolled back with everything else.
func TestAVetoRollsBackWhatHooksWrote(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		var stores gormstore.Stores

		handover := account.DeactivationHookFunc(func(ctx context.Context, a account.Account, _ int) error {
			return stores.Accounts.SetLocale(ctx, a.ID, "de") // a hand-over: a write in the transaction
		})
		veto := account.DeactivationHookFunc(func(context.Context, account.Account, int) error {
			return apperr.BadRequest("still owns leads").WithReason("crm.owns_leads")
		})

		_, admin, _, built := services(t, db, false, handover, veto)
		stores = built

		boris := person(t, admin, "boris")

		err := admin.Deactivate(t.Context(), account.DeactivateInput{ID: boris.ID})
		require.True(t, apperr.HasReason(err, "crm.owns_leads"))

		after, err := stores.Accounts.Find(t.Context(), boris.ID)
		require.NoError(t, err)
		require.True(t, after.Active, "still active")
		require.Equal(t, "en", after.Locale, "the hand-over's write was rolled back")

		pg21, err := admin.Changes(t.Context(), boris.ID, account.ChangesAll, account.Paging{})
		total := pg21.Total
		require.NoError(t, err)
		require.Equal(t, 1, total, "only the creation")
	})
}
