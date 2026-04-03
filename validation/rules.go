package validation

import (
	"fmt"
	"reflect"
	"strings"
	"time"

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

func MinRule(attribute string, value any, args string, required bool, fail func(Failure), subject any) {
	if !isPresent(value) && !required {
		return
	}
	if args == "" {
		fail(Fail(CodeMin)) // misconfigured rule — treat as failure
		return
	}
	var min int
	if _, err := fmt.Sscanf(args, "%d", &min); err != nil {
		// misconfigured rule param is a programmer error; treat as validation failure
		// rather than panicking mid-request.
		fail(Fail(CodeMin))
		return
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
	case int64:
		if int(v) < min {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	case float64:
		if int(v) < min {
			fail(FailWith(CodeMin, map[string]any{"min": min}))
		}
	default:
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if int(rv.Int()) < min {
				fail(FailWith(CodeMin, map[string]any{"min": min}))
			}
		case reflect.Float32, reflect.Float64:
			if int(rv.Float()) < min {
				fail(FailWith(CodeMin, map[string]any{"min": min}))
			}
		case reflect.Slice, reflect.Map, reflect.String:
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
	if args == "" {
		fail(Fail(CodeMax))
		return
	}
	var max int
	if _, err := fmt.Sscanf(args, "%d", &max); err != nil {
		// misconfigured rule param is a programmer error; treat as validation failure
		// rather than panicking mid-request.
		fail(Fail(CodeMax))
		return
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
	case int64:
		if int(v) > max {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	case float64:
		if int(v) > max {
			fail(FailWith(CodeMax, map[string]any{"max": max}))
		}
	default:
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if int(rv.Int()) > max {
				fail(FailWith(CodeMax, map[string]any{"max": max}))
			}
		case reflect.Float32, reflect.Float64:
			if int(rv.Float()) > max {
				fail(FailWith(CodeMax, map[string]any{"max": max}))
			}
		case reflect.Slice, reflect.Map, reflect.String:
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
		fail(Fail(CodeIn))
		return
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
