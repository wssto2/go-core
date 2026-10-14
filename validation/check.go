package validation

import (
	"fmt"
	"reflect"
	"strings"
)

// CheckRules reports a validation rule, on the type of any of the values or on a struct nested in
// it, that the validator does not know: a typo, or a rule from another library such as gte=0. At
// request time such a rule fails every request with 500, so call this at start-up (routes do) or in
// a test over an application's other inputs. The message names the type, the field and the rule.
func CheckRules(values ...any) error {
	v := New()

	for _, value := range values {
		t, ok := value.(reflect.Type)
		if !ok {
			t = reflect.TypeOf(value)
		}

		if err := v.checkType(t, map[reflect.Type]bool{}); err != nil {
			return err
		}
	}

	return nil
}

func (v *Validator) checkType(t reflect.Type, seen map[reflect.Type]bool) error {
	for t != nil && (t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
		t = t.Elem()
	}

	if t == nil || t.Kind() != reflect.Struct || seen[t] {
		return nil
	}

	seen[t] = true

	for _, f := range globalMetaCache.get(t).Fields {
		for _, r := range f.Rules {
			if _, ok := v.registry[r.name]; !ok {
				hint := ""
				if strings.ContainsAny(r.name, "=,") {
					hint = " (arguments follow a colon, as in max:10; rules are separated by |)"
				}

				return fmt.Errorf("validation: %s.%s has the unknown rule %q%s", t, f.GoName, r.name, hint)
			}
		}

		if f.ElemType != nil {
			if err := v.checkType(f.ElemType, seen); err != nil {
				return err
			}
		}
	}

	return nil
}
