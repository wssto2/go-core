package route

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authzhttp"
	"github.com/wssto2/go-core/binders"
	"github.com/wssto2/go-core/validation"
	"github.com/wssto2/go-core/web"
)

// Handled is a route with its handler: what an application mounts. Create one
// with Route.To or Raw.
type Handled interface {
	Spec() Spec
	// Mount registers the route on r, behind the authentication and the
	// permission check the security settings provide.
	Mount(r gin.IRoutes, security Security) error
	declaration() *declaration
}

// Bound is a typed route together with its handler.
type Bound struct {
	d     *declaration
	build func(security Security) []gin.HandlerFunc
	err   error
}

// To binds the handler to the route. The handler is a plain function; its
// input and output types must be the ones the route was declared with.
func (r Route[In, Out]) To(handler func(ctx context.Context, in In) (Out, error)) Bound {
	b := Bound{d: r.d}

	inType := reflect.TypeFor[In]()
	typed := inType != reflect.TypeFor[None]()

	if typed && inType.Kind() != reflect.Struct {
		b.err = fmt.Errorf("route %s: input %s must be a struct (use route.None for no input)", r.d.spec, inType)
		return b
	}

	hasBody := r.d.spec.Method == http.MethodPost || r.d.spec.Method == http.MethodPut || r.d.spec.Method == http.MethodPatch
	empty := reflect.TypeFor[Out]() == reflect.TypeFor[Empty]()

	b.build = func(security Security) []gin.HandlerFunc {
		return append(security.guard(r.d.spec), func(c *gin.Context) {
			var in In

			if typed {
				if err := bindInput(c, &in, hasBody); err != nil {
					web.Fail(c, err)
					return
				}
			}

			out, err := handler(c.Request.Context(), in)
			if err != nil {
				web.Fail(c, err)
				return
			}

			if empty {
				web.NoContent(c)
				return
			}

			web.Handle(c, out, nil)
		})
	}

	return b
}

// bindInput fills in from the URL path, the query string and, when the method
// has one, the body, then validates it.
func bindInput[In any](c *gin.Context, in *In, hasBody bool) error {
	if hasBody {
		if err := binders.BindRequest(c, in); err != nil {
			return err
		}
	}

	params := make(map[string][]string, len(c.Params))
	for _, p := range c.Params {
		params[p.Key] = []string{p.Value}
	}

	if err := binders.BindStrings(in, "path", params); err != nil {
		return err
	}

	if err := binders.BindStrings(in, "query", c.Request.URL.Query()); err != nil {
		return err
	}

	return validation.ValidateInput(in)
}

// Spec returns the declaration as data.
func (b Bound) Spec() Spec { return b.d.spec }

func (b Bound) declaration() *declaration { return b.d }

// Security is what routes are mounted behind: how a request is authenticated
// and who may do what. A route that is not Public needs Authenticate; one that
// Requires a permission also needs Authorizer.
type Security struct {
	// Authenticate runs, in order, before every non-public route. It must
	// reject an unauthenticated request with apperr.Unauthorized (see
	// auth.Authenticated) and leave the authz principal in the request context
	// (see authzhttp.Principals).
	Authenticate []gin.HandlerFunc
	// Authorizer checks the permission of routes declared with Requires.
	Authorizer authz.Authorizer
}

// guard is the chain in front of a route's handler.
func (s Security) guard(spec Spec) []gin.HandlerFunc {
	if spec.Public {
		return nil
	}

	chain := append([]gin.HandlerFunc(nil), s.Authenticate...)
	if spec.Permission != "" {
		chain = append(chain, authzhttp.Require(s.Authorizer, spec.Permission))
	}

	return chain
}

// check says what stops spec from being mounted with these settings.
func (s Security) check(spec Spec) error {
	switch {
	case spec.Public && spec.Permission != "":
		return errors.New("route " + spec.String() + " is Public but also Requires " + spec.Permission +
			": remove .Public() so it needs sign-in, or remove .Requires(...)")
	case spec.Public:
		return nil
	case len(s.Authenticate) == 0:
		return errors.New("route " + spec.String() + " needs an authenticated user but the application has no authentication: " +
			"pass gocore.WithAuthentication(...) to gocore.New, or mark the route .Public()")
	case spec.Permission != "" && s.Authorizer == nil:
		return errors.New("route " + spec.String() + " requires " + spec.Permission +
			" but the application has no authorizer: pass gocore.WithAuthorizer(...) to gocore.New")
	}

	return nil
}

// Mount registers the route on r behind its authentication and permission check.
func (b Bound) Mount(r gin.IRoutes, security Security) error {
	if b.err != nil {
		return b.err
	}

	if err := security.check(b.d.spec); err != nil {
		return err
	}

	r.Handle(b.d.spec.Method, b.d.spec.Path, b.build(security)...)

	return nil
}

// RawRoute is a declared route whose handler is a plain gin handler, for what
// the typed form cannot express: Server-Sent Events, downloads, uploads. It
// shows up in the route table with its method and path, without input or
// output. Declare it with Raw and bind its handler with To, like a typed route.
type RawRoute struct {
	d *declaration
}

// Raw declares a route that will be served by a plain gin handler.
func Raw(method, path string) RawRoute {
	return RawRoute{d: &declaration{spec: Spec{Method: method, Path: path, Untyped: true}}}
}

// Name gives the route a stable name.
func (r RawRoute) Name(name string) RawRoute {
	r.d.spec.Name = name
	return r
}

// Requires gates the route on a permission id from the authz catalogue.
func (r RawRoute) Requires(permission string) RawRoute {
	r.d.spec.Permission = permission
	return r
}

// Public opens the route to anyone; see Route.Public.
func (r RawRoute) Public() RawRoute {
	r.d.spec.Public = true
	return r
}

// Spec returns the declaration as data.
func (r RawRoute) Spec() Spec { return r.d.spec }

func (r RawRoute) declaration() *declaration { return r.d }

// To binds the handler to the route.
func (r RawRoute) To(handler gin.HandlerFunc) Bound {
	return Bound{d: r.d, build: func(security Security) []gin.HandlerFunc {
		return append(security.guard(r.d.spec), handler)
	}}
}
