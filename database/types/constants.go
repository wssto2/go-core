package types

type I18n map[string]string

const (
	Null               = "null"
	Sqlite             = "sqlite"
	Mysql              = "mysql"
	SqliteIntType      = "integer"
	MysqlIntType       = "int"
	SqliteStringType   = "text"
	MysqlStringType    = "varchar"
	SqliteFloatType    = "decimal(10,2)"
	MysqlFloatType     = "decimal(10,2) unsigned"
	SqliteDateType     = "date"
	MysqlDateType      = "date"
	SqliteDateTimeType = "datetime"
	MysqlDateTimeType  = "datetime"
	SqliteBoolType     = "boolean"
	MysqlBoolType      = "tinyint(1) unsigned"
	SqliteJSONType     = "json"
	MysqlJSONType      = "json"
)
