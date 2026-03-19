package database

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"

	"github.com/goccy/go-json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

/**
 * Int
 */
type NullString struct {
	value string `json:"-"`
}

func NewNullString(value string) NullString {
	return NullString{
		value: value,
	}
}

func (i *NullString) Get() string {
	return i.value
}

func (i *NullString) Set(value string) {
	i.value = value
}

func (i NullString) Value() (driver.Value, error) {
	return i.value, nil
}

func (i NullString) GormDataType() string {
	return MySQLString
}

func (i NullString) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Name() {
	case DriverSQLite:
		return SQLiteString

	case DriverMySQL:
		if field.TagSettings["TYPE"] == "" {
			return MySQLString
		}

		return field.TagSettings["TYPE"]
	}

	return field.TagSettings["TYPE"]
}

func (i NullString) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	if i.value == "" {
		return clause.Expr{
			SQL:  "NULL",
			Vars: nil,
		}
	}

	return clause.Expr{
		SQL:  "?",
		Vars: []interface{}{i.value},
	}
}

func (i *NullString) Scan(value interface{}) error {
	switch v := value.(type) {
	case int64:
		i.value = strconv.FormatInt(v, 10)
	case float64:
		i.value = strconv.FormatFloat(v, 'f', -1, 64)
	case []byte:
		i.value = string(v)
	case string:
		i.value = v
	default:
		return fmt.Errorf("unsupported type for String: %T", value)
	}

	return nil
}

func (i NullString) MarshalJSON() ([]byte, error) {
	if i.value == "" {
		return []byte(Null), nil
	}

	data, err := json.Marshal(i.value)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return data, nil
}

func (i *NullString) UnmarshalJSON(data []byte) error {
	if string(data) == Null {
		i.value = ""

		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	i.value = value

	return nil
}

func (i *NullString) IsEmpty() bool {
	return i.value == ""
}

func (i *NullString) IsEqual(value string) bool {
	return i.value == value
}

func (i *NullString) IsNotEqual(value string) bool {
	return i.value != value
}

func (i *NullString) Trim() *NullString {
	i.value = strings.TrimSpace(i.value)

	return i
}

func (i *NullString) ToUpper() *NullString {
	i.value = strings.ToUpper(i.value)

	return i
}

func (i *NullString) ToLower() *NullString {
	i.value = strings.ToLower(i.value)

	return i
}

func (i *NullString) Concat(value string) *NullString {
	i.value = strings.Join([]string{i.value, value}, "")

	return i
}

func (i *NullString) Split(separator string) []string {
	return strings.Split(i.value, separator)
}

func (i *NullString) Substring(start, end int) string {
	if start < 0 || end > len(i.value) || start > end {
		return ""
	}
	return i.value[start:end]
}

func (i *NullString) Cut(length int) string {
	if length > len(i.value) {
		return i.value
	}

	return i.value[:length]
}

func (i *NullString) Length() int {
	return len(i.value)
}

func (i *NullString) Contains(substr string) bool {
	return strings.Contains(i.value, substr)
}

func (i *NullString) StartsWith(substr string) bool {
	return strings.HasPrefix(i.value, substr)
}

func (i *NullString) EndsWith(substr string) bool {
	return strings.HasSuffix(i.value, substr)
}

func (i *NullString) ReplaceAll(old, new string) *NullString {
	i.value = strings.ReplaceAll(i.value, old, new)

	return i
}

func (i *NullString) Replace(old, new string) *NullString {
	i.value = strings.ReplaceAll(i.value, old, new)

	return i
}

func (i *NullString) GetInt() int {
	value, err := strconv.Atoi(i.value)
	if err != nil {
		return 0
	}

	return value
}
