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
 * Int
 */
type Int struct {
	value int `json:"-"`
}

func NewInt(value int) Int {
	return Int{
		value: value,
	}
}

func (i *Int) Get() int {
	return i.value
}

func (i *Int) Set(value int) {
	i.value = value
}

func (i Int) Value() (driver.Value, error) {
	return i.value, nil
}

func (i Int) GormDataType() string {
	return MySQLInt
}

func (i Int) GormDBDataType(db *gorm.DB, field *schema.Field) string {
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

func (i Int) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	return clause.Expr{
		SQL:  "?",
		Vars: []interface{}{i.value},
	}
}

func (i *Int) Scan(value interface{}) error {
	switch v := value.(type) {
	case int64:
		i.value = int(v)

	case float64:
		i.value = int(v)

	case []byte:
		val, err := strconv.Atoi(string(v))
		if err != nil {
			return fmt.Errorf("failed to convert byte slice to int: %w", err)
		}
		i.value = val

	case string:
		val, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("failed to convert string to int: %w", err)
		}
		i.value = val

	default:
		return fmt.Errorf("unsupported type for Int: %T", value)
	}

	return nil
}

func (i Int) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Itoa(i.value)), nil
}

func (i *Int) UnmarshalJSON(data []byte) error {
	if string(data) == Null {
		i.value = 0

		return nil
	}

	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("failed to unmarshal JSON data %s: %w", string(data), err)
	}

	i.value = value
	return nil
}

func (i *Int) String() string {
	return strconv.Itoa(i.value)
}

func (i *Int) IsZero() bool {
	return i.value == 0
}

func (i *Int) IsNotZero() bool {
	return i.value != 0
}

func (i *Int) IsEqual(value int) bool {
	return i.value == value
}

func (i *Int) IsNotEqual(value int) bool {
	return i.value != value
}

func (i *Int) IsGreaterThan(value int) bool {
	return i.value > value
}

func (i *Int) IsLessThan(value int) bool {
	return i.value < value
}

func (i *Int) IsGreaterThanOrEqual(value int) bool {
	return i.value >= value
}

func (i *Int) IsLessThanOrEqual(value int) bool {
	return i.value <= value
}

func (i *Int) IsIn(values ...int) bool {
	for _, v := range values {
		if i.value == v {
			return true
		}
	}

	return false
}

func (i *Int) IsNotIn(values ...int) bool {
	for _, v := range values {
		if i.value == v {
			return false
		}
	}

	return true
}

func (i *Int) IsBetween(min, max int) bool {
	return i.value >= min && i.value <= max
}

func (i *Int) IsNotBetween(min, max int) bool {
	return i.value < min || i.value > max
}
