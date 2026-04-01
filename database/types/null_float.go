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

// NullFloat wraps *float64.
type NullFloat struct {
	value *float64
}

func NewNullFloat(value float64) NullFloat {
	return NullFloat{value: &value}
}

func (f NullFloat) Value() (driver.Value, error) {
	if f.value == nil {
		return nil, nil
	}
	return *f.value, nil
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
			return err
		}
		f.value = &val
	case string:
		val, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return err
		}
		f.value = &val
	default:
		return fmt.Errorf("unsupported type for NullFloat: %T", value)
	}
	return nil
}

func (f NullFloat) GormDataType() string {
	return database.MySQLFloat
}

func (f NullFloat) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	if db.Name() == database.DriverMySQL && field.TagSettings["TYPE"] == "" {
		return "decimal(10,2)"
	}
	if t := field.TagSettings["TYPE"]; t != "" {
		return t
	}
	if db.Name() == database.DriverSQLite {
		return database.SQLiteFloat
	}
	return database.MySQLFloat
}

func (f NullFloat) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if f.value == nil {
		return clause.Expr{SQL: "NULL"}
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{*f.value}}
}

func (f NullFloat) MarshalJSON() ([]byte, error) {
	if f.value == nil {
		return []byte(database.Null), nil
	}
	return json.Marshal(*f.value)
}

func (f *NullFloat) UnmarshalJSON(data []byte) error {
	if string(data) == database.Null {
		f.value = nil
		return nil
	}
	var val float64
	if err := json.Unmarshal(data, &val); err != nil {
		return err
	}
	f.value = &val
	return nil
}

func (f NullFloat) Get() *float64 {
	return f.value
}

func (f *NullFloat) Set(v float64) {
	f.value = &v
}
