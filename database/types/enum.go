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

type Enum struct {
	value string
}

func NewEnum(value string) Enum {
	return Enum{value: value}
}

func (e Enum) GormDataType() string {
	return "enum"
}

func (e Enum) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Name() {
	case database.DriverSQLite:
		return "varchar(255)"
	case database.DriverMySQL:
		return fmt.Sprintf("enum(%s)", field.TagSettings["ENUM"])
	}
	return fmt.Sprintf("varchar(255) check (%s in (%s))", field.Name, field.TagSettings["ENUM"])
}

func (e Enum) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	return clause.Expr{SQL: "?", Vars: []interface{}{e.value}}
}

func (e *Enum) Scan(value interface{}) error {
	switch typedValue := value.(type) {
	case string:
		e.value = typedValue
	case []byte:
		e.value = string(typedValue)
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
	switch typedValue := v.(type) {
	case string:
		e.value = typedValue
	case []byte:
		e.value = string(typedValue)
	default:
		return fmt.Errorf("unsupported type for Enum: %T", v)
	}

	return nil
}

func (e Enum) Get() string {
	return e.value
}

func (e *Enum) Set(v string) {
	e.value = v
}
