package validation

import (
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/utils"
)

var (
	passwordLowercasePattern = regexp.MustCompile(`[a-z]`)
	passwordDigitPattern     = regexp.MustCompile(`\d`)
	passwordSpecialPattern   = regexp.MustCompile(`[@$!%*?&#]`)
	yearPattern              = regexp.MustCompile(`^\d{4}$`)
	monthPattern             = regexp.MustCompile(`^(0?[1-9]|1[0-2])$`)
)

func RequiredRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	if !isPresent(value) {
		fail(Fail(CodeRequired))
	}
}

// RequiredIfRule implements the required_if validation rule.
// The field under validation must be present and not empty if another field
// identified by form/json tag or Go field name is equal to a specific value.
// Rule syntax: required_if:other_field,value
func RequiredIfRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	otherField, expectedValue, err := parseRequiredIfArgs(attribute, args)
	if err != nil {
		panic(apperr.Internal(err))
	}

	otherValue, found, err := lookupSubjectFieldValue(subject, otherField)
	if err != nil {
		panic(apperr.Internal(err))
	}
	if !found {
		panic(apperr.Internal(NewErrInvalidRuleConfig("required_if", attribute, fmt.Sprintf("field %q was not found on subject", otherField))))
	}

	if fmt.Sprintf("%v", otherValue) == expectedValue && !isPresent(value) {
		fail(Fail(CodeRequiredIf))
	}
}

func EmailRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	valueStr, ok := value.(string)
	if !ok {
		if value == nil && !required {
			return
		}
		fail(Fail(CodeEmail))
		return
	}

	if valueStr == "" && !required {
		return
	}

	if !utils.IsValidEmail(valueStr) {
		fail(Fail(CodeEmail))
	}
}

func LenRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	if !isPresent(value) && !required {
		return
	}

	expectedLen, err := parseIntegerRuleArg("len", attribute, args)
	if err != nil {
		panic(apperr.Internal(err))
	}

	actualLen, ok := lengthOf(value)
	if !ok {
		fail(Fail(CodeLen))
		return
	}

	if actualLen != expectedLen {
		fail(FailWith(CodeLen, map[string]any{"len": expectedLen}))
	}
}

func BetweenRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	if !isPresent(value) && !required {
		return
	}

	min, max, err := parseBetweenArgs(attribute, args)
	if err != nil {
		panic(apperr.Internal(err))
	}

	switch v := value.(type) {
	case string:
		length := len([]rune(v))
		if length < min || length > max {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	case int:
		if v < min || v > max {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	case int8:
		if int(v) < min || int(v) > max {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	case int16:
		if int(v) < min || int(v) > max {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	case int32:
		if int(v) < min || int(v) > max {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	case int64:
		if v < int64(min) || v > int64(max) {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	case uint:
		if v < uint(min) || v > uint(max) {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	case uint8:
		if v < uint8(min) || v > uint8(max) {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	case uint16:
		if v < uint16(min) || v > uint16(max) {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	case uint32:
		if v < uint32(min) || v > uint32(max) {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	case uint64:
		if v < uint64(min) || v > uint64(max) {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	case float32:
		if float64(v) < float64(min) || float64(v) > float64(max) {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	case float64:
		if v < float64(min) || v > float64(max) {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
		return
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if rv.Int() < int64(min) || rv.Int() > int64(max) {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if rv.Uint() < uint64(min) || rv.Uint() > uint64(max) {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
	case reflect.Float32, reflect.Float64:
		if rv.Float() < float64(min) || rv.Float() > float64(max) {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
	case reflect.Slice, reflect.Map, reflect.Array:
		if rv.Len() < min || rv.Len() > max {
			fail(FailWith(CodeBetween, map[string]any{"min": min, "max": max}))
		}
	default:
		fail(Fail(CodeBetween))
	}
}

func SameRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	otherField := strings.TrimSpace(args)
	if otherField == "" {
		panic(apperr.Internal(NewErrInvalidRuleConfig("same", attribute, "missing comparison field")))
	}

	otherValue, found, err := lookupSubjectFieldValue(subject, otherField)
	if err != nil {
		panic(apperr.Internal(err))
	}
	if !found {
		panic(apperr.Internal(NewErrInvalidRuleConfig("same", attribute, fmt.Sprintf("field %q was not found on subject", otherField))))
	}

	if !reflect.DeepEqual(value, otherValue) {
		fail(Fail(CodeSame))
	}
}

func DifferentRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	otherField := strings.TrimSpace(args)
	if otherField == "" {
		panic(apperr.Internal(NewErrInvalidRuleConfig("different", attribute, "missing comparison field")))
	}

	otherValue, found, err := lookupSubjectFieldValue(subject, otherField)
	if err != nil {
		panic(apperr.Internal(err))
	}
	if !found {
		panic(apperr.Internal(NewErrInvalidRuleConfig("different", attribute, fmt.Sprintf("field %q was not found on subject", otherField))))
	}

	if reflect.DeepEqual(value, otherValue) {
		fail(Fail(CodeDifferent))
	}
}

func URLRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	str, ok := value.(string)
	if !ok {
		if !isPresent(value) && !required {
			return
		}
		fail(Fail(CodeURL))
		return
	}
	if str == "" && !required {
		return
	}

	parsed, err := url.ParseRequestURI(str)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		fail(Fail(CodeURL))
	}
}

func UUIDRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	str, ok := value.(string)
	if !ok {
		if !isPresent(value) && !required {
			return
		}
		fail(Fail(CodeUUID))
		return
	}
	if str == "" && !required {
		return
	}

	if _, err := uuid.Parse(str); err != nil {
		fail(Fail(CodeUUID))
	}
}

func isPresent(value any) bool {
	if value == nil {
		return false
	}

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.String:
		return strings.TrimSpace(v.String()) != ""
	case reflect.Slice, reflect.Map, reflect.Array:
		return v.Len() > 0
	case reflect.Ptr, reflect.Interface:
		return !v.IsNil()
	default:
		return true
	}
}

func MinRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	if !isPresent(value) && !required {
		return
	}

	min, err := parseIntegerRuleArg("min", attribute, args)
	if err != nil {
		panic(apperr.Internal(err))
	}

	switch v := value.(type) {
	case string:
		if len([]rune(v)) < min {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case int:
		if v < min {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case int8:
		if int(v) < min {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case int16:
		if int(v) < min {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case int32:
		if int(v) < min {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case int64:
		if v < int64(min) {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case uint:
		if v < uint(min) {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case uint8:
		if v < uint8(min) {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case uint16:
		if v < uint16(min) {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case uint32:
		if v < uint32(min) {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case uint64:
		if v < uint64(min) {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case float32:
		if float64(v) < float64(min) {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case float64:
		if v < float64(min) {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	default:
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if rv.Int() < int64(min) {
				fail(FailWith(CodeMin, map[string]any{"min": min}))
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			if rv.Uint() < uint64(min) {
				fail(FailWith(CodeMin, map[string]any{"min": min}))
			}
		case reflect.Float32, reflect.Float64:
			if rv.Float() < float64(min) {
				fail(FailWith(CodeMin, map[string]any{"min": min}))
			}
		case reflect.Slice, reflect.Map, reflect.Array, reflect.String:
			if rv.Len() < min {
				fail(FailWith(CodeMin, map[string]any{"min": min}))
			}
		}
	}
}

func MaxRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	if !isPresent(value) && !required {
		return
	}

	max, err := parseIntegerRuleArg("max", attribute, args)
	if err != nil {
		panic(apperr.Internal(err))
	}

	switch v := value.(type) {
	case string:
		if len([]rune(v)) > max {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case int:
		if v > max {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case int8:
		if int(v) > max {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case int16:
		if int(v) > max {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case int32:
		if int(v) > max {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case int64:
		if v > int64(max) {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case uint:
		if v > uint(max) {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case uint8:
		if v > uint8(max) {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case uint16:
		if v > uint16(max) {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case uint32:
		if v > uint32(max) {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case uint64:
		if v > uint64(max) {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case float32:
		if float64(v) > float64(max) {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case float64:
		if v > float64(max) {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	default:
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if rv.Int() > int64(max) {
				fail(FailWith(CodeMax, map[string]any{"max": max}))
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			if rv.Uint() > uint64(max) {
				fail(FailWith(CodeMax, map[string]any{"max": max}))
			}
		case reflect.Float32, reflect.Float64:
			if rv.Float() > float64(max) {
				fail(FailWith(CodeMax, map[string]any{"max": max}))
			}
		case reflect.Slice, reflect.Map, reflect.Array, reflect.String:
			if rv.Len() > max {
				fail(FailWith(CodeMax, map[string]any{"max": max}))
			}
		}
	}
}

func InRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	if !isPresent(value) && !required {
		return
	}
	if args == "" {
		panic(apperr.Internal(NewErrInvalidRuleConfig("in", attribute, "missing allowed values")))
	}

	allowed := strings.Split(args, "|")
	str := fmt.Sprintf("%v", value)
	for _, a := range allowed {
		if str == a {
			return
		}
	}
	fail(FailWith(CodeIn, map[string]any{"in": allowed}))
}

func DateRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	str, ok := value.(string)
	if !ok {
		if !isPresent(value) && !required {
			return
		}
		fail(Fail(CodeDate))
		return
	}
	if str == "" && !required {
		return
	}
	if _, err := time.Parse(time.DateOnly, str); err != nil {
		fail(Fail(CodeDate))
	}
}

func DateTimeRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	str, ok := value.(string)
	if !ok {
		if !isPresent(value) && !required {
			return
		}
		fail(Fail(CodeDate))
		return
	}
	if str == "" && !required {
		return
	}
	if _, err := time.Parse(time.RFC3339, str); err != nil {
		fail(Fail(CodeDate))
	}
}

// YearRule validates that the value is a valid year (e.g. "2024").
func YearRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	str, ok := value.(string)
	if !ok {
		if !isPresent(value) && !required {
			return
		}
		fail(Fail(CodeYear))
		return
	}
	if str == "" && !required {
		return
	}
	if !yearPattern.MatchString(str) {
		fail(Fail(CodeYear))
		return
	}
	if _, err := time.Parse("2006", str); err != nil {
		fail(Fail(CodeYear))
	}
}

// MonthRule validates that the value is a valid month (e.g. "05" or "5").
func MonthRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	str, ok := value.(string)
	if !ok {
		if !isPresent(value) && !required {
			return
		}
		fail(Fail(CodeMonth))
		return
	}
	if str == "" && !required {
		return
	}
	if !monthPattern.MatchString(str) {
		fail(Fail(CodeMonth))
	}
}

// PasswordRule validates that the value is a valid password.
func PasswordRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	str, ok := value.(string)
	if !ok {
		if !isPresent(value) && !required {
			return
		}
		fail(Fail(CodePassword))
		return
	}
	if str == "" && !required {
		return
	}

	valid := false
	if len(str) >= 8 && len(str) <= 20 {
		hasLowercase := passwordLowercasePattern.MatchString(str)
		hasDigits := len(passwordDigitPattern.FindAllString(str, -1)) >= 2
		hasSpecial := passwordSpecialPattern.MatchString(str)
		valid = hasLowercase && hasDigits && hasSpecial
	}

	if !valid {
		fail(Fail(CodePassword))
	}
}

// ConfirmedRule validates that the value is the same as the other field.
// Rule syntax: confirmed:{field}
func ConfirmedRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	otherField := strings.TrimSpace(args)
	if otherField == "" {
		panic(apperr.Internal(NewErrInvalidRuleConfig("confirmed", attribute, "missing comparison field")))
	}

	otherValue, found, err := lookupSubjectFieldValue(subject, otherField)
	if err != nil {
		panic(apperr.Internal(err))
	}
	if !found {
		panic(apperr.Internal(NewErrInvalidRuleConfig("confirmed", attribute, fmt.Sprintf("field %q was not found on subject", otherField))))
	}

	if !reflect.DeepEqual(value, otherValue) {
		fail(Fail(CodeConfirmed))
	}
}

func parseIntegerRuleArg(ruleName, attribute, args string) (int, error) {
	if strings.TrimSpace(args) == "" {
		return 0, NewErrInvalidRuleConfig(ruleName, attribute, "missing numeric argument")
	}

	value, err := strconv.Atoi(strings.TrimSpace(args))
	if err != nil {
		return 0, NewErrInvalidRuleConfig(ruleName, attribute, fmt.Sprintf("invalid numeric argument %q", args))
	}

	return value, nil
}

func parseBetweenArgs(attribute, args string) (int, int, error) {
	parts := strings.SplitN(args, ",", 2)
	if len(parts) != 2 {
		return 0, 0, NewErrInvalidRuleConfig("between", attribute, "expected format min,max")
	}

	min, err := parseIntegerRuleArg("between", attribute, parts[0])
	if err != nil {
		return 0, 0, err
	}
	max, err := parseIntegerRuleArg("between", attribute, parts[1])
	if err != nil {
		return 0, 0, err
	}
	if min > max {
		return 0, 0, NewErrInvalidRuleConfig("between", attribute, "min cannot be greater than max")
	}

	return min, max, nil
}

func parseRequiredIfArgs(attribute, args string) (string, string, error) {
	parts := strings.SplitN(args, ",", 2)
	if len(parts) != 2 {
		return "", "", NewErrInvalidRuleConfig("required_if", attribute, "expected format other_field,value")
	}

	otherField := strings.TrimSpace(parts[0])
	expectedValue := strings.TrimSpace(parts[1])
	if otherField == "" {
		return "", "", NewErrInvalidRuleConfig("required_if", attribute, "missing comparison field")
	}

	return otherField, expectedValue, nil
}

func lookupSubjectFieldValue(subject any, name string) (any, bool, error) {
	subjectVal := reflect.ValueOf(subject)
	if !subjectVal.IsValid() {
		return nil, false, NewErrInvalidRuleConfig("cross_field", name, "subject is nil")
	}

	if subjectVal.Kind() == reflect.Ptr {
		if subjectVal.IsNil() {
			return nil, false, NewErrInvalidRuleConfig("cross_field", name, "subject is nil")
		}
		subjectVal = subjectVal.Elem()
	}

	if subjectVal.Kind() != reflect.Struct {
		return nil, false, NewErrInvalidRuleConfig("cross_field", name, fmt.Sprintf("subject must be a struct, got %s", subjectVal.Kind()))
	}

	meta := globalMetaCache.get(subjectVal.Type())
	idx, ok := meta.NameLookup[name]
	if !ok {
		return nil, false, nil
	}

	fm := meta.Fields[idx]
	return subjectVal.FieldByIndex(fm.FieldIndex).Interface(), true, nil
}

func lengthOf(value any) (int, bool) {
	switch v := value.(type) {
	case string:
		return len([]rune(v)), true
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.String:
		return len([]rune(rv.String())), true
	case reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len(), true
	default:
		return 0, false
	}
}
