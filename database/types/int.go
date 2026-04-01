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

// Int wraps int.
type Int struct {
	value int
}

func NewInt(value int) Int {
	return Int{value: value}
}

func (i Int) Value() (driver.Value, error) {
	return i.value, nil
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
			return err
		}
		i.value = val
	case string:
		val, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		i.value = val
	default:
		return fmt.Errorf("unsupported type for Int: %T", value)
	}
	return nil
}

func (i Int) GormDataType() string {
	return database.MySQLInt
}

func (i Int) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	if db.Name() == database.DriverSQLite {
		return database.SQLiteInt
	}
	if t := field.TagSettings["TYPE"]; t != "" {
		return t
	}
	return database.MySQLInt
}

func (i Int) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	return clause.Expr{SQL: "?", Vars: []interface{}{i.value}}
}

func (i Int) MarshalJSON() ([]byte, error) {
	return json.Marshal(i.value)
}

func (i *Int) UnmarshalJSON(data []byte) error {
	if string(data) == database.Null {
		i.value = 0
		return nil
	}
	return json.Unmarshal(data, &i.value)
}

func (i Int) Get() int {
	return i.value
}

func (i *Int) Set(v int) {
	i.value = v
}

func (i Int) String() string {
	return strconv.Itoa(i.value)
}
