//nolint:exhaustruct,exhaustruct_v5,gochecknoglobals,lll,paralleltest,thelper,varnamelen // tests in the style of the package's others
package gormstore_test

import (
	"context"
	"log/slog"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
	"github.com/wssto2/go-core/incident/core"
	"github.com/wssto2/go-core/incident/gormstore"
	"github.com/wssto2/go-core/incident/migrations"
	"github.com/wssto2/go-core/incident/storetest"
	"gorm.io/gorm"
)

var allTables = []string{
	"incidents", "incident_occurrences", "incident_reports", "incident_comments", "incident_watchers",
	"incident_alert_cooldowns", "incident_digest_queue",
}

// tables creates the incident tables the way each database gets them: the SQL files on MySQL and MariaDB (what
// production runs), the models on SQLite (which cannot run MySQL DDL).
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

// empty clears the tables between subtests, children first, so every one starts from nothing.
func empty(t *testing.T, db *gorm.DB) {
	t.Helper()

	for _, allTable := range slices.Backward(allTables) {
		require.NoError(t, db.Exec("DELETE FROM "+allTable).Error)
	}
}

func TestStoreConforms(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		tables(t, db)

		storetest.Run(t, func(t *testing.T) core.Store {
			empty(t, db)

			return gormstore.New(db)
		})
	})
}
