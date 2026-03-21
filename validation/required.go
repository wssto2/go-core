package validation

import (
	"reflect"
	"strings"

	"github.com/wssto2/go-core/i18n"
)

func RequiredRule(ctx ValidationContext, attribute string, value any, args string, required bool, fail func(string), subject any) {
	lang := ctx.Locale()
	if lang == "" {
		lang = "en"
	}

	if !isPresent(value) {
		fail(i18n.T("validation_errors.required", lang))
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
