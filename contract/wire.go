package contract

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/wssto2/go-core/route"
)

// WireRules are what Wire holds the response types to, beyond the json tags
// every response type needs. The zero value checks the tags only.
type WireRules struct {
	// Forbidden are package paths (an import path and everything below it)
	// whose types are not to be returned by a route: an application's domain
	// packages, so a handler maps to a DTO instead of returning an entity.
	Forbidden []string
	// Except are paths inside Forbidden that are allowed after all, such as a
	// domain package of shared value types.
	Except []string
}

// Wire audits what the routes of groups put on the wire, and returns one
// finding per problem, sorted (none when the response types are sound). It
// reads the same output types Reachable lists, so it covers every typed route
// and no handler is searched for; a route declared with Raw has no type to read.
//
// Two checks:
//
//   - A struct reachable from an output (through pointers, slices, maps and
//     embedded structs) with an exported field that has no json name marshals
//     that field under its Go name, which the generated frontend types do not
//     expect. A `json:"-"` field is skipped, and so is a type with its own
//     MarshalJSON or MarshalText, which writes itself.
//
//   - An output type declared in a Forbidden package and not under Except.
//     Only the types a route returns are held to it, not those nested in one,
//     so a DTO may carry a shared value type.
//
//     problems, err := contract.Wire(app.Contracts(), contract.WireRules{
//     Forbidden: []string{"example.com/app/internal/domain"},
//     Except:    []string{"example.com/app/internal/domain/shared"},
//     })
//
// It fails where Reachable does, for a group Generate cannot write.
func Wire(groups []*route.Contract, rules WireRules) ([]string, error) {
	reached, err := Reachable(groups...)
	if err != nil {
		return nil, err
	}

	var findings []string

	audit := wireAudit{seen: map[reflect.Type]bool{}, untagged: map[reflect.Type][]string{}}

	for _, out := range reached.Outputs {
		if under(out.PkgPath(), rules.Forbidden) && !under(out.PkgPath(), rules.Except) {
			findings = append(findings,
				typeID(out)+" is returned by a route but declared in a forbidden package: map it to a DTO")
		}

		audit.visit(out)
	}

	for typ, fields := range audit.untagged {
		findings = append(findings, fmt.Sprintf(
			"%s has exported fields without a json tag: %s", typeID(typ), strings.Join(fields, ", ")))
	}

	sort.Strings(findings)

	return findings, nil
}

func typeID(t reflect.Type) string { return t.PkgPath() + "." + t.Name() }

// under reports whether pkg is one of paths or below it.
func under(pkg string, paths []string) bool {
	for _, path := range paths {
		if pkg == path || strings.HasPrefix(pkg, path+"/") {
			return true
		}
	}

	return false
}

// wireAudit walks the types an output reaches; each is read once.
type wireAudit struct {
	seen     map[reflect.Type]bool
	untagged map[reflect.Type][]string
}

func (audit *wireAudit) visit(typ reflect.Type) {
	typ = elem(typ)

	if typ.Kind() != reflect.Struct || audit.seen[typ] || writesItself(typ) {
		return
	}

	audit.seen[typ] = true

	for field := range typ.Fields() {
		audit.field(typ, field)
	}
}

func (audit *wireAudit) field(owner reflect.Type, field reflect.StructField) {
	if !field.IsExported() && !field.Anonymous {
		return
	}

	tag := field.Tag.Get("json")
	if tag == "-" {
		return
	}

	// A tag with no name (`json:",omitempty"`) keeps the Go name. An embedded struct without
	// a name is flattened into its parent, so it is walked, not flagged.
	if name, _, _ := strings.Cut(tag, ","); name == "" && field.IsExported() && !field.Anonymous {
		audit.untagged[owner] = append(audit.untagged[owner], field.Name)
	}

	audit.visit(field.Type)
}

// elem is the type a pointer, slice, array or map holds, however deep.
func elem(typ reflect.Type) reflect.Type {
	for slices.Contains([]reflect.Kind{reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map}, typ.Kind()) {
		typ = typ.Elem()
	}

	return typ
}

// writesItself reports whether typ marshals through its own method rather than
// field by field.
func writesItself(typ reflect.Type) bool {
	ptr := reflect.PointerTo(typ)

	return ptr.Implements(reflect.TypeFor[json.Marshaler]()) || ptr.Implements(reflect.TypeFor[encoding.TextMarshaler]())
}
