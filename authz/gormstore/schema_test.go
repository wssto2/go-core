package gormstore_test

import (
	"context"
	"io/fs"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/authz/migrations"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
	"gorm.io/gorm"
)

// sqlOf is the statements of the migration files in version order, without the
// comments and goose markers, whitespace squeezed.
func sqlOf(t *testing.T) string {
	t.Helper()

	entries, err := fs.ReadDir(migrations.Files, ".")
	require.NoError(t, err)

	var b strings.Builder

	for _, e := range entries {
		src, err := fs.ReadFile(migrations.Files, e.Name())
		require.NoError(t, err)

		for _, line := range strings.Split(string(src), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				b.WriteString(line + "\n")
			}
		}
	}

	return strings.Join(strings.Fields(b.String()), " ")
}

// The migration files are MySQLSchema, statement for statement.
func TestMigrationsAreMySQLSchema(t *testing.T) {
	require.Equal(t, strings.Join(strings.Fields(gormstore.MySQLSchema), " "), sqlOf(t))
}

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
// bigint where the DDL says INT.
func family(sqlType string) string {
	base, _, _ := strings.Cut(sqlType, "(")
	if base == "bigint" {
		return "int"
	}

	return base
}

// The tables the migration files create equal the ones the GORM models
// create (gormstore.Migrate): same columns in the same order, same nullability
// and defaults, same indexes. The one tolerated difference is the integer size,
// reported in the log: the models say int, which GORM maps to bigint on MySQL.
func TestMigrationsMatchTheModels(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("scratch", db)

		require.NoError(t, migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.Files).Up(context.Background()))

		tables := []string{"roles", "role_permissions", "role_bindings"}
		for _, table := range tables {
			require.NoError(t, db.Exec("RENAME TABLE "+table+" TO ddl_"+table).Error)
		}

		// A CHECK constraint name is unique per schema: note it, then free the name.
		checks := map[string][]string{}
		for _, table := range tables {
			checks["ddl_"+table] = checksOf(t, db, "ddl_"+table)
			for _, name := range checks["ddl_"+table] {
				require.NoError(t, db.Exec("ALTER TABLE ddl_"+table+" DROP CONSTRAINT "+name).Error)
			}
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
			require.Equal(t, checks["ddl_"+table], checksOf(t, db, table), table+" check constraints")
		}
	}, dbtest.MySQL, dbtest.MariaDB)
}

func checksOf(t *testing.T, db *gorm.DB, table string) []string {
	t.Helper()

	var names []string

	require.NoError(t, db.Raw(`SELECT constraint_name FROM information_schema.table_constraints
		WHERE table_schema = DATABASE() AND table_name = ? AND constraint_type = 'CHECK' ORDER BY constraint_name`, table).Scan(&names).Error)

	return names
}

func deref(s *string) string {
	if s == nil {
		return "<NULL>"
	}

	return *s
}
