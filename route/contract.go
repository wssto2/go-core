package route

import (
	"reflect"
	"strings"
)

// Contract is the named set of routes a feature offers, plus extra types its
// TypeScript contract needs. It is a package-level value: reading it never
// installs or runs anything.
type Contract struct {
	name   string
	routes []Declared
	types  []reflect.Type
	enums  []Enumeration
}

// Group collects a feature's routes into its Contract. The name is the
// feature's: the folder its TypeScript is generated into and the name of its
// route table ("tickets" gives ticketsRoutes). Use a single lowercase word, or
// kebab-case words.
func Group(name string, routes ...Declared) *Contract {
	c := &Contract{name: name, routes: routes}
	for _, r := range routes {
		d := r.declaration()
		d.groups = append(d.groups, c)
	}

	return c
}

// Types adds to the contract what no route names: a struct (pass a value of
// it; its zero value is enough) or an Enumeration, which every field of its
// type in the group's inputs and outputs is then rendered from.
func (c *Contract) Types(values ...any) *Contract {
	for _, v := range values {
		if e, ok := v.(Enumeration); ok {
			c.enums = append(c.enums, e)
			continue
		}

		c.types = append(c.types, reflect.TypeOf(v))
	}

	return c
}

// Name is the name the Contract was given.
func (c *Contract) Name() string { return c.name }

// Specs lists the declared routes in declaration order.
func (c *Contract) Specs() []Spec {
	specs := make([]Spec, len(c.routes))
	for i, r := range c.routes {
		specs[i] = r.Spec()
	}

	return specs
}

// ExtraTypes lists the types added with Types.
func (c *Contract) ExtraTypes() []reflect.Type {
	return append([]reflect.Type(nil), c.types...)
}

// Enums lists the enumerations added with Types.
func (c *Contract) Enums() []Enumeration { return append([]Enumeration(nil), c.enums...) }

// Unhandled returns the routes that are declared in a Contract together with
// a handled route but have no handler themselves: the one that was forgotten
// when a feature installed its routes.
func Unhandled(handled ...Handled) []Spec {
	have := make(map[*declaration]bool, len(handled))
	for _, h := range handled {
		have[h.declaration()] = true
	}

	var missing []Spec

	seen := map[*Contract]bool{}

	for _, h := range handled {
		for _, c := range h.declaration().groups {
			if seen[c] {
				continue
			}

			seen[c] = true

			for _, r := range c.routes {
				if !have[r.declaration()] {
					missing = append(missing, r.Spec())
				}
			}
		}
	}

	return missing
}

// String renders a spec as "GET /tickets/:id" for messages.
func (s Spec) String() string {
	var b strings.Builder
	b.WriteString(s.Method + " " + s.Path)

	if s.Name != "" {
		b.WriteString(" (" + s.Name + ")")
	}

	return b.String()
}
