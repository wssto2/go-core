//nolint:exhaustruct,exhaustruct_v5,paralleltest,thelper,varnamelen // tests in the style of the package's others
package gormstore_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
	"github.com/wssto2/go-core/savedview/core"
	"github.com/wssto2/go-core/savedview/gormstore"
	"github.com/wssto2/go-core/savedview/migrations"
	"github.com/wssto2/go-core/savedview/storetest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// tables creates the table the way each database gets it: the SQL file on MySQL and MariaDB (what production runs),
// the model on SQLite (which cannot run MySQL DDL).
func tables(t *testing.T, db *gorm.DB) {
	t.Helper()

	if db.Name() == "sqlite" {
		require.NoError(t, gormstore.Migrate(db))

		return
	}

	reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
	reg.AddConnection("scratch", db)

	migrator := migrate.New(reg, nil, slog.New(slog.DiscardHandler))
	require.NoError(t, migrator.Add("scratch", migrations.Files).Up(context.Background()))
}

func TestStoreConforms(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		tables(t, db)

		storetest.Run(t, func(t *testing.T) core.Store {
			require.NoError(t, db.Exec("DELETE FROM saved_views").Error)

			return gormstore.New(db)
		})
	})
}

func TestStoreJoinsOnlyItsOwnConnectionsTransaction(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, gormstore.Migrate(db))
		other, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "other.db")), &gorm.Config{})
		require.NoError(t, err)

		store := gormstore.New(db)
		view := &core.View{UserID: 1, ListKey: "k", Name: "n", State: "{}"}

		// Only a transaction on the other connection is in ctx: the store writes on its own and commits.
		foreignCtx, _ := dbtest.TxContext(t, other)
		require.NoError(t, store.Save(foreignCtx, view))

		var n int64
		require.NoError(t, db.Table("saved_views").Count(&n).Error)
		require.EqualValues(t, 1, n)
	}, dbtest.SQLite)
}
