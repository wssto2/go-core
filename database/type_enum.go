package database

import (
	"context"
	"fmt"

	"github.com/goccy/go-json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

/**
 * Enum
 *
 * Enum is a custom type that is enum in database and boolean in code
 * It is used to store enum values in database and convert them to boolean in code
 * If enum value is 0 it is false, if enum value is 1 it is true.
 */
type Enum struct {
	value bool
}

func NewEnum[T bool | int | string](value T) Enum {

	switch typedValue := any(value).(type) {
	case bool:
		return Enum{
			value: typedValue,
		}
	case int:
		return Enum{
			value: typedValue == 1,
		}
	case string:
		return Enum{
			value: typedValue == "1",
		}
	}

	return Enum{
		value: false,
	}
}

func (e Enum) GormDataType() string {
	return "enum"
}

func (e Enum) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Name() {
	case "sqlite":
		return "varchar(255)" // SQLite doesn't support enum
	case "mysql":
		return fmt.Sprintf("enum(%s)", field.TagSettings["ENUM"])
	case "postgres":
		return fmt.Sprintf("varchar(255) check (%s in (%s))", field.Name, field.TagSettings["ENUM"])
	}

	return fmt.Sprintf("varchar(255) check (%s in (%s))", field.Name, field.TagSettings["ENUM"])
}

func (e Enum) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if e.value {
		return clause.Expr{
			SQL:  "?",
			Vars: []interface{}{"1"},
		}
	}

	return clause.Expr{
		SQL:  "?",
		Vars: []interface{}{"0"},
	}
}

func (e *Enum) Scan(value interface{}) error {
	switch typedValue := value.(type) {
	case string:
		if typedValue == "1" {
			e.value = true
		} else {
			e.value = false
		}
	case []byte:
		if string(typedValue) == "1" {
			e.value = true
		} else {
			e.value = false
		}
	case int:
		if typedValue == 1 {
			e.value = true
		} else {
			e.value = false
		}
	case int64:
		if typedValue == 1 {
			e.value = true
		} else {
			e.value = false
		}
	default:
		return fmt.Errorf("unsupported type: %T", value)
	}

	return nil
}

func (e Enum) MarshalJSON() ([]byte, error) {
	if e.value {
		return []byte(`true`), nil
	}

	return []byte(`false`), nil
}

func (e *Enum) UnmarshalJSON(data []byte) error {
	if string(data) == "true" {
		e.value = true
		return nil
	}

	if string(data) == "false" {
		e.value = false
		return nil
	}

	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	if value == 1 {
		e.value = true
	} else {
		e.value = false
	}

	return nil
}

func (e *Enum) Get() bool {
	return e.value
}

func (e *Enum) Set(value bool) {
	e.value = value
}

func (e *Enum) String() string {
	if e.value {
		return "1"
	}

	return "0"
}

func (e *Enum) IsTrue() bool {
	return e.value
}

func (e *Enum) IsFalse() bool {
	return !e.value
}

func (e *Enum) IsEqual(value bool) bool {
	return e.value == value
}

func (e *Enum) IsNotEqual(value bool) bool {
	return e.value != value
}
