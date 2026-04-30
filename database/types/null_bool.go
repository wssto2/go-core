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

// NullBool wraps *bool for nullable boolean columns (tinyint(1) NULL).
// nil serialises as SQL NULL and JSON null.
// Use Bool for NOT NULL boolean columns.
type NullBool struct {
	value *bool
}

func NewNullBool(value bool) NullBool {
	return NullBool{value: &value}
}

// NewNullBoolPtr creates a NullBool from a *bool. Nil pointer → SQL NULL.
func NewNullBoolPtr(value *bool) NullBool {
	return NullBool{value: value}
}

func (b NullBool) Value() (driver.Value, error) {
	if b.value == nil {
		return nil, nil
	}
	return *b.value, nil
}

func (b *NullBool) Scan(value interface{}) error {
	if value == nil {
		b.value = nil
		return nil
	}
	var boolVal bool
	switch v := value.(type) {
	case bool:
		boolVal = v
	case int64:
		boolVal = v != 0
	case []byte:
		val, err := strconv.ParseBool(string(v))
		if err != nil {
			intVal, intErr := strconv.ParseInt(string(v), 10, 64)
			if intErr != nil {
				return fmt.Errorf("failed to convert byte slice to NullBool: %w", err)
			}
			boolVal = intVal != 0
		} else {
			boolVal = val
		}
	case string:
		val, err := strconv.ParseBool(v)
		if err != nil {
			intVal, intErr := strconv.ParseInt(v, 10, 64)
			if intErr != nil {
				return fmt.Errorf("failed to convert string to NullBool: %w", err)
			}
			boolVal = intVal != 0
		} else {
			boolVal = val
		}
	default:
		return fmt.Errorf("unsupported type for NullBool: %T", value)
	}
	b.value = &boolVal
	return nil
}

func (b NullBool) GormDataType() string {
	return database.MySQLBool
}

func (b NullBool) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Name() {
	case database.DriverSQLite:
		return database.SQLiteBool
	case database.DriverMySQL:
		if t := field.TagSettings["TYPE"]; t != "" {
			return t
		}
		return database.MySQLBool
	}
	return field.TagSettings["TYPE"]
}

func (b NullBool) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if b.value == nil {
		return clause.Expr{SQL: "NULL"}
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{*b.value}}
}

func (b NullBool) MarshalJSON() ([]byte, error) {
	if b.value == nil {
		return []byte(database.Null), nil
	}
	return json.Marshal(*b.value)
}

func (b *NullBool) UnmarshalJSON(data []byte) error {
	if string(data) == database.Null {
		b.value = nil
		return nil
	}
	var v bool
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	b.value = &v
	return nil
}

// Get returns the *bool pointer. Nil means the DB value was NULL.
func (b NullBool) Get() *bool {
	return b.value
}

// Set sets the value.
func (b *NullBool) Set(value bool) {
	b.value = &value
}

// IsNull reports whether the value is NULL.
func (b NullBool) IsNull() bool {
	return b.value == nil
}
