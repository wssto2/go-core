package route

import (
	"fmt"
	"github.com/wssto2/go-core/internal/binders"
	"net/http"
	"reflect"
	"slices"
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
	// AllOf holds the permissions of every further Requires after the first (which is Permission): all must hold.
	// AnyOf (RequiresAny) is a set of which holding one is enough. They combine: all of them must be satisfied.
	AllOf []string
	AnyOf []string
	// Public is true for a route anyone may call. Every other route needs an
	// authenticated principal, and Permission, when set, on top of that.
	Public bool
	// PersonOnly is true for a route only a person may call: a service account (an API key) is refused with 403.
	PersonOnly bool
	// Authenticated is true for a route that needs an authenticated principal and no permission: the handler decides
	// what that principal may do (a record checked against its owner, a lookup every signed-in user may make).
	Authenticated bool
	// Status is the success status the route answers; 0 means the default, 200 (204 for an Empty output).
	Status int
	In     reflect.Type
	Out    reflect.Type
	// FreeFormQuery is true when the input has a query:"*" url.Values field that takes the
	// query parameters no other field declares (datatable filters).
	FreeFormQuery bool
	// Untyped is true for routes declared with Raw: they have no In or Out.
	Untyped bool
	// MaxBody is the JSON body limit in bytes this route sets for itself (MaxBody); 0 keeps the application's.
	MaxBody int64
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
		Method:        method,
		Path:          path,
		In:            reflect.TypeFor[In](),
		Out:           reflect.TypeFor[Out](),
		FreeFormQuery: len(binders.FreeFormFields(reflect.TypeFor[In]())) > 0,
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
// define it. Chained, every one must hold: Requires(a).Requires(b) needs both.
func (r Route[In, Out]) Requires(permission string) Route[In, Out] {
	r.d.spec.require(permission)
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

// RequiresAny gates the route on holding at least one of the permissions.
func (r Route[In, Out]) RequiresAny(permissions ...string) Route[In, Out] {
	r.d.spec.AnyOf = permissions

	return r
}

// Public opens the route to anyone, signed in or not (a login form, a health
// probe). Without it the route needs an authenticated principal. A public
// route cannot also Requires a permission.
func (r Route[In, Out]) Public() Route[In, Out] {
	r.d.spec.Public = true
	return r
}

// Authenticated declares that any authenticated principal may call the route and the handler decides the rest:
// the access is declared, not forgotten. A route must declare one of Requires, RequiresAny, Public,
// PersonOnly or Authenticated, or starting the application fails. It cannot also be Public.
func (r Route[In, Out]) Authenticated() Route[In, Out] {
	r.d.spec.Authenticated = true

	return r
}

// PersonOnly refuses a service account (a request made with an API key) with 403, whatever
// permission it holds: for what only a person has, such as their own session, profile or devices.
// It is checked after the authentication, so a route that is Public cannot also be PersonOnly.
func (r Route[In, Out]) PersonOnly() Route[In, Out] {
	r.d.spec.PersonOnly = true

	return r
}

// MaxBody sets the largest JSON body, in bytes, this route accepts, instead of the application's limit
// (HTTPConfig.MaxBodyBytes). Raise it for the one route that takes a big payload. Multipart uploads have their own limit.
func (r Route[In, Out]) MaxBody(bytes int64) Route[In, Out] {
	r.d.spec.MaxBody = checkMaxBody(bytes)

	return r
}

// checkMaxBody refuses a limit that is not positive: a declaration mistake, found at start-up.
func checkMaxBody(bytes int64) int64 {
	if bytes <= 0 {
		panic(fmt.Sprintf("route: MaxBody(%d): the limit must be positive", bytes))
	}

	return bytes
}

// Spec returns the declaration as data.
func (r Route[In, Out]) Spec() Spec { return r.d.spec }

func (r Route[In, Out]) declaration() *declaration { return r.d }

// require adds a permission every caller must hold: the first is Permission, the rest AllOf.
func (s *Spec) require(permission string) {
	if s.Permission == "" {
		s.Permission = permission

		return
	}

	s.AllOf = append(slices.Clone(s.AllOf), permission)
}

// Needs lists every permission the route names, whatever the rule.
func (s Spec) Needs() []string {
	return slices.Concat(nonEmpty(s.Permission), s.AnyOf, s.AllOf)
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}

	return []string{s}
}

// DeclaresAccess reports whether the route says who may call it: it names a permission, or is Public, PersonOnly
// or Authenticated. A route that declares nothing is a forgotten permission, so starting the application refuses it.
func (s Spec) DeclaresAccess() bool {
	return s.Public || s.PersonOnly || s.Authenticated || len(s.Needs()) > 0
}

// permissionProblem says what is wrong with the permission settings of a route, "" when nothing.
func (s Spec) permissionProblem() string {
	if slices.Contains(s.Needs(), "") {
		return "route " + s.String() + " names an empty permission"
	}

	return ""
}
