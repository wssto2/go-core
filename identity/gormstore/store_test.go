package gormstore_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/audit"
	auditmigrations "github.com/wssto2/go-core/audit/migrations"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/gormstore"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/identity/migrations"
	"github.com/wssto2/go-core/identity/storetest"
	"gorm.io/gorm"
)

// tables creates the identity tables the way each database gets them: the SQL
// files on MySQL and MariaDB (what production runs), the models on SQLite
// (which cannot run MySQL DDL).
func tables(t *testing.T, db *gorm.DB) {
	t.Helper()

	if db.Name() == "sqlite" {
		require.NoError(t, gormstore.Migrate(db))
		require.NoError(t, audit.Migrate(db))

		return
	}

	reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
	reg.AddConnection("scratch", db)

	require.NoError(t, migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.Files).Add("scratch", auditmigrations.Files).Up(context.Background()))
}

// empty clears the tables between subtests, so every one starts from nothing.
func empty(t *testing.T, db *gorm.DB) {
	t.Helper()

	for _, table := range []string{"accounts", "user_signins", "tokens", "user_verification_codes", "user_reauth_attempts", "audit_logs"} {
		require.NoError(t, db.Exec("DELETE FROM "+table).Error)
	}
}

func TestStoresConform(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		tables(t, db)

		storetest.Run(t, func(t *testing.T) storetest.Stores {
			empty(t, db)

			stores := gormstore.New(db)

			return storetest.Stores{
				Accounts: stores.Accounts, SignIns: stores.SignIns, Sessions: stores.Sessions, Codes: stores.Codes, Reauth: stores.Reauth,
				Changes: gormstore.NewChangeLog(db), Activity: gormstore.NewActivityLog(db),
			}
		})
	})
}

// A session another hasher stored (arv-next keeps HMAC-hashed refresh tokens)
// is read and rotated with that hasher.
func TestRefreshHasherIsConfigurable(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		tables(t, db)

		stores := gormstore.New(db, gormstore.WithRefreshHasher(auth.NewHMACHasher([]byte("secret"))))
		at := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

		creds, err := stores.Sessions.Open(t.Context(), account.NewSession{AccountID: 1, At: at, ExpiresAt: at.Add(time.Hour)})
		require.NoError(t, err)

		var stored auth.Token
		require.NoError(t, db.Where("token_value = ?", creds.Access).Take(&stored).Error)
		assert.NotEqual(t, creds.Refresh, stored.RefreshToken, "only a hash is kept")
		assert.Equal(t, creds.Refresh[:8], stored.RefreshPrefix)

		_, _, err = stores.Sessions.Rotate(t.Context(), account.Rotation{Refresh: creds.Refresh, At: at, ExpiresAt: at.Add(time.Hour)})
		require.NoError(t, err)

		other := gormstore.New(db) // SHA-256: cannot read what HMAC wrote
		_, _, err = other.Sessions.Rotate(t.Context(), account.Rotation{Refresh: creds.Refresh, At: at, ExpiresAt: at.Add(time.Hour)})
		assert.ErrorIs(t, err, account.ErrSessionNotFound)
	})
}

// The services over the SQL stores: the lock is read off the history table.
func TestSignInOverTheSQLStores(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		tables(t, db)

		stores := gormstore.New(db)
		clock := identitytest.NewClock(identitytest.Epoch)

		svc, err := account.New(account.Deps{
			Accounts: stores.Accounts, SignIns: stores.SignIns, Sessions: stores.Sessions, Clock: clock, Hasher: identitytest.Hasher,
		}, account.Config{})
		require.NoError(t, err)

		seed := identitytest.Account(0, "ana", "secret")
		seed.CreatedAt = clock.Now()
		_, err = stores.Accounts.Create(t.Context(), seed)
		require.NoError(t, err)

		in := func(password string) account.LoginInput {
			return account.LoginInput{Login: "Ana", Password: password, Device: "test", IP: "10.0.0.1"}
		}

		for range 5 {
			_, err := svc.SignIn.Login(t.Context(), in("nope"))
			assert.True(t, apperr.HasReason(err, account.ReasonSignInFailed))
			clock.Advance(time.Second)
		}

		_, err = svc.SignIn.Login(t.Context(), in("secret"))
		assert.True(t, apperr.HasReason(err, account.ReasonSignInLocked), "five wrong passwords lock")

		clock.Advance(15 * time.Minute)

		signed, err := svc.SignIn.Login(t.Context(), in("secret"))
		require.NoError(t, err, "fifteen minutes later")

		got, err := svc.SignIn.Authenticate(t.Context(), signed.Credentials.Access)
		require.NoError(t, err)
		assert.Equal(t, "ana", got.Account.Login)

		refreshed, err := svc.SignIn.Refresh(t.Context(), account.RefreshInput{Token: signed.Credentials.Refresh})
		require.NoError(t, err)
		assert.NotEqual(t, signed.Credentials.Access, refreshed.Credentials.Access)
	})
}
