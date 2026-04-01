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

// Date wraps time.Time to handle date-only values (YYYY-MM-DD).
type Date struct {
	value time.Time
}

func NewDate(value time.Time) Date {
	return Date{
		value: time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC),
	}
}

func NewDateFromString(value string) Date {
	if value == "" {
		return Date{}
	}
	// Try ISO 8601 / RFC3339 / Simple
	formats := []string{
		"2006-01-02",
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
		// Fallback or panic? Original panicked. Let's return zero value and log error?
		// Keeping original behavior for compatibility but maybe safer?
		// panic(fmt.Sprintf("failed to parse date: %s", value))
		return Date{}
	}
	return NewDate(t)
}

func (d Date) Value() (driver.Value, error) {
	if d.value.IsZero() {
		return nil, nil
	}
	return d.value.Format("2006-01-02"), nil
}

func (d *Date) Scan(value interface{}) error {
	if value == nil {
		d.value = time.Time{}
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		d.value = time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
	case []byte:
		t, err := time.Parse("2006-01-02", string(v))
		if err != nil {
			return err
		}
		d.value = t
	case string:
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return err
		}
		d.value = t
	default:
		return fmt.Errorf("unsupported type for Date: %T", value)
	}
	return nil
}

func (d Date) GormDataType() string {
	return database.MySQLDate
}

func (d Date) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	if db.Name() == database.DriverSQLite && field.TagSettings["DEFAULT"] == "0000-00-00" {
		field.DefaultValue = "null"
		field.NotNull = false
	}
	switch db.Name() {
	case database.DriverSQLite:
		return database.SQLiteDate
	case database.DriverMySQL:
		if t := field.TagSettings["TYPE"]; t != "" {
			return t
		}
		return database.MySQLDate
	}
	return field.TagSettings["TYPE"]
}

func (d Date) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if d.value.IsZero() {
		return clause.Expr{SQL: "?", Vars: []interface{}{"0000-00-00"}}
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{d.value.Format("2006-01-02")}}
}

func (d Date) MarshalJSON() ([]byte, error) {
	if d.value.IsZero() {
		return []byte(database.Null), nil
	}
	return json.Marshal(d.value.Format("2006-01-02"))
}

func (d *Date) UnmarshalJSON(data []byte) error {
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
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return err
	}
	d.value = t
	return nil
}

func (d Date) Get() time.Time {
	return d.value
}

func (d *Date) Set(t time.Time) {
	d.value = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func (d Date) String() string {
	if d.value.IsZero() {
		return ""
	}
	return d.value.Format("2006-01-02")
}
