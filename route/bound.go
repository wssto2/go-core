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
	// Mount registers the route on r. a checks the route's permission; it may
	// be nil only for a route that needs none.
	Mount(r gin.IRoutes, a authz.Authorizer) error
	declaration() *declaration
}

// Bound is a typed route together with its handler.
type Bound struct {
	d     *declaration
	build func(a authz.Authorizer) []gin.HandlerFunc
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

	b.build = func(a authz.Authorizer) []gin.HandlerFunc {
		var chain []gin.HandlerFunc
		if p := r.d.spec.Permission; p != "" {
			chain = append(chain, authzhttp.Require(a, p))
		}

		return append(chain, func(c *gin.Context) {
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

// Mount registers the route on r behind its permission check.
func (b Bound) Mount(r gin.IRoutes, a authz.Authorizer) error {
	if b.err != nil {
		return b.err
	}

	if b.d.spec.Permission != "" && a == nil {
		return noAuthorizer(b.d.spec)
	}

	r.Handle(b.d.spec.Method, b.d.spec.Path, b.build(a)...)

	return nil
}

func noAuthorizer(s Spec) error {
	return errors.New("route " + s.String() + " requires " + s.Permission +
		" but the application has no authorizer: pass gocore.WithAuthorizer(...) to gocore.New")
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

// Spec returns the declaration as data.
func (r RawRoute) Spec() Spec { return r.d.spec }

func (r RawRoute) declaration() *declaration { return r.d }

// To binds the handler to the route.
func (r RawRoute) To(handler gin.HandlerFunc) Bound {
	return Bound{d: r.d, build: func(a authz.Authorizer) []gin.HandlerFunc {
		chain := []gin.HandlerFunc{}
		if p := r.d.spec.Permission; p != "" {
			chain = append(chain, authzhttp.Require(a, p))
		}

		return append(chain, handler)
	}}
}
