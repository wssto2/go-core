package binders

import (
	"reflect"
	"strconv"
)

// coerceValue sets field to value, coercing types as needed.
// Returns an error message if coercion fails, empty string on success.
//
// JSON parsing always produces: string, float64, bool, nil, []interface{}, map[string]interface{}
// Multipart parsing always produces strings for scalar values.
// This function handles both cases uniformly.
func coerceValue(field reflect.Value, value any, isMultipart bool) string {
	if value == nil {
		// Explicit null — leave field at its zero value.
		return ""
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
		return "unsupported value type"
	}
}

// coerceString handles string → target kind.
// For JSON (isMultipart=false): strings must stay strings. Sending "42" for an int field is a type error.
// For multipart (isMultipart=true): everything arrives as a string, so we parse numerics and booleans.
func coerceString(field reflect.Value, v string, isMultipart bool) string {
	switch field.Kind() {
	case reflect.String:
		field.SetString(v)
		return ""

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if !isMultipart {
			return "must be a number"
		}
		if v == "null" || v == "" {
			field.Set(reflect.Zero(field.Type()))
			return ""
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return "must be a number"
		}
		field.SetInt(n)
		return ""

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if !isMultipart {
			return "must be a positive number"
		}
		if v == "null" || v == "" {
			field.Set(reflect.Zero(field.Type()))
			return ""
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return "must be a positive number"
		}
		field.SetUint(n)
		return ""

	case reflect.Float32, reflect.Float64:
		if !isMultipart {
			return "must be a number"
		}
		if v == "null" || v == "" {
			field.Set(reflect.Zero(field.Type()))
			return ""
		}
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "must be a number"
		}
		field.SetFloat(n)
		return ""

	case reflect.Bool:
		if !isMultipart {
			return "must be a boolean"
		}
		switch v {
		case "true", "1":
			field.SetBool(true)
		case "false", "0", "null", "":
			field.SetBool(false)
		default:
			return "must be a boolean"
		}
		return ""

	default:
		return "unsupported field type: " + field.Kind().String()
	}
}

// coerceFloat64 handles JSON number → target kind.
// JSON numbers always decode as float64.
func coerceFloat64(field reflect.Value, v float64) string {
	switch field.Kind() {
	case reflect.Float32, reflect.Float64:
		field.SetFloat(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		field.SetInt(int64(v))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v < 0 {
			return "must be a positive number"
		}
		field.SetUint(uint64(v))
	case reflect.String:
		return "must be a string"
	case reflect.Bool:
		return "must be a boolean"
	default:
		return "unsupported field type: " + field.Kind().String()
	}
	return ""
}

// coerceBool handles JSON boolean → target kind.
func coerceBool(field reflect.Value, v bool) string {
	switch field.Kind() {
	case reflect.Bool:
		field.SetBool(v)
	case reflect.String:
		return "must be a string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "must be a number"
	case reflect.Float32, reflect.Float64:
		return "must be a number"
	default:
		return "unsupported field type: " + field.Kind().String()
	}
	return ""
}

// coerceSlice handles JSON arrays → slice fields.
func coerceSlice(field reflect.Value, v []interface{}, isMultipart bool) string {
	if field.Kind() != reflect.Slice {
		return "must be a list"
	}

	slice := reflect.MakeSlice(field.Type(), len(v), len(v))
	for i, elem := range v {
		msg := coerceValue(slice.Index(i), elem, isMultipart)
		if msg != "" {
			return "item " + strconv.Itoa(i) + ": " + msg
		}
	}

	field.Set(slice)
	return ""
}

// coerceMap handles JSON objects → map fields.
func coerceMap(field reflect.Value, v map[string]interface{}) string {
	if field.Kind() != reflect.Map {
		return "must be an object"
	}

	mapValue := reflect.MakeMap(field.Type())
	for key, val := range v {
		elem := reflect.New(field.Type().Elem()).Elem()
		if msg := coerceValue(elem, val, false); msg != "" {
			return "key " + key + ": " + msg
		}
		mapValue.SetMapIndex(reflect.ValueOf(key), elem)
	}

	field.Set(mapValue)
	return ""
}
