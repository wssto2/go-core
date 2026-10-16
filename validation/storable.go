package validation

import (
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CodeUnsupportedCharacters is the failure code of a string holding a character a utf8mb3 column cannot store.
// The application translates it; go-core carries no message text for it.
const CodeUnsupportedCharacters RuleCode = "unsupported_characters"

// HasUnsupportedChars reports whether s holds a character outside the Basic Multilingual Plane, one UTF-8 encodes in
// four bytes (emoji and other supplementary-plane symbols). Invalid bytes read as U+FFFD, three bytes, and pass.
func HasUnsupportedChars(s string) bool {
	for _, r := range s {
		if utf8.RuneLen(r) == utf8.UTFMax {
			return true
		}
	}

	return false
}

// CheckStorable is an opt-in check for an input whose strings land in utf8mb3 columns, which hold only the Basic
// Multilingual Plane: MySQL refuses a 4-byte character, and the whole write fails. Call it from the input's Validate
// method, after or instead of ValidateInput, and return its result as it is.
//
// It is not part of ValidateInput on purpose: go-core's databases default to utf8mb4, which stores any character, so
// only an application on legacy utf8mb3 tables needs it.
//
// It ignores validation tags and checks every string and *string field it can reach: it walks in (a struct or a
// pointer to one) through nested structs, pointers, interfaces, slices and arrays (a map is not walked), skipping
// unexported fields and `json:"-"`. A field is named by its json name, nested as "parent.child" and "list.0.child".
// It returns a *ValidationError with one CodeUnsupportedCharacters failure on each field that holds such a
// character, or nil when there is none.
func CheckStorable(in any) error {
	var fields []string

	collectUnstorable(reflect.ValueOf(in), "", &fields)

	if len(fields) == 0 {
		return nil
	}

	failures := make(map[string][]Failure, len(fields))
	debug := make(map[string][]string, len(fields))

	for _, f := range fields {
		failures[f] = []Failure{Fail(CodeUnsupportedCharacters)}
		debug[f] = []string{string(CodeUnsupportedCharacters)}
	}

	return NewValidationError("validation failed", failures, debug)
}

func collectUnstorable(val reflect.Value, prefix string, fields *[]string) {
	for val.Kind() == reflect.Pointer || val.Kind() == reflect.Interface {
		if val.IsNil() {
			return
		}

		val = val.Elem()
	}

	switch val.Kind() { //nolint:exhaustive // only strings and their containers can hold text
	case reflect.String:
		if HasUnsupportedChars(val.String()) {
			*fields = append(*fields, prefix)
		}
	case reflect.Struct:
		collectUnstorableStruct(val, prefix, fields)
	case reflect.Slice, reflect.Array:
		for idx := range val.Len() {
			collectUnstorable(val.Index(idx), joinStorablePath(prefix, strconv.Itoa(idx)), fields)
		}
	}
}

func collectUnstorableStruct(val reflect.Value, prefix string, fields *[]string) {
	t := val.Type()

	for idx := range t.NumField() {
		sf := t.Field(idx)
		if !sf.IsExported() {
			continue
		}

		if name := storableJSONName(sf); name != "-" {
			collectUnstorable(val.Field(idx), joinStorablePath(prefix, name), fields)
		}
	}
}

func storableJSONName(sf reflect.StructField) string {
	name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
	if name == "" {
		return sf.Name
	}

	return name
}

func joinStorablePath(prefix, name string) string {
	if prefix == "" {
		return name
	}

	return prefix + "." + name
}
