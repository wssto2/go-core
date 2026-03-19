package database

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

/**
 * Float
 */
type Float struct {
	value float64 `json:"-"`
}

func NewFloat(value float64) Float {
	return Float{
		value: value,
	}
}

func (f *Float) Get() float64 {
	return f.value
}

func (f *Float) Set(value float64) {
	f.value = value
}

func (f Float) Value() (driver.Value, error) {
	return f.value, nil
}

func (f Float) GormDataType() string {
	return MySQLFloat
}

func (f Float) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Name() {
	case DriverSQLite:
		return SQLiteFloat

	case DriverMySQL:
		if field.TagSettings["TYPE"] == "" {
			return "decimal(10,2)"
		}

		return field.TagSettings["TYPE"]
	}

	return field.TagSettings["TYPE"]
}

func (f Float) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	return clause.Expr{
		SQL:  "?",
		Vars: []interface{}{f.value},
	}
}

func (f *Float) Scan(value interface{}) error {
	switch v := value.(type) {
	case float64:
		f.value = v

	case float32:
		f.value = float64(v)

	case int64:
		f.value = float64(v)

	case []byte:
		val, err := strconv.ParseFloat(string(v), 64)
		if err != nil {
			return fmt.Errorf("failed to convert byte slice to float64: %w", err)
		}
		f.value = val

	case string:
		val, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("failed to convert string to float64: %w", err)
		}
		f.value = val

	default:
		return fmt.Errorf("unsupported type for Float: %T", value)
	}

	return nil
}

func (f Float) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.value)
}

func (f *Float) UnmarshalJSON(data []byte) error {
	if string(data) == Null {
		f.value = 0.0
		return nil
	}

	var value float64
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	f.value = value
	return nil
}

func (f *Float) String() string {
	return strconv.FormatFloat(f.value, 'f', -1, 64)
}

func (f *Float) IsZero() bool {
	return f.value == 0
}

func (f *Float) IsNotZero() bool {
	return f.value != 0
}

func (f *Float) IsEqual(value float64) bool {
	return f.value == value
}

func (f *Float) IsNotEqual(value float64) bool {
	return f.value != value
}

func (f *Float) IsGreaterThan(value float64) bool {
	return f.value > value
}

func (f *Float) IsLessThan(value float64) bool {
	return f.value < value
}

func (f *Float) IsGreaterThanOrEqual(value float64) bool {
	return f.value >= value
}

func (f *Float) IsLessThanOrEqual(value float64) bool {
	return f.value <= value
}

func (f *Float) IsIn(values ...float64) bool {
	for _, v := range values {
		if f.value == v {
			return true
		}
	}

	return false
}

func (f *Float) IsNotIn(values ...float64) bool {
	for _, v := range values {
		if f.value == v {
			return false
		}
	}

	return true
}

func (f *Float) IsBetween(min, max float64) bool {
	return f.value >= min && f.value <= max
}

func (f *Float) IsNotBetween(min, max float64) bool {
	return f.value < min || f.value > max
}
