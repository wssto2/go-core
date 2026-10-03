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
	name   string
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

// As gives the enumeration another TypeScript name than its Go type's, for a
// type that belongs to another package (authz.Kind) or whose name would be
// vague or clash in TypeScript:
//
//	var SubjectKinds = route.Enum(authz.KindUser, authz.KindServiceAccount).As("SubjectKind")
func (e Enumeration) As(name string) Enumeration {
	e.name = name
	return e
}

// Name is the TypeScript name: the one given to As, else the Go type's.
func (e Enumeration) Name() string {
	if e.name != "" {
		return e.name
	}

	return e.typ.Name()
}
