package utils

import (
	"math"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
)

// IfThen returns 'a' if condition is true, otherwise the zero value of T.
// Note: This is less safe than IfThenElse because the zero value might be meaningful.
func IfThen[T any](condition bool, a T) (b T) {
	return IfThenElse(condition, a, b)
}

// IfThenElse returns 'a' if condition is true, otherwise 'b'.
func IfThenElse[T any](condition bool, a T, b T) T {
	if condition {
		return a
	}
	return b
}

// WithDefault returns defaultValue if value is the zero value for its type (0 or "").
// Currently supports int and string.
func WithDefault[T int | string](value T, defaultValue T) T {
	switch typedValue := any(value).(type) {
	case int:
		if typedValue == 0 {
			return defaultValue
		}
	case string:
		if typedValue == "" {
			return defaultValue
		}
	}
	return value
}

// Ptr returns a pointer to the given value.
func Ptr[T any](value T) *T {
	return &value
}

// --- Conversion Helpers ---
func BoolToByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func ByteToBool(b byte) bool {
	return b == 1
}

// --- String Helpers ---

// ValidEmailRegexp is the standard email validation pattern.
var ValidEmailRegexp = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func IsValidEmail(email string) bool {
	return ValidEmailRegexp.MatchString(email)
}

// StringClean truncates a string to a given limit and trims it.
func StringClean(str string, limit int) string {
	if len(str) > limit {
		return strings.TrimSpace(str[:limit])
	}
	return strings.TrimSpace(str)
}

func StringContainsIgnoreCase(str, substr string) bool {
	return strings.Contains(strings.ToLower(str), strings.ToLower(substr))
}

// --- Slice Helpers ---
func StringSliceMergeUnique(original, slice *[]string) []string {
	result := append([]string{}, *original...)
	for _, item := range *slice {
		if !slices.Contains(result, item) {
			result = append(result, item)
		}
	}
	return result
}

// --- Time Helpers ---

// CurrentTimeStr returns the current time formatted as "YYYY-MM-DD HH:MM:SS".
func CurrentTimeStr() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// GetDateRange returns the date range based on the given value keyword.
// Supported: "today", "yesterday", "this_week", "last_week", "this_month", "last_month", "this_year", "last_year".
func GetDateRange(value string) (fromDate, toDate string) {
	now := time.Now()

	switch value {
	case "today":
		fromDate = now.Format("2006-01-02 00:00:00")
		toDate = now.Format("2006-01-02") + " 23:59:59"
	case "yesterday":
		yesterday := now.AddDate(0, 0, -1)
		fromDate = yesterday.Format("2006-01-02 00:00:00")
		toDate = yesterday.Format("2006-01-02") + " 23:59:59"
	case "this_week":
		// Monday is 0, Sunday is 6
		weekDay := int(now.Weekday())
		if weekDay == 0 { // Sunday
			weekDay = 6
		} else {
			weekDay--
		}
		fromDate = now.AddDate(0, 0, -weekDay).Format("2006-01-02 00:00:00")
		toDate = now.AddDate(0, 0, 6-weekDay).Format("2006-01-02") + " 23:59:59"
	case "last_week":
		weekDay := int(now.Weekday())
		if weekDay == 0 { // Sunday
			weekDay = 6
		} else {
			weekDay--
		}
		fromDate = now.AddDate(0, 0, -weekDay-7).Format("2006-01-02 00:00:00")
		toDate = now.AddDate(0, 0, -weekDay-1).Format("2006-01-02") + " 23:59:59"
	case "this_month":
		fromDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02 15:04:05")
		toDate = now.AddDate(0, 1, -now.Day()).Format("2006-01-02") + " 23:59:59"
	case "last_month":
		fromDate = time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02 15:04:05")
		toDate = now.AddDate(0, 0, -now.Day()).Format("2006-01-02") + " 23:59:59"
	case "this_year":
		fromDate = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02 15:04:05")
		toDate = time.Date(now.Year(), 12, 31, 23, 59, 59, 0, now.Location()).Format("2006-01-02 15:04:05")
	case "last_year":
		fromDate = time.Date(now.Year()-1, 1, 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02 15:04:05")
		toDate = time.Date(now.Year()-1, 12, 31, 23, 59, 59, 0, now.Location()).Format("2006-01-02 15:04:05")
	}

	return fromDate, toDate
}

// --- Reflection Helpers (Use with Caution) ---

// Pluck extracts a specific field from a slice of structs.
func Pluck[T any, K any](subject *[]T, key string, destination *[]K) {
	for _, item := range *subject {
		val := reflect.ValueOf(item)
		if val.Kind() == reflect.Struct {
			fieldVal := val.FieldByName(key)
			if fieldVal.IsValid() {
				*destination = append(*destination, fieldVal.Interface().(K))
			}
		}
	}
}

// SliceToMap converts a slice of structs to a slice of maps.
func SliceToMap[T any](slice []T) []map[string]any {
	result := make([]map[string]any, 0, len(slice))
	for _, item := range slice {
		result = append(result, ToMap(item))
	}
	return result
}

// ToMap converts a struct to a map.
func ToMap[T any](item T) map[string]any {
	itemMap := make(map[string]any)
	val := reflect.ValueOf(item)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return itemMap
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldName := typ.Field(i).Name
		itemMap[fieldName] = field.Interface()
	}

	return itemMap
}

// ToJsonMap converts a struct to a map using JSON tags.
func ToJsonMap[T any](item T) map[string]any {
	itemMap := make(map[string]any)
	val := reflect.ValueOf(item)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return itemMap
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// Skip unexported fields
		if !fieldType.IsExported() {
			continue
		}

		jsonTag := fieldType.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		fieldName := strings.Split(jsonTag, ",")[0]
		if fieldName == "" {
			fieldName = fieldType.Name
		}

		itemMap[fieldName] = field.Interface()
	}

	return itemMap
}

// --- Math Helpers ---
func Round(val float64, precision int) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}
