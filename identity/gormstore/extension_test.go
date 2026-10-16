package gormstore_test

import (
	"context"
	"encoding/json"
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

// scribbler is a UserExtension that writes in the transaction it is given (the account's locale, standing for the
// application's own table) and then answers or refuses as told.
type scribbler struct {
	account.UserExtension

	stores gormstore.Stores
	refuse error
	got    json.RawMessage
}

func (s *scribbler) scribble(ctx context.Context, id int) error {
	if err := s.stores.Accounts.SetLocale(ctx, id, "de"); err != nil {
		return err
	}

	return s.refuse
}

func (s *scribbler) Create(ctx context.Context, id int, ext json.RawMessage, _ int) (any, error) {
	s.got = ext

	return map[string]any{"id": id}, s.scribble(ctx, id)
}

func (s *scribbler) Update(ctx context.Context, id int, ext json.RawMessage, _ int) error {
	s.got = ext

	return s.scribble(ctx, id)
}

func (s *scribbler) Deactivate(ctx context.Context, a account.Account, details json.RawMessage, _ int) (any, error) {
	s.got = details

	return "handed over", s.scribble(ctx, a.ID)
}

func (s *scribbler) Ext(context.Context, []int) (map[int]any, error) { return nil, nil } //nolint:nilnil // nothing to add

var errRefused = apperr.New(nil, "dealer_bindings", apperr.CodeAlreadyExists).WithReason("shop.user.dealer_bindings")

// extended builds the users module over real tables with the extension.
func extended(t *testing.T, db *gorm.DB, x *scribbler) (*account.Admin, gormstore.Stores) {
	t.Helper()

	tables(t, db)
	empty(t, db)

	stores := gormstore.New(db)
	x.stores = stores

	svc, err := account.New(account.Deps{
		Accounts: stores.Accounts, SignIns: stores.SignIns, Sessions: stores.Sessions, Clock: realClock{}, Hasher: identitytest.Hasher,
	}, account.Config{})
	require.NoError(t, err)

	admin, err := account.NewAdmin(account.AdminDeps{
		Users: svc.Users, Search: stores.Accounts, History: stores.SignIns, Changes: gormstore.NewChangeLog(db),
		Activity: gormstore.NewActivityLog(db), Transact: database.NewTransactor(db), Extension: x,
	})
	require.NoError(t, err)

	return admin, stores
}

func create(admin *account.Admin, login string, ext json.RawMessage) (account.Detail, error) {
	return admin.CreateWithExt(context.Background(), account.CreateAccount{
		Login: login, Name: login, Email: login + "@example.test", Locale: "en", Password: "a long password", Ext: ext,
	})
}

// The extension writes in the transaction of the account's creation: its refusal leaves no account and no history.
func TestAnExtensionRefusalRollsBackTheCreation(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		x := &scribbler{refuse: errRefused}
		admin, stores := extended(t, db, x)

		_, err := create(admin, "boris", json.RawMessage(`{"dealer_id":3}`))
		require.True(t, apperr.HasReason(err, "shop.user.dealer_bindings"), "its refusal is the answer: %v", err)
		require.JSONEq(t, `{"dealer_id":3}`, string(x.got), "it was given the body's ext")

		_, err = stores.Accounts.FindByLogin(t.Context(), "boris")
		require.ErrorIs(t, err, account.ErrNotFound, "no account")

		var rows int64
		require.NoError(t, db.Table("audit_logs").Count(&rows).Error)
		require.Zero(t, rows, "no history either")

		x.refuse = nil

		made, err := create(admin, "boris", nil)
		require.NoError(t, err)
		require.Equal(t, map[string]any{"id": made.ID}, made.Ext, "what the extension answered")
	})
}

func TestAnExtensionRefusalRollsBackTheUpdate(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		x := &scribbler{}
		admin, stores := extended(t, db, x)

		boris, err := create(admin, "boris", nil)
		require.NoError(t, err)
		require.NoError(t, stores.Accounts.SetLocale(t.Context(), boris.ID, "en")) // the creation's own scribble, undone

		x.refuse = errRefused

		for name, in := range map[string]account.UpdateAccount{
			"a field differs":  {ID: boris.ID, Login: "boris", Name: "Boris B", Email: "boris@example.test", Locale: "en"},
			"no field differs": {ID: boris.ID, Login: "boris", Name: "boris", Email: "boris@example.test", Locale: "en"},
		} {
			_, err = admin.Update(t.Context(), in)
			require.True(t, apperr.HasReason(err, "shop.user.dealer_bindings"), name)

			after, err := stores.Accounts.Find(t.Context(), boris.ID)
			require.NoError(t, err)
			require.Equal(t, "boris", after.Name, name+": the name is unchanged")
			require.Equal(t, "en", after.Locale, name+": the extension's write was rolled back")
		}

		_, total, err := gormstore.NewChangeLog(db).Changes(t.Context(), account.ChangeQuery{AccountID: boris.ID, Limit: 10})
		require.NoError(t, err)
		require.Equal(t, 1, total, "only the creation is on the history")

		// accepted: the extension runs although no field differs, and its write is kept
		x.refuse = nil

		_, err = admin.Update(t.Context(), account.UpdateAccount{ID: boris.ID, Login: "boris", Name: "boris", Email: "boris@example.test", Locale: "en", Ext: json.RawMessage(`1`)})
		require.NoError(t, err)
		require.JSONEq(t, `1`, string(x.got))

		after, err := stores.Accounts.Find(t.Context(), boris.ID)
		require.NoError(t, err)
		require.Equal(t, "de", after.Locale, "the extension's write is in")
	})
}

func TestAnExtensionRefusalRollsBackTheDeactivation(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		x := &scribbler{}
		admin, stores := extended(t, db, x)

		boris, err := create(admin, "boris", nil)
		require.NoError(t, err)
		require.NoError(t, stores.Accounts.SetLocale(t.Context(), boris.ID, "en"))

		session, err := stores.Sessions.Open(t.Context(), account.NewSession{AccountID: boris.ID, At: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
		require.NoError(t, err)

		x.refuse = errRefused

		_, err = admin.DeactivateWithExt(t.Context(), account.DeactivateInput{ID: boris.ID, Details: json.RawMessage(`{"leads":2}`)})
		require.True(t, apperr.HasReason(err, "shop.user.dealer_bindings"))
		require.JSONEq(t, `{"leads":2}`, string(x.got))

		after, err := stores.Accounts.Find(t.Context(), boris.ID)
		require.NoError(t, err)
		require.True(t, after.Active, "still active")
		require.Equal(t, "en", after.Locale, "the extension's write was rolled back")

		_, err = stores.Sessions.Get(t.Context(), session.Access, time.Now())
		require.NoError(t, err, "the session lives")

		x.refuse = nil

		answer, err := admin.DeactivateWithExt(t.Context(), account.DeactivateInput{ID: boris.ID})
		require.NoError(t, err)
		require.Equal(t, "handed over", answer)

		after, err = stores.Accounts.Find(t.Context(), boris.ID)
		require.NoError(t, err)
		require.False(t, after.Active)
		require.Equal(t, "de", after.Locale, "the extension's write committed with it")

		_, err = stores.Sessions.Get(t.Context(), session.Access, time.Now())
		require.Error(t, err, "go-core still ended the sessions")
	})
}

// An administrator setting their own password keeps the session KeepToken names.
func TestSettingYourOwnPasswordKeepsTheSession(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		x := &scribbler{}
		admin, stores := extended(t, db, x)

		ana, err := create(admin, "ana", nil)
		require.NoError(t, err)

		here, err := stores.Sessions.Open(t.Context(), account.NewSession{AccountID: ana.ID, At: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
		require.NoError(t, err)

		other, err := stores.Sessions.Open(t.Context(), account.NewSession{AccountID: ana.ID, At: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
		require.NoError(t, err)

		require.NoError(t, admin.SetPassword(t.Context(), account.SetPassword{ID: ana.ID, Password: "a better password", ActorID: ana.ID, KeepToken: here.Access}))

		_, err = stores.Sessions.Get(t.Context(), here.Access, time.Now())
		require.NoError(t, err, "this one stays")

		_, err = stores.Sessions.Get(t.Context(), other.Access, time.Now())
		require.Error(t, err, "the other ends")
	})
}
