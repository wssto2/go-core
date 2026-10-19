package route

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authzhttp"
	"github.com/wssto2/go-core/internal/binders"
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
	// excused are declared routes of the same contract this application leaves out on purpose.
	excused []*declaration
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

	if typed {
		err := errors.Join(validation.CheckRules(inType), binders.CheckFreeForm(inType))
		if err != nil {
			b.err = fmt.Errorf("route %s: %w", r.d.spec, err)

			return b
		}
	}

	hasBody := r.d.spec.Method == http.MethodPost || r.d.spec.Method == http.MethodPut || r.d.spec.Method == http.MethodPatch
	status := cmp.Or(r.d.spec.Status, http.StatusOK)
	empty := reflect.TypeFor[Out]() == reflect.TypeFor[Empty]() || status == http.StatusNoContent

	b.build = func(security Security) []gin.HandlerFunc {
		return append(security.guard(r.d.spec), func(c *gin.Context) {
			var in In

			if typed {
				err := bindInput(c, &in, hasBody)
				if err != nil {
					web.Fail(c, err)
					return
				}
			}

			out, err := handler(context.WithValue(c.Request.Context(), exchangeKey{}, Exchange{c: c}), in)
			if err != nil {
				web.Fail(c, err)
				return
			}

			if empty {
				web.NoContent(c)
				return
			}

			if env, ok := any(out).(Enveloped); ok {
				web.JSON(c, status, env.Data(), env.Meta())

				return
			}

			web.JSON(c, status, out)
		})
	}

	return b
}

// bindInput fills in from the URL path, the query string and, when the method
// has one, the body, then validates it.
func bindInput[In any](c *gin.Context, input *In, hasBody bool) error { //nolint:varnamelen // gin c
	if hasBody {
		err := binders.BindRequest(c, input)
		if err != nil {
			return err //nolint:wrapcheck // binders' own validation error
		}
	}

	params := make(map[string][]string, len(c.Params))
	for _, p := range c.Params {
		params[p.Key] = []string{p.Value}
	}

	err := binders.BindStrings(input, "path", params)
	if err != nil {
		var ve *validation.ValidationError
		if errors.As(err, &ve) {
			return apperr.BadRequest("invalid " + strings.Join(slices.Sorted(maps.Keys(ve.Failures)), ", "))
		}

		return err //nolint:wrapcheck // binders' own validation error
	}

	err = binders.BindStrings(input, "query", c.Request.URL.Query())
	if err != nil {
		return err //nolint:wrapcheck // binders' own validation error
	}

	return validation.ValidateInput(input) //nolint:wrapcheck // validation's own error
}

// Excuses says that the routes, declared in the same contract as this one, are left out of this
// application on purpose, so Unhandled does not report them. A feature whose option drops part of its
// routes (the users administration, for a host with its own) binds the rest and excuses those; nothing
// is mounted for an excused route, so the host may serve the path itself.
func (b Bound) Excuses(routes ...Declared) Bound {
	b.excused = slices.Clone(b.excused)
	for _, r := range routes {
		b.excused = append(b.excused, r.declaration())
	}

	return b
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
	var chain []gin.HandlerFunc

	if spec.MaxBody > 0 {
		chain = append(chain, web.BodyLimitFor(spec.MaxBody))
	}

	return append(chain, s.access(spec)...)
}

// access is the part of the chain that checks who may call the route.
func (s Security) access(spec Spec) []gin.HandlerFunc {
	if spec.Public {
		return nil
	}

	chain := append([]gin.HandlerFunc(nil), s.Authenticate...)

	if spec.PersonOnly {
		chain = append(chain, refuseServiceAccount)
	}

	if spec.Permission != "" {
		chain = append(chain, authzhttp.Require(s.Authorizer, spec.Permission))
	}

	if len(spec.AnyOf) > 0 {
		chain = append(chain, authzhttp.RequireAny(s.Authorizer, spec.AnyOf...))
	}

	for _, permission := range spec.AllOf {
		chain = append(chain, authzhttp.Require(s.Authorizer, permission))
	}

	return chain
}

var errPersonOnly = errors.New("this is for a person; a service account cannot use it")

// refuseServiceAccount answers 403 to a request made as a service account.
func refuseServiceAccount(ctx *gin.Context) {
	if who, ok := authz.PrincipalFrom(ctx.Request.Context()); ok && who.Kind == authz.KindServiceAccount {
		why := map[string]any{"why": "person_only"}
		_ = ctx.Error(apperr.ForbiddenErr(errPersonOnly).WithReason(authz.ReasonForbidden, why))
		ctx.Abort()

		return
	}

	ctx.Next()
}

// problem is a mount error that names the route it is about.
func problem(text string) error { return errors.New(text) } //nolint:err113 // dynamic by design

// check says what stops spec from being mounted with these settings.
func (s Security) check(spec Spec) error {
	if text := spec.permissionProblem(); text != "" {
		return problem(text)
	}

	needs := spec.Needs()

	switch {
	case spec.Public && len(needs) > 0:
		return problem("route " + spec.String() + " is Public but also requires " + strings.Join(needs, ", ") +
			": remove .Public() so it needs sign-in, or remove .Requires(...)")
	case spec.Public && spec.PersonOnly:
		return problem("route " + spec.String() + " is Public but also PersonOnly: " +
			"a public route is not authenticated, so it has no service account to refuse")
	case spec.Public && spec.Authenticated:
		return problem("route " + spec.String() + " is Public but also Authenticated: " +
			"a public route is open to anyone, so remove .Public() to require sign-in, or remove .Authenticated()")
	case spec.Public:
		return nil
	case len(s.Authenticate) == 0:
		return errors.New("route " + spec.String() + " needs an authenticated user but the application has no authentication: " +
			"pass gocore.WithAuthentication(...) to gocore.New, or mark the route .Public()")
	case len(needs) > 0 && s.Authorizer == nil:
		return problem("route " + spec.String() + " requires " + strings.Join(needs, ", ") +
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

// Requires gates the route on a permission id from the authz catalogue; chained, every one must hold.
func (r RawRoute) Requires(permission string) RawRoute {
	r.d.spec.require(permission)
	return r
}

// RequiresAny gates the route on holding at least one of the permissions.
func (r RawRoute) RequiresAny(permissions ...string) RawRoute {
	r.d.spec.AnyOf = permissions

	return r
}

// MaxBody sets the largest JSON body this route accepts; see Route.MaxBody.
func (r RawRoute) MaxBody(bytes int64) RawRoute {
	r.d.spec.MaxBody = checkMaxBody(bytes)

	return r
}

// Public opens the route to anyone; see Route.Public.
func (r RawRoute) Public() RawRoute {
	r.d.spec.Public = true
	return r
}

// Authenticated declares that any authenticated principal may call the route; see Route.Authenticated.
func (r RawRoute) Authenticated() RawRoute {
	r.d.spec.Authenticated = true

	return r
}

// PersonOnly refuses a service account with 403; see Route.PersonOnly.
func (r RawRoute) PersonOnly() RawRoute {
	r.d.spec.PersonOnly = true

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
