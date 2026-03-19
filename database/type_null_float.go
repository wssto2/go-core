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
 * NullFloat
 */
type NullFloat struct {
	value *float64 `json:"-"`
}

func NewNullFloat(value float64) NullFloat {
	return NullFloat{
		value: &value,
	}
}

func (f *NullFloat) Get() float64 {
	if f.value == nil {
		panic("NullFloat.Get() called on null value — use GetOrZero() or check IsNull() first")
	}

	return *f.value
}

func (f *NullFloat) GetOrZero() float64 {
	if f.value == nil {
		return 0.0
	}
	return *f.value
}

func (f *NullFloat) Set(value float64) {
	f.value = &value
}

func (f *NullFloat) SetNull() {
	f.value = nil
}

func (f NullFloat) Value() (driver.Value, error) {
	if f.value == nil {
		return nil, nil
	}
	return *f.value, nil
}

func (f NullFloat) GormDataType() string {
	return MySQLFloat
}

func (f NullFloat) GormDBDataType(db *gorm.DB, field *schema.Field) string {
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

func (f NullFloat) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if f.value == nil {
		return clause.Expr{
			SQL:  "NULL",
			Vars: nil,
		}
	}

	return clause.Expr{
		SQL:  "?",
		Vars: []interface{}{*f.value},
	}
}

func (f *NullFloat) Scan(value interface{}) error {
	if value == nil {
		f.value = nil
		return nil
	}

	switch v := value.(type) {
	case float64:
		f.value = &v
	case float32:
		val := float64(v)
		f.value = &val
	case int64:
		val := float64(v)
		f.value = &val
	case []byte:
		val, err := strconv.ParseFloat(string(v), 64)
		if err != nil {
			return fmt.Errorf("failed to convert byte slice to float64: %w", err)
		}
		f.value = &val
	case string:
		val, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("failed to convert string to float64: %w", err)
		}
		f.value = &val
	default:
		return fmt.Errorf("unsupported type for NullFloat: %T", value)
	}

	return nil
}

func (f NullFloat) MarshalJSON() ([]byte, error) {
	if f.value == nil {
		return []byte(Null), nil
	}

	data, err := json.Marshal(*f.value)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return data, nil
}

func (f *NullFloat) UnmarshalJSON(data []byte) error {
	if string(data) == Null {
		f.value = nil
		return nil
	}

	var value float64
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	f.value = &value
	return nil
}

func (f *NullFloat) String() string {
	if f.value == nil {
		return ""
	}
	return strconv.FormatFloat(*f.value, 'f', -1, 64)
}

func (f *NullFloat) IsNull() bool {
	return f.value == nil
}

func (f *NullFloat) IsNotNull() bool {
	return f.value != nil
}

func (f *NullFloat) IsZero() bool {
	return f.value != nil && *f.value == 0
}

func (f *NullFloat) IsNotZero() bool {
	return f.value != nil && *f.value != 0
}

func (f *NullFloat) IsEqual(value float64) bool {
	return f.value != nil && *f.value == value
}

func (f *NullFloat) IsNotEqual(value float64) bool {
	return f.value == nil || *f.value != value
}

func (f *NullFloat) IsGreaterThan(value float64) bool {
	return f.value != nil && *f.value > value
}

func (f *NullFloat) IsLessThan(value float64) bool {
	return f.value != nil && *f.value < value
}

func (f *NullFloat) IsGreaterThanOrEqual(value float64) bool {
	return f.value != nil && *f.value >= value
}

func (f *NullFloat) IsLessThanOrEqual(value float64) bool {
	return f.value != nil && *f.value <= value
}

func (f *NullFloat) IsIn(values ...float64) bool {
	if f.value == nil {
		return false
	}

	for _, v := range values {
		if *f.value == v {
			return true
		}
	}

	return false
}

func (f *NullFloat) IsNotIn(values ...float64) bool {
	if f.value == nil {
		return true
	}

	for _, v := range values {
		if *f.value == v {
			return false
		}
	}

	return true
}

func (f *NullFloat) IsBetween(min, max float64) bool {
	return f.value != nil && *f.value >= min && *f.value <= max
}

func (f *NullFloat) IsNotBetween(min, max float64) bool {
	return f.value == nil || *f.value < min || *f.value > max
}

func (f *NullFloat) GetInt() int {
	if f.value == nil {
		return 0
	}
	return int(*f.value)
}
