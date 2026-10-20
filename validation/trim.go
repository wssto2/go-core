package validation

import (
	"fmt"
	"reflect"
	"strings"
)

// trimStruct is the pass behind the trim rule: it writes the field with surrounding whitespace
// removed back into value, for string and non-nil *string fields, recursing the way validation
// does (nested structs, pointers to them, slices and arrays of them). It runs before any rule.
// A trim field that cannot be written is an error, never a silent trim of a copy.
func trimStruct(value reflect.Value, prefix string) error {
	meta := globalMetaCache.get(value.Type())
	if !meta.HasAnyValidation {
		return nil
	}

	for _, field := range meta.Fields {
		fieldValue := value.FieldByIndex(field.FieldIndex)
		attribute := prefix + field.Alias

		if field.Trim {
			err := trimField(fieldValue, attribute)
			if err != nil {
				return err
			}
		}

		if field.ElemType != nil {
			err := trimNested(fieldValue, attribute)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func trimField(field reflect.Value, attribute string) error {
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return nil
		}

		field = field.Elem()
	}

	if field.Kind() != reflect.String {
		return nil // CheckRules reports a trim on any other type
	}

	if !field.CanSet() {
		return fmt.Errorf( //nolint:err113 // names the field
			"validator: field %q has the trim rule but its input is not addressable; pass the input by pointer",
			attribute,
		)
	}

	field.SetString(strings.TrimSpace(field.String()))

	return nil
}

func trimNested(field reflect.Value, prefix string) error {
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return nil
		}

		field = field.Elem()
	}

	if field.Kind() == reflect.Struct {
		return trimStruct(field, prefix+".")
	}

	if field.Kind() != reflect.Slice && field.Kind() != reflect.Array {
		return nil
	}

	for index := range field.Len() {
		err := trimElement(field.Index(index), fmt.Sprintf("%s[%d].", prefix, index))
		if err != nil {
			return err
		}
	}

	return nil
}

func trimElement(elem reflect.Value, prefix string) error {
	if elem.Kind() == reflect.Pointer {
		if elem.IsNil() {
			return nil
		}

		elem = elem.Elem()
	}

	if elem.Kind() != reflect.Struct {
		return nil
	}

	return trimStruct(elem, prefix)
}
