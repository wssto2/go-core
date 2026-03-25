package validation

import (
	"reflect"
	"strings"

	"github.com/wssto2/go-core/utils"
)

func RequiredRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	if !isPresent(value) {
		fail(Fail(CodeRequired))
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

func isPresent(value any) bool {
	if value == nil {
		return false
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.String:
		return strings.TrimSpace(v.String()) != ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() != 0
	case reflect.Float32, reflect.Float64:
		return v.Float() != 0
	case reflect.Slice, reflect.Map:
		return v.Len() > 0
	case reflect.Ptr, reflect.Interface:
		return !v.IsNil()
	default:
		return true
	}
}
