//nolint:exhaustruct,exhaustruct_v5,paralleltest,varnamelen,lll // table tests in the style of the package's others
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

const (
	tInt, tNullInt           = "types.Int", "types.NullInt"
	tFloat, tNullFloat       = "types.Float", "types.NullFloat"
	tBool, tNullBool         = "types.Bool", "types.NullBool"
	tString, tNullString     = "types.String", "types.NullString"
	tDate, tNullDate         = "types.Date", "types.NullDate"
	tDateTime, tNullDateTime = "types.DateTime", "types.NullDateTime"
	tZeroDate, tZeroDateTime = "types.ZeroDate", "types.ZeroDateTime"
	tEnum, tLegacy           = "types.Enum", "types.LegacyBoolEnum"
	tBytes                   = "[]byte"

	dbTinyint, dbDate, dbEnum = "tinyint", "date", "enum"
	tinyint1                  = "tinyint(1)"

	zeroDate     = "0000-00-00"
	zeroDateTime = "0000-00-00 00:00:00"
)

// driverColumn is named so that the embedded interface does not collide with
// the ColumnType method below.
type driverColumn = gorm.ColumnType

// column is a gorm.ColumnType with only the three answers the type map reads;
// the embedded nil interface panics on any other call, so the map cannot grow
// a dependency on one unnoticed.
type column struct {
	driverColumn

	sql        string // "tinyint(1)", "enum('0','1')"; "" = the driver does not say
	nullable   *bool  // nil = the driver does not say
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

// columnCase is one answer the map must give: the column of database type dbType and want.
type columnCase struct {
	name   string
	dbType string
	col    column
	want   string
}

// plainOrNull are the cases of a type that is nullable-or-not and nothing else: NOT NULL,
// nullable, and a driver that does not report nullability (taken as NOT NULL).
func plainOrNull(dbType, plain, null string) []columnCase {
	return []columnCase{
		{dbType + " NOT NULL", dbType, column{nullable: new(false)}, plain},
		{dbType + " nullable", dbType, column{nullable: new(true)}, null},
		{dbType + " nullability unknown", dbType, column{}, plain},
	}
}

func columnCases() []columnCase {
	var cases []columnCase

	for _, dbType := range []string{"smallint", "mediumint", "int", "bigint"} {
		cases = append(cases, plainOrNull(dbType, tInt, tNullInt)...)
	}

	for _, dbType := range []string{"float", "double", "decimal", "numeric", "real"} {
		cases = append(cases, plainOrNull(dbType, tFloat, tNullFloat)...)
	}

	for _, dbType := range []string{"char", "varchar", "tinytext", "text", "mediumtext", "longtext", "set"} {
		cases = append(cases, plainOrNull(dbType, tString, tNullString)...)
	}

	for _, dbType := range []string{"binary", "varbinary", "tinyblob", "blob", "mediumblob", "longblob"} {
		cases = append(cases,
			columnCase{dbType + " NOT NULL", dbType, column{nullable: new(false)}, tBytes},
			columnCase{dbType + " nullable", dbType, column{nullable: new(true)}, tBytes},
		)
	}

	cases = append(cases, columnCase{"boolean ignores nullability", "boolean", column{nullable: new(true)}, tBool})

	cases = append(cases, tinyintCases()...)
	cases = append(cases, dateCases()...)

	return append(cases, enumCases()...)
}

func tinyintCases() []columnCase {
	return []columnCase{
		{"tinyint(1) is a bool", dbTinyint, column{sql: tinyint1, nullable: new(false)}, tBool},
		{"nullable tinyint(1)", dbTinyint, column{sql: tinyint1, nullable: new(true)}, tNullBool},
		{"tinyint(1) of unknown nullability", dbTinyint, column{sql: tinyint1}, tBool},
		{"tinyint(1) unsigned is a bool", dbTinyint, column{sql: "tinyint(1) unsigned", nullable: new(false)}, tBool},
		{"tinyint(4) is an int", dbTinyint, column{sql: "tinyint(4)", nullable: new(false)}, tInt},
		{"nullable tinyint(4)", dbTinyint, column{sql: "tinyint(4)", nullable: new(true)}, tNullInt},
		{"tinyint of unknown width", dbTinyint, column{nullable: new(false)}, tInt},
	}
}

func dateCases() []columnCase {
	var cases []columnCase

	for _, dbType := range []string{"datetime", "timestamp"} {
		cases = append(cases,
			columnCase{dbType + " NOT NULL", dbType, column{nullable: new(false)}, tDateTime},
			columnCase{dbType + " nullable", dbType, column{nullable: new(true)}, tNullDateTime},
			columnCase{dbType + " NOT NULL defaulting to the zero datetime", dbType, column{nullable: new(false), defaultVal: new(zeroDateTime)}, tZeroDateTime},
			columnCase{dbType + " nullable ignores the zero default", dbType, column{nullable: new(true), defaultVal: new(zeroDateTime)}, tNullDateTime},
			columnCase{dbType + " with the zero date only", dbType, column{nullable: new(false), defaultVal: new(zeroDate)}, tDateTime},
			columnCase{dbType + " defaulting to now", dbType, column{nullable: new(false), defaultVal: new("CURRENT_TIMESTAMP")}, tDateTime},
		)
	}

	return append(cases,
		columnCase{"date NOT NULL", dbDate, column{nullable: new(false)}, tDate},
		columnCase{"date nullable", dbDate, column{nullable: new(true)}, tNullDate},
		columnCase{"date NOT NULL defaulting to the zero date", dbDate, column{nullable: new(false), defaultVal: new(zeroDate)}, tZeroDate},
		columnCase{"date of unknown nullability defaulting to the zero date", dbDate, column{defaultVal: new(zeroDate)}, tZeroDate},
		columnCase{"date nullable ignores the zero default", dbDate, column{nullable: new(true), defaultVal: new(zeroDate)}, tNullDate},
		columnCase{"date with another default", dbDate, column{nullable: new(false), defaultVal: new("2000-01-01")}, tDate},
		columnCase{"date with the zero datetime", dbDate, column{nullable: new(false), defaultVal: new(zeroDateTime)}, tDate},
	)
}

func enumCases() []columnCase {
	return []columnCase{
		{"enum('0','1') is a legacy bool", dbEnum, column{sql: "enum('0','1')"}, tLegacy},
		{"enum('1','0') is not: the order matters", dbEnum, column{sql: "enum('1','0')"}, tEnum},
		{"two-value enum of words", dbEnum, column{sql: "enum('yes','no')"}, tEnum},
		{"enum of three values", dbEnum, column{sql: "enum('0','1','2')"}, tEnum},
		{"single-value enum", dbEnum, column{sql: "enum('a')"}, tEnum},
		{"enum the driver does not describe", dbEnum, column{}, tString},
	}
}

func TestColumnTypes(t *testing.T) {
	columnTypes := types.ColumnTypes()
	covered := map[string]bool{}

	for _, tc := range columnCases() {
		covered[tc.dbType] = true

		t.Run(tc.name, func(t *testing.T) {
			mapper, ok := columnTypes[tc.dbType]
			require.True(t, ok, "no mapping for %s", tc.dbType)
			assert.Equal(t, tc.want, mapper(tc.col))
		})
	}

	for dbType := range columnTypes {
		assert.True(t, covered[dbType], "no case for %s", dbType)
	}
}

// Every type the map names exists in this package, so a renamed type cannot
// leave the generator emitting code that does not compile.
func TestColumnTypesNameRealTypes(t *testing.T) {
	declared := map[string]bool{tBytes: true}

	//nolint:staticcheck // the package is parsed whole, build tags do not matter for its type names
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
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
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						declared["types."+typeSpec.Name.Name] = true
					}
				}
			}
		}
	}

	columnTypes := types.ColumnTypes()

	for _, tc := range columnCases() {
		got := columnTypes[tc.dbType](tc.col)
		assert.True(t, declared[got], "%s maps to %q, which database/types does not declare", tc.name, got)
	}
}
