package i18n

import (
	"fmt"
	"strconv"
)

func flattenJSON(prefix, lang string, data any, result map[string]string) {
	switch typedValue := data.(type) {
	case map[string]any:
		for k, val := range typedValue {
			newPrefix := k
			if prefix != "" {
				newPrefix = prefix + "." + k
			}
			flattenJSON(newPrefix, lang, val, result)
		}
	default:
		if prefix != "" {
			result[lang+prefix] = toString(typedValue)
		}
	}
}

// toString renders a decoded JSON scalar as the string a translation holds.
func toString(value any) string { //nolint:cyclop
	switch typedValue := value.(type) {
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
