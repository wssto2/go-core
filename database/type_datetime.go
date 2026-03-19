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
 * DateTime
 */
type DateTime struct {
	value time.Time `json:"-"`
}

func NewDateTime[T time.Time | string](value T) DateTime {
	var parsedTime time.Time
	switch v := any(value).(type) {
	case time.Time:
		parsedTime = v
	case string:
		return NewDateTimeFromString(v)
	default:
		parsedTime = time.Time{}
	}

	return DateTime{
		value: parsedTime,
	}
}

func NewDateTimeFromString(value string) DateTime {
	if value == "" {
		return DateTime{value: time.Time{}}
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
	return DateTime{
		value: t,
	}
}

func (d *DateTime) Get() time.Time {
	return d.value
}

func (d *DateTime) Set(value time.Time) {
	d.value = value
}

func (d *DateTime) SetFromString(value string) {
	newDateTime := NewDateTimeFromString(value)
	d.value = newDateTime.value
}

func (d *DateTime) SetNow() {
	d.value = time.Now().UTC()
}

func (d DateTime) Value() (driver.Value, error) {
	if d.value.IsZero() {
		return nil, nil
	}
	return d.value.Format("2006-01-02 15:04:05"), nil
}

func (d DateTime) GormDataType() string {
	return MySQLDateTime
}

func (d DateTime) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	// SQLite doesn't support 0000-00-00 00:00:00 for default value
	if db.Name() == "sqlite" && field.TagSettings["DEFAULT"] == "0000-00-00 00:00:00" {
		field.DefaultValue = "null"
		field.NotNull = false
	}

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

func (d DateTime) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if d.value.IsZero() {
		return clause.Expr{
			SQL:  "?",
			Vars: []interface{}{"0000-00-00 00:00:00"},
		}
	}

	return clause.Expr{
		SQL:  "?",
		Vars: []interface{}{d.value.UTC().Format("2006-01-02 15:04:05")},
	}
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
			return fmt.Errorf("failed to parse datetime: %w", err)
		}
		d.value = t
	case string:
		t, err := time.Parse("2006-01-02 15:04:05", v)
		if err != nil {
			return fmt.Errorf("failed to parse datetime: %w", err)
		}
		d.value = t
	default:
		return fmt.Errorf("unsupported type for DateTime: %T", value)
	}

	return nil
}

func (d DateTime) MarshalJSON() ([]byte, error) {
	if d.value.IsZero() {
		return []byte(Null), nil
	}

	formattedValue := d.value.Format("2006-01-02 15:04:05")
	return json.Marshal(formattedValue)
}

func (d *DateTime) UnmarshalJSON(data []byte) error {
	if string(data) == Null {
		d.value = time.Time{}
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	if value == "" {
		d.value = time.Time{}
		return nil
	}

	t, err := time.Parse("2006-01-02 15:04:05", value)
	if err != nil {
		return fmt.Errorf("failed to parse datetime: %w", err)
	}

	d.value = t
	return nil
}

func (d *DateTime) String() string {
	if d.value.IsZero() {
		return ""
	}
	return d.value.Format("2006-01-02 15:04:05")
}

func (d *DateTime) IsZero() bool {
	return d.value.IsZero()
}

func (d *DateTime) IsNotZero() bool {
	return !d.value.IsZero()
}

func (d *DateTime) IsEqual(value time.Time) bool {
	return d.value.Equal(value)
}

func (d *DateTime) IsNotEqual(value time.Time) bool {
	return !d.IsEqual(value)
}

func (d *DateTime) IsAfter(value time.Time) bool {
	return d.value.After(value)
}

func (d *DateTime) IsBefore(value time.Time) bool {
	return d.value.Before(value)
}

func (d *DateTime) IsBetween(start, end time.Time) bool {
	return (d.value.Equal(start) || d.value.After(start)) &&
		(d.value.Equal(end) || d.value.Before(end))
}
