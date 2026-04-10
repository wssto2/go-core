package types

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strconv"

	"github.com/goccy/go-json"
	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// NullString stores a string that is written to the DB as NULL when empty.
// JSON null and JSON "" both deserialise to the empty string.
// The empty string serialises back to JSON null.
// Use this for optional text columns where empty and absent are equivalent.
// If you need to distinguish "" from NULL, use a *string field instead.
type NullString struct {
	value string
}

func NewNullString(value string) NullString {
	return NullString{value: value}
}

func (s NullString) Value() (driver.Value, error) {
	return s.value, nil
}

func (s *NullString) Scan(value interface{}) error {
	switch v := value.(type) {
	case string:
		s.value = v
	case []byte:
		s.value = string(v)
	case int64:
		s.value = strconv.FormatInt(v, 10)
	case float64:
		s.value = strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		s.value = "" // Treat DB NULL as empty string
	default:
		return fmt.Errorf("unsupported type for NullString: %T", value)
	}
	return nil
}

func (s NullString) GormDataType() string {
	return database.MySQLString
}

func (s NullString) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	if db.Name() == database.DriverSQLite {
		return database.SQLiteString
	}
	if t := field.TagSettings["TYPE"]; t != "" {
		return t
	}
	size := field.Size
	if size <= 0 {
		size = 255
	}
	return fmt.Sprintf("varchar(%d)", size)
}

func (s NullString) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if s.value == "" {
		return clause.Expr{SQL: "NULL"}
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{s.value}}
}

func (s NullString) MarshalJSON() ([]byte, error) {
	if s.value == "" {
		return []byte(database.Null), nil
	}
	return json.Marshal(s.value)
}

func (s *NullString) UnmarshalJSON(data []byte) error {
	if string(data) == database.Null {
		s.value = ""
		return nil
	}
	return json.Unmarshal(data, &s.value)
}

func (s NullString) Get() string {
	return s.value
}

func (s *NullString) Set(v string) {
	s.value = v
}

func (s NullString) String() string {
	return s.value
}
