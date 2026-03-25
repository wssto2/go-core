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

// NullInt wraps *int.
type NullInt struct {
	value *int
}

func NewNullInt(value int) NullInt {
	return NullInt{value: &value}
}

func (i NullInt) Value() (driver.Value, error) {
	if i.value == nil {
		return nil, nil
	}
	return *i.value, nil
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
			return err
		}
		i.value = &val
	case string:
		val, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		i.value = &val
	default:
		return fmt.Errorf("unsupported type for NullInt: %T", value)
	}
	return nil
}

func (i NullInt) GormDataType() string {
	return database.MySQLInt
}

func (i NullInt) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	if db.Name() == database.DriverSQLite {
		return database.SQLiteInt
	}
	if t := field.TagSettings["TYPE"]; t != "" {
		return t
	}
	return database.MySQLInt
}

func (i NullInt) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if i.value == nil {
		return clause.Expr{SQL: "NULL"}
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{*i.value}}
}

func (i NullInt) MarshalJSON() ([]byte, error) {
	if i.value == nil {
		return []byte(database.Null), nil
	}
	return json.Marshal(*i.value)
}

func (i *NullInt) UnmarshalJSON(data []byte) error {
	if string(data) == database.Null {
		i.value = nil
		return nil
	}
	var val int
	if err := json.Unmarshal(data, &val); err != nil {
		return err
	}
	i.value = &val
	return nil
}

func (i NullInt) Get() *int {
	return i.value
}

func (i *NullInt) Set(v int) {
	i.value = &v
}
