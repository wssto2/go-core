package migrations_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/audit/migrations"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
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
	// Output: 20261016000010_audit_logs.sql
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

func indexNames(t *testing.T, db *gorm.DB, table string) []string {
	t.Helper()

	var names []string

	require.NoError(t, db.Raw(`SELECT DISTINCT index_name FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? ORDER BY index_name`, table).Scan(&names).Error)

	return names
}

// The file creates the table audit.Migrate creates: the same columns in the same
// order, with the same types, nullability and defaults. It adds two indexes,
// and nothing else.
func TestTheFileCreatesWhatAuditMigrateCreates(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("scratch", db)

		require.NoError(t, migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.Files).Up(context.Background()))
		require.NoError(t, db.Exec("RENAME TABLE audit_logs TO ddl_audit_logs").Error)
		require.NoError(t, audit.Migrate(db))

		want, got := columnsOf(t, db, "ddl_audit_logs"), columnsOf(t, db, "audit_logs")
		require.Equal(t, got, want)

		require.Equal(t, []string{"PRIMARY"}, indexNames(t, db, "audit_logs"))
		require.ElementsMatch(t, []string{"PRIMARY", "idx_audit_logs_actor_created", "idx_audit_logs_entity"}, indexNames(t, db, "ddl_audit_logs"))
	}, dbtest.MySQL, dbtest.MariaDB)
}
