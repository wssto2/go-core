package binders

import (
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

// coerceMap handles JSON objects → map fields.
func coerceMap(field reflect.Value, v map[string]interface{}) *validation.Failure {
	if field.Kind() != reflect.Map {
		return utils.Ptr(validation.Fail(validation.CodeMustBeObject))
	}

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
