// audit/diff.go
package audit

import (
	"reflect"
	"strings"
)

// Diff returns the field names that differ between two structs.
// Both must be the same type. Uses json tags as field names when present.
//
// Fields tagged with audit:"-" are always excluded from the diff,
// which is useful for system-managed fields like updated_at and updated_by
// that change on every write but carry no semantic diff value.
func Diff(before, after any) []string {
	var changed []string

	beforeVal, bok := structValue(before)
	afterVal, aok := structValue(after)

	// Nothing to compare: a missing state (nil, a nil pointer), a non-struct or two different types.
	if !bok || !aok || beforeVal.Type() != afterVal.Type() {
		return nil
	}

	bt := beforeVal.Type()

	for i := range bt.NumField() {
		field := bt.Field(i)
		// Skip fields explicitly excluded from diffs, and unexported ones (Interface would panic).
		if field.Tag.Get("audit") == "-" || !field.IsExported() {
			continue
		}

		if !reflect.DeepEqual(beforeVal.Field(i).Interface(), afterVal.Field(i).Interface()) {
			tag := field.Tag.Get("json")
			if tag == "" || tag == "-" {
				tag = field.Name
			}
			// strip omitempty etc
			if idx := strings.Index(tag, ","); idx != -1 {
				tag = tag[:idx]
			}
			changed = append(changed, tag)
		}
	}

	return changed
}

// structValue is v as a struct value, through one pointer; false for nil, a nil pointer or a non-struct.
func structValue(v any) (reflect.Value, bool) {
	value := reflect.ValueOf(v)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}, false
		}

		value = value.Elem()
	}

	return value, value.Kind() == reflect.Struct
}
