package types

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/goccy/go-json"
	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// NullDate wraps *time.Time for nullable date values.
type NullDate struct {
	value *time.Time
}

func NewNullDate(value time.Time) NullDate {
	t := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	return NullDate{value: &t}
}

func (d NullDate) Value() (driver.Value, error) {
	if d.value == nil {
		return nil, nil
	}
	return d.value.Format("2006-01-02"), nil
}

func (d *NullDate) Scan(value interface{}) error {
	if value == nil {
		d.value = nil
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		t := time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
		d.value = &t
	case []byte:
		t, err := time.Parse("2006-01-02", string(v))
		if err != nil {
			return err
		}
		d.value = &t
	case string:
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return err
		}
		d.value = &t
	default:
		return fmt.Errorf("unsupported type for NullDate: %T", value)
	}
	return nil
}

func (d NullDate) GormDataType() string {
	return database.MySQLDate
}

func (d NullDate) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	if db.Name() == database.DriverSQLite && field.TagSettings["DEFAULT"] == "0000-00-00" {
		field.DefaultValue = "null"
		field.NotNull = false
	}
	if t := field.TagSettings["TYPE"]; t != "" {
		return t
	}
	if db.Name() == database.DriverSQLite {
		return database.SQLiteDate
	}
	return database.MySQLDate
}

func (d NullDate) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if d.value == nil {
		return clause.Expr{SQL: "NULL"}
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{d.value.Format("2006-01-02")}}
}

func (d NullDate) MarshalJSON() ([]byte, error) {
	if d.value == nil {
		return []byte(database.Null), nil
	}
	return json.Marshal(d.value.Format("2006-01-02"))
}

func (d *NullDate) UnmarshalJSON(data []byte) error {
	if string(data) == database.Null {
		d.value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		d.value = nil
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return err
	}
	d.value = &t
	return nil
}

func (d NullDate) Get() *time.Time {
	return d.value
}

func (d *NullDate) Set(t time.Time) {
	v := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	d.value = &v
}
