package types

import (
	"reflect"
	"strings"

	"gorm.io/gorm"
)

// typeName is how generated code spells T from outside this package.
func typeName[T any]() string { return "types." + reflect.TypeFor[T]().Name() }

// ColumnTypes maps a database column type to the go-core type a generated
// entity should use for it, keyed by the lower-case type name the driver
// reports ("varchar", "tinyint", "enum"). It is the argument of gorm.io/gen's
// Generator.WithDataTypeMap, which go-core does not import:
//
//	generator.WithDataTypeMap(types.ColumnTypes())
//
// The values are the names as the generated file spells them, "types.Int",
// "types.NullString", so it imports this package as types. Binary columns map
// to "[]byte". The rules:
//
//   - A nullable column gets the Null* type, a NOT NULL one the plain type.
//   - tinyint(1) is a bool; every other tinyint, smallint, mediumint, int and
//     bigint is an Int. float, double, decimal, numeric and real are Floats.
//     boolean is a Bool.
//   - A NOT NULL date or datetime/timestamp whose default is the MySQL 5 zero
//     ("0000-00-00", "0000-00-00 00:00:00") is a ZeroDate or ZeroDateTime;
//     otherwise Date and DateTime (a timestamp is a DateTime).
//   - enum('0','1') is a LegacyBoolEnum; any other enum an Enum. set and the
//     text types are Strings.
//
// The returned map is new on every call, so the caller may add its own types.
func ColumnTypes() map[string]func(gorm.ColumnType) string {
	nullable := func(plain, null string) func(gorm.ColumnType) string {
		return func(c gorm.ColumnType) string {
			if n, ok := c.Nullable(); ok && n {
				return null
			}

			return plain
		}
	}

	// zeroDefault picks the Zero type for a NOT NULL column whose default is
	// the MySQL 5 zero value, which stands for "no value yet".
	zeroDefault := func(plain, null, zero, zeroValue string) func(gorm.ColumnType) string {
		return func(c gorm.ColumnType) string {
			if n, ok := c.Nullable(); ok && n {
				return null
			}

			if def, ok := c.DefaultValue(); ok && def == zeroValue {
				return zero
			}

			return plain
		}
	}

	bytes := func(gorm.ColumnType) string { return "[]byte" }
	integer := nullable(typeName[Int](), typeName[NullInt]())
	float := nullable(typeName[Float](), typeName[NullFloat]())
	str := nullable(typeName[String](), typeName[NullString]())
	datetime := zeroDefault(typeName[DateTime](), typeName[NullDateTime](), typeName[ZeroDateTime](), "0000-00-00 00:00:00")

	return map[string]func(gorm.ColumnType) string{
		// tinyint(1) is MySQL's canonical boolean; wider tinyints are integers.
		"tinyint": func(c gorm.ColumnType) string {
			if ct, ok := c.ColumnType(); ok && strings.Contains(ct, "(1)") {
				return nullable(typeName[Bool](), typeName[NullBool]())(c)
			}

			return integer(c)
		},
		"smallint":  integer,
		"mediumint": integer,
		"int":       integer,
		"bigint":    integer,

		"float":   float,
		"double":  float,
		"decimal": float,
		"numeric": float,
		"real":    float,

		"boolean": func(gorm.ColumnType) string { return typeName[Bool]() },

		"date":      zeroDefault(typeName[Date](), typeName[NullDate](), typeName[ZeroDate](), "0000-00-00"),
		"datetime":  datetime,
		"timestamp": datetime,

		"char":       str,
		"varchar":    str,
		"tinytext":   str,
		"text":       str,
		"mediumtext": str,
		"longtext":   str,
		"set":        str,

		// enum('0','1') is the legacy boolean; a column type the driver does
		// not report, or any other enum, is read as the plain kind.
		"enum": func(c gorm.ColumnType) string {
			ct, ok := c.ColumnType()
			if !ok {
				return typeName[String]()
			}

			values := strings.Split(ct, ",")
			if len(values) == 2 && strings.Contains(values[0], "0") && strings.Contains(values[1], "1") {
				return typeName[LegacyBoolEnum]()
			}

			return typeName[Enum]()
		},

		"binary":     bytes,
		"varbinary":  bytes,
		"tinyblob":   bytes,
		"blob":       bytes,
		"mediumblob": bytes,
		"longblob":   bytes,
	}
}
