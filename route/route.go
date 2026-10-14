package route

import (
	"fmt"
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
	// Public is true for a route anyone may call. Every other route needs an
	// authenticated principal, and Permission, when set, on top of that.
	Public bool
	// Status is the success status the route answers; 0 means the default, 200 (204 for an Empty output).
	Status int
	In     reflect.Type
	Out    reflect.Type
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
	// malformedPath is the status for a path segment that cannot be parsed; 0 keeps 422.
	malformedPath int
}

// malformedStatus is the route's own setting, else the first group's that has one, else 0.
func (d *declaration) malformedStatus() int {
	if d.malformedPath != 0 {
		return d.malformedPath
	}

	for _, g := range d.groups {
		if g.malformedPath != 0 {
			return g.malformedPath
		}
	}

	return 0
}

// checkMalformedStatus refuses anything but 400 and 422: a declaration mistake, found at start-up.
func checkMalformedStatus(status int) int {
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		panic(fmt.Sprintf("route: MalformedPath(%d): only 400 and 422 (Unprocessable) are allowed", status))
	}

	return status
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

// Status sets the success status the route answers, for example http.StatusCreated for a create.
// Only 2xx is accepted; anything else is a declaration mistake and panics at start-up. With
// http.StatusNoContent the route answers no body (an Empty output does the same).
func (r Route[In, Out]) Status(status int) Route[In, Out] {
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		panic(fmt.Sprintf("route: Status(%d): only 2xx success statuses are allowed", status))
	}

	r.d.spec.Status = status

	return r
}

// Public opens the route to anyone, signed in or not (a login form, a health
// probe). Without it the route needs an authenticated principal. A public
// route cannot also Requires a permission.
func (r Route[In, Out]) Public() Route[In, Out] {
	r.d.spec.Public = true
	return r
}

// MalformedPath sets the status for a path parameter that cannot be parsed (an id that is
// not a number): 400 or 422, which is the default. It wins over the group's setting.
func (r Route[In, Out]) MalformedPath(status int) Route[In, Out] {
	r.d.malformedPath = checkMalformedStatus(status)

	return r
}

// Spec returns the declaration as data.
func (r Route[In, Out]) Spec() Spec { return r.d.spec }

func (r Route[In, Out]) declaration() *declaration { return r.d }
