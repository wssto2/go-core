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
	formKey     string // value of the `form` tag
	rules       []rule // parsed validation rules
	structField reflect.StructField
}

// rule is a single parsed validation rule, e.g. {name: "max", param: "30"}
type rule struct {
	name  string
	param string // empty for rules with no parameter (e.g. "required", "email")
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

		validationTag := sf.Tag.Get("validation")
		rules := parseRules(validationTag)

		fields = append(fields, fieldMeta{
			index:       i,
			formKey:     formKey,
			rules:       rules,
			structField: sf,
		})
	}

	return fields
}

// parseRules splits "required|max:30|in:1,2" into a slice of rule structs.
func parseRules(tag string) []rule {
	if tag == "" {
		return nil
	}

	parts := strings.Split(tag, "|")
	rules := make([]rule, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		name, param, _ := strings.Cut(part, ":")
		rules = append(rules, rule{
			name:  strings.TrimSpace(name),
			param: strings.TrimSpace(param),
		})
	}

	return rules
}

// buildFieldIndex builds a map from form key → fieldMeta for O(1) lookup.
// Called once per request — avoids the O(n*m) double loop in the original.
func buildFieldIndex(fields []fieldMeta) map[string]*fieldMeta {
	index := make(map[string]*fieldMeta, len(fields))
	for i := range fields {
		index[fields[i].formKey] = &fields[i]
	}
	return index
}
