package types

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strconv"

	"github.com/goccy/go-json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// String wraps string.
type String struct {
	value string
}

func NewString(value string) String {
	return String{value: value}
}

func (s String) Value() (driver.Value, error) {
	return s.value, nil
}

func (s *String) Scan(value interface{}) error {
	switch v := value.(type) {
	case string:
		s.value = v
	case []byte:
		s.value = string(v)
	case int64:
		s.value = strconv.FormatInt(v, 10)
	case float64:
		s.value = strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Errorf("unsupported type for String: %T", value)
	}
	return nil
}

func (s String) GormDataType() string {
	return MysqlStringType
}

func (s String) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	if db.Name() == Sqlite {
		return SqliteStringType
	}
	if t := field.TagSettings["TYPE"]; t != "" {
		return t
	}
	return MysqlStringType
}

func (s String) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	return clause.Expr{SQL: "?", Vars: []interface{}{s.value}}
}

func (s String) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.value)
}

func (s *String) UnmarshalJSON(data []byte) error {
	if string(data) == Null {
		s.value = ""
		return nil
	}
	return json.Unmarshal(data, &s.value)
}

func (s String) Get() string {
	return s.value
}

func (s *String) Set(v string) {
	s.value = v
}

func (s String) String() string {
	return s.value
}
