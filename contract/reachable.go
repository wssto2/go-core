package contract

import (
	"fmt"
	"reflect"

	"github.com/wssto2/go-core/route"
)

// Types are the Go types Generate writes TypeScript for. go2ts walks the fields of each one, so the types they
// nest are written too and are not listed.
type Types struct {
	// Inputs are the input structs that get a Zod schema (schemas.ts), a route's input, route.None and an input
	// with no body field aside.
	Inputs []reflect.Type
	// Outputs are the output structs and the extra types of Types (entities.ts): the rows of a list and the
	// data of a page or an envelope included.
	Outputs []reflect.Type
	// Enums are the enumerations the groups declare.
	Enums []route.Enumeration
}

// Reachable lists the types Generate would write for groups, from the same walk, so an application hands its
// installed groups (App.Contracts) to this instead of keeping a list of types beside its routes. Each type is
// listed once, in the order the groups and their routes are met. It fails where Generate does for a group.
func Reachable(groups ...*route.Contract) (Types, error) {
	var out Types

	seenIn, seenOut := map[reflect.Type]bool{}, map[reflect.Type]bool{}
	seenEnum := map[reflect.Type]bool{}

	for _, g := range groups {
		p, err := planOf(g)
		if err != nil {
			return Types{}, fmt.Errorf("contract: group %q: %w", g.Name(), err)
		}

		for _, v := range p.ins[:p.nIns] {
			if t := reflect.TypeOf(v); !seenIn[t] {
				seenIn[t] = true
				out.Inputs = append(out.Inputs, t)
			}
		}

		for _, v := range p.outs[:p.nOuts] {
			if t := reflect.TypeOf(v); !seenOut[t] {
				seenOut[t] = true
				out.Outputs = append(out.Outputs, t)
			}
		}

		for _, e := range g.Enums() {
			if !seenEnum[e.Type()] {
				seenEnum[e.Type()] = true
				out.Enums = append(out.Enums, e)
			}
		}
	}

	return out, nil
}
