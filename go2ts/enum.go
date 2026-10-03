// Package go2ts generates TypeScript types and Zod schemas from Go structs.
package go2ts

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// EnumEntry declares a named string type and its values. Pass it in the slice
// of GenerateTypes or GenerateSchemas: a field of that type is then rendered as
// the union (or z.enum) instead of string. A named string type that is not
// declared stays string.
type EnumEntry struct {
	typ    reflect.Type
	values []string
	ifUsed bool
}

// Enum declares t (a named string type) with its values; the type is written
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
	if c == nil || t.Kind() != reflect.String || t.Name() == "" {
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
