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

// Float wraps float64.
type Float struct {
	value float64
}

func NewFloat(value float64) Float {
	return Float{value: value}
}

func (f Float) Value() (driver.Value, error) {
	return f.value, nil
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
			return err
		}
		f.value = val
	case string:
		val, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return err
		}
		f.value = val
	default:
		return fmt.Errorf("unsupported type for Float: %T", value)
	}
	return nil
}

func (f Float) GormDataType() string {
	return MysqlFloatType
}

func (f Float) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	if db.Name() == Mysql && field.TagSettings["TYPE"] == "" {
		return "decimal(10,2)" // Default preference from original code
	}
	if t := field.TagSettings["TYPE"]; t != "" {
		return t
	}
	return MysqlFloatType
}

func (f Float) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	return clause.Expr{SQL: "?", Vars: []interface{}{f.value}}
}

func (f Float) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.value)
}

func (f *Float) UnmarshalJSON(data []byte) error {
	if string(data) == Null {
		f.value = 0
		return nil
	}
	return json.Unmarshal(data, &f.value)
}

func (f Float) Get() float64 {
	return f.value
}

func (f *Float) Set(v float64) {
	f.value = v
}

func (f Float) String() string {
	return strconv.FormatFloat(f.value, 'f', -1, 64)
}
