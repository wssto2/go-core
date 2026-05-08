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

// NullString wraps *string for nullable string columns.
// nil serialises as SQL NULL and JSON null.
// Empty string is treated as NULL — both on read and write.
// Use String for NOT NULL string columns.
// Use *string directly if you need to distinguish "" from NULL.
type NullString struct {
	value *string
}

// NewNullString creates a NullString. Empty string is stored as nil (NULL).
func NewNullString(value string) NullString {
	if value == "" {
		return NullString{}
	}
	return NullString{value: &value}
}

// NewNullStringPtr creates a NullString from a *string. Nil pointer → SQL NULL.
func NewNullStringPtr(value *string) NullString {
	if value == nil || *value == "" {
		return NullString{}
	}
	return NullString{value: value}
}

func (s NullString) Value() (driver.Value, error) {
	if s.value == nil {
		return nil, nil
	}
	return *s.value, nil
}

func (s *NullString) Scan(value interface{}) error {
	switch v := value.(type) {
	case string:
		if v == "" {
			s.value = nil
		} else {
			s.value = &v
		}
	case []byte:
		str := string(v)
		if str == "" {
			s.value = nil
		} else {
			s.value = &str
		}
	case int64:
		str := strconv.FormatInt(v, 10)
		s.value = &str
	case float64:
		str := strconv.FormatFloat(v, 'f', -1, 64)
		s.value = &str
	case nil:
		s.value = nil
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
	if s.value == nil {
		return clause.Expr{SQL: "NULL"}
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{*s.value}}
}

func (s NullString) MarshalJSON() ([]byte, error) {
	if s.value == nil {
		return []byte(database.Null), nil
	}
	return json.Marshal(*s.value)
}

func (s *NullString) UnmarshalJSON(data []byte) error {
	if string(data) == database.Null {
		s.value = nil
		return nil
	}
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v == "" {
		s.value = nil
	} else {
		s.value = &v
	}
	return nil
}

// Get returns the *string pointer. Nil means the DB value was NULL or empty.
func (s NullString) Get() *string {
	return s.value
}

// GetOrEmpty returns the string value, or "" if NULL.
func (s NullString) GetOrEmpty() string {
	if s.value == nil {
		return ""
	}
	return *s.value
}

// IsNull reports whether the value is NULL.
func (s NullString) IsNull() bool {
	return s.value == nil
}

func (s *NullString) Set(v string) {
	if v == "" {
		s.value = nil
	} else {
		s.value = &v
	}
}

func (s NullString) String() string {
	return s.GetOrEmpty()
}
