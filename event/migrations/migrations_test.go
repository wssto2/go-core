package migrations_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/event/migrations"
	"gorm.io/gorm"
)

func TestMigrationsAreMariaDB103Portable(t *testing.T) {
	dbtest.RequirePortable(t, migrations.Files)
}

func ExampleFiles() {
	entries, _ := migrations.Files.ReadDir(".")
	for _, e := range entries {
		fmt.Println(e.Name())
	}
	// Output:
	// 20261016000020_event_outbox.sql
	// 20261016000021_event_consumer_attempts.sql
}

type column struct {
	Name     string
	Type     string `gorm:"column:column_type"`
	Nullable string `gorm:"column:is_nullable"`
	Default  *string
	Extra    string
}

func columnsOf(t *testing.T, db *gorm.DB, table string) []column {
	t.Helper()

	var cols []column

	require.NoError(t, db.Raw(`SELECT column_name AS name, column_type, is_nullable, column_default AS `+"`default`"+`, extra
		FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? ORDER BY ordinal_position`, table).Scan(&cols).Error)

	return cols
}

func indexesOf(t *testing.T, db *gorm.DB, table string) []string {
	t.Helper()

	var rows []string

	require.NoError(t, db.Raw(`SELECT CONCAT(index_name, ':', non_unique, ':', GROUP_CONCAT(column_name ORDER BY seq_in_index)) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? GROUP BY index_name, non_unique ORDER BY index_name`, table).Scan(&rows).Error)

	return rows
}

// The files create the tables event.Migrate creates from the models: the same
// columns in the same order with the same types, nullability and defaults, and
// the same indexes.
func TestTheFilesCreateWhatMigrateCreates(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("scratch", db)

		require.NoError(t, migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.Files).Up(context.Background()))

		for _, table := range []string{"outbox_events", "event_consumer_attempts"} {
			require.NoError(t, db.Exec("RENAME TABLE "+table+" TO ddl_"+table).Error)
		}

		require.NoError(t, event.Migrate(db))

		for _, table := range []string{"outbox_events", "event_consumer_attempts"} {
			require.NotEmpty(t, columnsOf(t, db, table), table)
			require.Equal(t, columnsOf(t, db, table), columnsOf(t, db, "ddl_"+table), table)
			require.Equal(t, indexesOf(t, db, table), indexesOf(t, db, "ddl_"+table), table)
		}
	}, dbtest.MySQL, dbtest.MariaDB)
}

// Running the files twice, or over tables the application already has, changes nothing.
func TestRunningTheFilesOverExistingTablesIsHarmless(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, event.Migrate(db))

		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("scratch", db)
		require.NoError(t, migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.Files).Up(context.Background()))
	}, dbtest.MySQL, dbtest.MariaDB)
}
