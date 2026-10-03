package route_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/contract"
	"github.com/wssto2/go-core/route"
)

const PermView = "tickets.ticket:view"

type ShowInput struct {
	ID int `path:"id"`
}

type Ticket struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

var (
	Show   = route.Get[ShowInput, Ticket]("/tickets/:id").Name("tickets.show").Requires(PermView)
	Close  = route.Delete[ShowInput, route.Empty]("/tickets/:id")
	Routes = route.Group("tickets", Show, Close)
)

func showTicket(_ context.Context, in ShowInput) (Ticket, error) {
	return Ticket{ID: in.ID, Title: "Printer on fire"}, nil
}

// signedIn is how an application tells routes to authenticate: here every
// request counts as authenticated. A real one is auth.Authenticated(provider)
// and authzhttp.Principals(resolve); gocore.WithAuthentication passes them on.
func signedIn(a authz.Authorizer) route.Security {
	return route.Security{
		Authenticate: []gin.HandlerFunc{func(c *gin.Context) { c.Next() }},
		Authorizer:   a,
	}
}

func serve(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), method, target, nil))

	return rec
}

// A route is a value you can read without running anything.
func ExampleGet() {
	spec := Show.Spec()
	fmt.Println(spec.Method, spec.Path, spec.Permission, spec.In.Name(), spec.Out.Name())
	// Output: GET /tickets/:id tickets.ticket:view ShowInput Ticket
}

// To ties a plain function to the declaration; the router calls it with the
// bound input.
func ExampleRoute_To() {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	bound := Show.To(showTicket)
	if err := bound.Mount(engine, signedIn(authztest.AllowAll())); err != nil {
		fmt.Println(err)
		return
	}

	rec := serve(engine, http.MethodGet, "/tickets/7")
	fmt.Println(rec.Code, rec.Body.String())
	// Output: 200 {"success":true,"data":{"id":7,"title":"Printer on fire"}}
}

// Empty answers 204 without a body.
func ExampleEmpty() {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	closeTicket := Close.To(func(context.Context, ShowInput) (route.Empty, error) {
		return route.Empty{}, nil
	})
	_ = closeTicket.Mount(engine, signedIn(nil))

	rec := serve(engine, http.MethodDelete, "/tickets/7")
	fmt.Println(rec.Code, rec.Body.Len())
	// Output: 204 0
}

// None is the input of a route that takes none.
func ExampleNone() {
	health := route.Get[route.None, string]("/ping")
	spec := health.Spec()
	fmt.Println(spec.In.Name())
	// Output: None
}

// Group lists a feature's routes; Unhandled finds the one that has no handler.
func ExampleGroup() {
	handled := []route.Handled{Show.To(showTicket)}
	for _, missing := range route.Unhandled(handled...) {
		fmt.Println("no handler:", missing)
	}
	// Output: no handler: DELETE /tickets/:id
}

// Raw declares a route served by a plain gin handler, such as a download. Like
// a typed route, it is declared first and bound with To.
func ExampleRaw() {
	declared := route.Raw(http.MethodGet, "/tickets/export")
	export := declared.To(func(c *gin.Context) {
		c.String(http.StatusOK, "id,title")
	})

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	_ = export.Mount(engine, signedIn(nil))

	fmt.Println(declared.Spec().Untyped, serve(engine, http.MethodGet, "/tickets/export").Body.String())
	// Output: true id,title
}

// Types adds a type that no route uses to the contract.
func ExampleContract_Types() {
	type Priority int

	contract := route.Group("tickets", Show).Types(Priority(0))
	fmt.Println(contract.ExtraTypes()[0].Name())
	// Output: Priority
}

// Public opens a route to anyone; every other route needs an authenticated
// principal.
func ExampleRoute_Public() {
	login := route.Get[route.None, string]("/login").Public()
	fmt.Println(login.Spec().Public, Show.Spec().Public)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	_ = login.To(func(context.Context, route.None) (string, error) { return "form", nil }).
		Mount(engine, route.Security{}) // no authentication configured: fine for a public route

	fmt.Println(serve(engine, http.MethodGet, "/login").Code)
	// Output:
	// true false
	// 200
}

// A group's name is its folder in the generated TypeScript and the name of its
// route table.
func ExampleContract_Name() {
	fmt.Println(Routes.Name())
	// Output: tickets
}

type Status string

const (
	StatusOpen   Status = "open"
	StatusClosed Status = "closed"
)

type Summary struct {
	ID     int    `json:"id"`
	Status Status `json:"status"`
}

// Enum lists the values of a named string type once; the contract renders every
// field of that type as the union.
func ExampleEnum() {
	statuses := route.Enum(StatusOpen, StatusClosed)
	list := route.Get[route.None, []Summary]("/summaries").Name("tickets.summaries")
	group := route.Group("tickets", list).Types(statuses)

	dir, _ := os.MkdirTemp("", "enum")
	defer func() { _ = os.RemoveAll(dir) }()

	if err := contract.Generate(dir, group); err != nil {
		fmt.Println(err)
		return
	}

	src, _ := os.ReadFile(filepath.Join(dir, "tickets", "entities.ts")) //nolint:gosec // temp dir
	_, types, _ := strings.Cut(string(src), "\n")                       // the first line names the go-core version
	fmt.Print(types)
	// Output:
	// export type Status = "open" | "closed";
	//
	// export type Summary = {
	//   id: number | null;
	//   status: Status;
	// };
}
