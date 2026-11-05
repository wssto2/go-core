package gormstore_test

import (
	"context"
	"io/fs"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/authz/migrations"
	"github.com/wssto2/go-core/authz/storetest"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/database/migrate"
	"gorm.io/gorm"
)

// sqlOf is the statements of the migration files in version order, without the
// comments and goose markers, whitespace squeezed.
func sqlOf(t *testing.T) string { return sqlOfFS(t, migrations.Files) }

func sqlOfFS(t *testing.T, files fs.FS) string {
	t.Helper()

	entries, err := fs.ReadDir(files, ".")
	require.NoError(t, err)

	var b strings.Builder

	for _, e := range entries {
		src, err := fs.ReadFile(files, e.Name())
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

// The prefixed files are MySQLSchemaWithPrefix, and a prefix of "" changes nothing.
func TestPrefixedMigrationsArePrefixedMySQLSchema(t *testing.T) {
	squeeze := func(s string) string { return strings.Join(strings.Fields(s), " ") }

	require.Equal(t, squeeze(gormstore.MySQLSchemaWithPrefix("authz_")), sqlOfFS(t, migrations.WithPrefix("authz_")))
	require.Equal(t, gormstore.MySQLSchema, gormstore.MySQLSchemaWithPrefix(""))
	require.Equal(t, sqlOf(t), sqlOfFS(t, migrations.WithPrefix("")))
	require.Contains(t, gormstore.MySQLSchemaWithPrefix("authz_"), "CREATE TABLE IF NOT EXISTS authz_roles (")
	require.Contains(t, gormstore.MySQLSchemaWithPrefix("authz_"), "UNIQUE KEY uq_role_bindings (")
}

// Prefixed tables: the goose files and the models agree on the columns, and the store
// passes its conformance suite on them, beside a roles table of someone else's.
func TestPrefixedTablesOnEveryDatabase(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.Exec("CREATE TABLE roles (id INT PRIMARY KEY)").Error)

		if db.Name() == "sqlite" {
			require.NoError(t, gormstore.MigrateWithPrefix(db, "authz_"))
		} else {
			reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
			reg.AddConnection("scratch", db)
			runner := migrate.New(reg, nil, slog.New(slog.DiscardHandler)).Add("scratch", migrations.WithPrefix("authz_"))
			require.NoError(t, runner.Up(context.Background()))

			// a CHECK name is unique per schema: free it for the second set
			require.NoError(t, db.Exec("ALTER TABLE authz_role_bindings DROP CONSTRAINT chk_role_bindings_role").Error)
			require.NoError(t, gormstore.MigrateWithPrefix(db, "model_"))

			for _, table := range []string{"roles", "role_permissions", "role_bindings"} {
				want, got := columnsOf(t, db, "authz_"+table), columnsOf(t, db, "model_"+table)
				require.Len(t, got, len(want), table)

				for i := range want {
					require.Equal(t, want[i].Name, got[i].Name, table)
					require.Equal(t, family(want[i].Type), family(got[i].Type), table+"."+want[i].Name)
				}
			}
		}

		storetest.Run(t, func(t *testing.T) authz.Store {
			for _, table := range []string{"authz_role_bindings", "authz_role_permissions", "authz_roles"} {
				require.NoError(t, db.Exec("DELETE FROM "+table).Error)
			}
			return gormstore.New(db, gormstore.WithTablePrefix("authz_"))
		})

	}, dbtest.SQLite, dbtest.MySQL, dbtest.MariaDB)
}
