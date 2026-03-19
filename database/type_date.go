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
 * Date
 */
type Date struct {
	value time.Time `json:"-"`
}

func NewDate(value time.Time) Date {
	// Ensure time component is zeroed out for date-only values
	return Date{
		value: time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC),
	}
}

func NewDateFromString(value string) Date {
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

func (d *Date) Get() time.Time {
	return d.value
}

func (d *Date) Set(value time.Time) {
	d.value = time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func (d *Date) SetFromString(value string) {
	newDate := NewDateFromString(value)
	d.value = newDate.value
}

func (d *Date) SetNow() {
	now := time.Now().UTC()
	d.value = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func (d Date) Value() (driver.Value, error) {
	if d.value.IsZero() {
		return nil, nil
	}
	return d.value.Format("2006-01-02"), nil
}

func (d Date) GormDataType() string {
	return MySQLDate
}

func (d Date) GormDBDataType(db *gorm.DB, field *schema.Field) string {
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

func (d Date) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if d.value.IsZero() {
		return clause.Expr{
			SQL:  "?",
			Vars: []interface{}{"0000-00-00"},
		}
	}

	return clause.Expr{
		SQL:  "?",
		Vars: []interface{}{d.value.Format("2006-01-02")},
	}
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
			return fmt.Errorf("failed to parse date: %w", err)
		}
		d.value = t
	case string:
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return fmt.Errorf("failed to parse date: %w", err)
		}
		d.value = t
	default:
		return fmt.Errorf("unsupported type for Date: %T", value)
	}

	return nil
}

func (d Date) MarshalJSON() ([]byte, error) {
	if d.value.IsZero() {
		return []byte(Null), nil
	}

	return json.Marshal(d.value.Format("2006-01-02"))
}

func (d *Date) UnmarshalJSON(data []byte) error {
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

	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return fmt.Errorf("failed to parse date: %w", err)
	}

	d.value = t
	return nil
}

func (d *Date) String() string {
	if d.value.IsZero() {
		return ""
	}
	return d.value.Format("2006-01-02")
}

func (d *Date) IsZero() bool {
	return d.value.IsZero()
}

func (d *Date) IsNotZero() bool {
	return !d.value.IsZero()
}

func (d *Date) IsEqual(value time.Time) bool {
	compareDate := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	return d.value.Equal(compareDate)
}

func (d *Date) IsNotEqual(value time.Time) bool {
	return !d.IsEqual(value)
}

func (d *Date) IsAfter(value time.Time) bool {
	compareDate := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	return d.value.After(compareDate)
}

func (d *Date) IsBefore(value time.Time) bool {
	compareDate := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	return d.value.Before(compareDate)
}

func (d *Date) IsBetween(start, end time.Time) bool {
	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)

	return (d.value.Equal(startDate) || d.value.After(startDate)) &&
		(d.value.Equal(endDate) || d.value.Before(endDate))
}
