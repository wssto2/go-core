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

// hostEntity and hostUpdate are the record type and verb of the host's own audit rows.
const (
	hostEntity = "users"
	hostUpdate = "update"
)

func auditRow(entity string, actor int, action string) audit.AuditLog {
	//nolint:exhaustruct,exhaustruct_v5 // the rest is filled by the database
	return audit.AuditLog{EntityType: entity, EntityID: 5, ActorID: actor, Action: action}
}

func changesOf(only ...account.ChangeAction) account.ChangeQuery {
	//nolint:exhaustruct,exhaustruct_v5 // no offset, no exclusion
	return account.ChangeQuery{AccountID: 5, Limit: 10, Only: only}
}

func changeBy(actor int, action account.ChangeAction) account.Change {
	//nolint:exhaustruct,exhaustruct_v5 // no before and after values
	return account.Change{AccountID: 5, ActorID: actor, Action: action, Fields: []string{"x"}}
}

// G3: the history of a host whose audit trail names its people "users" with the verbs
// create/update/delete is read as it is and written in the same words.
func TestChangeLogOverTheHostsAuditRecord(t *testing.T) {
	t.Parallel()

	dbtest.Run(t, func(t *testing.T, conn *gorm.DB) {
		t.Helper()
		tables(t, conn)
		empty(t, conn)

		// rows the host wrote before identity, and one of identity's own type that is not this history
		rows := []audit.AuditLog{
			auditRow(hostEntity, 1, "create"), auditRow(hostEntity, 1, hostUpdate),
			auditRow(hostEntity, 2, "delete"), auditRow("account", 2, "created"),
		}
		for i := range rows {
			require.NoError(t, conn.Create(&rows[i]).Error)
		}

		log := gormstore.NewChangeLog(conn, gormstore.AuditRecord(hostEntity, map[account.ChangeAction]string{
			account.ChangeCreated: "create", account.ChangeUpdated: hostUpdate, account.ChangeDeactivated: "delete",
			account.ChangeProfile: hostUpdate, // two actions share a name
		}))

		got, total, err := log.Changes(t.Context(), changesOf())
		require.NoError(t, err)
		require.Equal(t, 3, total)
		assert.Equal(t, []account.ChangeAction{account.ChangeDeactivated, account.ChangeUpdated, account.ChangeCreated},
			[]account.ChangeAction{got[0].Action, got[1].Action, got[2].Action}, "read back as identity's actions, newest first")

		only, total, err := log.Changes(t.Context(), changesOf(account.ChangeDeactivated))
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Equal(t, account.ChangeDeactivated, only[0].Action, "a filter speaks the host's verbs")

		for _, action := range []account.ChangeAction{account.ChangeProfile, account.ChangePassword} {
			require.NoError(t, log.Record(t.Context(), changeBy(3, action)))
		}

		var written []audit.AuditLog
		require.NoError(t, conn.Where("actor_id = ?", 3).Order("id").Find(&written).Error)
		require.Len(t, written, 2)
		assert.Equal(t, hostEntity, written[0].EntityType)
		assert.Equal(t, hostUpdate, written[0].Action, "the host's word")
		assert.Equal(t, "password", written[1].Action, "an action without a name keeps its own")

		counts, err := log.ChangeCounts(t.Context(), 5)
		require.NoError(t, err)
		assert.Equal(t, 2, counts[account.ChangeUpdated], "the update row and the profile row are both read as updated")
		assert.Equal(t, 1, counts[account.ChangeCreated])
	})
}
