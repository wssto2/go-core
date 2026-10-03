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
	"github.com/wssto2/go-core/notification"
	"github.com/wssto2/go-core/notification/migrations"
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
	// Output: 20261016000030_notifications.sql
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

func up(t *testing.T, db *gorm.DB) {
	t.Helper()

	reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
	reg.AddConnection("scratch", db)
	require.NoError(t, migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.Files).Up(context.Background()))
}

// The file creates the table notification.Migrate creates from the model: the same columns in the same
// order with the same types, nullability and defaults, and the same indexes.
func TestTheFileCreatesWhatMigrateCreates(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		up(t, db)
		require.NoError(t, db.Exec("RENAME TABLE notifications TO ddl_notifications").Error)
		require.NoError(t, notification.Migrate(db))

		require.NotEmpty(t, columnsOf(t, db, "notifications"))
		require.Equal(t, columnsOf(t, db, "notifications"), columnsOf(t, db, "ddl_notifications"))
		require.Equal(t, indexesOf(t, db, "notifications"), indexesOf(t, db, "ddl_notifications"))
	}, dbtest.MySQL, dbtest.MariaDB)
}

// arv-next's notifications table, read with SHOW CREATE TABLE, as it is in its local database today.
// The file must create exactly these columns and keys, so arv-next adopts it with MarkApplied.
func TestTheFileIsArvNextsTable(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		up(t, db)

		got := columnsOf(t, db, "notifications")
		names := make([]string, len(got))
		types := make([]string, len(got))

		for i, c := range got {
			names[i], types[i] = c.Name, c.Type+"/"+c.Nullable
		}

		require.Equal(t, []string{"id", "user_id", "category", "title", "body", "link", "data", "dedupe_key", "read_at", "created_at"}, names)
		require.Equal(t, []string{
			"bigint(20) unsigned/NO", "int(10) unsigned/NO", "varchar(64)/NO", "varchar(160)/NO", "varchar(500)/NO", "varchar(255)/NO",
			"json/NO", "varchar(128)/NO", "datetime/YES", "datetime/NO",
		}, normalizeTypes(types))
		require.ElementsMatch(t, []string{
			"PRIMARY:0:id", "notifications_dedupe_key:0:dedupe_key", "notifications_user_id:1:user_id,id", "notifications_user_read:1:user_id,read_at",
		}, indexesOf(t, db, "notifications"))
	}, dbtest.MySQL, dbtest.MariaDB)
}

// normalizeTypes makes MySQL 9 ("bigint unsigned", "int unsigned") and MariaDB 10.3 ("bigint(20) unsigned",
// "longtext" for json) name the same column the same way.
func normalizeTypes(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		switch s {
		case "bigint unsigned/NO":
			s = "bigint(20) unsigned/NO"
		case "int unsigned/NO":
			s = "int(10) unsigned/NO"
		case "longtext/NO":
			s = "json/NO"
		}

		out[i] = s
	}

	return out
}

// Running the file twice, or over a table the application already has, changes nothing.
func TestRunningTheFileOverAnExistingTableIsHarmless(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, notification.Migrate(db))
		up(t, db)
	}, dbtest.MySQL, dbtest.MariaDB)
}
