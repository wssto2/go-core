package route_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/authz/authztest"
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
	Routes = route.Group(Show, Close)
)

func showTicket(_ context.Context, in ShowInput) (Ticket, error) {
	return Ticket{ID: in.ID, Title: "Printer on fire"}, nil
}

func serve(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))

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
	if err := bound.Mount(engine, authztest.AllowAll()); err != nil {
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
	_ = closeTicket.Mount(engine, nil)

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
	_ = export.Mount(engine, nil)

	fmt.Println(declared.Spec().Untyped, serve(engine, http.MethodGet, "/tickets/export").Body.String())
	// Output: true id,title
}

// Types adds a type that no route uses to the contract.
func ExampleContract_Types() {
	type Priority int

	contract := route.Group(Show).Types(Priority(0))
	fmt.Println(contract.ExtraTypes()[0].Name())
	// Output: Priority
}
