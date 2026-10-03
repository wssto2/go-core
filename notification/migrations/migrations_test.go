package migrations_test

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
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
	// Output:
	// 20261016000030_notifications.sql
	// 20261016000031_notification_deliveries.sql
	// 20261016000032_notification_settings.sql
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

var tables = []string{"notifications", "notification_deliveries", "notification_preferences", "notification_quiet_hours"}

// The files create the tables notification.Migrate creates from the models: the same columns in the same
// order with the same types, nullability and defaults, and the same indexes.
func TestTheFilesCreateWhatMigrateCreates(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		up(t, db)

		for _, table := range tables {
			require.NoError(t, db.Exec("RENAME TABLE "+table+" TO ddl_"+table).Error)
		}

		require.NoError(t, notification.Migrate(db))

		for _, table := range tables {
			require.NotEmpty(t, columnsOf(t, db, table))
			require.Equal(t, columnsOf(t, db, table), columnsOf(t, db, "ddl_"+table), table)
			require.Equal(t, indexesOf(t, db, table), indexesOf(t, db, "ddl_"+table), table)
		}
	}, dbtest.MySQL, dbtest.MariaDB)
}

// arv-next's tables, read with SHOW CREATE TABLE, as they are in its local database today (notification_deliveries
// after its push and settings migrations and the retention index). The files must create exactly these columns and
// keys, so arv-next adopts them with MarkApplied.
func TestTheFilesAreArvNextsTables(t *testing.T) {
	want := map[string]struct {
		columns []string
		indexes []string
	}{
		"notifications": {
			[]string{
				"id bigint unsigned/NO", "user_id int unsigned/NO", "category varchar(64)/NO", "title varchar(160)/NO", "body varchar(500)/NO", "link varchar(255)/NO",
				"data json/NO", "dedupe_key varchar(128)/NO", "read_at datetime/YES", "created_at datetime/NO",
			},
			[]string{"PRIMARY:0:id", "notifications_dedupe_key:0:dedupe_key", "notifications_user_id:1:user_id,id", "notifications_user_read:1:user_id,read_at"},
		},
		"notification_deliveries": {
			[]string{
				"id bigint unsigned/NO", "notification_id bigint unsigned/NO", "device_id bigint unsigned/NO", "address varchar(255)/YES", "channel varchar(16)/NO",
				"status varchar(16)/NO", "attempts int unsigned/NO", "next_attempt_at datetime/NO", "expires_at datetime/NO", "sent_at datetime/YES",
				"last_status smallint/YES", "last_error varchar(500)/YES", "created_at datetime/NO", "updated_at datetime/NO",
			},
			[]string{
				"PRIMARY:0:id", "notification_deliveries_target:0:notification_id,device_id,channel", "notification_deliveries_due:1:status,next_attempt_at",
				"notification_deliveries_device:1:device_id", "notification_deliveries_finished:1:status,updated_at",
			},
		},
		"notification_preferences": {
			[]string{"user_id int unsigned/NO", "category varchar(64)/NO", "channel varchar(16)/NO", "enabled tinyint(1)/NO", "updated_at datetime/NO"},
			[]string{"PRIMARY:0:user_id,category,channel"},
		},
		"notification_quiet_hours": {
			[]string{"user_id int unsigned/NO", "enabled tinyint(1)/NO", "start_minute smallint unsigned/NO", "end_minute smallint unsigned/NO", "updated_at datetime/NO"},
			[]string{"PRIMARY:0:user_id"},
		},
	}

	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		up(t, db)

		for _, table := range tables {
			var got []string
			for _, c := range columnsOf(t, db, table) {
				got = append(got, c.Name+" "+normalizeType(c.Type)+"/"+c.Nullable)
			}

			require.Equal(t, want[table].columns, got, table)
			require.ElementsMatch(t, want[table].indexes, indexesOf(t, db, table), table)
		}
	}, dbtest.MySQL, dbtest.MariaDB)
}

var displayWidth = regexp.MustCompile(`^(bigint|int|smallint)\(\d+\)`)

// normalizeType makes MySQL 9 ("bigint unsigned", "int unsigned") and MariaDB 10.3 ("bigint(20) unsigned",
// "longtext" for json) name the same column type the same way.
func normalizeType(s string) string {
	if s == "longtext" {
		return "json"
	}

	return displayWidth.ReplaceAllString(s, "$1")
}

// Running the file twice, or over a table the application already has, changes nothing.
func TestRunningTheFileOverAnExistingTableIsHarmless(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, notification.Migrate(db))
		up(t, db)
	}, dbtest.MySQL, dbtest.MariaDB)
}
