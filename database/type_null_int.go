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
 * NullInt
 */
type NullInt struct {
	value *int `json:"-"`
}

func NewNullInt(value int) NullInt {
	return NullInt{
		value: &value,
	}
}

func (i *NullInt) Get() int {
	if i.value == nil {
		panic("NullInt.Get() called on null value — use GetOrZero() or check IsNull() first")
	}
	return *i.value
}

func (i *NullInt) GetOrZero() int {
	if i.value == nil {
		return 0
	}
	return *i.value
}

func (i *NullInt) Set(value int) {
	i.value = &value
}

func (i *NullInt) SetNull() {
	i.value = nil
}

func (i NullInt) Value() (driver.Value, error) {
	if i.value == nil {
		return nil, nil
	}
	return *i.value, nil
}

func (i NullInt) GormDataType() string {
	return MySQLInt
}

func (i NullInt) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Name() {
	case DriverSQLite:
		return SQLiteInt

	case DriverMySQL:
		if field.TagSettings["TYPE"] == "" {
			return MySQLInt
		}

		return field.TagSettings["TYPE"]
	}

	return field.TagSettings["TYPE"]
}

func (i NullInt) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if i.value == nil {
		return clause.Expr{
			SQL:  "NULL",
			Vars: nil,
		}
	}

	return clause.Expr{
		SQL:  "?",
		Vars: []interface{}{*i.value},
	}
}

func (i *NullInt) Scan(value interface{}) error {
	if value == nil {
		i.value = nil
		return nil
	}

	switch v := value.(type) {
	case int64:
		val := int(v)
		i.value = &val
	case float64:
		val := int(v)
		i.value = &val
	case []byte:
		val, err := strconv.Atoi(string(v))
		if err != nil {
			return fmt.Errorf("failed to convert byte slice to int: %w", err)
		}
		i.value = &val
	case string:
		val, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("failed to convert string to int: %w", err)
		}
		i.value = &val
	default:
		return fmt.Errorf("unsupported type for NullInt: %T", value)
	}

	return nil
}

func (i NullInt) MarshalJSON() ([]byte, error) {
	if i.value == nil {
		return []byte(Null), nil
	}

	data, err := json.Marshal(*i.value)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return data, nil
}

func (i *NullInt) UnmarshalJSON(data []byte) error {
	if string(data) == Null {
		i.value = nil
		return nil
	}

	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	i.value = &value
	return nil
}

func (i *NullInt) String() string {
	if i.value == nil {
		return ""
	}
	return strconv.Itoa(*i.value)
}

func (i *NullInt) IsNull() bool {
	return i.value == nil
}

func (i *NullInt) IsNotNull() bool {
	return i.value != nil
}

func (i *NullInt) IsZero() bool {
	return i.value != nil && *i.value == 0
}

func (i *NullInt) IsNotZero() bool {
	return i.value != nil && *i.value != 0
}

func (i *NullInt) IsEqual(value int) bool {
	return i.value != nil && *i.value == value
}

func (i *NullInt) IsNotEqual(value int) bool {
	return i.value == nil || *i.value != value
}

func (i *NullInt) IsGreaterThan(value int) bool {
	return i.value != nil && *i.value > value
}

func (i *NullInt) IsLessThan(value int) bool {
	return i.value != nil && *i.value < value
}

func (i *NullInt) IsGreaterThanOrEqual(value int) bool {
	return i.value != nil && *i.value >= value
}

func (i *NullInt) IsLessThanOrEqual(value int) bool {
	return i.value != nil && *i.value <= value
}

func (i *NullInt) IsIn(values ...int) bool {
	if i.value == nil {
		return false
	}

	for _, v := range values {
		if *i.value == v {
			return true
		}
	}

	return false
}

func (i *NullInt) IsNotIn(values ...int) bool {
	if i.value == nil {
		return true
	}

	for _, v := range values {
		if *i.value == v {
			return false
		}
	}

	return true
}

func (i *NullInt) IsBetween(min, max int) bool {
	return i.value != nil && *i.value >= min && *i.value <= max
}

func (i *NullInt) IsNotBetween(min, max int) bool {
	return i.value == nil || *i.value < min || *i.value > max
}
