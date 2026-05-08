package types

import (
	"context"
	"fmt"

	"github.com/goccy/go-json"
	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// LegacyBoolEnum is a legacy type for boolean enums stored as "1"/"0" in the database.
// This is used for backward compatibility with existing database schemas that use ENUM("0","1").
type LegacyBoolEnum struct {
	value bool
}

func NewLegacyBoolEnum[T bool | int | string](value T) LegacyBoolEnum {
	switch v := any(value).(type) {
	case bool:
		return LegacyBoolEnum{value: v}
	case int:
		return LegacyBoolEnum{value: v == 1}
	case string:
		return LegacyBoolEnum{value: v == "1"}
	}

	return LegacyBoolEnum{value: false}
}

func (e LegacyBoolEnum) GormDataType() string {
	return "enum"
}

func (e LegacyBoolEnum) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Name() {
	case database.DriverSQLite:
		return "varchar(255)"
	case database.DriverMySQL:
		return fmt.Sprintf("enum(%s)", field.TagSettings["ENUM"])
	}

	return fmt.Sprintf("varchar(255) check (%s in (%s))", field.Name, field.TagSettings["ENUM"])
}

func (e LegacyBoolEnum) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	val := "0"
	if e.value {
		val = "1"
	}

	return clause.Expr{SQL: "?", Vars: []any{val}}
}

func (e *LegacyBoolEnum) Scan(value any) error {
	switch typedValue := value.(type) {
	case string:
		e.value = typedValue == "1"
	case []byte:
		e.value = string(typedValue) == "1"
	case int64:
		e.value = typedValue == 1
	default:
		return fmt.Errorf("unsupported type for Enum: %T", value)
	}

	return nil
}

func (e LegacyBoolEnum) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.value)
}

func (e *LegacyBoolEnum) UnmarshalJSON(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}

	switch val := v.(type) {
	case bool:
		e.value = val
	case float64:
		e.value = val == 1
	case string:
		e.value = val == "1" || val == "true"
	}

	return nil
}

func (e LegacyBoolEnum) Get() bool {
	return e.value
}

func (e *LegacyBoolEnum) Set(v bool) {
	e.value = v
}

func (e LegacyBoolEnum) String() string {
	if e.value {
		return "1"
	}

	return "0"
}
