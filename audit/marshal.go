package audit

import (
	"encoding/json"
	"reflect"
	"strings"
)

// marshal serializes v to JSON. Callers should pre-process with stripAuditFields
// to ensure audit:"-" tagged fields are excluded before marshaling.
func marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// stripAuditFields walks a reflect.Value and returns a plain map[string]any
// for structs, omitting fields tagged audit:"-". Nested structs are walked
// recursively. Non-struct types are returned as their native interface{} value
// so json.Marshal handles them normally.
//
// This must run before Mask() so struct tag information is still available.
func stripAuditFields(rv reflect.Value) any {
	// Dereference pointers.
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	// Unwrap interface values.
	if rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	// Guard against zero/invalid values (e.g. nil interface fields).
	if !rv.IsValid() {
		return nil
	}

	if rv.Kind() != reflect.Struct {
		return rv.Interface()
	}

	rt := rv.Type()
	out := make(map[string]any, rt.NumField())

	for i := range rt.NumField() {
		field := rt.Field(i)

		// Skip unexported fields — calling .Interface() on them panics.
		if !field.IsExported() {
			continue
		}

		if field.Tag.Get("audit") == "-" {
			continue
		}

		// Determine JSON key name.
		key := field.Tag.Get("json")
		if key == "-" {
			continue
		}
		if key == "" {
			key = field.Name
		} else if idx := strings.Index(key, ","); idx != -1 {
			if key[:idx] == "-" {
				continue
			}
			key = key[:idx]
		}

		out[key] = stripAuditFields(rv.Field(i))
	}

	return out
}
