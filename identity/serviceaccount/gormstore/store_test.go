package gormstore_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
	"github.com/wssto2/go-core/identity/serviceaccount"
	"github.com/wssto2/go-core/identity/serviceaccount/gormstore"
	"github.com/wssto2/go-core/identity/serviceaccount/migrations"
	"github.com/wssto2/go-core/identity/serviceaccount/storetest"
	"gorm.io/gorm"
)

// tables creates the service account tables the way each database gets them: the SQL files on MySQL and
// MariaDB (what production runs), the models on SQLite (which cannot run MySQL DDL).
func tables(t *testing.T, db *gorm.DB) {
	t.Helper()

	if db.Name() == "sqlite" {
		require.NoError(t, gormstore.Migrate(db))

		return
	}

	reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
	reg.AddConnection("scratch", db)

	require.NoError(t, migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.Files).Up(context.Background()))
}

// empty clears the tables between subtests, so every one starts from nothing.
func empty(t *testing.T, db *gorm.DB) {
	t.Helper()

	for _, table := range []string{"service_accounts", "service_account_keys", "service_account_key_notices"} {
		require.NoError(t, db.Exec("DELETE FROM "+table).Error)
	}
}

func TestStoreConforms(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		tables(t, db)

		storetest.Run(t, func(t *testing.T) serviceaccount.Store {
			empty(t, db)

			return gormstore.New(db)
		})
	})
}
