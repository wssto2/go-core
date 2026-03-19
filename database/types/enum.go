package types

import (
	"context"
	"fmt"

	"github.com/goccy/go-json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// Enum wraps a boolean logic for database enums.
// Warning: This implementation maps specific ENUM values (often "1"/"0") to boolean true/false.
type Enum struct {
	value bool
}

func NewEnum[T bool | int | string](value T) Enum {
	switch v := any(value).(type) {
	case bool:
		return Enum{value: v}
	case int:
		return Enum{value: v == 1}
	case string:
		return Enum{value: v == "1"}
	}
	return Enum{value: false}
}

func (e Enum) GormDataType() string {
	return "enum"
}

func (e Enum) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Name() {
	case Sqlite:
		return "varchar(255)"
	case Mysql:
		return fmt.Sprintf("enum(%s)", field.TagSettings["ENUM"])
	}
	return fmt.Sprintf("varchar(255) check (%s in (%s))", field.Name, field.TagSettings["ENUM"])
}

func (e Enum) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	val := "0"
	if e.value {
		val = "1"
	}
	return clause.Expr{SQL: "?", Vars: []interface{}{val}}
}

func (e *Enum) Scan(value interface{}) error {
	switch v := value.(type) {
	case string:
		e.value = v == "1"
	case []byte:
		e.value = string(v) == "1"
	case int64:
		e.value = v == 1
	default:
		return fmt.Errorf("unsupported type for Enum: %T", value)
	}
	return nil
}

func (e Enum) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.value)
}

func (e *Enum) UnmarshalJSON(data []byte) error {
	var v interface{}
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

func (e Enum) Get() bool {
	return e.value
}

func (e *Enum) Set(v bool) {
	e.value = v
}

func (e Enum) String() string {
	if e.value {
		return "1"
	}
	return "0"
}
