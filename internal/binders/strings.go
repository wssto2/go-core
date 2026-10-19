package binders

import (
	"fmt"
	"maps"
	"net/url"
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

	failures := make(map[string][]validation.Failure)
	debug := make(map[string][]string)

	free := declaredKeys(rv.Type(), tag)
	bindFields(rv, tag, values, free, failures, debug)

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

// bindFields fills the tagged fields of value, going into embedded structs as the
// route's path check does.
func bindFields(
	value reflect.Value, tag string, values map[string][]string, declared map[string]bool,
	failures map[string][]validation.Failure, debug map[string][]string,
) {
	rt := value.Type()

	for idx := range rt.NumField() {
		field := rt.Field(idx)

		if field.Anonymous && field.Type.Kind() == reflect.Struct && field.Tag.Get(tag) == "" {
			bindFields(value.Field(idx), tag, values, declared, failures, debug)

			continue
		}

		if tag == "query" && field.Tag.Get(tag) == FreeForm && field.Type == freeFormType() {
			rest := url.Values{}

			for key, vals := range values {
				if !declared[key] {
					rest[key] = vals
				}
			}

			value.Field(idx).Set(reflect.ValueOf(rest))

			continue
		}

		bindField(value.Field(idx), field.Tag.Get(tag), values, failures, debug)
	}
}

// bindField fills one field from the values under key.
func bindField(
	field reflect.Value, key string, values map[string][]string,
	failures map[string][]validation.Failure, debug map[string][]string,
) {
	vals := values[key]
	if key == "" || key == "-" || len(vals) == 0 {
		return
	}

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

// FreeForm is the tag value of a field that takes the query parameters no other field declares:
//
//	Filters url.Values `json:"-" query:"*"`
const FreeForm = "*"

func freeFormType() reflect.Type { return reflect.TypeFor[url.Values]() }

// declaredKeys is every key that a field of the struct (embedded ones too) names under tag.
func declaredKeys(typ reflect.Type, tag string) map[string]bool {
	keys := map[string]bool{}

	for field := range typ.Fields() {
		name := field.Tag.Get(tag)

		switch {
		case field.Anonymous && field.Type.Kind() == reflect.Struct && name == "":
			maps.Copy(keys, declaredKeys(field.Type, tag))
		case name != "" && name != FreeForm:
			keys[name] = true
		}
	}

	return keys
}

// FreeFormFields lists the fields of the input type (embedded structs included) tagged query:"*".
// More than one is a declaration mistake, and so is one that is not an url.Values: see CheckFreeForm.
func FreeFormFields(typ reflect.Type) []reflect.StructField {
	var out []reflect.StructField

	if typ.Kind() != reflect.Struct {
		return nil
	}

	for field := range typ.Fields() {
		name := field.Tag.Get("query")

		switch {
		case field.Anonymous && field.Type.Kind() == reflect.Struct && name == "":
			out = append(out, FreeFormFields(field.Type)...)
		case name == FreeForm:
			out = append(out, field)
		}
	}

	return out
}

// CheckFreeForm refuses an input with more than one query:"*" field, or one that is not an url.Values.
func CheckFreeForm(typ reflect.Type) error {
	fields := FreeFormFields(typ)

	if len(fields) > 1 {
		return fmt.Errorf("binders: %s has %d query:\"*\" fields, at most one", typ, len(fields)) //nolint:err113 // names it
	}

	if len(fields) == 1 && fields[0].Type != freeFormType() {
		return fmt.Errorf( //nolint:err113 // names the field
			"binders: %s.%s is tagged query:\"*\" but is %s, not url.Values", typ, fields[0].Name, fields[0].Type)
	}

	return nil
}
