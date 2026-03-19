package database

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/goccy/go-json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

/**
 * NullDateTime
 */
type NullDateTime struct {
	value *time.Time `json:"-"`
}

func NewNullDateTime[T time.Time | string](value T) NullDateTime {
	val := time.Time{}
	switch typedValue := any(value).(type) {
	case time.Time:
		val = typedValue
	case string:
		return NewNullDateTimeFromString(typedValue)
	}

	return NullDateTime{
		value: &val,
	}
}

func NewNullDateTimeFromString(value string) NullDateTime {
	if value == "" {
		return NullDateTime{value: nil}
	}

	// Try to parse different date formats
	formats := []string{
		"2006-01-02T15:04:05.000Z", // ISO 8601 format with milliseconds
		"2006-01-02T15:04:05Z",     // ISO 8601 format without milliseconds
		"2006-01-02 15:04:05",      // MySQL datetime format
		"2006-01-02 15:04",         // Simple date format
		time.RFC3339,               // RFC3339 format
		"2006-01-02",               // Simple date format
	}

	var parsedDate time.Time
	var err error

	for _, format := range formats {
		parsedDate, err = time.Parse(format, value)
		if err == nil {
			break
		}
	}

	if err != nil {
		panic(fmt.Sprintf("failed to parse date: %s, error: %v", value, err))
	}

	t := time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), parsedDate.Hour(), parsedDate.Minute(), parsedDate.Second(), 0, time.UTC)
	return NullDateTime{
		value: &t,
	}
}

func (d *NullDateTime) Get() time.Time {
	if d.value == nil {
		panic("NullDateTime.Get() called on null value — use GetOrZero() or check IsNull() first")
	}
	return *d.value
}

func (d *NullDateTime) GetOrZero() time.Time {
	if d.value == nil {
		return time.Time{}
	}
	return *d.value
}

func (d *NullDateTime) Set(value time.Time) {
	d.value = &value
}

func (d *NullDateTime) SetFromString(value string) {
	newDateTime := NewNullDateTimeFromString(value)
	d.value = newDateTime.value
}

func (d *NullDateTime) SetNow() {
	now := time.Now().UTC()
	d.value = &now
}

func (d *NullDateTime) SetNull() {
	d.value = nil
}

func (d NullDateTime) Value() (driver.Value, error) {
	if d.value == nil {
		return nil, nil
	}
	return d.value.Format("2006-01-02 15:04:05"), nil
}

func (d NullDateTime) GormDataType() string {
	return MySQLDateTime
}

func (d NullDateTime) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Name() {
	case DriverSQLite:
		return SQLiteDateTime

	case DriverMySQL:
		if field.TagSettings["TYPE"] == "" {
			return MySQLDateTime
		}

		return field.TagSettings["TYPE"]
	}

	return field.TagSettings["TYPE"]
}

func (d NullDateTime) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if d.value == nil {
		return clause.Expr{
			SQL:  "NULL",
			Vars: nil,
		}
	}

	return clause.Expr{
		SQL:  "?",
		Vars: []interface{}{d.value.Format("2006-01-02 15:04:05")},
	}
}

func (d *NullDateTime) Scan(value interface{}) error {
	if value == nil {
		d.value = nil
		return nil
	}

	switch v := value.(type) {
	case time.Time:
		d.value = &v
	case []byte:
		t, err := time.Parse("2006-01-02 15:04:05", string(v))
		if err != nil {
			return fmt.Errorf("failed to parse datetime: %w", err)
		}
		d.value = &t
	case string:
		t, err := time.Parse("2006-01-02 15:04:05", v)
		if err != nil {
			return fmt.Errorf("failed to parse datetime: %w", err)
		}
		d.value = &t
	default:
		return fmt.Errorf("unsupported type for NullDateTime: %T", value)
	}

	return nil
}

func (d NullDateTime) MarshalJSON() ([]byte, error) {
	if d.value == nil {
		return []byte(Null), nil
	}

	return json.Marshal(d.value.Format("2006-01-02 15:04:05"))
}

func (d *NullDateTime) UnmarshalJSON(data []byte) error {
	if string(data) == Null {
		d.value = nil
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	if value == "" {
		d.value = nil
		return nil
	}

	t, err := time.Parse("2006-01-02 15:04:05", value)
	if err != nil {
		return fmt.Errorf("failed to parse datetime: %w", err)
	}

	d.value = &t
	return nil
}

func (d *NullDateTime) String() string {
	if d.value == nil {
		return ""
	}
	return d.value.Format("2006-01-02 15:04:05")
}

func (d *NullDateTime) IsNull() bool {
	return d.value == nil
}

func (d *NullDateTime) IsNotNull() bool {
	return d.value != nil
}

func (d *NullDateTime) IsZero() bool {
	return d.value != nil && d.value.IsZero()
}

func (d *NullDateTime) IsNotZero() bool {
	return d.value != nil && !d.value.IsZero()
}

func (d *NullDateTime) IsEqual(value time.Time) bool {
	if d.value == nil {
		return false
	}
	return d.value.Equal(value)
}

func (d *NullDateTime) IsNotEqual(value time.Time) bool {
	return !d.IsEqual(value)
}

func (d *NullDateTime) IsAfter(value time.Time) bool {
	if d.value == nil {
		return false
	}
	return d.value.After(value)
}

func (d *NullDateTime) IsBefore(value time.Time) bool {
	if d.value == nil {
		return false
	}
	return d.value.Before(value)
}

func (d *NullDateTime) IsBetween(start, end time.Time) bool {
	if d.value == nil {
		return false
	}

	return (d.value.Equal(start) || d.value.After(start)) &&
		(d.value.Equal(end) || d.value.Before(end))
}
