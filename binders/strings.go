package binders

import (
	"reflect"

	"github.com/wssto2/go-core/validation"
)

// BindStrings fills the fields of v that carry the named struct tag (for example
// `path:"id"` or `query:"page"`) from string values, such as URL path
// parameters or the query string. Numbers and booleans are parsed from their
// text; a field with a list type takes every value of its key. Fields without
// the tag, and keys with no value, are left alone.
//
// A value that does not parse yields a validation error naming the key, like
// BindRequest does for a body.
func BindStrings[T any](v *T, tag string, values map[string][]string) error {
	rv := reflect.ValueOf(v).Elem()
	if rv.Kind() != reflect.Struct {
		return nil
	}

	rt := rv.Type()
	failures := make(map[string][]validation.Failure)
	debug := make(map[string][]string)

	for i := range rt.NumField() {
		key := rt.Field(i).Tag.Get(tag)
		if key == "" || key == "-" {
			continue
		}

		vals := values[key]
		if len(vals) == 0 {
			continue
		}

		field := rv.Field(i)

		var raw any = vals[0]
		if isList(field.Type()) {
			list := make([]any, len(vals))
			for j, s := range vals {
				list[j] = s
			}
			raw = list
		}

		if f := coerceValue(field, raw, true); f != nil {
			failures[key] = append(failures[key], *f)
			debug[key] = append(debug[key], "coerce")
		}
	}

	if len(failures) > 0 {
		return validation.NewValidationError("validation failed", failures, debug)
	}

	return nil
}

func isList(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	return t.Kind() == reflect.Slice
}
