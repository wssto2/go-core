package route

import (
	"net/http"
	"reflect"
)

// None is the input of a route that takes none.
type None struct{}

// Empty is the output of a route that answers 204 No Content.
type Empty struct{}

// Spec describes a declared route. It is plain data: method, path, name,
// permission and the reflect types of the input and output.
type Spec struct {
	Method     string
	Path       string
	Name       string
	Permission string
	In         reflect.Type
	Out        reflect.Type
	// Untyped is true for routes declared with Raw: they have no In or Out.
	Untyped bool
}

// Declared is a route that can be listed in a Group: every Route and every
// RawRoute.
type Declared interface {
	Spec() Spec
	declaration() *declaration
}

// declaration is the identity shared by a route, the handler bound to it and
// the groups that list it, so a missing handler can be found.
type declaration struct {
	spec   Spec
	groups []*Contract
}

// Route is a declared route with input type In and output type Out.
// Declare it with Get, Post, Put, Patch or Delete.
type Route[In, Out any] struct {
	d *declaration
}

func declare[In, Out any](method, path string) Route[In, Out] {
	return Route[In, Out]{d: &declaration{spec: Spec{
		Method: method,
		Path:   path,
		In:     reflect.TypeFor[In](),
		Out:    reflect.TypeFor[Out](),
	}}}
}

// Get declares a GET route.
func Get[In, Out any](path string) Route[In, Out] { return declare[In, Out](http.MethodGet, path) }

// Post declares a POST route.
func Post[In, Out any](path string) Route[In, Out] { return declare[In, Out](http.MethodPost, path) }

// Put declares a PUT route.
func Put[In, Out any](path string) Route[In, Out] { return declare[In, Out](http.MethodPut, path) }

// Patch declares a PATCH route.
func Patch[In, Out any](path string) Route[In, Out] { return declare[In, Out](http.MethodPatch, path) }

// Delete declares a DELETE route.
func Delete[In, Out any](path string) Route[In, Out] {
	return declare[In, Out](http.MethodDelete, path)
}

// Name gives the route a stable name, used by generated clients.
func (r Route[In, Out]) Name(name string) Route[In, Out] {
	r.d.spec.Name = name
	return r
}

// Requires gates the route on a permission id from the application's
// authz catalogue. Starting the application fails if the catalogue does not
// define it.
func (r Route[In, Out]) Requires(permission string) Route[In, Out] {
	r.d.spec.Permission = permission
	return r
}

// Spec returns the declaration as data.
func (r Route[In, Out]) Spec() Spec { return r.d.spec }

func (r Route[In, Out]) declaration() *declaration { return r.d }
