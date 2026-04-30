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

// mysql5Zero is the sentinel value MySQL 5 uses for NOT NULL datetime columns
// that have no meaningful value yet. It is stored and read back as this string.
const mysql5Zero = "0000-00-00 00:00:00"

// ZeroDateTime is a *time.Time wrapper for MySQL 5 NOT NULL datetime columns
// that have no meaningful initial value (e.g. "last activated", "last deactivated").
//
// When the value is nil it serializes to "0000-00-00 00:00:00" instead of SQL NULL,
// satisfying the NOT NULL constraint. When scanning, the MySQL 5 zero sentinel is
// read back as nil so callers never see the meaningless zero date.
//
// Use NullDateTime for columns where NULL is a valid DB value.
// Use ZeroDateTime for legacy NOT NULL datetime columns you cannot alter.
type ZeroDateTime struct {
	value *time.Time
}

func NewZeroDateTime(value time.Time) ZeroDateTime {
	return ZeroDateTime{value: &value}
}

// NewZeroDateTimePtr creates a ZeroDateTime from a *time.Time.
// A nil pointer will be stored as 0000-00-00 00:00:00 in the database.
func NewZeroDateTimePtr(value *time.Time) ZeroDateTime {
	if value == nil {
		return ZeroDateTime{}
	}
	return ZeroDateTime{value: value}
}

func (d ZeroDateTime) Value() (driver.Value, error) {
	if d.value == nil {
		return mysql5Zero, nil
	}
	return d.value.Format("2006-01-02 15:04:05"), nil
}

func (d *ZeroDateTime) Scan(value interface{}) error {
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
		d.value = &v
	case []byte:
		s := string(v)
		if s == mysql5Zero || s == "" {
			d.value = nil
			return nil
		}
		t, err := time.Parse("2006-01-02 15:04:05", s)
		if err != nil {
			return err
		}
		d.value = &t
	case string:
		if v == mysql5Zero || v == "" {
			d.value = nil
			return nil
		}
		t, err := time.Parse("2006-01-02 15:04:05", v)
		if err != nil {
			return err
		}
		d.value = &t
	default:
		return fmt.Errorf("unsupported type for ZeroDateTime: %T", value)
	}
	return nil
}

func (d ZeroDateTime) GormDataType() string {
	return database.MySQLDateTime
}

func (d ZeroDateTime) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	if t := field.TagSettings["TYPE"]; t != "" {
		return t
	}
	if db.Name() == database.DriverSQLite {
		return database.SQLiteDateTime
	}
	return database.MySQLDateTime
}

func (d ZeroDateTime) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if d.value == nil {
		return clause.Expr{SQL: "?", Vars: []interface{}{mysql5Zero}}
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{d.value.Format("2006-01-02 15:04:05")}}
}

func (d ZeroDateTime) MarshalJSON() ([]byte, error) {
	if d.value == nil {
		return []byte(database.Null), nil
	}
	return json.Marshal(d.value.Format("2006-01-02 15:04:05"))
}

func (d *ZeroDateTime) UnmarshalJSON(data []byte) error {
	if string(data) == database.Null {
		d.value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" || s == mysql5Zero {
		d.value = nil
		return nil
	}
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return err
	}
	d.value = &t
	return nil
}

func (d ZeroDateTime) Get() *time.Time {
	return d.value
}

func (d *ZeroDateTime) Set(t time.Time) {
	d.value = &t
}
