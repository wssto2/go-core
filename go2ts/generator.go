package go2ts

import (
	"fmt"
	"os"
	"reflect"
	"strings"
)

type GenContext struct {
	Processed  map[string]bool
	TypeNames  map[string]string
	NameOwners map[string]string
}

// NamedEntry pairs a Go value with an explicit TypeScript name override.
// Use As to construct one.
type NamedEntry struct {
	value interface{}
	name  string
}

// As generates the TypeScript type for v using name instead of the struct's
// own name. Pass it in the slice anywhere a plain struct value is accepted:
//
//	go2ts.GenerateTypes([]any{
//	    domainlead.Lead{},
//	    go2ts.As(domainlead.Phase{}, "LeadPhase"),
//	}, dir)
//
// The override applies everywhere the type appears — including as a nested
// field type in other generated structs.
func As(v interface{}, name string) NamedEntry {
	return NamedEntry{value: v, name: name}
}

func (c *GenContext) ensureMaps() {
	if c.Processed == nil {
		c.Processed = make(map[string]bool)
	}
	if c.TypeNames == nil {
		c.TypeNames = make(map[string]string)
	}
	if c.NameOwners == nil {
		c.NameOwners = make(map[string]string)
	}
}

func typeKey(t reflect.Type) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.PkgPath() != "" && t.Name() != "" {
		return t.PkgPath() + "." + t.Name()
	}
	return t.String()
}

func (c *GenContext) resolveTypeName(t reflect.Type, parentName string) string {
	c.ensureMaps()

	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	key := typeKey(t)
	if resolved, ok := c.TypeNames[key]; ok {
		return resolved
	}

	baseName := t.Name()
	resolvedName := baseName
	if owner, ok := c.NameOwners[baseName]; ok && owner != key && parentName != "" {
		resolvedName = parentName + baseName
	}

	c.TypeNames[key] = resolvedName
	c.NameOwners[resolvedName] = key

	return resolvedName
}

// unwrapEntry returns the underlying value and, if the entry is a NamedEntry,
// pre-seeds the name override into ctx before the type is processed.
func unwrapEntry(entry interface{}, ctx *GenContext) interface{} {
	named, ok := entry.(NamedEntry)
	if !ok {
		return entry
	}

	t := reflect.TypeOf(named.value)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	ctx.ensureMaps()
	key := typeKey(t)
	ctx.TypeNames[key] = named.name
	ctx.NameOwners[named.name] = key

	return named.value
}

// Converts a Go struct to a TypeScript type definition.
func structToTs(s interface{}, ctx *GenContext) (string, string, map[string]interface{}, error) {
	reflectType := reflect.TypeOf(s)
	children := make(map[string]interface{})

	// Handle pointer types
	if reflectType.Kind() == reflect.Ptr {
		reflectType = reflectType.Elem()
	}

	if reflectType.Kind() != reflect.Struct {
		return "", "", nil, fmt.Errorf("expected a struct, got %s", reflectType.Kind())
	}

	typeName := ctx.resolveTypeName(reflectType, "")

	// Avoid regenerating the same struct
	if ctx.Processed[typeName] {
		return "", "", nil, nil
	}
	ctx.Processed[typeName] = true

	typeFields := ""

	for i := 0; i < reflectType.NumField(); i++ {
		field := reflectType.Field(i)

		// Skip unexported fields
		if field.PkgPath != "" {
			continue
		}

		// Skip json:"-"
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		jsonName := field.Name
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				jsonName = parts[0]
			}
		}

		canBeUndefined := strings.Contains(jsonTag, "omitempty")

		fieldType := mapGoTypeToTs(field.Type, typeName, children, ctx)

		if field.Name == "ID" {
			fieldType += " | null"
		}

		if canBeUndefined {
			typeFields += fmt.Sprintf("  %s?: %s;\n", jsonName, fieldType)
		} else {
			typeFields += fmt.Sprintf("  %s: %s;\n", jsonName, fieldType)
		}
	}

	definition := typeName + " = {\n" + typeFields + "}"

	return typeName, definition, children, nil
}

// Maps Go type to TypeScript equivalent, collecting nested structs
func mapGoTypeToTs(t reflect.Type, parentName string, children map[string]interface{}, ctx *GenContext) string {
	isPointer := false
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
		isPointer = true
	}

	var tsType string

	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Float32, reflect.Float64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		tsType = "number"
	case reflect.Bool:
		tsType = "boolean"
	case reflect.String:
		tsType = "string"
	case reflect.Slice, reflect.Array:
		elemType := mapCollectionElemTypeToTs(t.Elem(), parentName, children, ctx)
		tsType = fmt.Sprintf("%s[]", elemType)
	case reflect.Struct:
		name := t.Name()
		switch name {
		case "Time":
			tsType = "string"
		case "String":
			tsType = "string"
		case "NullString":
			tsType = "string | null"
		case "Int":
			tsType = "number"
		case "NullInt":
			tsType = "number | null"
		case "Float":
			tsType = "number"
		case "NullFloat":
			tsType = "number | null"
		case "Date":
			tsType = "string"
		case "NullDate":
			tsType = "string | null"
		case "DateTime":
			tsType = "string"
		case "NullDateTime":
			tsType = "string | null"
		case "Bool":
			tsType = "boolean"
		case "Enum":
			tsType = "boolean"
		default:
			resolvedName := ctx.resolveTypeName(t, parentName)
			tsType = resolvedName
			children[resolvedName] = reflect.New(t).Interface()
		}
	default:
		tsType = "any"
	}

	if isPointer && !strings.Contains(tsType, "null") {
		tsType += " | null"
	}

	return tsType
}

func mapCollectionElemTypeToTs(t reflect.Type, parentName string, children map[string]interface{}, ctx *GenContext) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	elemType := mapGoTypeToTs(t, parentName, children, ctx)
	if strings.Contains(elemType, " | ") {
		return fmt.Sprintf("(%s)", elemType)
	}

	return elemType
}

func GenerateTypes(entities []interface{}, dir string) error {
	err := os.MkdirAll(dir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("error creating directory: %w", err)
	}

	ctx := &GenContext{}

	// Pre-register all aliases so cross-references in other types resolve correctly
	// regardless of the order entries appear in the slice.
	for _, entry := range entities {
		unwrapEntry(entry, ctx)
	}

	pending := entities
	for len(pending) > 0 {
		current := unwrapEntry(pending[0], ctx)
		pending = pending[1:]

		typeName, typeDef, children, err := structToTs(current, ctx)
		if err != nil {
			return fmt.Errorf("error converting %T to TypeScript: %w", current, err)
		}
		if typeName == "" {
			continue
		}

		// Imports. children is a map, so iterate it in sorted key order:
		// unordered iteration made the emitted import block differ between
		// otherwise identical runs, which defeats any "regenerate and diff"
		// drift check in CI.
		childNames := sortedKeys(children)

		out := "// This file is auto-generated. Do not edit manually.\n"
		for _, childName := range childNames {
			// Avoid circular imports
			if childName == typeName {
				continue
			}

			out += fmt.Sprintf("import type { %s } from './%s';\n", childName, childName)
		}
		if len(children) > 0 {
			out += "\n"
		}

		out += fmt.Sprintf("export type %s;\n", typeDef)

		err = os.WriteFile(fmt.Sprintf("%s/%s.ts", dir, typeName), []byte(out), 0644)
		if err != nil {
			return fmt.Errorf("error writing file: %w", err)
		}

		// Enqueue children in the same sorted order, so the traversal order
		// (and thus which type wins an alias-name collision) is stable too.
		for _, childName := range childNames {
			pending = append(pending, children[childName])
		}
	}

	return nil
}
