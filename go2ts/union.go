package go2ts

import (
	"fmt"
	"strings"
)

// GeneratedHeader is the first line of every generated TypeScript file.
const GeneratedHeader = "// This file is auto-generated. Do not edit manually.\n"

// Quote returns s as a single-quoted TypeScript string literal.
func Quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`)
	return "'" + r.Replace(s) + "'"
}

// StringUnion renders an exported string-literal union type, one member per
// line, in the order given:
//
//	export type Permission =
//	  | 'crm.lead:view'
//	  | 'crm.lead:update';
//
// With no values the type is `never`.
func StringUnion(name string, values []string) string {
	if len(values) == 0 {
		return fmt.Sprintf("export type %s = never;\n", name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "export type %s =\n", name)
	for i, v := range values {
		sep := ""
		if i == len(values)-1 {
			sep = ";"
		}
		fmt.Fprintf(&b, "  | %s%s\n", Quote(v), sep)
	}
	return b.String()
}
