package binders

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// validateField runs all stateless validation rules for a single field.
// Returns two parallel slices: human-readable messages and rule names.
// Empty slices mean the field is valid.
//
// rawVal is the original value from the request (used for required checks).
// allRaw is the full request map (used for required_if cross-field checks).
func validateField(meta *fieldMeta, fieldValue reflect.Value, rawVal any, allRaw map[string]any) (msgs []string, ruleNames []string) {
	for _, r := range meta.rules {
		var msg string

		switch r.name {
		case "required":
			msg = validateRequired(rawVal)
		case "required_if":
			msg = validateRequiredIf(r.param, rawVal, allRaw)
		case "max":
			msg = validateMax(fieldValue, r.param)
		case "min":
			msg = validateMin(fieldValue, r.param)
		case "in":
			msg = validateIn(fieldValue, r.param)
		case "email":
			msg = validateEmail(fieldValue)
		case "date":
			msg = validateDate(fieldValue)
		default:
			// Unknown rule in the binder — skip silently.
			// The validator package handles app-specific rules (exists, vin, etc.).
			// If the rule is unknown in BOTH binder and validator, validator.Validate
			// will return ErrUnknownRule.
			continue
		}

		if msg != "" {
			msgs = append(msgs, msg)
			ruleNames = append(ruleNames, r.name)
		}
	}

	return msgs, ruleNames
}

func validateRequired(raw any) string {
	if raw == nil {
		return "field is required"
	}
	if s, ok := raw.(string); ok && strings.TrimSpace(s) == "" {
		return "field is required"
	}
	return ""
}

// validateRequiredIf handles "required_if:otherField,expectedValue".
// The field is required only when otherField == expectedValue in the request.
//
// Example: "required_if:vrsta,1" → required when vrsta field equals 1.
func validateRequiredIf(param string, raw any, allRaw map[string]any) string {
	otherKey, expectedVal, ok := strings.Cut(param, ",")
	if !ok {
		return "" // malformed rule — skip
	}

	otherRaw, exists := allRaw[otherKey]
	if !exists {
		return ""
	}

	if rawToString(otherRaw) != strings.TrimSpace(expectedVal) {
		return "" // condition not met
	}

	return validateRequired(raw)
}

// validateMax: "max:N" → string max rune count, number max value, slice max length.
func validateMax(field reflect.Value, param string) string {
	n, err := strconv.ParseFloat(param, 64)
	if err != nil {
		return ""
	}
	switch field.Kind() {
	case reflect.String:
		if float64(len([]rune(field.String()))) > n {
			return "must be at most " + param + " characters"
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if float64(field.Int()) > n {
			return "must be at most " + param
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if float64(field.Uint()) > n {
			return "must be at most " + param
		}
	case reflect.Float32, reflect.Float64:
		if field.Float() > n {
			return "must be at most " + param
		}
	case reflect.Slice:
		if float64(field.Len()) > n {
			return "must have at most " + param + " items"
		}
	}
	return ""
}

// validateMin: "min:N" → string min rune count, number min value.
func validateMin(field reflect.Value, param string) string {
	n, err := strconv.ParseFloat(param, 64)
	if err != nil {
		return ""
	}
	switch field.Kind() {
	case reflect.String:
		if float64(len([]rune(field.String()))) < n {
			return "must be at least " + param + " characters"
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if float64(field.Int()) < n {
			return "must be at least " + param
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if float64(field.Uint()) < n {
			return "must be at least " + param
		}
	case reflect.Float32, reflect.Float64:
		if field.Float() < n {
			return "must be at least " + param
		}
	}
	return ""
}

// validateIn: "in:a,b,c" → field value must match one of the comma-separated options.
func validateIn(field reflect.Value, param string) string {
	current := rawToString(field.Interface())
	for _, v := range strings.Split(param, ",") {
		if strings.TrimSpace(v) == current {
			return ""
		}
	}
	return "must be one of: " + param
}

// validEmailRegexp is the single source of truth for email validation.
// Shared with validator.IsValidEmail via the same regexp — both must stay in sync.
// If you change this, update the copy in validator/helpers.go too.
var validEmailRegexp = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// validateEmail: empty string passes (combine with "required" for mandatory fields).
func validateEmail(field reflect.Value) string {
	if field.Kind() != reflect.String {
		return ""
	}
	val := field.String()
	if val == "" {
		return ""
	}
	if !validEmailRegexp.MatchString(val) {
		return "must be a valid email address"
	}
	return ""
}

var supportedDateFormats = []string{
	"2006-01-02", // ISO: YYYY-MM-DD
	"02.01.2006", // European: DD.MM.YYYY
	"01/02/2006", // US: MM/DD/YYYY
}

// validateDate: empty string passes (combine with "required" for mandatory fields).
func validateDate(field reflect.Value) string {
	if field.Kind() != reflect.String {
		return ""
	}
	val := strings.TrimSpace(field.String())
	if val == "" {
		return ""
	}
	for _, format := range supportedDateFormats {
		if _, err := time.Parse(format, val); err == nil {
			return ""
		}
	}
	return "must be a valid date (YYYY-MM-DD)"
}

// rawToString converts a JSON-parsed value to its string representation.
// Used for cross-field comparisons (required_if) and in-list checks.
func rawToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", val)
	default:
		return ""
	}
}
