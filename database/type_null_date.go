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
 * NullDate
 */
type NullDate struct {
	value *time.Time `json:"-"`
}

func NewNullDate(value time.Time) NullDate {
	// Ensure time component is zeroed out for date-only values
	t := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	return NullDate{
		value: &t,
	}
}

func NewNullDateFromString(value string) Date {
	if value == "" {
		return Date{value: time.Time{}}
	}

	// Try to parse different date formats
	formats := []string{
		"2006-01-02T15:04:05.000Z", // ISO 8601 format with milliseconds
		"2006-01-02T15:04:05Z",     // ISO 8601 format without milliseconds
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

	// Ensure time component is zeroed out for date-only values
	t := time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), 0, 0, 0, 0, time.UTC)
	return Date{
		value: t,
	}
}

func (d *NullDate) Get() time.Time {
	if d.value == nil {
		panic("NullDate.Get() called on null value — use GetOrZero() or check IsNull() first")
	}
	return *d.value
}

func (d *NullDate) GetOrZero() time.Time {
	if d.value == nil {
		return time.Time{}
	}
	return *d.value
}

func (d *NullDate) Set(value time.Time) {
	t := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	d.value = &t
}

func (d *NullDate) SetFromString(value string) {
	newDate := NewNullDateFromString(value)
	d.value = &newDate.value
}

func (d *NullDate) SetNow() {
	now := time.Now().UTC()
	t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	d.value = &t
}

func (d *NullDate) SetNull() {
	d.value = nil
}

func (d NullDate) Value() (driver.Value, error) {
	if d.value == nil {
		return nil, nil
	}
	return d.value.Format("2006-01-02"), nil
}

func (d NullDate) GormDataType() string {
	return MySQLDate
}

func (d NullDate) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	// SQLite doesn't support 0000-00-00 for default value
	if db.Name() == "sqlite" && field.TagSettings["DEFAULT"] == "0000-00-00" {
		field.DefaultValue = "null"
		field.NotNull = false
	}

	switch db.Name() {
	case DriverSQLite:
		return SQLiteDate

	case DriverMySQL:
		if field.TagSettings["TYPE"] == "" {
			return MySQLDate
		}

		return field.TagSettings["TYPE"]
	}

	return field.TagSettings["TYPE"]
}

func (d NullDate) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if d.value == nil {
		return clause.Expr{
			SQL:  "NULL",
			Vars: nil,
		}
	}

	return clause.Expr{
		SQL:  "?",
		Vars: []interface{}{d.value.Format("2006-01-02")},
	}
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
			return fmt.Errorf("failed to parse date: %w", err)
		}
		d.value = &t
	case string:
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return fmt.Errorf("failed to parse date: %w", err)
		}
		d.value = &t
	default:
		return fmt.Errorf("unsupported type for NullDate: %T", value)
	}

	return nil
}

func (d NullDate) MarshalJSON() ([]byte, error) {
	if d.value == nil {
		return []byte(Null), nil
	}

	return json.Marshal(d.value.Format("2006-01-02"))
}

func (d *NullDate) UnmarshalJSON(data []byte) error {
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

	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return fmt.Errorf("failed to parse date: %w", err)
	}

	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	d.value = &t
	return nil
}

func (d *NullDate) String() string {
	if d.value == nil {
		return ""
	}
	return d.value.Format("2006-01-02")
}

func (d *NullDate) IsNull() bool {
	return d.value == nil
}

func (d *NullDate) IsNotNull() bool {
	return d.value != nil
}

func (d *NullDate) IsZero() bool {
	return d.value != nil && d.value.IsZero()
}

func (d *NullDate) IsNotZero() bool {
	return d.value != nil && !d.value.IsZero()
}

func (d *NullDate) IsEqual(value time.Time) bool {
	if d.value == nil {
		return false
	}
	compareDate := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	return d.value.Equal(compareDate)
}

func (d *NullDate) IsNotEqual(value time.Time) bool {
	return !d.IsEqual(value)
}

func (d *NullDate) IsAfter(value time.Time) bool {
	if d.value == nil {
		return false
	}
	compareDate := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	return d.value.After(compareDate)
}

func (d *NullDate) IsBefore(value time.Time) bool {
	if d.value == nil {
		return false
	}
	compareDate := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	return d.value.Before(compareDate)
}

func (d *NullDate) IsBetween(start, end time.Time) bool {
	if d.value == nil {
		return false
	}

	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)

	return (d.value.Equal(startDate) || d.value.After(startDate)) &&
		(d.value.Equal(endDate) || d.value.Before(endDate))
}
