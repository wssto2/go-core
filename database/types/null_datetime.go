package types

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

// NullDateTime wraps *time.Time for nullable datetime values.
type NullDateTime struct {
	value *time.Time
}

func NewNullDateTime(value time.Time) NullDateTime {
	return NullDateTime{value: &value}
}

func (d NullDateTime) Value() (driver.Value, error) {
	if d.value == nil {
		return nil, nil
	}
	return d.value.Format("2006-01-02 15:04:05"), nil
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
			return err
		}
		d.value = &t
	case string:
		t, err := time.Parse("2006-01-02 15:04:05", v)
		if err != nil {
			return err
		}
		d.value = &t
	default:
		return fmt.Errorf("unsupported type for NullDateTime: %T", value)
	}
	return nil
}

func (d NullDateTime) GormDataType() string {
	return MysqlDateTimeType
}

func (d NullDateTime) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	if db.Name() == Sqlite && field.TagSettings["DEFAULT"] == "0000-00-00 00:00:00" {
		field.DefaultValue = "null"
		field.NotNull = false
	}
	if t := field.TagSettings["TYPE"]; t != "" {
		return t
	}
	if db.Name() == Sqlite {
		return SqliteDateTimeType
	}
	return MysqlDateTimeType
}

func (d NullDateTime) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if d.value == nil {
		return clause.Expr{SQL: "NULL"}
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{d.value.Format("2006-01-02 15:04:05")}}
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
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
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

func (d NullDateTime) Get() *time.Time {
	return d.value
}

func (d *NullDateTime) Set(t time.Time) {
	d.value = &t
}
