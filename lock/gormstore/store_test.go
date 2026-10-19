//nolint:exhaustruct,exhaustruct_v5,lll,paralleltest,thelper,varnamelen // tests in the style of the package's others
package gormstore_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
	"github.com/wssto2/go-core/lock"
	"github.com/wssto2/go-core/lock/gormstore"
	"github.com/wssto2/go-core/lock/migrations"
	"github.com/wssto2/go-core/lock/storetest"
	"gorm.io/gorm"
)

// tables creates the table the way each database gets it: the SQL file on MySQL and MariaDB (what production
// runs), the model on SQLite (which cannot run MySQL DDL).
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

var _ lock.Store = (*gormstore.Store)(nil)

func TestStoreConforms(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		tables(t, db)

		storetest.Run(t, func(t *testing.T) lock.Store {
			require.NoError(t, db.Exec("DELETE FROM locks").Error)

			return gormstore.New(db)
		})
	})
}
