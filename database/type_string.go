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
 * String
 */
type String struct {
	value string `json:"-"`
}

func NewString(value string) String {
	return String{
		value: value,
	}
}

func (i *String) Get() string {
	return i.value
}

func (i *String) Set(value string) {
	i.value = value
}

func (i String) Value() (driver.Value, error) {
	return i.value, nil
}

func (i String) GormDataType() string {
	return MySQLString
}

func (i String) GormDBDataType(db *gorm.DB, field *schema.Field) string {
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

func (i String) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	return clause.Expr{
		SQL:  "?",
		Vars: []interface{}{i.value},
	}
}

func (i *String) Scan(value interface{}) error {
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

func (i String) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(i.value)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return data, nil
}

func (i *String) UnmarshalJSON(data []byte) error {
	if string(data) == Null {
		i.value = ""

		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("failed to unmarshal JSON data %s: %w", string(data), err)
	}

	i.value = value

	return nil
}

func (i *String) IsEmpty() bool {
	return i.value == ""
}

func (i *String) IsNotEmpty() bool {
	return !i.IsEmpty()
}

func (i *String) IsEqual(value string) bool {
	return i.value == value
}

func (i *String) IsNotEqual(value string) bool {
	return i.value != value
}

func (i *String) Trim() {
	i.value = strings.TrimSpace(i.value)
}

func (i *String) ToUpper() {
	i.value = strings.ToUpper(i.value)
}

func (i *String) ToLower() {
	i.value = strings.ToLower(i.value)
}

func (i *String) Concat(value string) {
	i.value = strings.Join([]string{i.value, value}, "")
}

func (i *String) Split(separator string) []string {
	return strings.Split(i.value, separator)
}

func (i *String) Substring(start, end int) string {
	if start < 0 || end > len(i.value) || start > end {
		return ""
	}
	return i.value[start:end]
}

func (i *String) Cut(length int) string {
	if length > len(i.value) {
		return i.value
	}

	return i.value[:length]
}

func (i *String) Length() int {
	return len(i.value)
}

func (i *String) Contains(substr string) bool {
	return strings.Contains(i.value, substr)
}

func (i *String) StartsWith(substr string) bool {
	return strings.HasPrefix(i.value, substr)
}

func (i *String) EndsWith(substr string) bool {
	return strings.HasSuffix(i.value, substr)
}

func (i *String) ReplaceAll(old, new string) {
	i.value = strings.ReplaceAll(i.value, old, new)
}

func (i *String) Replace(old, new string) {
	i.value = strings.ReplaceAll(i.value, old, new)
}

func (i *String) GetInt() int {
	value, err := strconv.Atoi(i.value)
	if err != nil {
		return 0
	}

	return value
}
