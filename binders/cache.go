package binders

import (
	"reflect"
	"sync"
)

// fieldMeta holds the parsed tag information for a single struct field.
// Built once per struct type and cached — never rebuilt per request.
type fieldMeta struct {
	index       int    // position in the struct
	formKey     string // value of the `form` tag
	structField reflect.StructField
}

// structCache caches fieldMeta slices keyed by reflect.Type.
// Reflection is expensive — we pay it once per type, not once per request.
var structCache sync.Map // map[reflect.Type][]fieldMeta

// getFieldMeta returns the cached field metadata for type T.
// On first call it parses the struct tags and stores the result.
func getFieldMeta(t reflect.Type) []fieldMeta {
	if cached, ok := structCache.Load(t); ok {
		return cached.([]fieldMeta)
	}

	fields := buildFieldMeta(t)
	structCache.Store(t, fields)
	return fields
}

func buildFieldMeta(t reflect.Type) []fieldMeta {
	fields := make([]fieldMeta, 0, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)

		formKey := sf.Tag.Get("form")
		if formKey == "" || formKey == "-" {
			continue
		}

		fields = append(fields, fieldMeta{
			index:       i,
			formKey:     formKey,
			structField: sf,
		})
	}

	return fields
}
