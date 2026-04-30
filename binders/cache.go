package binders

import (
	"reflect"
	"strings"
	"sync"
)

// fieldMeta holds the parsed tag information for a single struct field.
// Built once per struct type and cached — never rebuilt per request.
type fieldMeta struct {
	index       int    // position in the struct
	formKey     string // resolved binding key
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

// resolveKey returns the binding key for a struct field.
// Priority: `form` tag → `json` tag (name part only, strips options like ",omitempty") → skip.
// A value of "-" in either tag means explicitly excluded.
func resolveKey(sf reflect.StructField) string {
	if tag := sf.Tag.Get("form"); tag != "" {
		if tag == "-" {
			return ""
		}
		return tag
	}
	if tag := sf.Tag.Get("json"); tag != "" {
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" || name == "" {
			return ""
		}
		return name
	}
	return ""
}

func buildFieldMeta(t reflect.Type) []fieldMeta {
	fields := make([]fieldMeta, 0, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)

		key := resolveKey(sf)
		if key == "" {
			continue
		}

		fields = append(fields, fieldMeta{
			index:       i,
			formKey:     key,
			structField: sf,
		})
	}

	return fields
}
