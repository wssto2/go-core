// Package utils provides utility functions for converting types to strings and integers.
package utils

import (
	"fmt"
	"strconv"
)

// ToString converts various types to string.
func ToString[T any](value T) string { //nolint:cyclop
	switch typedValue := any(value).(type) {
	case int:
		return strconv.Itoa(typedValue)
	case int8:
		return strconv.FormatInt(int64(typedValue), 10)
	case int16:
		return strconv.FormatInt(int64(typedValue), 10)
	case int32:
		return strconv.FormatInt(int64(typedValue), 10)
	case int64:
		return strconv.FormatInt(typedValue, 10)
	case uint:
		return strconv.FormatUint(uint64(typedValue), 10)
	case uint8:
		return strconv.FormatUint(uint64(typedValue), 10)
	case uint16:
		return strconv.FormatUint(uint64(typedValue), 10)
	case uint32:
		return strconv.FormatUint(uint64(typedValue), 10)
	case uint64:
		return strconv.FormatUint(typedValue, 10)
	case float32:
		return strconv.FormatFloat(float64(typedValue), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(typedValue, 'f', -1, 64)
	case string:
		return typedValue
	case bool:
		if typedValue {
			return "true"
		}

		return "false"
	case []byte:
		return string(typedValue)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", typedValue)
	}
}

// ToInt converts various types to int.
func ToInt[T int | int8 | int16 | int32 | int64 | uint8 | uint16 | uint32 | float32 | float64 | string](value T) int { //nolint:cyclop,lll
	switch typedValue := any(value).(type) {
	case int:
		return typedValue
	case int8:
		return int(typedValue)
	case int16:
		return int(typedValue)
	case int32:
		return int(typedValue)
	case int64:
		return int(typedValue)
	case uint8:
		return int(typedValue)
	case uint16:
		return int(typedValue)
	case uint32:
		return int(typedValue)
	case float32:
		return int(typedValue)
	case float64:
		return int(typedValue)
	case string:
		i, err := strconv.Atoi(typedValue)
		if err != nil {
			return 0
		}

		return i
	case bool:
		if typedValue {
			return 1
		}

		return 0
	case []byte:
		i, err := strconv.Atoi(string(typedValue))
		if err != nil {
			return 0
		}

		return i
	default:
		return 0
	}
}
