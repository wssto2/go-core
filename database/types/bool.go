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

// Bool wraps a boolean value for GORM, handling various DB representations (0/1, true/false).
type Bool struct {
	value bool
}

func NewBool(value bool) Bool {
	return Bool{value: value}
}

func (b Bool) Value() (driver.Value, error) {
	return b.value, nil
}

func (b *Bool) Scan(value interface{}) error {
	switch v := value.(type) {
	case bool:
		b.value = v
	case int64:
		b.value = v != 0
	case []byte:
		val, err := strconv.ParseBool(string(v))
		if err != nil {
			intVal, intErr := strconv.ParseInt(string(v), 10, 64)
			if intErr != nil {
				return fmt.Errorf("failed to convert byte slice to bool: %w", err)
			}
			b.value = intVal != 0
		} else {
			b.value = val
		}
	case string:
		val, err := strconv.ParseBool(v)
		if err != nil {
			intVal, intErr := strconv.ParseInt(v, 10, 64)
			if intErr != nil {
				return fmt.Errorf("failed to convert string to bool: %w", err)
			}
			b.value = intVal != 0
		} else {
			b.value = val
		}
	default:
		return fmt.Errorf("unsupported type for Bool: %T", value)
	}
	return nil
}

func (b Bool) GormDataType() string {
	return MysqlBoolType
}

func (b Bool) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Name() {
	case Sqlite:
		return SqliteBoolType
	case Mysql:
		if t := field.TagSettings["TYPE"]; t != "" {
			return t
		}
		return MysqlBoolType
	}
	return field.TagSettings["TYPE"]
}

func (b Bool) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	return clause.Expr{
		SQL:  "?",
		Vars: []interface{}{b.value},
	}
}

func (b Bool) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.value)
}

func (b *Bool) UnmarshalJSON(data []byte) error {
	if string(data) == Null {
		b.value = false
		return nil
	}
	return json.Unmarshal(data, &b.value)
}

// Get returns the boolean value.
func (b Bool) Get() bool {
	return b.value
}

// Set sets the boolean value.
func (b *Bool) Set(value bool) {
	b.value = value
}

func (b Bool) String() string {
	return strconv.FormatBool(b.value)
}

func (b Bool) IsTrue() bool {
	return b.value
}

func (b Bool) IsFalse() bool {
	return !b.value
}
