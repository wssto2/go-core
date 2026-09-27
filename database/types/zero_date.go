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

const mysql5ZeroDate = "0000-00-00"

// ZeroDate is a *time.Time wrapper for MySQL 5 NOT NULL date columns
// that have no meaningful initial value.
//
// When the value is nil it serializes to "0000-00-00" instead of SQL NULL,
// satisfying the NOT NULL constraint. When scanning, the MySQL 5 zero sentinel
// is read back as nil so callers never see the meaningless zero date.
//
// Use NullDate for columns where NULL is a valid DB value.
// Use ZeroDate for legacy NOT NULL date columns you cannot alter.
type ZeroDate struct {
	value *time.Time
}

// NewZeroDate creates a ZeroDate from a time.Time. Go's zero time (and any
// time on 0001-01-01) means "no value", like nil: it is stored as 0000-00-00.
func NewZeroDate(value time.Time) ZeroDate {
	return ZeroDate{value: zeroDateValue(value)}
}

// NewZeroDatePtr creates a ZeroDate from a *time.Time.
// A nil pointer, or one to Go's zero time, will be stored as 0000-00-00 in the database.
func NewZeroDatePtr(value *time.Time) ZeroDate {
	if value == nil {
		return ZeroDate{}
	}

	return ZeroDate{value: zeroDateValue(*value)}
}

// zeroDateValue truncates value to its calendar date (at UTC midnight), or returns nil
// when that date is Go's zero date (0001-01-01), which ZeroDate and NullDate
// treat as "no value" so it is never written as 0001-01-01.
func zeroDateValue(value time.Time) *time.Time {
	t := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	if t.IsZero() {
		return nil
	}

	return &t
}

func (d ZeroDate) Value() (driver.Value, error) {
	if d.value == nil || d.value.IsZero() {
		return mysql5ZeroDate, nil
	}
	return d.value.Format("2006-01-02"), nil
}

func (d *ZeroDate) Scan(value interface{}) error {
	if value == nil {
		d.value = nil
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		if v.IsZero() {
			d.value = nil
			return nil
		}
		t := time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
		d.value = &t
	case []byte:
		s := string(v)
		if s == mysql5ZeroDate || s == "" {
			d.value = nil
			return nil
		}
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return err
		}
		d.value = &t
	case string:
		if v == mysql5ZeroDate || v == "" {
			d.value = nil
			return nil
		}
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return err
		}
		d.value = &t
	default:
		return fmt.Errorf("unsupported type for ZeroDate: %T", value)
	}
	return nil
}

func (d ZeroDate) GormDataType() string {
	return database.MySQLDate
}

func (d ZeroDate) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	if t := field.TagSettings["TYPE"]; t != "" {
		return t
	}
	if db.Name() == database.DriverSQLite {
		return database.SQLiteDate
	}
	return database.MySQLDate
}

func (d ZeroDate) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if d.value == nil || d.value.IsZero() {
		return clause.Expr{SQL: "?", Vars: []interface{}{mysql5ZeroDate}}
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{d.value.Format("2006-01-02")}}
}

func (d ZeroDate) MarshalJSON() ([]byte, error) {
	if d.value == nil {
		return []byte(database.Null), nil
	}
	return json.Marshal(d.value.Format("2006-01-02"))
}

func (d *ZeroDate) UnmarshalJSON(data []byte) error {
	if string(data) == database.Null {
		d.value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" || s == mysql5ZeroDate {
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

func (d ZeroDate) Get() *time.Time {
	return d.value
}

func (d ZeroDate) GetOrEmpty() time.Time {
	if d.value == nil {
		return time.Time{}
	}

	return *d.value
}

func (d ZeroDate) GetOrDefault(defaultValue time.Time) time.Time {
	if d.value == nil {
		return defaultValue
	}

	return *d.value
}

// Set replaces the value; Go's zero time clears it (stored as 0000-00-00).
func (d *ZeroDate) Set(t time.Time) {
	d.value = zeroDateValue(t)
}

func (d *ZeroDate) IsNull() bool {
	return d.value == nil
}
