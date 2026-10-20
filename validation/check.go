package validation

import (
	"fmt"
	"reflect"
	"strings"
)

// CheckRules reports a validation rule, on the type of any of the values or on a struct nested in
// it, that the validator does not know (a typo, or a rule from another library such as gte=0), and
// a trim rule that cannot work: on a field that is not a string or *string, or together with in.
// At request time an unknown rule fails every request with 400, so call this at start-up (routes
// do) or in a test over an application's other inputs. The message names the type, the field and
// the rule.
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

		if field.Trim {
			err := checkTrim(typ, field)
			if err != nil {
				return err
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

func checkTrim(typ reflect.Type, field cachedFieldMeta) error {
	base := field.Type
	if base.Kind() == reflect.Pointer {
		base = base.Elem()
	}

	if base.Kind() != reflect.String {
		return fmt.Errorf( //nolint:err113 // names it
			"validation: %s.%s has the trim rule but is a %s; trim applies to string and *string",
			typ, field.GoName, field.Type,
		)
	}

	if hasRule(field.Rules, "in") {
		return fmt.Errorf( //nolint:err113 // names it
			"validation: %s.%s combines trim with in; an enum value is not trimmed, a padded one is a client bug",
			typ, field.GoName,
		)
	}

	return nil
}
