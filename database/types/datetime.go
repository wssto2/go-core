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

// DateTime wraps time.Time to handle datetime values (YYYY-MM-DD HH:MM:SS).
type DateTime struct {
	value time.Time
}

func NewDateTime(value time.Time) DateTime {
	return DateTime{value: value}
}

func NewDateTimeFromString(value string) DateTime {
	if value == "" {
		return DateTime{}
	}
	formats := []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
		"2006-01-02T15:04:05Z",
	}
	var t time.Time
	var err error
	for _, f := range formats {
		t, err = time.Parse(f, value)
		if err == nil {
			break
		}
	}
	if err != nil {
		return DateTime{}
	}
	return NewDateTime(t)
}

func (d DateTime) Value() (driver.Value, error) {
	if d.value.IsZero() {
		return nil, nil
	}
	return d.value.Format("2006-01-02 15:04:05"), nil
}

func (d *DateTime) Scan(value interface{}) error {
	if value == nil {
		d.value = time.Time{}
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		d.value = v
	case []byte:
		t, err := time.Parse("2006-01-02 15:04:05", string(v))
		if err != nil {
			return err
		}
		d.value = t
	case string:
		t, err := time.Parse("2006-01-02 15:04:05", v)
		if err != nil {
			return err
		}
		d.value = t
	default:
		return fmt.Errorf("unsupported type for DateTime: %T", value)
	}
	return nil
}

func (d DateTime) GormDataType() string {
	return database.MySQLDateTime
}

func (d DateTime) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	if db.Name() == database.DriverSQLite && field.TagSettings["DEFAULT"] == "0000-00-00 00:00:00" {
		field.DefaultValue = "null"
		field.NotNull = false
	}
	switch db.Name() {
	case database.DriverSQLite:
		return database.SQLiteDateTime
	case database.DriverMySQL:
		if t := field.TagSettings["TYPE"]; t != "" {
			return t
		}
		return database.MySQLDateTime
	}
	return field.TagSettings["TYPE"]
}

func (d DateTime) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if d.value.IsZero() {
		return clause.Expr{SQL: "?", Vars: []interface{}{"0000-00-00 00:00:00"}}
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{d.value.UTC().Format("2006-01-02 15:04:05")}}
}

func (d DateTime) MarshalJSON() ([]byte, error) {
	if d.value.IsZero() {
		return []byte(database.Null), nil
	}
	return json.Marshal(d.value.Format("2006-01-02 15:04:05"))
}

func (d *DateTime) UnmarshalJSON(data []byte) error {
	if string(data) == database.Null {
		d.value = time.Time{}
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		d.value = time.Time{}
		return nil
	}
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return err
	}
	d.value = t
	return nil
}

func (d DateTime) Get() time.Time {
	return d.value
}

func (d *DateTime) Set(t time.Time) {
	d.value = t
}

func (d DateTime) String() string {
	if d.value.IsZero() {
		return ""
	}
	return d.value.Format("2006-01-02 15:04:05")
}
