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
	"github.com/wssto2/go-core/identity/gormstore"
	"github.com/wssto2/go-core/identity/migrations"
	"gorm.io/gorm"
)

type column struct {
	Name     string
	Type     string `gorm:"column:column_type"`
	Nullable string `gorm:"column:is_nullable"`
	Default  *string
	Extra    string
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

	require.NoError(t, db.Raw(`SELECT column_name AS name, column_type, is_nullable, column_default AS `+"`default`"+`, extra
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

// family drops the display width and the integer size: GORM maps a Go int to
// bigint where the DDL says INT, and time.Time to datetime(3). Unsigned stays
// unsigned.
func family(sqlType string) string {
	base, _, _ := strings.Cut(sqlType, "(")
	if base == "bigint" {
		return "int"
	}

	if strings.HasSuffix(sqlType, " unsigned") {
		return strings.TrimSuffix(base, " unsigned") + " unsigned"
	}

	return base
}

func deref(s *string) string {
	if s == nil {
		return "<NULL>"
	}

	return *s
}

// The tables the migration files create equal the ones the GORM models create
// (gormstore.Migrate): same columns in the same order, same nullability and
// defaults, same indexes. The tolerated difference is the integer size and the
// datetime precision, reported in the log. tokens is auth.Token's table, so this
// also holds the file equal to the model go-core's auth package uses.
func TestMigrationsMatchTheModels(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("scratch", db)

		require.NoError(t, migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.Files).Up(context.Background()))

		tables := []string{"accounts", "user_signins", "tokens", "user_verification_codes", "user_reauth_attempts"}
		for _, table := range tables {
			require.NoError(t, db.Exec("RENAME TABLE "+table+" TO ddl_"+table).Error)
		}

		require.NoError(t, gormstore.Migrate(db))

		for _, table := range tables {
			want, got := columnsOf(t, db, "ddl_"+table), columnsOf(t, db, table)
			require.Len(t, got, len(want), table)

			for i := range want {
				w, g := want[i], got[i]
				require.Equal(t, w.Name, g.Name, table)
				require.Equal(t, family(w.Type), family(g.Type), table+"."+w.Name)
				require.Equal(t, w.Nullable, g.Nullable, table+"."+w.Name)
				require.Equal(t, w.Extra, g.Extra, table+"."+w.Name)
				require.Equal(t, deref(w.Default), deref(g.Default), table+"."+w.Name)

				if w.Type != g.Type {
					t.Logf("%s.%s: DDL %s, models %s", table, w.Name, w.Type, g.Type)
				}
			}

			require.Equal(t, indexesOf(t, db, "ddl_"+table), indexesOf(t, db, table), table+" indexes")
		}
	}, dbtest.MySQL, dbtest.MariaDB)
}
