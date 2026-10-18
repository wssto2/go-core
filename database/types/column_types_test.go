package types_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database/types"
	"gorm.io/gorm"
)

// driverColumn is named so that the embedded interface does not collide with
// the ColumnType method below.
type driverColumn = gorm.ColumnType

// column is a gorm.ColumnType with only the three answers the type map reads;
// the embedded nil interface panics on any other call, so the map cannot grow
// a dependency on one unnoticed.
type column struct {
	driverColumn

	sql        string // "tinyint(1)", "enum('0','1')"; "" = unknown
	nullable   *bool
	defaultVal *string
}

func (c column) ColumnType() (string, bool) { return c.sql, c.sql != "" }

func (c column) Nullable() (bool, bool) {
	if c.nullable == nil {
		return false, false
	}

	return *c.nullable, true
}

func (c column) DefaultValue() (string, bool) {
	if c.defaultVal == nil {
		return "", false
	}

	return *c.defaultVal, true
}

func ptr[T any](v T) *T { return &v }

type columnCase struct {
	name   string
	dbType string
	col    column
	want   string
}

func columnCases() []columnCase {
	null, notNull := ptr(true), ptr(false)

	return []columnCase{
		{"tinyint(1) is a bool", "tinyint", column{sql: "tinyint(1)", nullable: notNull}, "types.Bool"},
		{"nullable tinyint(1)", "tinyint", column{sql: "tinyint(1)", nullable: null}, "types.NullBool"},
		{"tinyint(1) with unknown nullability", "tinyint", column{sql: "tinyint(1)"}, "types.Bool"},
		{"tinyint(4) is an int", "tinyint", column{sql: "tinyint(4)", nullable: notNull}, "types.Int"},
		{"nullable tinyint(4)", "tinyint", column{sql: "tinyint(4)", nullable: null}, "types.NullInt"},
		{"tinyint with unknown full type", "tinyint", column{nullable: notNull}, "types.Int"},
		{"tinyint(1) unsigned is a bool", "tinyint", column{sql: "tinyint(1) unsigned", nullable: notNull}, "types.Bool"},

		{"int", "int", column{nullable: notNull}, "types.Int"},
		{"nullable int", "int", column{nullable: null}, "types.NullInt"},
		{"smallint", "smallint", column{nullable: notNull}, "types.Int"},
		{"nullable smallint", "smallint", column{nullable: null}, "types.NullInt"},
		{"mediumint", "mediumint", column{nullable: notNull}, "types.Int"},
		{"nullable mediumint", "mediumint", column{nullable: null}, "types.NullInt"},
		{"bigint", "bigint", column{nullable: notNull}, "types.Int"},
		{"nullable bigint", "bigint", column{nullable: null}, "types.NullInt"},

		{"float", "float", column{nullable: notNull}, "types.Float"},
		{"nullable float", "float", column{nullable: null}, "types.NullFloat"},
		{"double", "double", column{nullable: notNull}, "types.Float"},
		{"nullable double", "double", column{nullable: null}, "types.NullFloat"},
		{"decimal", "decimal", column{nullable: notNull}, "types.Float"},
		{"nullable decimal", "decimal", column{nullable: null}, "types.NullFloat"},
		{"numeric", "numeric", column{nullable: notNull}, "types.Float"},
		{"nullable numeric", "numeric", column{nullable: null}, "types.NullFloat"},
		{"real", "real", column{nullable: notNull}, "types.Float"},
		{"nullable real", "real", column{nullable: null}, "types.NullFloat"},

		{"boolean ignores nullability", "boolean", column{nullable: null}, "types.Bool"},

		{"date", "date", column{nullable: notNull}, "types.Date"},
		{"nullable date", "date", column{nullable: null}, "types.NullDate"},
		{"nullable date ignores the zero default", "date", column{nullable: null, defaultVal: ptr("0000-00-00")}, "types.NullDate"},
		{"NOT NULL date defaulting to 0000-00-00", "date", column{nullable: notNull, defaultVal: ptr("0000-00-00")}, "types.ZeroDate"},
		{"date with another default", "date", column{nullable: notNull, defaultVal: ptr("2000-01-01")}, "types.Date"},
		{"date with unknown nullability and zero default", "date", column{defaultVal: ptr("0000-00-00")}, "types.ZeroDate"},

		{"datetime", "datetime", column{nullable: notNull}, "types.DateTime"},
		{"nullable datetime", "datetime", column{nullable: null}, "types.NullDateTime"},
		{"NOT NULL datetime defaulting to the zero datetime", "datetime", column{nullable: notNull, defaultVal: ptr("0000-00-00 00:00:00")}, "types.ZeroDateTime"},
		{"datetime with a date-only zero default", "datetime", column{nullable: notNull, defaultVal: ptr("0000-00-00")}, "types.DateTime"},
		{"timestamp", "timestamp", column{nullable: notNull}, "types.DateTime"},
		{"nullable timestamp", "timestamp", column{nullable: null}, "types.NullDateTime"},
		{"NOT NULL timestamp defaulting to the zero datetime", "timestamp", column{nullable: notNull, defaultVal: ptr("0000-00-00 00:00:00")}, "types.ZeroDateTime"},
		{"timestamp defaulting to now", "timestamp", column{nullable: notNull, defaultVal: ptr("CURRENT_TIMESTAMP")}, "types.DateTime"},

		{"varchar", "varchar", column{nullable: notNull}, "types.String"},
		{"nullable varchar", "varchar", column{nullable: null}, "types.NullString"},
		{"char", "char", column{nullable: notNull}, "types.String"},
		{"nullable char", "char", column{nullable: null}, "types.NullString"},
		{"tinytext", "tinytext", column{nullable: notNull}, "types.String"},
		{"nullable tinytext", "tinytext", column{nullable: null}, "types.NullString"},
		{"text", "text", column{nullable: notNull}, "types.String"},
		{"nullable text", "text", column{nullable: null}, "types.NullString"},
		{"mediumtext", "mediumtext", column{nullable: notNull}, "types.String"},
		{"nullable mediumtext", "mediumtext", column{nullable: null}, "types.NullString"},
		{"longtext", "longtext", column{nullable: notNull}, "types.String"},
		{"nullable longtext", "longtext", column{nullable: null}, "types.NullString"},
		{"set", "set", column{nullable: notNull}, "types.String"},
		{"nullable set", "set", column{nullable: null}, "types.NullString"},

		{"enum('0','1') is a legacy bool", "enum", column{sql: "enum('0','1')"}, "types.LegacyBoolEnum"},
		{"enum('1','0') is not (the order matters)", "enum", column{sql: "enum('1','0')"}, "types.Enum"},
		{"two-value enum of words", "enum", column{sql: "enum('yes','no')"}, "types.Enum"},
		{"enum of three values", "enum", column{sql: "enum('0','1','2')"}, "types.Enum"},
		{"single-value enum", "enum", column{sql: "enum('a')"}, "types.Enum"},
		{"enum with an unknown full type", "enum", column{}, "types.String"},

		{"binary", "binary", column{}, "[]byte"},
		{"varbinary", "varbinary", column{nullable: null}, "[]byte"},
		{"tinyblob", "tinyblob", column{}, "[]byte"},
		{"blob", "blob", column{nullable: notNull}, "[]byte"},
		{"mediumblob", "mediumblob", column{}, "[]byte"},
		{"longblob", "longblob", column{}, "[]byte"},
	}
}

func TestColumnTypes(t *testing.T) {
	m := types.ColumnTypes()
	covered := map[string]bool{}

	for _, tc := range columnCases() {
		covered[tc.dbType] = true

		t.Run(tc.name, func(t *testing.T) {
			fn, ok := m[tc.dbType]
			require.True(t, ok, "no mapping for %s", tc.dbType)
			assert.Equal(t, tc.want, fn(tc.col))
		})
	}

	for dbType := range m {
		assert.True(t, covered[dbType], "the table has no case for %s", dbType)
	}
}

// Every type the map names exists in this package, so a renamed type cannot
// leave the generator emitting code that does not compile.
func TestColumnTypesNameRealTypes(t *testing.T) {
	declared := map[string]bool{"[]byte": true}

	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	require.NoError(t, err)

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.TYPE {
					continue
				}

				for _, spec := range gen.Specs {
					declared["types."+spec.(*ast.TypeSpec).Name.Name] = true
				}
			}
		}
	}

	m := types.ColumnTypes()

	for _, tc := range columnCases() {
		got := m[tc.dbType](tc.col)
		assert.True(t, declared[got], "%s maps to %q, which database/types does not declare", tc.name, got)
	}
}
