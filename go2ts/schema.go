package go2ts

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"unicode"
)

type valRule struct {
	name string
	args string
}

// parseValidationTag parses "required|max:50|email" into individual rules.
func parseValidationTag(tag string) []valRule {
	if tag == "" {
		return nil
	}
	parts := strings.Split(tag, "|")
	rules := make([]valRule, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.Index(part, ":")
		if idx == -1 {
			rules = append(rules, valRule{name: part})
		} else {
			rules = append(rules, valRule{
				name: strings.TrimSpace(part[:idx]),
				args: strings.TrimSpace(part[idx+1:]),
			})
		}
	}
	return rules
}

func hasValRule(rules []valRule, name string) bool {
	for _, r := range rules {
		if r.name == name {
			return true
		}
	}
	return false
}

func valRuleArgs(rules []valRule, name string) (string, bool) {
	for _, r := range rules {
		if r.name == name {
			return r.args, true
		}
	}
	return "", false
}

// isCrossFieldRule returns true for rules that require sibling field context
// and cannot be expressed as a standalone Zod field validator.
func isCrossFieldRule(name string) bool {
	switch name {
	case "required_if", "required_unless", "same", "confirmed", "different", "password":
		return true
	}
	return false
}

// knownCustomType returns true for the well-known nullable/custom struct types
// that have explicit Zod mappings.
func knownCustomType(name string) bool {
	switch name {
	case "Time", "String", "NullString", "Int", "NullInt",
		"Float", "NullFloat", "Date", "NullDate", "DateTime", "NullDateTime", "Bool", "Enum":
		return true
	}
	return false
}

// mapGoTypeToZodBase maps a Go reflect.Type to a base Zod expression.
// Returns (zodExpr, isNullable).
func mapGoTypeToZodBase(t reflect.Type) (string, bool) {
	isNullable := false
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
		isNullable = true
	}

	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "z.number().int()", isNullable
	case reflect.Float32, reflect.Float64:
		return "z.number()", isNullable
	case reflect.Bool:
		return "z.boolean()", isNullable
	case reflect.String:
		return "z.string()", isNullable
	case reflect.Slice, reflect.Array:
		elemExpr, _ := mapGoTypeToZodBase(t.Elem())
		return fmt.Sprintf("z.array(%s)", elemExpr), isNullable
	case reflect.Struct:
		switch t.Name() {
		case "Time":
			return "z.string().datetime()", isNullable
		case "String":
			return "z.string()", isNullable
		case "NullString":
			return "z.string()", true
		case "Int":
			return "z.number().int()", isNullable
		case "NullInt":
			return "z.number().int()", true
		case "Float":
			return "z.number()", isNullable
		case "NullFloat":
			return "z.number()", true
		case "Date":
			return "z.string().date()", isNullable
		case "NullDate":
			return "z.string().date()", true
		case "DateTime":
			return "z.string().datetime()", isNullable
		case "NullDateTime":
			return "z.string().datetime()", true
		case "Bool":
			return "z.boolean()", isNullable
		case "Enum":
			return "z.number().int()", isNullable
		default:
			return fmt.Sprintf("%sSchema", t.Name()), isNullable
		}
	}
	return "z.unknown()", isNullable
}

// isStringBase returns true if the Go type resolves to a string-like Zod base.
func isStringBase(t reflect.Type) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.String {
		return true
	}
	if t.Kind() == reflect.Struct {
		switch t.Name() {
		case "String", "NullString":
			return true
		}
	}
	return false
}

// buildLiteralUnion generates a Zod literal or union of literals from a
// comma-separated list of values (e.g. "1,2" → "z.union([z.literal(1), z.literal(2)])").
func buildLiteralUnion(args string) string {
	values := strings.Split(args, ",")
	if len(values) == 1 {
		return fmt.Sprintf("z.literal(%s)", formatZodLiteral(strings.TrimSpace(values[0])))
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, fmt.Sprintf("z.literal(%s)", formatZodLiteral(strings.TrimSpace(v))))
	}
	return fmt.Sprintf("z.union([%s])", strings.Join(parts, ", "))
}

func formatZodLiteral(v string) string {
	if isNumericLiteral(v) {
		return v
	}
	return fmt.Sprintf("%q", v)
}

func isNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if i == 0 && c == '-' {
			continue
		}
		if !unicode.IsDigit(c) && c != '.' {
			return false
		}
	}
	return true
}

// fieldToZodExpr builds the full Zod expression for a single struct field.
// hasCrossField is true when the validation tag contained cross-field rules
// that were skipped (they need manual .superRefine() on the object schema).
func fieldToZodExpr(ft reflect.Type, validationTag string) (expr string, hasCrossField bool) {
	rules := parseValidationTag(validationTag)
	isRequired := hasValRule(rules, "required")

	for _, r := range rules {
		if isCrossFieldRule(r.name) {
			hasCrossField = true
		}
	}

	// `in` rule replaces the base type entirely with a literal union.
	if inArgs, ok := valRuleArgs(rules, "in"); ok {
		return buildLiteralUnion(inArgs), hasCrossField
	}

	base, isNullable := mapGoTypeToZodBase(ft)
	expr = base

	// needsOrEmpty: non-required format rules on non-nullable strings must allow
	// empty string since Go's validator skips format checks on empty non-required fields.
	needsOrEmpty := false

	for _, r := range rules {
		switch r.name {
		case "required":
			if isStringBase(ft) {
				expr += ".min(1)"
			}
		case "min":
			expr += fmt.Sprintf(".min(%s)", r.args)
		case "max":
			expr += fmt.Sprintf(".max(%s)", r.args)
		case "len":
			if isStringBase(ft) {
				expr += fmt.Sprintf(".length(%s)", r.args)
			} else {
				// For numeric types, check exact digit count via refine
				expr += fmt.Sprintf(".refine((n) => String(n).length === %s, { message: 'Must be exactly %s digits' })", r.args, r.args)
			}
		case "between":
			parts := strings.SplitN(r.args, ",", 2)
			if len(parts) == 2 {
				expr += fmt.Sprintf(".min(%s).max(%s)",
					strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		case "email":
			expr += ".email()"
			if !isRequired && !isNullable {
				needsOrEmpty = true
			}
		case "url":
			expr += ".url()"
			if !isRequired && !isNullable {
				needsOrEmpty = true
			}
		case "uuid":
			expr += ".uuid()"
			if !isRequired && !isNullable {
				needsOrEmpty = true
			}
		case "date":
			if !strings.Contains(expr, ".date()") && !strings.Contains(expr, ".datetime()") {
				expr += ".date()"
				if !isRequired && !isNullable {
					needsOrEmpty = true
				}
			}
		case "date_time":
			if !strings.Contains(expr, ".datetime()") {
				expr += ".datetime()"
				if !isRequired && !isNullable {
					needsOrEmpty = true
				}
			}
		}
	}

	// Allow empty string for non-required format-validated string fields,
	// matching Go's behaviour of skipping format checks on empty non-required values.
	if needsOrEmpty {
		expr += ".or(z.literal(''))"
	}

	if isNullable {
		expr += ".nullable()"
		if !isRequired {
			expr += ".optional()"
		}
	}

	return expr, hasCrossField
}

// structToZod generates a Zod schema string and the inferred TS type for s.
// Returns the type name, the file content, nested child structs to process,
// and any error.
func structToZod(s interface{}, ctx *GenContext) (typeName string, output string, children map[string]interface{}, err error) {
	rt := reflect.TypeOf(s)
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return "", "", nil, fmt.Errorf("expected a struct, got %s", rt.Kind())
	}

	typeName = rt.Name()
	if ctx.Processed[typeName] {
		return "", "", nil, nil
	}
	ctx.Processed[typeName] = true

	children = make(map[string]interface{})

	var fieldsBuilder strings.Builder
	var crossFieldComments []string

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		if field.PkgPath != "" { // unexported
			continue
		}

		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		jsonName := field.Name
		if jsonTag != "" {
			if parts := strings.Split(jsonTag, ","); parts[0] != "" {
				jsonName = parts[0]
			}
		}

		validationTag := field.Tag.Get("validation")
		zodExpr, hasCrossField := fieldToZodExpr(field.Type, validationTag)

		fieldsBuilder.WriteString(fmt.Sprintf("  %s: %s,\n", jsonName, zodExpr))

		if hasCrossField {
			crossFieldComments = append(crossFieldComments,
				fmt.Sprintf("  // %s: cross-field rules (%s) — add manually via .superRefine()", jsonName, validationTag))
		}

		// Collect nested struct children that need their own schema file.
		ft := field.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && !knownCustomType(ft.Name()) && ft.Name() != "" && ft.Name() != typeName {
			children[ft.Name()] = reflect.New(ft).Interface()
		}
	}

	schemaName := typeName + "Schema"

	var sb strings.Builder
	sb.WriteString("// This file is auto-generated. Do not edit manually.\n")
	sb.WriteString("import { z } from 'zod';\n")

	if len(children) > 0 {
		sb.WriteString("\n")
		for childName := range children {
			sb.WriteString(fmt.Sprintf("import { %sSchema } from './%s';\n", childName, childName))
		}
	}

	sb.WriteString("\n")

	if len(crossFieldComments) > 0 {
		sb.WriteString("// NOTE: The following fields have cross-field validation rules that cannot\n")
		sb.WriteString("// be generated. Add them manually via .superRefine() on the schema if needed:\n")
		for _, c := range crossFieldComments {
			sb.WriteString(c + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("export const %s = z.object({\n", schemaName))
	sb.WriteString(fieldsBuilder.String())
	sb.WriteString("});\n\n")
	sb.WriteString(fmt.Sprintf("export type %s = z.infer<typeof %s>;\n", typeName, schemaName))

	return typeName, sb.String(), children, nil
}

// GenerateSchemas generates Zod schema files for the given request/input structs.
// Each struct produces one .ts file containing:
//   - A z.object() schema constant named FooSchema
//   - An inferred TypeScript type: export type Foo = z.infer<typeof FooSchema>
//
// Use this for request/input structs that have `validation` struct tags.
// For entity types (responses), use GenerateTypes instead.
func GenerateSchemas(structs []interface{}, dir string) error {
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("error creating directory: %w", err)
	}

	ctx := &GenContext{Processed: make(map[string]bool)}

	pending := structs
	for len(pending) > 0 {
		current := pending[0]
		pending = pending[1:]

		typeName, output, children, err := structToZod(current, ctx)
		if err != nil {
			return fmt.Errorf("error converting %T to Zod schema: %w", current, err)
		}
		if typeName == "" {
			continue
		}

		path := fmt.Sprintf("%s/%s.ts", dir, typeName)
		if err := os.WriteFile(path, []byte(output), 0644); err != nil {
			return fmt.Errorf("error writing %s: %w", path, err)
		}

		for _, child := range children {
			pending = append(pending, child)
		}
	}

	return nil
}
