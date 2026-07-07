package binders

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"

	"github.com/wssto2/go-core/utils"
	"github.com/wssto2/go-core/validation"
)

// coerceValue sets field to value, coercing types as needed.
// Returns an error message if coercion fails, empty string on success.
//
// JSON parsing always produces: string, float64, bool, nil, []interface{}, map[string]interface{}
// Multipart parsing always produces strings for scalar values.
// This function handles both cases uniformly.
func coerceValue(field reflect.Value, value any, isMultipart bool) *validation.Failure {
	if value == nil {
		// Explicit null — leave field at its zero value.
		return nil
	}

	// Pointer fields (used to distinguish "not provided" from an explicit
	// zero, e.g. for nullable DB columns) allocate on first write, then
	// coerce into the pointed-to value.
	if field.Kind() == reflect.Ptr {
		return coercePointer(field, value, isMultipart)
	}

	// Types with custom JSON decoding (e.g. time.Time) are handled by
	// round-tripping the already-decoded value back through their own
	// UnmarshalJSON, rather than by the generic kind-based switch below.
	if unmarshaler, ok := jsonUnmarshaler(field); ok {
		return coerceViaUnmarshaler(unmarshaler, value)
	}

	switch v := value.(type) {
	case string:
		return coerceString(field, v, isMultipart)
	case float64:
		return coerceFloat64(field, v)
	case bool:
		return coerceBool(field, v)
	case []interface{}:
		return coerceSlice(field, v, isMultipart)
	case map[string]interface{}:
		return coerceMap(field, v)
	default:
		return utils.Ptr(validation.Fail(validation.CodeInvalidType))
	}
}

// coercePointer allocates field (if nil) and coerces value into the
// pointed-to element.
func coercePointer(field reflect.Value, value any, isMultipart bool) *validation.Failure {
	if field.IsNil() {
		field.Set(reflect.New(field.Type().Elem()))
	}
	return coerceValue(field.Elem(), value, isMultipart)
}

// jsonUnmarshaler returns field's json.Unmarshaler implementation, if any.
// time.Time is the primary case: JSON parsing decodes it as a plain string,
// so it must be routed through its own UnmarshalJSON rather than the
// generic kind-based switch in coerceValue.
func jsonUnmarshaler(field reflect.Value) (json.Unmarshaler, bool) {
	if !field.CanAddr() {
		return nil, false
	}
	u, ok := field.Addr().Interface().(json.Unmarshaler)
	return u, ok
}

// coerceViaUnmarshaler re-marshals the already-decoded value back to JSON
// bytes and feeds them to u.UnmarshalJSON.
func coerceViaUnmarshaler(u json.Unmarshaler, value any) *validation.Failure {
	raw, err := json.Marshal(value)
	if err != nil {
		return utils.Ptr(validation.Fail(validation.CodeInvalidType))
	}
	if err := u.UnmarshalJSON(raw); err != nil {
		return utils.Ptr(validation.Fail(validation.CodeInvalidType))
	}
	return nil
}

// coerceString handles string → target kind.
// For JSON (isMultipart=false): strings must stay strings. Sending "42" for an int field is a type error.
// For multipart (isMultipart=true): everything arrives as a string, so we parse numerics and booleans.
func coerceString(field reflect.Value, v string, isMultipart bool) *validation.Failure {
	switch field.Kind() {
	case reflect.String:
		field.SetString(v)
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if !isMultipart {
			return utils.Ptr(validation.Fail(validation.CodeMustBeNumber))
		}
		if v == "null" || v == "" {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return utils.Ptr(validation.Fail(validation.CodeMustBeNumber))
		}
		field.SetInt(n)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if !isMultipart {
			return utils.Ptr(validation.Fail(validation.CodeMustBePositiveNumber))
		}
		if v == "null" || v == "" {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return utils.Ptr(validation.Fail(validation.CodeMustBePositiveNumber))
		}
		field.SetUint(n)
		return nil

	case reflect.Float32, reflect.Float64:
		if !isMultipart {
			return utils.Ptr(validation.Fail(validation.CodeMustBeNumber))
		}
		if v == "null" || v == "" {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return utils.Ptr(validation.Fail(validation.CodeMustBeNumber))
		}
		field.SetFloat(n)
		return nil

	case reflect.Bool:
		if !isMultipart {
			return utils.Ptr(validation.Fail(validation.CodeMustBeBoolean))
		}
		switch v {
		case "true", "1":
			field.SetBool(true)
		case "false", "0", "null", "":
			field.SetBool(false)
		default:
			return utils.Ptr(validation.Fail(validation.CodeMustBeBoolean))
		}
		return nil

	default:
		return utils.Ptr(validation.Fail(validation.CodeInvalidType))
	}
}

// coerceFloat64 handles JSON number → target kind.
// JSON numbers always decode as float64.
func coerceFloat64(field reflect.Value, v float64) *validation.Failure {
	switch field.Kind() {
	case reflect.Float32, reflect.Float64:
		field.SetFloat(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v != math.Trunc(v) {
			return utils.Ptr(validation.Fail(validation.CodeMustBeNumber))
		}
		field.SetInt(int64(v))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v < 0 {
			return utils.Ptr(validation.Fail(validation.CodeMustBePositiveNumber))
		}
		field.SetUint(uint64(v))
	case reflect.String:
		return utils.Ptr(validation.Fail(validation.CodeMustBeString))
	case reflect.Bool:
		return utils.Ptr(validation.Fail(validation.CodeMustBeBoolean))
	default:
		return utils.Ptr(validation.Fail(validation.CodeInvalidType))
	}
	return nil
}

// coerceBool handles JSON boolean → target kind.
func coerceBool(field reflect.Value, v bool) *validation.Failure {
	switch field.Kind() {
	case reflect.Bool:
		field.SetBool(v)
	case reflect.String:
		return utils.Ptr(validation.Fail(validation.CodeMustBeString))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return utils.Ptr(validation.Fail(validation.CodeMustBeNumber))
	case reflect.Float32, reflect.Float64:
		return utils.Ptr(validation.Fail(validation.CodeMustBeNumber))
	default:
		return utils.Ptr(validation.Fail(validation.CodeInvalidType))
	}
	return nil
}

// coerceSlice handles JSON arrays → slice fields.
func coerceSlice(field reflect.Value, v []interface{}, isMultipart bool) *validation.Failure {
	if field.Kind() != reflect.Slice {
		return utils.Ptr(validation.Fail(validation.CodeMustBeList))
	}

	slice := reflect.MakeSlice(field.Type(), len(v), len(v))
	for i, elem := range v {
		msg := coerceValue(slice.Index(i), elem, isMultipart)
		if msg != nil {
			return msg
		}
	}

	field.Set(slice)
	return nil
}

// coerceMap handles JSON objects → map or struct fields.
func coerceMap(field reflect.Value, v map[string]interface{}) *validation.Failure {
	switch field.Kind() {
	case reflect.Map:
		return coerceMapIntoMap(field, v)
	case reflect.Struct:
		return coerceMapIntoStruct(field, v)
	default:
		return utils.Ptr(validation.Fail(validation.CodeMustBeObject))
	}
}

func coerceMapIntoMap(field reflect.Value, v map[string]interface{}) *validation.Failure {
	mapValue := reflect.MakeMap(field.Type())
	for key, val := range v {
		elem := reflect.New(field.Type().Elem()).Elem()
		if msg := coerceValue(elem, val, false); msg != nil {
			return msg
		}
		mapValue.SetMapIndex(reflect.ValueOf(key), elem)
	}

	field.Set(mapValue)
	return nil
}

func coerceMapIntoStruct(field reflect.Value, v map[string]interface{}) *validation.Failure {
	for _, meta := range getFieldMeta(field.Type()) {
		rawVal, present := v[meta.formKey]
		if !present || rawVal == nil {
			continue
		}

		if msg := coerceValue(field.Field(meta.index), rawVal, false); msg != nil {
			return msg
		}
	}

	return nil
}
