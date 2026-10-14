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
	validator := New()

	for _, value := range values {
		t, ok := value.(reflect.Type)
		if !ok {
			t = reflect.TypeOf(value)
		}

		err := validator.checkType(t, map[reflect.Type]bool{})
		if err != nil {
			return err
		}
	}

	return nil
}

func (v *Validator) checkType(typ reflect.Type, seen map[reflect.Type]bool) error {
	typ = innerStructType(typ)
	if typ == nil || seen[typ] {
		return nil
	}

	seen[typ] = true

	for _, field := range globalMetaCache.get(typ).Fields {
		for _, rule := range field.Rules {
			if _, ok := v.registry[rule.name]; !ok {
				return unknownRule(typ, field.GoName, rule.name)
			}
		}

		if field.ElemType == nil {
			continue
		}

		err := v.checkType(field.ElemType, seen)
		if err != nil {
			return err
		}
	}

	return nil
}

func unknownRule(typ reflect.Type, field, rule string) error {
	hint := ""
	if strings.ContainsAny(rule, "=,") {
		hint = " (arguments follow a colon, as in max:10; rules are separated by |)"
	}

	return fmt.Errorf("validation: %s.%s has the unknown rule %q%s", typ, field, rule, hint) //nolint:err113 // names it
}
