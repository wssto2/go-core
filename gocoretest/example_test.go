package gocoretest_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/route"
)

type Ticket struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type ShowInput struct {
	ID int `path:"id"`
}

var Show = route.Get[ShowInput, Ticket]("/tickets/:id")

type Service struct{ app *gocore.App }

func (s Service) Show(_ context.Context, in ShowInput) (Ticket, error) {
	return Ticket{ID: in.ID, Title: s.app.Clock().Now().Format("2006-01-02")}, nil
}

func Install(app *gocore.App) {
	app.Routes(Show.To(Service{app}.Show))
}

// A feature test is plain function calls: build the app, install, call.
func Example() {
	t := &testing.T{}

	app := gocoretest.New(t)
	Install(app)

	rec := gocoretest.Do(t, app, http.MethodGet, "/tickets/7", nil)
	fmt.Println(gocoretest.Decode[Ticket](t, rec))
	// Output: {7 2026-01-02}
}
