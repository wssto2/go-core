//nolint:exhaustruct,exhaustruct_v5,lll,paralleltest,thelper,varnamelen // tests in the style of the package's others
package gormstore_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
	"github.com/wssto2/go-core/incident/gormstore"
	"github.com/wssto2/go-core/incident/migrations"
	"gorm.io/gorm"
)

type column struct {
	Name     string
	Type     string `gorm:"column:column_type"`
	Nullable string `gorm:"column:is_nullable"`
}

type index struct {
	Name   string
	Seq    int
	Column string
	Unique int
}

func columnsOf(t *testing.T, db *gorm.DB, table string) []column {
	t.Helper()

	var cols []column

	require.NoError(t, db.Raw(`SELECT column_name AS name, column_type, is_nullable
		FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? ORDER BY ordinal_position`, table).Scan(&cols).Error)

	return cols
}

func indexesOf(t *testing.T, db *gorm.DB, table string) []index {
	t.Helper()

	var idx []index

	require.NoError(t, db.Raw(`SELECT index_name AS name, seq_in_index AS seq, column_name AS `+"`column`"+`, non_unique AS `+"`unique`"+`
		FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? ORDER BY index_name, seq_in_index`, table).Scan(&idx).Error)

	return idx
}

// family drops what the models do not say: the display width, the integer size and sign, the enum's values, the
// kind of string and text.
func family(sqlType string) string {
	base, _, _ := strings.Cut(sqlType, "(")
	base, _, _ = strings.Cut(base, " ")

	switch base {
	case "bigint", "int":
		return "int"
	case "enum", "char", "varchar":
		return "string"
	case "text", "mediumtext", "longtext", "json":
		return "text"
	}

	return base
}

// The tables the migration files create equal the ones the GORM models create (gormstore.Migrate): the same columns in
// the same order, nullability and kind of type, and the same indexes. The models say less than the DDL (enums are
// strings, integers are bigint), which the log reports.
func TestMigrationsMatchTheModels(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("scratch", db)

		require.NoError(t, migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.Files).Up(context.Background()))

		for _, table := range allTables {
			require.NoError(t, db.Exec("RENAME TABLE "+table+" TO ddl_"+table).Error)
		}

		require.NoError(t, gormstore.Migrate(db))

		for _, table := range allTables {
			want, got := columnsOf(t, db, "ddl_"+table), columnsOf(t, db, table)
			require.Len(t, got, len(want), table)

			for i := range want {
				w, g := want[i], got[i]
				require.Equal(t, w.Name, g.Name, table)
				require.Equal(t, family(w.Type), family(g.Type), table+"."+w.Name)
				require.Equal(t, w.Nullable, g.Nullable, table+"."+w.Name)

				if w.Type != g.Type {
					t.Logf("%s.%s: DDL %s, models %s", table, w.Name, w.Type, g.Type)
				}
			}

			require.Equal(t, indexesOf(t, db, "ddl_"+table), indexesOf(t, db, table), table+" indexes")
		}
	}, dbtest.MySQL, dbtest.MariaDB)
}
