package binders

import (
	"reflect"
	"strings"
	"sync"
)

// fieldMeta holds the parsed tag information for a single struct field.
// Built once per struct type and cached — never rebuilt per request.
type fieldMeta struct {
	index       []int  // field index path, through embedded structs (reflect.Value.FieldByIndex)
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
	return appendFieldMeta(make([]fieldMeta, 0, t.NumField()), t, nil, false)
}

// appendFieldMeta lists the bindable fields of t. An untagged embedded struct is flattened
// the way encoding/json does, and inside it path:"…" and query:"…" fields are not body fields.
func appendFieldMeta(fields []fieldMeta, t reflect.Type, prefix []int, embedded bool) []fieldMeta {
	for i := range t.NumField() {
		sf := t.Field(i)
		index := append(append([]int(nil), prefix...), i)

		key := resolveKey(sf)
		if isPromoted(sf, key) {
			fields = appendFieldMeta(fields, sf.Type, index, true)

			continue
		}

		if key == "" || (embedded && isURLParam(sf)) {
			continue
		}

		fields = append(fields, fieldMeta{index: index, formKey: key, structField: sf})
	}

	return fields
}

// isPromoted: an embedded struct without a binding name hands its fields to the parent.
func isPromoted(sf reflect.StructField, key string) bool {
	return sf.Anonymous && sf.Type.Kind() == reflect.Struct && key == "" &&
		sf.Tag.Get("json") == "" && sf.Tag.Get("form") == ""
}

func isURLParam(sf reflect.StructField) bool {
	return sf.Tag.Get("path") != "" || sf.Tag.Get("query") != ""
}
