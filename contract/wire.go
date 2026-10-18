package contract

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
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

	w := wireWalk{seen: map[reflect.Type]bool{}, untagged: map[reflect.Type][]string{}}

	for _, out := range reached.Outputs {
		if under(out.PkgPath(), rules.Forbidden) && !under(out.PkgPath(), rules.Except) {
			findings = append(findings, fmt.Sprintf("%s is returned by a route but declared in a forbidden package: map it to a DTO", typeID(out)))
		}

		w.visit(out)
	}

	for typ, fields := range w.untagged {
		findings = append(findings, fmt.Sprintf("%s has exported fields without a json tag: %s", typeID(typ), strings.Join(fields, ", ")))
	}

	sort.Strings(findings)

	return findings, nil
}

func typeID(t reflect.Type) string { return t.PkgPath() + "." + t.Name() }

// under reports whether pkg is one of paths or below it.
func under(pkg string, paths []string) bool {
	for _, p := range paths {
		if pkg == p || strings.HasPrefix(pkg, p+"/") {
			return true
		}
	}

	return false
}

type wireWalk struct {
	seen     map[reflect.Type]bool
	untagged map[reflect.Type][]string
}

func (w *wireWalk) visit(t reflect.Type) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct || w.seen[t] {
		return
	}

	w.seen[t] = true

	if writesItself(t) {
		return
	}

	for field := range t.Fields() {
		if !field.IsExported() && !field.Anonymous {
			continue
		}

		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}

		// A tag with no name (`json:",omitempty"`) keeps the Go name; an embedded struct
		// without a name is flattened into its parent, which is why it is walked, not flagged.
		if name, _, _ := strings.Cut(tag, ","); name == "" && field.IsExported() && !field.Anonymous {
			w.untagged[t] = append(w.untagged[t], field.Name)
		}

		w.visit(field.Type)
	}
}

// writesItself reports whether t marshals through its own method rather than
// field by field.
func writesItself(t reflect.Type) bool {
	p := reflect.PointerTo(t)

	return p.Implements(reflect.TypeFor[json.Marshaler]()) || p.Implements(reflect.TypeFor[encoding.TextMarshaler]())
}
