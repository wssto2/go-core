// Package go2ts generates TypeScript types and Zod schemas from Go structs.
package go2ts

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// EnumEntry declares a named string type, or a type written as text (see
// WritesText), and its values. Pass it in the slice of GenerateTypes or
// GenerateSchemas: a field of that type is then rendered as the union (or
// z.enum) instead of string. A named string type that is not declared stays
// string, and so does a type written as text.
type EnumEntry struct {
	typ    reflect.Type
	values []string
	ifUsed bool
}

// Enum declares t (a named string type, or a type written as text) with its values; the type is written
// even when no struct uses it.
func Enum(t reflect.Type, values []string) EnumEntry {
	return EnumEntry{typ: t, values: values}
}

// EnumIfUsed declares t like Enum but writes it only when a struct refers to it.
func EnumIfUsed(t reflect.Type, values []string) EnumEntry {
	return EnumEntry{typ: t, values: values, ifUsed: true}
}

// register tells ctx the enums of entries, so fields of those types resolve.
func (c *GenContext) register(entries []interface{}) {
	c.ensureMaps()

	for _, e := range entries {
		if named, ok := e.(NamedEntry); ok {
			e = named.value
		}

		if en, ok := e.(EnumEntry); ok {
			c.Enums[typeKey(en.typ)] = en
		}
	}
}

// enumOf returns the declaration of t when t is a declared enum.
func (c *GenContext) enumOf(t reflect.Type) (EnumEntry, bool) {
	if c == nil || wireKind(t) != reflect.String || t.Name() == "" {
		return EnumEntry{}, false
	}

	en, ok := c.Enums[typeKey(t)]

	return en, ok
}

// enumFile renders the enum as a types file (zod false) or a schema file.
func enumFile(name string, values []string, zod bool) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		b, _ := json.Marshal(v)
		quoted[i] = string(b)
	}

	if zod {
		return GeneratedHeader + "import { z } from 'zod';\n\n" +
			fmt.Sprintf("export const %sSchema = z.enum([%s]);\n\nexport type %s = z.infer<typeof %sSchema>;\n", name, strings.Join(quoted, ", "), name, name)
	}

	return GeneratedHeader + fmt.Sprintf("export type %s = %s;\n", name, strings.Join(quoted, " | "))
}

// used is the entry enqueued when a struct uses the enum: written even if only declared.
func (e EnumEntry) used() EnumEntry {
	e.ifUsed = false
	return e
}

// WritesText reports whether encoding/json writes a value of t as a JSON string
// through MarshalText, so the wire carries text although t's kind is not
// string: an int with MarshalText (authz.Qualifier), a struct, a byte array.
// encoding/json prefers MarshalJSON, so a type with one is not text, and the
// types go2ts maps by name (time.Time, go-core's database/types) keep their
// mapping. A named string type is not listed here: its kind says string already.
func WritesText(t reflect.Type) bool {
	switch {
	case t.Name() == "" || t.Kind() == reflect.String || t.Kind() == reflect.Interface || t.Kind() == reflect.Pointer:
		return false
	case t.Kind() == reflect.Struct && knownCustomType(t.Name()):
		return false
	}

	return isMarshaled[encoding.TextMarshaler](t) && !isMarshaled[json.Marshaler](t)
}

// isMarshaled reports whether t or *t implements M.
func isMarshaled[M any](t reflect.Type) bool {
	m := reflect.TypeFor[M]()

	return t.Implements(m) || reflect.PointerTo(t).Implements(m)
}

// wireKind is the kind go2ts maps t by: string for a type written as text.
func wireKind(t reflect.Type) reflect.Kind {
	if WritesText(t) {
		return reflect.String
	}

	return t.Kind()
}
