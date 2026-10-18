package types

import (
	"reflect"
	"strings"

	"gorm.io/gorm"
)

const enumColumn = "enum"

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
	integer := nullable(typeName[Int](), typeName[NullInt]())
	float := nullable(typeName[Float](), typeName[NullFloat]())
	text := nullable(typeName[String](), typeName[NullString]())
	datetime := zeroDefault(
		typeName[DateTime](), typeName[NullDateTime](), typeName[ZeroDateTime](), "0000-00-00 00:00:00")
	bytes := func(gorm.ColumnType) string { return "[]byte" }

	return map[string]func(gorm.ColumnType) string{
		"tinyint":   tinyint,
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

		"char":       text,
		"varchar":    text,
		"tinytext":   text,
		"text":       text,
		"mediumtext": text,
		"longtext":   text,
		"set":        text,

		enumColumn: enum,

		"binary":     bytes,
		"varbinary":  bytes,
		"tinyblob":   bytes,
		"blob":       bytes,
		"mediumblob": bytes,
		"longblob":   bytes,
	}
}

// nullable picks null for a nullable column and plain for any other, one whose
// nullability the driver does not report included.
func nullable(plain, null string) func(gorm.ColumnType) string {
	return func(col gorm.ColumnType) string {
		if isNull, ok := col.Nullable(); ok && isNull {
			return null
		}

		return plain
	}
}

// zeroDefault is nullable, except that a NOT NULL column whose default is the
// MySQL 5 zero value zeroValue, which stands for "no value yet", gets zero.
func zeroDefault(plain, null, zero, zeroValue string) func(gorm.ColumnType) string {
	return func(col gorm.ColumnType) string {
		if isNull, ok := col.Nullable(); ok && isNull {
			return null
		}

		if def, ok := col.DefaultValue(); ok && def == zeroValue {
			return zero
		}

		return plain
	}
}

// tinyint is MySQL's canonical boolean when it is tinyint(1); a wider tinyint
// is an integer.
func tinyint(col gorm.ColumnType) string {
	if full, ok := col.ColumnType(); ok && strings.Contains(full, "(1)") {
		return nullable(typeName[Bool](), typeName[NullBool]())(col)
	}

	return nullable(typeName[Int](), typeName[NullInt]())(col)
}

// enum is the legacy boolean for enum('0','1'), a column type the driver does
// not report is read as a String, and any other enum is an Enum.
func enum(col gorm.ColumnType) string {
	full, ok := col.ColumnType()
	if !ok {
		return typeName[String]()
	}

	values := strings.Split(full, ",")
	if len(values) == 2 && strings.Contains(values[0], "0") && strings.Contains(values[1], "1") {
		return typeName[LegacyBoolEnum]()
	}

	return typeName[Enum]()
}
