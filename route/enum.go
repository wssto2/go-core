package route

import "reflect"

// Enumeration is a named string type together with the values it may hold,
// declared once with Enum and listed in a group with Contract.Types. A DTO
// field of that type becomes the union of its values in the generated types
// and a z.enum in the generated schemas; a named string type that is not
// declared as an Enumeration stays string.
type Enumeration struct {
	typ    reflect.Type
	values []string
}

// Enum declares the values of a named string type. The type comes from the
// constants:
//
//	type Status string
//
//	const (
//	    StatusActive Status = "active"
//	    StatusLocked Status = "locked"
//	)
//
//	var Statuses = route.Enum(StatusActive, StatusLocked)
//
//	var Routes = route.Group("tickets", List, Show).Types(Statuses)
func Enum[T ~string](values ...T) Enumeration {
	e := Enumeration{typ: reflect.TypeFor[T](), values: make([]string, len(values))}
	for i, v := range values {
		e.values[i] = string(v)
	}

	return e
}

// Type is the Go type the values belong to.
func (e Enumeration) Type() reflect.Type { return e.typ }

// Values lists the values in declaration order.
func (e Enumeration) Values() []string { return append([]string(nil), e.values...) }
